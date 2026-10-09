/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 */
#include "stationclock.h"

#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <netdb.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <time.h>
#include <unistd.h>

#include "cJSON.h"
#include "common.h"

static uint64_t
mono_ms(void)
{
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000 + (uint64_t)(ts.tv_nsec / 1000000);
}

static uint64_t
host_ms(void)
{
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    return (uint64_t)ts.tv_sec * 1000 + (uint64_t)(ts.tv_nsec / 1000000);
}

/* Splits http://host:port/path into parts. Returns false on anything else. */
static bool
parse_url(const char* url, char* host, size_t hostCap, int* port, char* path, size_t pathCap)
{
    const char* p = url;
    if (strncmp(p, "http://", 7) != 0)
        return false;
    p += 7;
    const char* slash = strchr(p, '/');
    const char* colon = strchr(p, ':');
    const char* hostEnd = slash ? slash : p + strlen(p);
    if (colon && colon < hostEnd)
        hostEnd = colon;

    size_t hlen = (size_t)(hostEnd - p);
    if (hlen == 0 || hlen >= hostCap)
        return false;
    memcpy(host, p, hlen);
    host[hlen] = '\0';

    *port = 80;
    if (colon && (!slash || colon < slash)) {
        long v = strtol(colon + 1, NULL, 10);
        if (v < 1 || v > 65535)
            return false;
        *port = (int)v;
    }
    snprintf(path, pathCap, "%s", slash ? slash : "/");
    return true;
}

/* A one-shot HTTP/1.0 GET. Returns a malloc'd body on 200, else NULL. */
static char*
http_get(const char* host, int port, const char* path, int timeoutMs)
{
    char portStr[8];
    snprintf(portStr, sizeof(portStr), "%d", port);
    struct addrinfo hints = {0};
    hints.ai_family = AF_UNSPEC;
    hints.ai_socktype = SOCK_STREAM;
    struct addrinfo* res = NULL;
    if (getaddrinfo(host, portStr, &hints, &res) != 0 || !res)
        return NULL;

    int fd = -1;
    uint64_t deadline = mono_ms() + (uint64_t)timeoutMs;
    for (struct addrinfo* ai = res; ai; ai = ai->ai_next) {
        fd = socket(ai->ai_family, ai->ai_socktype, ai->ai_protocol);
        if (fd < 0)
            continue;
        int flags = fcntl(fd, F_GETFL, 0);
        fcntl(fd, F_SETFL, flags | O_NONBLOCK);
        int rc = connect(fd, ai->ai_addr, ai->ai_addrlen);
        if (rc == 0) {
            fcntl(fd, F_SETFL, flags);
            break;
        }
        if (errno == EINPROGRESS) {
            struct pollfd pfd = {fd, POLLOUT, 0};
            int budget = (int)(deadline - mono_ms());
            if (budget > 0 && poll(&pfd, 1, budget) > 0) {
                int soerr = 0;
                socklen_t slen = sizeof(soerr);
                if (getsockopt(fd, SOL_SOCKET, SO_ERROR, &soerr, &slen) == 0 && soerr == 0) {
                    fcntl(fd, F_SETFL, flags);
                    break;
                }
            }
        }
        close(fd);
        fd = -1;
    }
    freeaddrinfo(res);
    if (fd < 0)
        return NULL;

    char req[PM_URL_LEN + 128];
    int reqLen = snprintf(req, sizeof(req),
                          "GET %s HTTP/1.0\r\nHost: %s\r\nAccept: application/json\r\nConnection: close\r\n\r\n",
                          path, host);
    if (send(fd, req, (size_t)reqLen, 0) != reqLen) {
        close(fd);
        return NULL;
    }

    size_t cap = 8192, len = 0;
    char* buf = malloc(cap);
    if (!buf) {
        close(fd);
        return NULL;
    }
    for (;;) {
        int budget = (int)(deadline - mono_ms());
        if (budget <= 0)
            break;
        struct pollfd pfd = {fd, POLLIN, 0};
        if (poll(&pfd, 1, budget) <= 0)
            break;
        if (len + 4096 > cap) {
            cap *= 2;
            char* nb = realloc(buf, cap);
            if (!nb)
                break;
            buf = nb;
        }
        ssize_t n = recv(fd, buf + len, cap - len - 1, 0);
        if (n <= 0)
            break;
        len += (size_t)n;
        if (len > (1 << 20))
            break;
    }
    close(fd);
    buf[len] = '\0';

    char* status = strstr(buf, " ");
    if (!status || strncmp(buf, "HTTP/", 5) != 0 || atoi(status + 1) != 200) {
        free(buf);
        return NULL;
    }
    char* body = strstr(buf, "\r\n\r\n");
    if (!body) {
        free(buf);
        return NULL;
    }
    char* out = strdup(body + 4);
    free(buf);
    return out;
}

/* Parses an RFC3339Nano timestamp to UTC ms. Accepts 'Z' or a numeric offset. */
static bool
parse_rfc3339(const char* s, uint64_t* utcMs)
{
    struct tm tm = {0};
    int y, mon, d, h, mi, sec;
    int consumed = 0;
    if (sscanf(s, "%d-%d-%dT%d:%d:%d%n", &y, &mon, &d, &h, &mi, &sec, &consumed) != 6)
        return false;
    tm.tm_year = y - 1900;
    tm.tm_mon = mon - 1;
    tm.tm_mday = d;
    tm.tm_hour = h;
    tm.tm_min = mi;
    tm.tm_sec = sec;

    const char* p = s + consumed;
    long ms = 0;
    if (*p == '.') {
        p++;
        int digits = 0;
        long frac = 0;
        while (*p >= '0' && *p <= '9') {
            if (digits < 3)
                frac = frac * 10 + (*p - '0');
            digits++;
            p++;
        }
        for (; digits < 3; digits++)
            frac *= 10;
        ms = frac;
    }

    long offsetSec = 0;
    if (*p == 'Z' || *p == '\0') {
        offsetSec = 0;
    } else if (*p == '+' || *p == '-') {
        int sign = (*p == '-') ? -1 : 1;
        int oh = 0, om = 0;
        if (sscanf(p + 1, "%d:%d", &oh, &om) < 1)
            return false;
        offsetSec = sign * (oh * 3600 + om * 60);
    } else {
        return false;
    }

    time_t t = timegm(&tm);
    if (t == (time_t)-1)
        return false;
    *utcMs = (uint64_t)((long long)(t - offsetSec) * 1000 + ms);
    return true;
}

static void
poll_once(StationClock* sc)
{
    char host[PM_ID_LEN], path[PM_URL_LEN];
    int port;
    if (!parse_url(sc->url, host, sizeof(host), &port, path, sizeof(path))) {
        rd_log("clock: bad url %s", sc->url);
        return;
    }

    int budget = sc->pollMs > 2000 ? 2000 : sc->pollMs;
    char* body = http_get(host, port, path, budget);
    if (!body)
        return;

    cJSON* root = cJSON_Parse(body);
    free(body);
    if (!root)
        return;

    const cJSON* ss = cJSON_GetObjectItemCaseSensitive(root, "sync_status");
    const cJSON* rt = cJSON_GetObjectItemCaseSensitive(root, "reported_time");
    if (!cJSON_IsString(ss) || !cJSON_IsString(rt)) {
        cJSON_Delete(root);
        return;
    }

    uint64_t reportedMs;
    bool timeOk = parse_rfc3339(rt->valuestring, &reportedMs);
    bool synced = pointmap_status_synced(sc->pm, ss->valuestring);

    pthread_mutex_lock(&sc->lock);
    uint64_t mono = mono_ms();
    sc->lastOkMonoMs = mono;
    sc->lastStatusSynced = synced;
    snprintf(sc->lastStatus, sizeof(sc->lastStatus), "%s", ss->valuestring);
    if (timeOk) {
        sc->baseReportedMs = reportedMs;
        sc->baseMonoMs = mono;
        sc->haveBase = true;
    }
    pthread_mutex_unlock(&sc->lock);

    cJSON_Delete(root);
}

static void*
clock_thread(void* arg)
{
    StationClock* sc = arg;
    while (sc->running) {
        poll_once(sc);
        for (int slept = 0; slept < sc->pollMs && sc->running; slept += 50)
            usleep(50 * 1000);
    }
    return NULL;
}

void
sc_init(StationClock* sc, const PointMap* pm)
{
    memset(sc, 0, sizeof(*sc));
    pthread_mutex_init(&sc->lock, NULL);
    sc->pm = pm;
    snprintf(sc->url, sizeof(sc->url), "%s", pm->clock.url);
    sc->pollMs = pm->clock.poll_ms;
    sc->staleAfterMs = pm->clock.stale_after_ms;
}

void
sc_start(StationClock* sc)
{
    sc->running = true;
    pthread_create(&sc->thread, NULL, clock_thread, sc);
}

void
sc_stop(StationClock* sc)
{
    if (!sc->running)
        return;
    sc->running = false;
    pthread_join(sc->thread, NULL);
    pthread_mutex_destroy(&sc->lock);
}

bool
sc_now(StationClock* sc, uint64_t* utcMs)
{
    pthread_mutex_lock(&sc->lock);
    uint64_t mono = mono_ms();
    bool fresh = sc->lastOkMonoMs != 0 && (mono - sc->lastOkMonoMs) <= (uint64_t)sc->staleAfterMs;
    bool synced = fresh && sc->lastStatusSynced;

    if (sc->haveBase)
        *utcMs = sc->baseReportedMs + (mono - sc->baseMonoMs);
    else
        *utcMs = host_ms();
    pthread_mutex_unlock(&sc->lock);
    return synced;
}
