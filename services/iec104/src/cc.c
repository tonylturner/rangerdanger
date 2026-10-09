/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * iec104-cc: control-centre SCADA front end, IEC 104 controlling station
 * (client) for the spike RTU. It is the trusted operator path that
 * increment 3 puts behind the firewall.
 *
 * Behaviour:
 *   - STARTDT, then a station general interrogation to seed the cache.
 *   - Track spontaneous breaker and current events in a local cache, each
 *     value carrying quality and a receipt time.
 *   - On an optional one-shot request, drive the breaker with
 *     select-before-execute and report the confirmations.
 *   - On link loss, mark every cached value invalid and not-topical and
 *     log it, then reconnect and re-run GI so the cache recovers. This is
 *     the operator-view-goes-stale behaviour the EU lab teaches.
 */
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "client.h"
#include "common.h"
#include "cs101_information_objects.h"
#include "hal_thread.h"
#include "hal_time.h"
#include "points.h"

typedef struct {
    bool valid; /* false until first seen */
    QualityDescriptor quality;
    DoublePointValue breaker;
    float current;
    float voltage;
    uint64_t updatedMs;
} Cache;

static Cache cache;
static volatile sig_atomic_t stop = 0;

static void
on_signal(int sig)
{
    (void)sig;
    stop = 1;
}

static void
mark_stale(void)
{
    cache.quality = IEC60870_QUALITY_INVALID | IEC60870_QUALITY_NON_TOPICAL;
    cache.updatedMs = Hal_getTimeInMs();
    char q[32];
    rd_log("cache marked stale on link loss: breaker=%s q=%s", rd_dpi_str(cache.breaker),
           rd_quality_str(cache.quality, q, sizeof(q)));
}

static void
on_asdu(void* param, CS101_ASDU asdu)
{
    (void)param;
    TypeID type = CS101_ASDU_getTypeID(asdu);
    int n = CS101_ASDU_getNumberOfElements(asdu);

    for (int i = 0; i < n; i++) {
        InformationObject io = CS101_ASDU_getElement(asdu, i);
        if (!io)
            continue;
        int ioa = InformationObject_getObjectAddress(io);

        if ((type == M_DP_NA_1 || type == M_DP_TB_1) && ioa == RD_IOA_BREAKER) {
            cache.breaker = DoublePointInformation_getValue((DoublePointInformation)io);
            cache.quality = DoublePointInformation_getQuality((DoublePointInformation)io);
            cache.valid = true;
            cache.updatedMs = Hal_getTimeInMs();
        } else if (type == M_ME_NC_1 && ioa == RD_IOA_CURRENT) {
            cache.current = MeasuredValueShort_getValue((MeasuredValueShort)io);
            cache.valid = true;
        } else if (type == M_ME_NC_1 && ioa == RD_IOA_VOLTAGE) {
            cache.voltage = MeasuredValueShort_getValue((MeasuredValueShort)io);
        }
        InformationObject_destroy(io);
    }
}

static bool
general_interrogation(RdClient* c, int timeoutMs)
{
    unsigned long cursor = rd_client_cursor(c);
    rd_log("send C_IC_NA_1 (station GI) ca=%d", RD_CA);
    if (!CS104_Connection_sendInterrogationCommand(c->con, CS101_COT_ACTIVATION, RD_CA, IEC60870_QOI_STATION)) {
        rd_log("GI send failed");
        return false;
    }

    RdResponse r;
    if (!rd_client_wait_response(c, &cursor, C_IC_NA_1, RD_COT_CONFIRM, timeoutMs, &r) || r.negative) {
        rd_log("GI not confirmed");
        return false;
    }
    rd_log("GI ACT-CON received");

    if (!rd_client_wait_response(c, &cursor, C_IC_NA_1, RD_COT_TERMINATION, timeoutMs, &r)) {
        rd_log("GI ACT-TERM not received");
        return false;
    }
    char q[32];
    rd_log("GI complete: breaker=%s current=%.1f voltage=%.1f q=%s", rd_dpi_str(cache.breaker), (double)cache.current,
           (double)cache.voltage, rd_quality_str(cache.quality, q, sizeof(q)));
    return true;
}

/* select-before-execute of the breaker command; returns true when the RTU confirms the execute. */
static bool
operate_breaker(RdClient* c, int dcs, int timeoutMs)
{
    RdResponse r;
    unsigned long cursor;

    cursor = rd_client_cursor(c);
    rd_log("SELECT C_DC_NA_1 ioa=%d dcs=%d(%s)", RD_IOA_BREAKER_CMD, dcs, rd_dpi_str(dcs));
    InformationObject sel = (InformationObject)DoubleCommand_create(NULL, RD_IOA_BREAKER_CMD, dcs, true, 0);
    CS104_Connection_sendProcessCommandEx(c->con, CS101_COT_ACTIVATION, RD_CA, sel);
    InformationObject_destroy(sel);

    if (!rd_client_wait_response(c, &cursor, C_DC_NA_1, RD_COT_CONFIRM, timeoutMs, &r) || r.negative) {
        rd_log("select rejected or not confirmed");
        return false;
    }
    rd_log("select confirmed");

    cursor = rd_client_cursor(c);
    rd_log("EXECUTE C_DC_NA_1 ioa=%d dcs=%d(%s)", RD_IOA_BREAKER_CMD, dcs, rd_dpi_str(dcs));
    InformationObject exe = (InformationObject)DoubleCommand_create(NULL, RD_IOA_BREAKER_CMD, dcs, false, 0);
    CS104_Connection_sendProcessCommandEx(c->con, CS101_COT_ACTIVATION, RD_CA, exe);
    InformationObject_destroy(exe);

    if (!rd_client_wait_response(c, &cursor, C_DC_NA_1, RD_COT_CONFIRM, timeoutMs, &r) || r.negative) {
        rd_log("execute rejected or not confirmed");
        return false;
    }
    rd_log("execute confirmed; waiting for breaker to reach %s", rd_dpi_str(dcs));

    /* ACT-TERM follows once the RTU has moved the breaker. */
    if (!rd_client_wait_response(c, &cursor, C_DC_NA_1, RD_COT_TERMINATION, timeoutMs, &r))
        rd_log("execute ACT-TERM not received");
    else
        rd_log("execute ACT-TERM received; breaker=%s", rd_dpi_str(cache.breaker));
    return true;
}

static void
usage(void)
{
    fprintf(stderr, "Usage: iec104-cc HOST[:PORT] [-oa N] [-t1 S] [-t2 S] [-t3 S]\n"
                    "                 [-operate open|close] [-poll-ms MS] [-once]\n"
                    "\n"
                    "Connects, runs station GI, tracks spontaneous events, and recovers the\n"
                    "cache after link loss. -operate drives the breaker once via SBO after GI.\n"
                    "-once exits after the first GI (and -operate) instead of monitoring.\n");
    exit(2);
}

int
main(int argc, char** argv)
{
    if (argc < 2 || argv[1][0] == '-')
        usage();

    char host[128];
    int port;
    if (!rd_parse_endpoint(argv[1], host, sizeof(host), &port))
        usage();

    RdClientOptions opt = {0};
    opt.oa = 1;
    int operate = -1;
    int pollMs = 2000;
    bool once = false;
    int timeoutMs = 10000;

    for (int i = 2; i < argc; i++) {
        const char* a = argv[i];
        const char* v = (i + 1 < argc) ? argv[i + 1] : NULL;
        long n;
        if (strcmp(a, "-oa") == 0 && v && rd_parse_int(v, 0, 255, &n)) {
            opt.oa = (int)n;
            i++;
        } else if (strcmp(a, "-t1") == 0 && v && rd_parse_int(v, 1, 255, &n)) {
            opt.t1 = (int)n;
            i++;
        } else if (strcmp(a, "-t2") == 0 && v && rd_parse_int(v, 1, 255, &n)) {
            opt.t2 = (int)n;
            i++;
        } else if (strcmp(a, "-t3") == 0 && v && rd_parse_int(v, 1, 255, &n)) {
            opt.t3 = (int)n;
            i++;
        } else if (strcmp(a, "-poll-ms") == 0 && v && rd_parse_int(v, 100, 600000, &n)) {
            pollMs = (int)n;
            i++;
        } else if (strcmp(a, "-operate") == 0 && v) {
            if (strcmp(v, "open") == 0)
                operate = IEC60870_DOUBLE_POINT_OFF;
            else if (strcmp(v, "close") == 0)
                operate = IEC60870_DOUBLE_POINT_ON;
            else
                usage();
            i++;
        } else if (strcmp(a, "-once") == 0) {
            once = true;
        } else {
            usage();
        }
    }

    rd_log_init("cc");
    struct sigaction sa = {0};
    sa.sa_handler = on_signal;
    sigaction(SIGINT, &sa, NULL);
    sigaction(SIGTERM, &sa, NULL);

    RdClient client;
    rd_client_init(&client, host, port, &opt, on_asdu, NULL);
    rd_log("control centre target=%s:%d oa=%d", host, port, opt.oa);

    bool operated = false;
    uint64_t lastPoll = 0;

    while (!stop) {
        if (!rd_client_connect(&client)) {
            rd_log("connect failed; retrying in 2s");
            Thread_sleep(2000);
            continue;
        }
        rd_log("connected");

        if (!rd_client_startdt(&client, timeoutMs)) {
            rd_log("STARTDT not confirmed; dropping link");
            CS104_Connection_close(client.con);
            Thread_sleep(1000);
            continue;
        }
        rd_log("STARTDT confirmed");

        if (!general_interrogation(&client, timeoutMs)) {
            CS104_Connection_close(client.con);
            Thread_sleep(1000);
            continue;
        }

        if (operate >= 0 && !operated)
            operated = operate_breaker(&client, operate, timeoutMs);

        if (once) {
            rd_log("once: exiting after GI");
            break;
        }

        /* Monitor until the link drops. */
        while (!stop && !rd_client_is_closed(&client)) {
            uint64_t now = Hal_getTimeInMs();
            if (now - lastPoll >= (uint64_t)pollMs) {
                lastPoll = now;
                char q[32];
                rd_log("cache breaker=%s current=%.1f voltage=%.1f q=%s age=%llums", rd_dpi_str(cache.breaker),
                       (double)cache.current, (double)cache.voltage, rd_quality_str(cache.quality, q, sizeof(q)),
                       (unsigned long long)(now - cache.updatedMs));
            }
            Thread_sleep(50);
        }

        if (!stop) {
            mark_stale();
            rd_log("link lost; reconnecting");
            CS104_Connection_close(client.con);
            Thread_sleep(1000);
        }
    }

    rd_log("stopping");
    rd_client_destroy(&client);
    return 0;
}
