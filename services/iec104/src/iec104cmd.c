/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * iec104cmd: a small IEC 104 client for students. It connects, starts
 * data transfer, runs one operation and prints the station's responses.
 * This is the Lab 2.3 tool: it issues commands the way a legitimate
 * master would, so students can see how the RTU answers a command with
 * and without a prior select, and so the hardening lab has something to
 * allow or block.
 *
 * Operations:
 *   gi                               station general interrogation
 *   stopstart                        STOPDT then STARTDT on the open link
 *   dc  <ioa> <open|close> [-sbo]    double command (C_DC_NA_1)
 *   sc  <ioa> <on|off>     [-sbo]    single command (C_SC_NA_1)
 *
 * Without -sbo the command is a direct execute (no select). The spike RTU
 * requires select-before-execute on its breaker, so a direct execute is
 * answered with a negative confirmation: that rejection is the point of
 * the exercise, not an error.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "client.h"
#include "common.h"
#include "cs101_information_objects.h"
#include "hal_thread.h"
#include "points.h"

static void
usage(void)
{
    fprintf(stderr,
            "Usage: iec104cmd HOST[:PORT] [-oa N] [-ca N] <operation>\n"
            "\n"
            "Operations:\n"
            "  gi                            station general interrogation\n"
            "  stopstart                     STOPDT then STARTDT on the open link\n"
            "  dc <ioa> <open|close>         double command C_DC_NA_1\n"
            "  sc <ioa> <on|off>             single command C_SC_NA_1\n"
            "\n"
            "Command options:\n"
            "  -sbo            select before execute (default: direct execute)\n"
            "  -delay-ms N     wait N ms between select and execute (force select expiry)\n"
            "  -mismatch       execute the opposite value to the one selected\n"
            "\n"
            "Without -sbo the command is a direct execute (no select). A station that\n"
            "requires select-before-execute answers that with a negative confirmation.\n");
    exit(2);
}

typedef struct {
    bool sbo;      /* select before execute */
    int delayMs;   /* wait between select and execute (to force select expiry) */
    int execVal;   /* value to execute; -1 means the selected value */
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

    /* Successful commands are terminated once the process has moved. */
    if (rd_client_wait_response(c, &cursor, type, RD_COT_TERMINATION, timeoutMs, &r))
        rd_log("execute terminated (ACT-TERM)");
    return true;
}

static InformationObject
make_double(int ioa, int val, bool select)
{
    return (InformationObject)DoubleCommand_create(NULL, ioa, val, select, 0);
}

static InformationObject
make_single(int ioa, int val, bool select)
{
    return (InformationObject)SingleCommand_create(NULL, ioa, val != 0, select, 0);
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
    int ca = RD_CA;
    int timeoutMs = 10000;

    /* Collect options and leave the operation words in argv order. */
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
    RdClient client;
    rd_client_init(&client, host, port, &opt, NULL, NULL);

    rd_log("connecting to %s:%d oa=%d ca=%d", host, port, opt.oa, ca);
    if (!rd_client_connect(&client)) {
        rd_log("connect failed");
        rd_client_destroy(&client);
        return 1;
    }
    if (!rd_client_startdt(&client, timeoutMs)) {
        rd_log("STARTDT not confirmed");
        rd_client_destroy(&client);
        return 1;
    }
    rd_log("link started");

    int rc = 0;
    const char* op = words[0];

    if (strcmp(op, "gi") == 0) {
        unsigned long cursor = rd_client_cursor(&client);
        RdResponse r;
        rd_log("send station GI");
        CS104_Connection_sendInterrogationCommand(client.con, CS101_COT_ACTIVATION, ca, IEC60870_QOI_STATION);
        if (rd_client_wait_response(&client, &cursor, C_IC_NA_1, RD_COT_CONFIRM, timeoutMs, &r) && !r.negative) {
            rd_log("GI confirmed");
            if (rd_client_wait_response(&client, &cursor, C_IC_NA_1, RD_COT_TERMINATION, timeoutMs, &r))
                rd_log("GI terminated");
            else
                rc = 1;
        } else {
            rd_log("GI not confirmed");
            rc = 1;
        }
    } else if (strcmp(op, "stopstart") == 0 && nWords == 1) {
        rd_log("send STOPDT act");
        if (!rd_client_stopdt(&client, timeoutMs)) {
            rd_log("STOPDT not confirmed");
            rc = 1;
        } else {
            rd_log("STOPDT confirmed; send STARTDT act");
            if (rd_client_startdt(&client, timeoutMs)) {
                rd_log("STARTDT confirmed");
            } else {
                rd_log("STARTDT not confirmed");
                rc = 1;
            }
        }
    } else if (strcmp(op, "dc") == 0 && nWords == 3) {
        long ioa;
        if (!rd_parse_int(words[1], 0, 0xffffff, &ioa))
            usage();
        int val = IEC60870_DOUBLE_POINT_OFF;
        if (strcmp(words[2], "open") == 0)
            val = IEC60870_DOUBLE_POINT_OFF;
        else if (strcmp(words[2], "close") == 0)
            val = IEC60870_DOUBLE_POINT_ON;
        else
            usage();
        if (mismatch)
            cmd.execVal = (val == IEC60870_DOUBLE_POINT_ON) ? IEC60870_DOUBLE_POINT_OFF : IEC60870_DOUBLE_POINT_ON;
        rc = run_select_execute(&client, C_DC_NA_1, ca, make_double, (int)ioa, val, &cmd, timeoutMs) ? 0 : 1;
    } else if (strcmp(op, "sc") == 0 && nWords == 3) {
        long ioa;
        if (!rd_parse_int(words[1], 0, 0xffffff, &ioa))
            usage();
        int val = 0;
        if (strcmp(words[2], "on") == 0)
            val = 1;
        else if (strcmp(words[2], "off") == 0)
            val = 0;
        else
            usage();
        if (mismatch)
            cmd.execVal = val ? 0 : 1;
        rc = run_select_execute(&client, C_SC_NA_1, ca, make_single, (int)ioa, val, &cmd, timeoutMs) ? 0 : 1;
    } else {
        usage();
    }

    rd_client_destroy(&client);
    return rc;
}
