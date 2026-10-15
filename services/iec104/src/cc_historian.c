/* RangerDanger IEC 104 snapshot publisher. SPDX-License-Identifier: GPL-3.0-or-later */
#include "cc_historian.h"
#include "cc_state.h"
#include "common.h"

#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <netdb.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

static struct {
    pthread_t thread;
    bool running;
    volatile sig_atomic_t* stop;
    struct sockaddr_in target, local;
    char host[128], path[256];
    int port;
} publisher;

static bool
ready(int fd, short events, uint64_t deadline)
{
    uint64_t now = cc_mono_ms();
    if (now >= deadline) return false;
    struct pollfd p = {fd, events, 0};
    return poll(&p, 1, (int)(deadline - now)) > 0 && (p.revents & events);
}

/* One total deadline covers connect, headers, payload and response. No retry. */
static bool
push(const char* body)
{
    uint64_t deadline = cc_mono_ms() + 500;
    int fd = socket(AF_INET, SOCK_STREAM | SOCK_CLOEXEC | SOCK_NONBLOCK, 0);
    if (fd < 0) return false;
    bool ok = false;
    if (bind(fd, (struct sockaddr*)&publisher.local, sizeof(publisher.local)) != 0) goto done;
    if (connect(fd, (struct sockaddr*)&publisher.target, sizeof(publisher.target)) != 0) {
        if (errno != EINPROGRESS || !ready(fd, POLLOUT, deadline)) goto done;
        int error = 0;
        socklen_t n = sizeof(error);
        if (getsockopt(fd, SOL_SOCKET, SO_ERROR, &error, &n) != 0 || error) goto done;
    }
    char header[640];
    int hn = snprintf(header, sizeof(header),
        "POST %s HTTP/1.1\r\nHost: %s:%d\r\nContent-Type: application/json\r\n"
        "Content-Length: %zu\r\nConnection: close\r\n\r\n",
        publisher.path, publisher.host, publisher.port, strlen(body));
    if (hn < 0 || (size_t)hn >= sizeof(header)) goto done;
    const char* parts[] = {header, body};
    for (int i = 0; i < 2; i++) {
        size_t sent = 0, len = strlen(parts[i]);
        while (sent < len) {
            if (!ready(fd, POLLOUT, deadline)) goto done;
            ssize_t n = send(fd, parts[i] + sent, len - sent, MSG_NOSIGNAL);
            if (n < 0 && (errno == EINTR || errno == EAGAIN)) continue;
            if (n <= 0) goto done;
            sent += (size_t)n;
        }
    }
    char response[4096];
    size_t used = 0;
    while (used < sizeof(response) - 1) {
        if (!ready(fd, POLLIN, deadline)) goto done;
        ssize_t n = recv(fd, response + used, sizeof(response) - 1 - used, 0);
        if (n < 0 && (errno == EINTR || errno == EAGAIN)) continue;
        if (n <= 0) goto done;
        used += (size_t)n;
        response[used] = '\0';
        if (strstr(response, "\r\n\r\n")) {
            int code = 0;
            ok = sscanf(response, "HTTP/1.%*d %d", &code) == 1 && code >= 200 && code < 300;
            break;
        }
    }
done:
    close(fd);
    return ok;
}

static void*
run(void* unused)
{
    (void)unused;
    uint64_t next = cc_mono_ms();
    while (!*publisher.stop) {
        if (cc_mono_ms() < next) {
            usleep(10000);
            continue;
        }
        next = cc_mono_ms() + 1000;
        cJSON* state = cc_render_state();
        char* body = cJSON_PrintUnformatted(state);
        cJSON_Delete(state);
        bool ok = body && push(body);
        free(body);
        pthread_mutex_lock(&cc.lock);
        if (ok) cc_stamp_now(cc.historianLastOk);
        else cc.historianFailures++;
        pthread_mutex_unlock(&cc.lock);
    }
    return NULL;
}

bool
cc_historian_start(const char* url, const char* bindAddr, volatile sig_atomic_t* stop, char* err, size_t errLen)
{
    if (!url && !bindAddr) return true;
    if (!url || !bindAddr || strncmp(url, "http://", 7)) goto invalid;
    const char* slash = strchr(url + 7, '/');
    if (!slash || strlen(slash) >= sizeof(publisher.path) || strpbrk(url, "\r\n ")) goto invalid;
    char endpoint[160];
    size_t len = (size_t)(slash - url - 7);
    if (!len || len >= sizeof(endpoint)) goto invalid;
    memcpy(endpoint, url + 7, len);
    endpoint[len] = '\0';
    if (!rd_parse_endpoint(endpoint, publisher.host, sizeof(publisher.host), &publisher.port)) goto invalid;
    if (!strchr(endpoint, ':')) publisher.port = 80;
    snprintf(publisher.path, sizeof(publisher.path), "%s", slash);
    publisher.local.sin_family = AF_INET;
    if (inet_pton(AF_INET, bindAddr, &publisher.local.sin_addr) != 1) goto invalid;
    /* Resolve once at startup, never on the link or publishing hot path. */
    struct addrinfo hints = {.ai_family = AF_INET, .ai_socktype = SOCK_STREAM};
    struct addrinfo* addr = NULL;
    if (getaddrinfo(publisher.host, NULL, &hints, &addr) != 0) goto invalid;
    publisher.target = *(struct sockaddr_in*)addr->ai_addr;
    publisher.target.sin_port = htons((uint16_t)publisher.port);
    freeaddrinfo(addr);
    int fd = socket(AF_INET, SOCK_STREAM, 0);
    if (fd < 0) goto invalid;
    int rc = bind(fd, (struct sockaddr*)&publisher.local, sizeof(publisher.local));
    close(fd);
    if (rc) goto invalid;
    publisher.stop = stop;
    if (pthread_create(&publisher.thread, NULL, run, NULL) != 0) goto invalid;
    publisher.running = true;
    return true;
invalid:
    snprintf(err, errLen, "invalid or unbindable --historian-url / --historian-bind");
    return false;
}

void
cc_historian_stop(void)
{
    if (publisher.running) pthread_join(publisher.thread, NULL);
}
