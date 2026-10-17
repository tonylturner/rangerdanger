/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 */
#include "cc_http.h"

#include <arpa/inet.h>
#include <ctype.h>
#include <errno.h>
#include <netinet/in.h>
#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <unistd.h>

#include "cJSON.h"
#include "common.h"

#define MAX_TARGET 512

static int listenFd = -1;
static CcHttpHandler handler;
static pthread_mutex_t countLock = PTHREAD_MUTEX_INITIALIZER;
static int activeConnections;

static const char*
reason_phrase(int status)
{
    switch (status) {
    case 200:
        return "OK";
    case 202:
        return "Accepted";
    case 400:
        return "Bad Request";
    case 404:
        return "Not Found";
    case 405:
        return "Method Not Allowed";
    case 408:
        return "Request Timeout";
    case 409:
        return "Conflict";
    case 411:
        return "Length Required";
    case 413:
        return "Content Too Large";
    case 414:
        return "URI Too Long";
    case 431:
        return "Request Header Fields Too Large";
    case 501:
        return "Not Implemented";
    case 503:
        return "Service Unavailable";
    case 505:
        return "HTTP Version Not Supported";
    default:
        return "Internal Server Error";
    }
}

void
cc_http_error(CcHttpResponse* resp, int status, const char* text)
{
    cJSON* o = cJSON_CreateObject();
    cJSON_AddStringToObject(o, "error", text);
    free(resp->body);
    resp->body = cJSON_PrintUnformatted(o);
    resp->status = status;
    cJSON_Delete(o);
}

static void
send_all(int fd, const char* buf, size_t len)
{
    while (len > 0) {
        ssize_t n = send(fd, buf, len, MSG_NOSIGNAL);
        if (n < 0 && errno == EINTR)
            continue;
        if (n <= 0)
            return;
        buf += n;
        len -= (size_t)n;
    }
}

static void
write_response(int fd, const CcHttpResponse* r)
{
    const char* body = r->body ? r->body : "";
    size_t bodyLen = strlen(body);
    char head[512];
    int n = snprintf(head, sizeof(head),
                     "HTTP/1.1 %d %s\r\n"
                     "Content-Type: application/json\r\n"
                     "Content-Length: %zu\r\n"
                     "Cache-Control: no-store\r\n"
                     "Connection: close\r\n"
                     "%s%s%s"
                     "\r\n",
                     r->status, reason_phrase(r->status), bodyLen, r->allow ? "Allow: " : "", r->allow ? r->allow : "",
                     r->allow ? "\r\n" : "");
    send_all(fd, head, (size_t)n);
    send_all(fd, body, bodyLen);
}

static void
respond_error(int fd, int status, const char* text)
{
    CcHttpResponse r = {0};
    cc_http_error(&r, status, text);
    write_response(fd, &r);
    free(r.body);
}

/* Parses "Name: value" header lines between start and end; fills Content-Length. */
static int
parse_headers(char* start, char* end, long* contentLength, const char** err)
{
    *contentLength = -1;
    char* line = start;
    while (line < end) {
        char* eol = strstr(line, "\r\n");
        if (!eol || eol > end)
            eol = end;
        *eol = '\0';
        if (*line == ' ' || *line == '\t') {
            *err = "obsolete header line folding";
            return 400;
        }
        char* colon = strchr(line, ':');
        if (!colon || colon == line) {
            *err = "malformed header line";
            return 400;
        }
        *colon = '\0';
        char* value = colon + 1;
        while (*value == ' ' || *value == '\t')
            value++;
        char* vend = value + strlen(value);
        while (vend > value && (vend[-1] == ' ' || vend[-1] == '\t'))
            *--vend = '\0';

        if (strcasecmp(line, "Transfer-Encoding") == 0) {
            *err = "Transfer-Encoding is not supported; send Content-Length";
            return 501;
        }
        if (strcasecmp(line, "Content-Length") == 0) {
            if (*value == '\0' || strlen(value) > 9) {
                *err = "invalid Content-Length";
                return strlen(value) > 9 ? 413 : 400;
            }
            for (const char* p = value; *p; p++) {
                if (!isdigit((unsigned char)*p)) {
                    *err = "invalid Content-Length";
                    return 400;
                }
            }
            long cl = strtol(value, NULL, 10);
            if (*contentLength >= 0 && *contentLength != cl) {
                *err = "conflicting Content-Length headers";
                return 400;
            }
            *contentLength = cl;
        }
        line = eol + 2;
    }
    return 0;
}

/* Splits "METHOD SP TARGET SP VERSION"; returns 0 or an HTTP error status. */
static int
parse_request_line(char* line, char** method, char** target, const char** err)
{
    char* sp1 = strchr(line, ' ');
    char* sp2 = sp1 ? strchr(sp1 + 1, ' ') : NULL;
    if (!sp1 || !sp2 || strchr(sp2 + 1, ' ')) {
        *err = "malformed request line";
        return 400;
    }
    *sp1 = '\0';
    *sp2 = '\0';
    *method = line;
    *target = sp1 + 1;
    const char* version = sp2 + 1;

    size_t ml = strlen(*method);
    if (ml == 0 || ml > 7) {
        *err = "malformed method";
        return 400;
    }
    for (const char* p = *method; *p; p++) {
        if (*p < 'A' || *p > 'Z') {
            *err = "malformed method";
            return 400;
        }
    }
    if (strncmp(version, "HTTP/", 5) != 0) {
        *err = "malformed request line";
        return 400;
    }
    if (strcmp(version, "HTTP/1.1") != 0 && strcmp(version, "HTTP/1.0") != 0) {
        *err = "only HTTP/1.0 and HTTP/1.1 are supported";
        return 505;
    }
    if ((*target)[0] != '/') {
        *err = "request target must be an absolute path";
        return 400;
    }
    if (strlen(*target) >= MAX_TARGET) {
        *err = "request target too long";
        return 414;
    }
    return 0;
}

static void
serve(int fd, char* buf, char* body)
{
    size_t used = 0;
    char* headerEnd = NULL;
    while (!headerEnd) {
        if (used >= CC_HTTP_MAX_HEADER) {
            respond_error(fd, 431, "request header too large");
            return;
        }
        ssize_t n = recv(fd, buf + used, CC_HTTP_MAX_HEADER - used, 0);
        if (n < 0 && errno == EINTR)
            continue;
        if (n <= 0) {
            if (n < 0 && (errno == EAGAIN || errno == EWOULDBLOCK) && used > 0)
                respond_error(fd, 408, "request not received in time");
            return;
        }
        used += (size_t)n;
        buf[used] = '\0';
        headerEnd = strstr(buf, "\r\n\r\n");
    }

    char* bodyStart = headerEnd + 4;
    size_t have = used - (size_t)(bodyStart - buf);
    char* lineEnd = strstr(buf, "\r\n");
    *lineEnd = '\0';

    const char* err = NULL;
    char* method;
    char* target;
    long contentLength = -1;
    int status = parse_request_line(buf, &method, &target, &err);
    if (status == 0 && lineEnd < headerEnd)
        status = parse_headers(lineEnd + 2, headerEnd, &contentLength, &err);
    if (status != 0) {
        respond_error(fd, status, err);
        return;
    }
    if (contentLength < 0) {
        if (strcmp(method, "POST") == 0) {
            respond_error(fd, 411, "POST needs a Content-Length body");
            return;
        }
        contentLength = 0;
    }
    if (contentLength > CC_HTTP_MAX_BODY) {
        respond_error(fd, 413, "request body too large");
        return;
    }

    size_t want = (size_t)contentLength;
    size_t got = have < want ? have : want;
    memcpy(body, bodyStart, got);
    while (got < want) {
        ssize_t n = recv(fd, body + got, want - got, 0);
        if (n < 0 && errno == EINTR)
            continue;
        if (n <= 0) {
            if (n < 0 && (errno == EAGAIN || errno == EWOULDBLOCK))
                respond_error(fd, 408, "request body not received in time");
            return;
        }
        got += (size_t)n;
    }
    body[want] = '\0';

    char* query = strchr(target, '?');
    if (query)
        *query++ = '\0';

    CcHttpRequest req = {
        .method = method, .path = target, .query = query ? query : "", .body = body, .bodyLen = want};
    CcHttpResponse resp = {.status = 500};
    handler(&req, &resp);
    if (!resp.body)
        cc_http_error(&resp, 500, "no response");
    write_response(fd, &resp);
    free(resp.body);
}

static void
set_timeouts(int fd)
{
    struct timeval tv = {.tv_sec = CC_HTTP_IO_TIMEOUT_S, .tv_usec = 0};
    setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv));
    setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &tv, sizeof(tv));
}

/*
 * Half-closes, then discards what the client still sends (bounded), so a
 * response to a request we did not read in full (413, 431) is not lost to
 * a TCP reset caused by closing with unread data.
 */
static void
lingering_close(int fd)
{
    shutdown(fd, SHUT_WR);
    struct timeval tv = {.tv_sec = 1, .tv_usec = 0};
    setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv));
    char sink[4096];
    size_t total = 0;
    while (total < 64 * 1024) {
        ssize_t n = recv(fd, sink, sizeof(sink), 0);
        if (n <= 0)
            break;
        total += (size_t)n;
    }
    close(fd);
}

static void*
connection_thread(void* arg)
{
    int fd = (int)(intptr_t)arg;
    char* buf = malloc(CC_HTTP_MAX_HEADER + 1);
    char* body = malloc(CC_HTTP_MAX_BODY + 1);
    if (buf && body)
        serve(fd, buf, body);
    free(buf);
    free(body);
    lingering_close(fd);

    pthread_mutex_lock(&countLock);
    activeConnections--;
    pthread_mutex_unlock(&countLock);
    return NULL;
}

static void*
accept_thread(void* arg)
{
    (void)arg;
    pthread_attr_t attr;
    pthread_attr_init(&attr);
    pthread_attr_setdetachstate(&attr, PTHREAD_CREATE_DETACHED);
    pthread_attr_setstacksize(&attr, 256 * 1024);

    for (;;) {
        int fd = accept4(listenFd, NULL, NULL, SOCK_CLOEXEC);
        if (fd < 0) {
            if (errno != EINTR && errno != ECONNABORTED) {
                rd_log("http accept: %s", strerror(errno));
                usleep(100 * 1000); /* e.g. EMFILE: back off instead of spinning */
            }
            continue;
        }
        set_timeouts(fd);

        pthread_mutex_lock(&countLock);
        bool full = activeConnections >= CC_HTTP_MAX_CONNECTIONS;
        if (!full)
            activeConnections++;
        pthread_mutex_unlock(&countLock);
        if (full) {
            respond_error(fd, 503, "too many concurrent connections");
            close(fd);
            continue;
        }

        pthread_t th;
        if (pthread_create(&th, &attr, connection_thread, (void*)(intptr_t)fd) != 0) {
            respond_error(fd, 503, "cannot start a connection thread");
            close(fd);
            pthread_mutex_lock(&countLock);
            activeConnections--;
            pthread_mutex_unlock(&countLock);
        }
    }
    return NULL;
}

bool
cc_http_start(const char* host, int port, CcHttpHandler h, char* err, size_t errLen)
{
    struct sockaddr_in sa = {.sin_family = AF_INET, .sin_port = htons((uint16_t)port)};
    if (inet_pton(AF_INET, host, &sa.sin_addr) != 1) {
        snprintf(err, errLen, "--http %s is not an IPv4 address", host);
        return false;
    }
    listenFd = socket(AF_INET, SOCK_STREAM | SOCK_CLOEXEC, 0);
    if (listenFd < 0) {
        snprintf(err, errLen, "socket: %s", strerror(errno));
        return false;
    }
    int one = 1;
    setsockopt(listenFd, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one));
    if (bind(listenFd, (struct sockaddr*)&sa, sizeof(sa)) != 0 || listen(listenFd, 64) != 0) {
        snprintf(err, errLen, "cannot listen on %s:%d: %s", host, port, strerror(errno));
        close(listenFd);
        listenFd = -1;
        return false;
    }

    handler = h;
    pthread_t th;
    if (pthread_create(&th, NULL, accept_thread, NULL) != 0) {
        snprintf(err, errLen, "cannot start the HTTP accept thread");
        return false;
    }
    pthread_detach(th);
    return true;
}
