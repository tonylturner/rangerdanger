/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * iec104cmd: a small IEC 104 client for students and the EU gate. It
 * connects, starts data transfer, and either runs one operation (gi,
 * stopstart, dc, sc) or holds a monitoring session.
 *
 * The `monitor` operation keeps one session open for a fixed hold and, with
 * --json, prints one JSON object per line: a connected event, an asdu event
 * per received monitoring object, gi_complete after each general
 * interrogation, held when the hold elapses, and link_down if the link drops
 * first. The gate uses it to hold a session across a policy apply and a
 * conntrack kill. Exit codes (contract §6): 0 after held / a successful
 * one-shot, 1 on one-shot failure (including missing command ACT-TERM), 2
 * on connect failure, 3 on link loss during a monitor hold.
 *
 * Every one-shot operation also accepts --json: it prints a single result
 * object. In --json mode the human log is routed to stderr so stdout is a
 * clean JSONL stream.
 */
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "client.h"
#include "common.h"
#include "cs101_information_objects.h"
#include "hal_thread.h"
#include "hal_time.h"

#define EXIT_OK 0
#define EXIT_FAIL 1
#define EXIT_CONNECT 2
#define EXIT_LINK_LOSS 3

static bool jsonMode = false;
static pthread_mutex_t outLock = PTHREAD_MUTEX_INITIALIZER;

static void
usage(void)
{
    fprintf(stderr,
            "Usage: iec104cmd HOST[:PORT] [-oa N] [-ca N] [--json] <operation>\n"
            "\n"
            "Operations:\n"
            "  gi                            station general interrogation\n"
            "  stopstart                     STOPDT then STARTDT on the open link\n"
            "  dc <ioa> <open|close>         double command C_DC_NA_1\n"
            "  rc <ioa> <lower|raise>        regulating step C_RC_NA_1\n"
            "  sc <ioa> <on|off>             single command C_SC_NA_1\n"
            "  monitor --hold SECONDS [--gi-every SECONDS]\n"
            "                                hold a session, stream received ASDUs\n"
            "\n"
            "Command options:\n"
            "  -sbo            select before execute (default: direct execute)\n"
            "  -delay-ms N     wait N ms between select and execute\n"
            "  -mismatch       execute the opposite value to the one selected\n"
            "  --json          emit JSON (JSONL for monitor); logs go to stderr\n");
    exit(2);
}

static const char*
dp_word(int dpi)
{
    switch (dpi) {
    case IEC60870_DOUBLE_POINT_INTERMEDIATE:
        return "intermediate";
    case IEC60870_DOUBLE_POINT_OFF:
        return "open";
    case IEC60870_DOUBLE_POINT_ON:
        return "closed";
    default:
        return "indeterminate";
    }
}

/* Prints one JSON object as a single line under the output lock. */
static void
emit_line(const char* json)
{
    pthread_mutex_lock(&outLock);
    fputs(json, stdout);
    fputc('\n', stdout);
    fflush(stdout);
    pthread_mutex_unlock(&outLock);
}

/* Prints an asdu event per monitoring information object in `asdu`. */
static void
emit_asdu_events(CS101_ASDU asdu)
{
    TypeID type = CS101_ASDU_getTypeID(asdu);
    if ((int)type < 1 || (int)type > 40)
        return; /* monitoring types only; skip command echoes */
    int cot = CS101_ASDU_getCOT(asdu);
    int n = CS101_ASDU_getNumberOfElements(asdu);
    char at[48];
    rd_now_str(at, sizeof(at));

    for (int i = 0; i < n; i++) {
        InformationObject io = CS101_ASDU_getElement(asdu, i);
        if (!io)
            continue;
        int ioa = InformationObject_getObjectAddress(io);
        char value[64] = "null";
        char quality[32] = "GOOD";
        char timeField[64];
        snprintf(timeField, sizeof(timeField), "null");

        switch (type) {
        case M_DP_NA_1:
            snprintf(value, sizeof(value), "\"%s\"", dp_word(DoublePointInformation_getValue((DoublePointInformation)io)));
            rd_quality_str(DoublePointInformation_getQuality((DoublePointInformation)io), quality, sizeof(quality));
            break;
        case M_DP_TB_1: {
            DoublePointWithCP56Time2a dp = (DoublePointWithCP56Time2a)io;
            snprintf(value, sizeof(value), "\"%s\"", dp_word(DoublePointInformation_getValue((DoublePointInformation)dp)));
            rd_quality_str(DoublePointInformation_getQuality((DoublePointInformation)dp), quality, sizeof(quality));
            char ts[48];
            snprintf(timeField, sizeof(timeField), "\"%s\"",
                     rd_cp56_str(DoublePointWithCP56Time2a_getTimestamp(dp), ts, sizeof(ts)));
            break;
        }
        case M_SP_NA_1:
            snprintf(value, sizeof(value), "%s", SinglePointInformation_getValue((SinglePointInformation)io) ? "true" : "false");
            rd_quality_str(SinglePointInformation_getQuality((SinglePointInformation)io), quality, sizeof(quality));
            break;
        case M_SP_TB_1: {
            SinglePointWithCP56Time2a sp = (SinglePointWithCP56Time2a)io;
            snprintf(value, sizeof(value), "%s", SinglePointInformation_getValue((SinglePointInformation)sp) ? "true" : "false");
            rd_quality_str(SinglePointInformation_getQuality((SinglePointInformation)sp), quality, sizeof(quality));
            char ts[48];
            snprintf(timeField, sizeof(timeField), "\"%s\"",
                     rd_cp56_str(SinglePointWithCP56Time2a_getTimestamp(sp), ts, sizeof(ts)));
            break;
        }
        case M_ST_NA_1:
        case M_ST_TB_1: {
            snprintf(value, sizeof(value), "{\"value\":%d,\"transient\":%s}",
                StepPositionInformation_getValue((StepPositionInformation)io),
                StepPositionInformation_isTransient((StepPositionInformation)io) ? "true" : "false");
            rd_quality_str(StepPositionInformation_getQuality((StepPositionInformation)io), quality, sizeof(quality));
            if (type == M_ST_TB_1) {
                char ts[48];
                snprintf(timeField, sizeof(timeField), "\"%s\"", rd_cp56_str(
                    StepPositionWithCP56Time2a_getTimestamp((StepPositionWithCP56Time2a)io), ts, sizeof(ts)));
            }
            break;
        }
        case M_ME_NC_1:
            snprintf(value, sizeof(value), "%.3f", (double)MeasuredValueShort_getValue((MeasuredValueShort)io));
            rd_quality_str(MeasuredValueShort_getQuality((MeasuredValueShort)io), quality, sizeof(quality));
            break;
        default:
            InformationObject_destroy(io);
            continue;
        }

        char line[320];
        snprintf(line, sizeof(line),
                 "{\"event\":\"asdu\",\"at\":\"%s\",\"type\":%d,\"cot\":%d,\"ioa\":%d,\"value\":%s,"
                 "\"quality\":\"%s\",\"time\":%s}",
                 at, (int)type, cot, ioa, value, quality, timeField);
        emit_line(line);
        InformationObject_destroy(io);
    }
}

static void
on_asdu_monitor(void* param, CS101_ASDU asdu)
{
    (void)param;
    if (jsonMode)
        emit_asdu_events(asdu);
}

/* ---- one-shot command helpers --------------------------------------- */

typedef struct {
    bool sbo;
    int delayMs;
    int execVal;
} CmdOptions;

static bool
run_select_execute(RdClient* c, TypeID type, int ca, InformationObject (*make)(int ioa, int val, bool select), int ioa,
                   int val, const CmdOptions* o, int timeoutMs)
{
    RdResponse r;
    unsigned long cursor;
    int execVal = (o->execVal >= 0) ? o->execVal : val;

    if (o->sbo) {
        cursor = rd_client_cursor(c);
        rd_log("SELECT ioa=%d val=%d", ioa, val);
        InformationObject sel = make(ioa, val, true);
        CS104_Connection_sendProcessCommandEx(c->con, CS101_COT_ACTIVATION, ca, sel);
        InformationObject_destroy(sel);
        if (!rd_client_wait_response(c, &cursor, type, RD_COT_CONFIRM, timeoutMs, &r)) {
            rd_log("no select confirmation");
            return false;
        }
        rd_log("select %s", r.negative ? "REJECTED (negative)" : "confirmed");
        if (r.negative)
            return false;
    }

    if (o->delayMs > 0) {
        rd_log("waiting %dms before execute", o->delayMs);
        Thread_sleep(o->delayMs);
    }

    cursor = rd_client_cursor(c);
    rd_log("EXECUTE ioa=%d val=%d", ioa, execVal);
    InformationObject exe = make(ioa, execVal, false);
    CS104_Connection_sendProcessCommandEx(c->con, CS101_COT_ACTIVATION, ca, exe);
    InformationObject_destroy(exe);
    if (!rd_client_wait_response(c, &cursor, type, RD_COT_CONFIRM, timeoutMs, &r)) {
        rd_log("no execute confirmation");
        return false;
    }
    rd_log("execute %s", r.negative ? "REJECTED (negative)" : "confirmed");
    if (r.negative)
        return false;

    if (!rd_client_wait_response(c, &cursor, type, RD_COT_TERMINATION, timeoutMs, &r)) {
        rd_log("no execute termination (ACT-TERM)");
        return false;
    }
    rd_log("execute terminated (ACT-TERM) negative=%d", r.negative);
    return !r.negative;
}

static InformationObject
make_double(int ioa, int val, bool select)
{
    return (InformationObject)DoubleCommand_create(NULL, ioa, val, select, 0);
}

static InformationObject
make_step(int ioa, int val, bool select)
{
    return (InformationObject)StepCommand_create(NULL, ioa, val, select, 0);
}

static InformationObject
make_single(int ioa, int val, bool select)
{
    return (InformationObject)SingleCommand_create(NULL, ioa, val != 0, select, 0);
}

static void
emit_result(const char* op, bool ok)
{
    char at[48];
    char line[160];
    snprintf(line, sizeof(line), "{\"event\":\"result\",\"at\":\"%s\",\"op\":\"%s\",\"status\":\"%s\"}",
             rd_now_str(at, sizeof(at)), op, ok ? "ok" : "failed");
    emit_line(line);
}

/* ---- monitor --------------------------------------------------------- */

static int
run_monitor(RdClient* c, const char* host, int port, int ca, int holdS, int giEveryS, int timeoutMs)
{
    char at[48];

    if (jsonMode) {
        /* lib60870's client exposes no socket getters; report the configured peer, local unknown. */
        char line[220];
        snprintf(line, sizeof(line),
                 "{\"event\":\"connected\",\"at\":\"%s\",\"local\":null,\"peer\":\"%s:%d\"}",
                 rd_now_str(at, sizeof(at)), host, port);
        emit_line(line);
    }

    unsigned long cursor = rd_client_cursor(c);
    RdResponse r;
    rd_log("send station GI");
    CS104_Connection_sendInterrogationCommand(c->con, CS101_COT_ACTIVATION, ca, IEC60870_QOI_STATION);
    if (rd_client_wait_response(c, &cursor, C_IC_NA_1, RD_COT_TERMINATION, timeoutMs, &r) && jsonMode) {
        char line[96];
        snprintf(line, sizeof(line), "{\"event\":\"gi_complete\",\"at\":\"%s\"}", rd_now_str(at, sizeof(at)));
        emit_line(line);
    }

    uint64_t start = Hal_getMonotonicTimeInMs();
    uint64_t holdMs = (uint64_t)holdS * 1000;
    uint64_t lastGi = start;

    while (Hal_getMonotonicTimeInMs() - start < holdMs) {
        if (rd_client_is_closed(c)) {
            if (jsonMode) {
                char line[140];
                snprintf(line, sizeof(line), "{\"event\":\"link_down\",\"at\":\"%s\",\"reason\":\"peer_closed\"}",
                         rd_now_str(at, sizeof(at)));
                emit_line(line);
            }
            rd_log("link down during monitor hold");
            return EXIT_LINK_LOSS;
        }
        uint64_t now = Hal_getMonotonicTimeInMs();
        if (giEveryS > 0 && now - lastGi >= (uint64_t)giEveryS * 1000) {
            lastGi = now;
            cursor = rd_client_cursor(c);
            CS104_Connection_sendInterrogationCommand(c->con, CS101_COT_ACTIVATION, ca, IEC60870_QOI_STATION);
            if (rd_client_wait_response(c, &cursor, C_IC_NA_1, RD_COT_TERMINATION, timeoutMs, &r) && jsonMode) {
                char line[96];
                snprintf(line, sizeof(line), "{\"event\":\"gi_complete\",\"at\":\"%s\"}", rd_now_str(at, sizeof(at)));
                emit_line(line);
            }
        }
        Thread_sleep(50);
    }

    if (jsonMode) {
        char line[96];
        snprintf(line, sizeof(line), "{\"event\":\"held\",\"at\":\"%s\"}", rd_now_str(at, sizeof(at)));
        emit_line(line);
    }
    rd_log("hold complete");
    return EXIT_OK;
}

int
main(int argc, char** argv)
{
    if (argc < 3 || argv[1][0] == '-')
        usage();

    char host[128];
    int port;
    if (!rd_parse_endpoint(argv[1], host, sizeof(host), &port))
        usage();

    RdClientOptions opt = {0};
    opt.oa = 1;
    /* Match the station defaults: t2 must be strictly below the RTU's t1,
     * including after a GI with fewer than w outstanding final reports. */
    opt.t0 = 10;
    opt.t1 = 10;
    opt.t2 = 5;
    opt.t3 = 5;
    int ca = 1;
    int timeoutMs = 10000;
    int holdS = -1, giEveryS = 0;

    const char* words[8];
    int nWords = 0;
    CmdOptions cmd = {false, 0, -1};
    bool mismatch = false;

    for (int i = 2; i < argc; i++) {
        const char* a = argv[i];
        long n;
        if (strcmp(a, "-oa") == 0 && i + 1 < argc && rd_parse_int(argv[++i], 0, 255, &n)) {
            opt.oa = (int)n;
        } else if (strcmp(a, "-ca") == 0 && i + 1 < argc && rd_parse_int(argv[++i], 0, 65535, &n)) {
            ca = (int)n;
        } else if (strcmp(a, "--json") == 0) {
            jsonMode = true;
        } else if (strcmp(a, "--hold") == 0 && i + 1 < argc && rd_parse_int(argv[++i], 1, 86400, &n)) {
            holdS = (int)n;
        } else if (strcmp(a, "--gi-every") == 0 && i + 1 < argc && rd_parse_int(argv[++i], 1, 86400, &n)) {
            giEveryS = (int)n;
        } else if (strcmp(a, "-sbo") == 0) {
            cmd.sbo = true;
        } else if (strcmp(a, "-mismatch") == 0) {
            cmd.sbo = true;
            mismatch = true;
        } else if (strcmp(a, "-delay-ms") == 0 && i + 1 < argc && rd_parse_int(argv[++i], 0, 600000, &n)) {
            cmd.sbo = true;
            cmd.delayMs = (int)n;
        } else if (a[0] == '-') {
            usage();
        } else if (nWords < 8) {
            words[nWords++] = a;
        } else {
            usage();
        }
    }

    if (nWords == 0)
        usage();

    rd_log_init("iec104cmd");
    if (jsonMode)
        rd_log_to_stderr(true);

    const char* op = words[0];
    bool monitor = strcmp(op, "monitor") == 0;

    RdClient client;
    rd_client_init(&client, host, port, &opt, monitor ? on_asdu_monitor : NULL, NULL);

    rd_log("connecting to %s:%d oa=%d ca=%d", host, port, opt.oa, ca);
    if (!rd_client_connect(&client)) {
        rd_log("connect failed");
        rd_client_destroy(&client);
        return EXIT_CONNECT;
    }
    if (!rd_client_startdt(&client, timeoutMs)) {
        rd_log("STARTDT not confirmed");
        rd_client_destroy(&client);
        return EXIT_CONNECT;
    }
    rd_log("link started");

    int rc = EXIT_OK;

    if (monitor) {
        if (holdS < 0)
            usage();
        rc = run_monitor(&client, host, port, ca, holdS, giEveryS, timeoutMs);
    } else if (strcmp(op, "gi") == 0) {
        unsigned long cursor = rd_client_cursor(&client);
        RdResponse r;
        rd_log("send station GI");
        CS104_Connection_sendInterrogationCommand(client.con, CS101_COT_ACTIVATION, ca, IEC60870_QOI_STATION);
        bool ok = rd_client_wait_response(&client, &cursor, C_IC_NA_1, RD_COT_CONFIRM, timeoutMs, &r) && !r.negative &&
                  rd_client_wait_response(&client, &cursor, C_IC_NA_1, RD_COT_TERMINATION, timeoutMs, &r);
        rd_log(ok ? "GI terminated" : "GI not confirmed");
        rc = ok ? EXIT_OK : EXIT_FAIL;
        if (jsonMode)
            emit_result("gi", ok);
    } else if (strcmp(op, "stopstart") == 0 && nWords == 1) {
        bool ok = rd_client_stopdt(&client, timeoutMs) && rd_client_startdt(&client, timeoutMs);
        rd_log(ok ? "STOPDT then STARTDT confirmed" : "stopstart failed");
        rc = ok ? EXIT_OK : EXIT_FAIL;
        if (jsonMode)
            emit_result("stopstart", ok);
    } else if (strcmp(op, "dc") == 0 && nWords == 3) {
        long ioa;
        if (!rd_parse_int(words[1], 0, 0xffffff, &ioa))
            usage();
        int val;
        if (strcmp(words[2], "open") == 0)
            val = IEC60870_DOUBLE_POINT_OFF;
        else if (strcmp(words[2], "close") == 0)
            val = IEC60870_DOUBLE_POINT_ON;
        else
            usage();
        if (mismatch)
            cmd.execVal = (val == IEC60870_DOUBLE_POINT_ON) ? IEC60870_DOUBLE_POINT_OFF : IEC60870_DOUBLE_POINT_ON;
        bool ok = run_select_execute(&client, C_DC_NA_1, ca, make_double, (int)ioa, val, &cmd, timeoutMs);
        rc = ok ? EXIT_OK : EXIT_FAIL;
        if (jsonMode)
            emit_result("dc", ok);
    } else if (strcmp(op, "rc") == 0 && nWords == 3) {
        long ioa;
        if (!rd_parse_int(words[1], 0, 0xffffff, &ioa)) usage();
        int val = !strcmp(words[2], "lower") ? 1 : !strcmp(words[2], "raise") ? 2 : -1;
        if (val < 0) usage();
        if (mismatch) cmd.execVal = val == 1 ? 2 : 1;
        bool ok = run_select_execute(&client, C_RC_NA_1, ca, make_step, (int)ioa, val, &cmd, timeoutMs);
        rc = ok ? EXIT_OK : EXIT_FAIL;
        if (jsonMode) emit_result("rc", ok);
    } else if (strcmp(op, "sc") == 0 && nWords == 3) {
        long ioa;
        if (!rd_parse_int(words[1], 0, 0xffffff, &ioa))
            usage();
        int val;
        if (strcmp(words[2], "on") == 0)
            val = 1;
        else if (strcmp(words[2], "off") == 0)
            val = 0;
        else
            usage();
        if (mismatch)
            cmd.execVal = val ? 0 : 1;
        bool ok = run_select_execute(&client, C_SC_NA_1, ca, make_single, (int)ioa, val, &cmd, timeoutMs);
        rc = ok ? EXIT_OK : EXIT_FAIL;
        if (jsonMode)
            emit_result("sc", ok);
    } else {
        usage();
    }

    rd_client_destroy(&client);
    return rc;
}
