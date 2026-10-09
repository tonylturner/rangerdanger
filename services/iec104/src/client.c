/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 */
#include "client.h"

#include <string.h>
#include <time.h>

#include "common.h"

const int RD_COT_CONFIRM[] = {CS101_COT_ACTIVATION_CON, CS101_COT_DEACTIVATION_CON, CS101_COT_UNKNOWN_TYPE_ID,
                              CS101_COT_UNKNOWN_COT,    CS101_COT_UNKNOWN_CA,       CS101_COT_UNKNOWN_IOA,
                              0};
const int RD_COT_TERMINATION[] = {CS101_COT_ACTIVATION_TERMINATION, 0};

static bool
cot_in(int cot, const int* set)
{
    for (; *set; set++) {
        if (*set == cot)
            return true;
    }
    return false;
}

static void
deadline_from_now(struct timespec* ts, int timeoutMs)
{
    clock_gettime(CLOCK_REALTIME, ts);
    ts->tv_sec += timeoutMs / 1000;
    ts->tv_nsec += (long)(timeoutMs % 1000) * 1000000L;
    if (ts->tv_nsec >= 1000000000L) {
        ts->tv_sec += 1;
        ts->tv_nsec -= 1000000000L;
    }
}

/* Records a confirmation so a waiter can match it; caller must not hold c->lock. */
static void
record_response(RdClient* c, CS101_ASDU asdu)
{
    RdResponse r;
    r.type = CS101_ASDU_getTypeID(asdu);
    r.cot = CS101_ASDU_getCOT(asdu);
    r.negative = CS101_ASDU_isNegative(asdu);
    r.ioa = -1;

    if (CS101_ASDU_getNumberOfElements(asdu) > 0) {
        InformationObject io = CS101_ASDU_getElement(asdu, 0);
        if (io) {
            r.ioa = InformationObject_getObjectAddress(io);
            InformationObject_destroy(io);
        }
    }

    pthread_mutex_lock(&c->lock);
    c->responses[c->nResponses % RD_RESPONSES] = r;
    c->nResponses++;
    pthread_cond_broadcast(&c->cond);
    pthread_mutex_unlock(&c->lock);
}

/* lib60870 receive-thread callback. */
static bool
on_received_asdu(void* param, int address, CS101_ASDU asdu)
{
    (void)address; /* CS104 carries no link address */
    RdClient* c = param;
    int cot = CS101_ASDU_getCOT(asdu);

    rd_log_asdu("rx", asdu);

    if (cot_in(cot, RD_COT_CONFIRM) || cot_in(cot, RD_COT_TERMINATION))
        record_response(c, asdu);

    if (c->onAsdu)
        c->onAsdu(c->onAsduParam, asdu);

    return true;
}

static void
on_connection_event(void* param, CS104_Connection connection, CS104_ConnectionEvent event)
{
    (void)connection;
    RdClient* c = param;

    pthread_mutex_lock(&c->lock);
    switch (event) {
    case CS104_CONNECTION_OPENED:
        c->closed = false;
        break;
    case CS104_CONNECTION_STARTDT_CON_RECEIVED:
        c->startdtCon = true;
        break;
    case CS104_CONNECTION_STOPDT_CON_RECEIVED:
        c->stopdtCon = true;
        break;
    case CS104_CONNECTION_CLOSED:
    case CS104_CONNECTION_FAILED:
        c->closed = true;
        break;
    }
    pthread_cond_broadcast(&c->cond);
    pthread_mutex_unlock(&c->lock);

    static const char* names[] = {"opened", "closed", "startdt-con", "stopdt-con", "failed"};
    rd_log("link %s", names[event]);
}

void
rd_client_init(RdClient* c, const char* host, int port, const RdClientOptions* opt, RdAsduCallback onAsdu, void* param)
{
    memset(c, 0, sizeof(*c));
    pthread_mutex_init(&c->lock, NULL);
    pthread_cond_init(&c->cond, NULL);
    c->onAsdu = onAsdu;
    c->onAsduParam = param;
    c->closed = true;

    c->con = CS104_Connection_create(host, port);

    if (opt) {
        CS104_APCIParameters apci = CS104_Connection_getAPCIParameters(c->con);
        if (opt->t0)
            apci->t0 = opt->t0;
        if (opt->t1)
            apci->t1 = opt->t1;
        if (opt->t2)
            apci->t2 = opt->t2;
        if (opt->t3)
            apci->t3 = opt->t3;
        CS104_Connection_setAPCIParameters(c->con, apci);

        if (opt->oa)
            CS104_Connection_setOriginatorAddress(c->con, (uint8_t)opt->oa);
    }

    CS104_Connection_setConnectionHandler(c->con, on_connection_event, c);
    CS104_Connection_setASDUReceivedHandler(c->con, on_received_asdu, c);
}

void
rd_client_destroy(RdClient* c)
{
    if (c->con)
        CS104_Connection_destroy(c->con);
    c->con = NULL;
    pthread_cond_destroy(&c->cond);
    pthread_mutex_destroy(&c->lock);
}

bool
rd_client_connect(RdClient* c)
{
    pthread_mutex_lock(&c->lock);
    c->closed = false;
    c->startdtCon = false;
    c->stopdtCon = false;
    pthread_mutex_unlock(&c->lock);

    return CS104_Connection_connect(c->con);
}

bool
rd_client_is_closed(RdClient* c)
{
    pthread_mutex_lock(&c->lock);
    bool closed = c->closed;
    pthread_mutex_unlock(&c->lock);
    return closed;
}

static bool
wait_flag(RdClient* c, bool* flag, int timeoutMs)
{
    struct timespec deadline;
    deadline_from_now(&deadline, timeoutMs);

    pthread_mutex_lock(&c->lock);
    while (!*flag && !c->closed) {
        if (pthread_cond_timedwait(&c->cond, &c->lock, &deadline) != 0)
            break;
    }
    bool ok = *flag;
    pthread_mutex_unlock(&c->lock);
    return ok;
}

/* Clears a confirmation flag before its request goes out, so a repeat waits for a fresh CON. */
static void
clear_flag(RdClient* c, bool* flag)
{
    pthread_mutex_lock(&c->lock);
    *flag = false;
    pthread_mutex_unlock(&c->lock);
}

bool
rd_client_startdt(RdClient* c, int timeoutMs)
{
    clear_flag(c, &c->startdtCon);
    CS104_Connection_sendStartDT(c->con);
    return wait_flag(c, &c->startdtCon, timeoutMs);
}

bool
rd_client_stopdt(RdClient* c, int timeoutMs)
{
    clear_flag(c, &c->stopdtCon);
    CS104_Connection_sendStopDT(c->con);
    return wait_flag(c, &c->stopdtCon, timeoutMs);
}

unsigned long
rd_client_cursor(RdClient* c)
{
    pthread_mutex_lock(&c->lock);
    unsigned long n = c->nResponses;
    pthread_mutex_unlock(&c->lock);
    return n;
}

bool
rd_client_wait_response(RdClient* c, unsigned long* cursor, TypeID type, const int* cots, int timeoutMs,
                        RdResponse* out)
{
    struct timespec deadline;
    deadline_from_now(&deadline, timeoutMs);

    pthread_mutex_lock(&c->lock);
    for (;;) {
        /* Never look further back than the ring holds. */
        if (c->nResponses > *cursor + RD_RESPONSES)
            *cursor = c->nResponses - RD_RESPONSES;

        while (*cursor < c->nResponses) {
            RdResponse r = c->responses[*cursor % RD_RESPONSES];
            (*cursor)++;
            if (r.type == type && cot_in(r.cot, cots)) {
                if (out)
                    *out = r;
                pthread_mutex_unlock(&c->lock);
                return true;
            }
        }

        if (c->closed) {
            pthread_mutex_unlock(&c->lock);
            return false;
        }
        if (pthread_cond_timedwait(&c->cond, &c->lock, &deadline) != 0) {
            pthread_mutex_unlock(&c->lock);
            return false;
        }
    }
}
