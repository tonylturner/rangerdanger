/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * iec104-cc: the control centre, an IEC 104 controlling station (client)
 * with an HTTP API for the portal (contract §6). It holds one persistent
 * session to the RTU and learns plant state only through IEC 104:
 *
 *   - STARTDT and a station GI after every connect; reconnect every
 *     reconnect_interval_ms after the session ends.
 *   - A per-point cache (value, quality, RTU observation time, receipt
 *     time, COT). On link loss every point keeps its value and times and
 *     goes IV+NT "link_lost"; it is GOOD again only when a fresh report for
 *     it arrives on the new session.
 *   - Select-before-execute commands with correlated ACT-CON, ACT-TERM and
 *     COT 11 feedback, one at a time, never retried.
 *   - A journal ring of link, GI, point-quality and command events.
 *
 * Threads: the link thread (main), lib60870's receive thread, the HTTP
 * accept thread and one thread per HTTP connection. See cc_state.h for the
 * locking rule.
 */
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cc_api.h"
#include "cc_http.h"
#include "cc_link.h"
#include "cc_state.h"
#include "common.h"
#include "pointmap.h"

static volatile sig_atomic_t stopRequested = 0;

static void
on_signal(int sig)
{
    (void)sig;
    stopRequested = 1;
}

static void
usage(void)
{
    fprintf(stderr, "Usage: iec104-cc --config PATH --bind ADDR --target HOST[:PORT] --http ADDR:PORT\n"
                    "\n"
                    "IEC 104 controlling station. Holds one session to the RTU at --target from the\n"
                    "local address --bind, caches the monitored points of the point map at --config,\n"
                    "and serves /api/health, /api/state, /api/commands and /api/journal on --http.\n");
    exit(2);
}

/* The CC drives every command as select-before-execute and correlates double-point feedback. */
static bool
check_commands(const PointMap* pm, char* err, size_t errLen)
{
    for (int i = 0; i < pm->n_commands; i++) {
        const PmCommand* c = &pm->commands[i];
        if (c->kind != PM_KIND_DOUBLE_COMMAND || !c->sbo) {
            snprintf(err, errLen, "command %s: the control centre supports only SBO double commands", c->id);
            return false;
        }
        if (pm->points[c->feedback_index].kind != PM_KIND_DOUBLE_POINT) {
            snprintf(err, errLen, "command %s: feedback point %s must be a double point", c->id, c->feedback_id);
            return false;
        }
    }
    return true;
}

int
main(int argc, char** argv)
{
    const char* config = NULL;
    const char* bindAddr = NULL;
    const char* target = NULL;
    const char* http = NULL;

    for (int i = 1; i < argc; i++) {
        const char* a = argv[i];
        const char* v = (i + 1 < argc) ? argv[i + 1] : NULL;
        if (!v)
            usage();
        if (strcmp(a, "--config") == 0)
            config = v;
        else if (strcmp(a, "--bind") == 0)
            bindAddr = v;
        else if (strcmp(a, "--target") == 0)
            target = v;
        else if (strcmp(a, "--http") == 0)
            http = v;
        else
            usage();
        i++;
    }
    if (!config || !bindAddr || !target || !http)
        usage();

    char host[128];
    int port;
    char httpHost[64];
    int httpPort;
    if (!rd_parse_endpoint(target, host, sizeof(host), &port) || !strchr(http, ':') ||
        !rd_parse_endpoint(http, httpHost, sizeof(httpHost), &httpPort))
        usage();

    rd_log_init("cc");
    signal(SIGPIPE, SIG_IGN);

    char err[256];
    PointMap* pm = pointmap_load(config, err, sizeof(err));
    if (!pm) {
        rd_log("config error: %s", err);
        return 1;
    }
    if (!check_commands(pm, err, sizeof(err))) {
        rd_log("config error: %s", err);
        pointmap_free(pm);
        return 1;
    }

    char peer[CC_PEER_LEN];
    snprintf(peer, sizeof(peer), "%s:%d", host, port);
    if (!cc_state_init(pm, peer, err, sizeof(err)) || !cc_link_init(host, port, bindAddr, err, sizeof(err)) ||
        !cc_http_start(httpHost, httpPort, cc_api_handle, err, sizeof(err))) {
        rd_log("fatal: %s", err);
        pointmap_free(pm);
        return 1;
    }

    struct sigaction sa = {0};
    sa.sa_handler = on_signal;
    sa.sa_flags = SA_RESTART;
    sigaction(SIGINT, &sa, NULL);
    sigaction(SIGTERM, &sa, NULL);

    rd_log("control centre target=%s bind=%s http=%s:%d ca=%d points=%d commands=%d cache_epoch=%s", peer, bindAddr,
           httpHost, httpPort, pm->ca, pm->n_points, pm->n_commands, cc.epoch);

    cc_link_run(&stopRequested);

    rd_log("stopping");
    cc_link_shutdown();
    return 0;
}
