/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * iec104-rtu: substation RTU / gateway, IEC 104 controlled station
 * (server) for common addresses 1 and 2, built on lib60870-C.
 *
 * lib60870 owns the session layer (APCI, k/w, t0-t3, STARTDT/STOPDT,
 * TESTFR). This file owns what the library deliberately leaves to the
 * application: the point set, general interrogation, and the
 * select-before-execute state machine for the breaker command.
 *
 * Spontaneous data goes straight to every started connection instead of
 * through the library's event queue, so the commanding connection sees
 * ACT-CON, then the return information, then ACT-TERM in that order.
 * The price is that events are not buffered while no client is started;
 * a client recovers state with general interrogation after reconnecting.
 *
 * SIGUSR1 simulates a local protection trip, a spontaneous change that
 * no IEC 104 client caused.
 */
#include <pthread.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "common.h"
#include "cs104_slave.h"
#include "hal_thread.h"
#include "hal_time.h"
#include "points.h"

#define MAX_CONNECTIONS 8
#define EVENT_QUEUE 64

/* Load current while the breaker is closed; a flat load until increment 3 reads the feeder. */
#define CLOSED_CURRENT_A 182.5f
#define BUS_VOLTAGE_KV 20.4f

/*
 * Connections are keyed by their peer "ip:port" string, not by the
 * IMasterConnection pointer: lib60870 pools and reuses those structs, so a
 * previous client's late CONNECTION_CLOSED would otherwise match — and
 * wrongly clear — the selection of the client that reused the struct. The
 * ephemeral source port makes the peer string unique for the window that
 * matters.
 */
#define PEER_LEN 64

typedef struct {
    bool active;
    char peer[PEER_LEN];
    IMasterConnection conn; /* for sending ACT-TERM; validated by peer */
    int dcs;
    int qu;
    uint64_t deadline;
} Selection;

typedef struct {
    IMasterConnection conn;
    char peer[PEER_LEN];
    CS104_PeerConnectionEvent event;
} ConnEvent;

typedef struct {
    bool active;
    char peer[PEER_LEN];
    IMasterConnection conn;
    int dcs;
    CS101_ASDU command; /* clone of the execute, answered with ACT-TERM */
} PendingExecute;

typedef struct {
    char peer[PEER_LEN];
    IMasterConnection conn;
} Started;

static struct {
    pthread_mutex_t lock;
    CS104_Slave slave;
    CS101_AppLayerParameters al;
    int selectTimeoutMs;

    DoublePointValue breaker;
    float current;
    float voltage;

    Selection sel;
    PendingExecute pend;

    Started started[MAX_CONNECTIONS];
    int nStarted;

    pthread_mutex_t evLock; /* guards events only; never held while calling lib60870 */
    ConnEvent events[EVENT_QUEUE];
    int nEvents;
} rtu;

static volatile sig_atomic_t stopRequested = 0;
static volatile sig_atomic_t tripRequested = 0;

static bool
known_ca(int ca)
{
    return ca == RD_CA || ca == RD_CA_AUX;
}

static void
on_signal(int sig)
{
    if (sig == SIGUSR1)
        tripRequested = 1;
    else
        stopRequested = 1;
}

static const char*
peer(IMasterConnection conn, char* buf, int len)
{
    if (IMasterConnection_getPeerAddress(conn, buf, len) <= 0)
        snprintf(buf, (size_t)len, "?");
    return buf;
}

/* Caller holds rtu.lock. */
static void
broadcast(CS101_ASDU asdu)
{
    rd_log_asdu("tx-all", asdu);
    for (int i = 0; i < rtu.nStarted; i++)
        IMasterConnection_sendASDU(rtu.started[i].conn, asdu);
}

/* Caller holds rtu.lock. */
static void
send_breaker_event(CS101_CauseOfTransmission cot)
{
    struct sCP56Time2a now;
    CP56Time2a_createFromMsTimestamp(&now, Hal_getTimeInMs());

    CS101_ASDU asdu = CS101_ASDU_create(rtu.al, false, cot, 0, RD_CA, false, false);
    InformationObject io = (InformationObject)DoublePointWithCP56Time2a_create(
        NULL, RD_IOA_BREAKER, rtu.breaker, IEC60870_QUALITY_GOOD, &now);
    CS101_ASDU_addInformationObject(asdu, io);
    InformationObject_destroy(io);
    broadcast(asdu);
    CS101_ASDU_destroy(asdu);
}

static void
add_measurement(CS101_ASDU asdu, int ioa, float value)
{
    InformationObject io =
        (InformationObject)MeasuredValueShort_create(NULL, ioa, value, IEC60870_QUALITY_GOOD);
    CS101_ASDU_addInformationObject(asdu, io);
    InformationObject_destroy(io);
}

/* Caller holds rtu.lock. */
static void
send_measurements(IMasterConnection conn, int ca, int oa)
{
    CS101_ASDU asdu =
        CS101_ASDU_create(rtu.al, false, CS101_COT_INTERROGATED_BY_STATION, oa, ca, false, false);

    if (ca == RD_CA) {
        add_measurement(asdu, RD_IOA_CURRENT, rtu.current);
        add_measurement(asdu, RD_IOA_VOLTAGE, rtu.voltage);
        add_measurement(asdu, RD_IOA_EDGE_16BIT_MAX, 65.535f);
        add_measurement(asdu, RD_IOA_EDGE_16BIT_OVERFLOW, 65.536f);
        add_measurement(asdu, RD_IOA_EDGE_24BIT_MAX, 167.77215f);
    } else if (ca == RD_CA_AUX) {
        add_measurement(asdu, RD_IOA_AUX_FREQUENCY, 50.02f);
    }

    IMasterConnection_sendASDU(conn, asdu);
    CS101_ASDU_destroy(asdu);
}

/* Caller holds rtu.lock. The current follows the breaker; it is a measurand, so COT 3. */
static void
send_current_event(void)
{
    CS101_ASDU asdu = CS101_ASDU_create(rtu.al, false, CS101_COT_SPONTANEOUS, 0, RD_CA, false, false);
    InformationObject io =
        (InformationObject)MeasuredValueShort_create(NULL, RD_IOA_CURRENT, rtu.current, IEC60870_QUALITY_GOOD);
    CS101_ASDU_addInformationObject(asdu, io);
    InformationObject_destroy(io);
    broadcast(asdu);
    CS101_ASDU_destroy(asdu);
}

/* Caller holds rtu.lock. Returns true when the position changed. */
static bool
set_breaker(DoublePointValue v, CS101_CauseOfTransmission cot)
{
    if (rtu.breaker == v)
        return false;
    rtu.breaker = v;
    rtu.current = (v == IEC60870_DOUBLE_POINT_ON) ? CLOSED_CURRENT_A : 0.0f;
    send_breaker_event(cot);
    send_current_event();
    return true;
}

static void
clear_selection(const char* why)
{
    if (rtu.sel.active)
        rd_log("selection ioa=%d dcs=%d cleared: %s", RD_IOA_BREAKER_CMD, rtu.sel.dcs, why);
    rtu.sel = (Selection){0};
}

static bool
interrogation_handler(void* param, IMasterConnection conn, CS101_ASDU asdu, uint8_t qoi)
{
    (void)param;
    rd_log_asdu("rx", asdu);

    int ca = CS101_ASDU_getCA(asdu);
    if (!known_ca(ca)) {
        CS101_ASDU_setCOT(asdu, CS101_COT_UNKNOWN_CA);
        CS101_ASDU_setNegative(asdu, true);
        IMasterConnection_sendASDU(conn, asdu);
        return true;
    }

    /* GI completes inside this handler, so there is never one in progress to deactivate. */
    if (CS101_ASDU_getCOT(asdu) == CS101_COT_DEACTIVATION) {
        CS101_ASDU_setCOT(asdu, CS101_COT_DEACTIVATION_CON);
        CS101_ASDU_setNegative(asdu, true);
        IMasterConnection_sendASDU(conn, asdu);
        return true;
    }

    /* No point is assigned to an interrogation group. */
    if (qoi != IEC60870_QOI_STATION) {
        IMasterConnection_sendACT_CON(conn, asdu, true);
        return true;
    }

    int oa = CS101_ASDU_getOA(asdu);

    /* Holding the lock keeps a spontaneous change from overtaking the GI snapshot. */
    pthread_mutex_lock(&rtu.lock);

    IMasterConnection_sendACT_CON(conn, asdu, false);

    if (ca == RD_CA) {
        CS101_ASDU dp =
            CS101_ASDU_create(rtu.al, false, CS101_COT_INTERROGATED_BY_STATION, oa, ca, false, false);
        InformationObject io = (InformationObject)DoublePointInformation_create(
            NULL, RD_IOA_BREAKER, rtu.breaker, IEC60870_QUALITY_GOOD);
        CS101_ASDU_addInformationObject(dp, io);
        InformationObject_destroy(io);
        IMasterConnection_sendASDU(conn, dp);
        CS101_ASDU_destroy(dp);
    }

    send_measurements(conn, ca, oa);

    IMasterConnection_sendACT_TERM(conn, asdu);

    pthread_mutex_unlock(&rtu.lock);
    return true;
}

static void
reply_negative(IMasterConnection conn, CS101_ASDU asdu, CS101_CauseOfTransmission cot, const char* why)
{
    int ioa = -1;
    if (CS101_ASDU_getNumberOfElements(asdu) > 0) {
        InformationObject io = CS101_ASDU_getElement(asdu, 0);
        if (io) {
            ioa = InformationObject_getObjectAddress(io);
            InformationObject_destroy(io);
        }
    }
    rd_log("reject ioa=%d cot=%d: %s", ioa, (int)cot, why);
    CS101_ASDU_setCOT(asdu, cot);
    CS101_ASDU_setNegative(asdu, true);
    IMasterConnection_sendASDU(conn, asdu);
}

/* Caller holds rtu.lock. */
static void
handle_double_command(IMasterConnection conn, const char* peerStr, CS101_ASDU asdu, DoubleCommand dc)
{
    CS101_CauseOfTransmission cot = CS101_ASDU_getCOT(asdu);
    int dcs = DoubleCommand_getState(dc);
    int qu = DoubleCommand_getQU(dc);
    uint64_t now = Hal_getMonotonicTimeInMs();

    if (rtu.sel.active && now >= rtu.sel.deadline)
        clear_selection("select timeout elapsed");

    bool ownsSelection = rtu.sel.active && strcmp(rtu.sel.peer, peerStr) == 0;

    if (cot == CS101_COT_DEACTIVATION) {
        if (ownsSelection) {
            clear_selection("deactivated by the selecting client");
            CS101_ASDU_setCOT(asdu, CS101_COT_DEACTIVATION_CON);
            IMasterConnection_sendASDU(conn, asdu);
        } else {
            CS101_ASDU_setCOT(asdu, CS101_COT_DEACTIVATION_CON);
            CS101_ASDU_setNegative(asdu, true);
            IMasterConnection_sendASDU(conn, asdu);
        }
        return;
    }

    if (dcs != IEC60870_DOUBLE_POINT_OFF && dcs != IEC60870_DOUBLE_POINT_ON) {
        reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON, "DCS 0 and 3 are not permitted");
        return;
    }

    if (DoubleCommand_isSelect(dc)) {
        if (rtu.pend.active) {
            reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON, "previous execute still in progress");
            return;
        }
        if (rtu.sel.active && !ownsSelection) {
            reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON, "selected by another connection");
            return;
        }
        rtu.sel.active = true;
        snprintf(rtu.sel.peer, PEER_LEN, "%s", peerStr);
        rtu.sel.conn = conn;
        rtu.sel.dcs = dcs;
        rtu.sel.qu = qu;
        rtu.sel.deadline = now + (uint64_t)rtu.selectTimeoutMs;
        rd_log("select ioa=%d dcs=%d qu=%d by %s accepted, expires in %d ms", RD_IOA_BREAKER_CMD, dcs, qu, peerStr,
               rtu.selectTimeoutMs);
        IMasterConnection_sendACT_CON(conn, asdu, false);
        return;
    }

    if (!ownsSelection) {
        reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON,
                       rtu.sel.active ? "selected by another connection" : "execute without a valid selection");
        return;
    }
    if (dcs != rtu.sel.dcs || qu != rtu.sel.qu) {
        clear_selection("execute does not match the selection");
        reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON, "execute value differs from selected value");
        return;
    }

    clear_selection("executed");
    IMasterConnection_sendACT_CON(conn, asdu, false);
    rtu.pend.active = true;
    snprintf(rtu.pend.peer, PEER_LEN, "%s", peerStr);
    rtu.pend.conn = conn;
    rtu.pend.dcs = dcs;
    rtu.pend.command = CS101_ASDU_clone(asdu, NULL);
}

static bool
asdu_handler(void* param, IMasterConnection conn, CS101_ASDU asdu)
{
    (void)param;

    if (CS101_ASDU_getTypeID(asdu) != C_DC_NA_1)
        return false; /* the library answers COT 44, unknown type */

    rd_log_asdu("rx", asdu);

    int ca = CS101_ASDU_getCA(asdu);
    if (!known_ca(ca)) {
        reply_negative(conn, asdu, CS101_COT_UNKNOWN_CA, "unknown common address");
        return true;
    }

    CS101_CauseOfTransmission cot = CS101_ASDU_getCOT(asdu);
    if (cot != CS101_COT_ACTIVATION && cot != CS101_COT_DEACTIVATION) {
        reply_negative(conn, asdu, CS101_COT_UNKNOWN_COT, "commands accept only ACT and DEACT");
        return true;
    }

    InformationObject io = CS101_ASDU_getElement(asdu, 0);
    if (io == NULL || CS101_ASDU_getNumberOfElements(asdu) != 1 ||
        ca != RD_CA || InformationObject_getObjectAddress(io) != RD_IOA_BREAKER_CMD) {
        if (io)
            InformationObject_destroy(io);
        reply_negative(conn, asdu, CS101_COT_UNKNOWN_IOA, "no command point at this IOA");
        return true;
    }

    char peerStr[PEER_LEN];
    peer(conn, peerStr, sizeof(peerStr));

    pthread_mutex_lock(&rtu.lock);
    handle_double_command(conn, peerStr, asdu, (DoubleCommand)io);
    pthread_mutex_unlock(&rtu.lock);

    InformationObject_destroy(io);
    return true;
}

static bool
allowed_ca_handler(void* param, int ca)
{
    (void)param;
    return known_ca(ca);
}

static bool
connection_request_handler(void* param, const char* ip)
{
    (void)param;
    rd_log("connection request from %s", ip);
    return true;
}

/* Caller holds rtu.lock. */
static void
forget_connection(const char* peerStr)
{
    for (int i = 0; i < rtu.nStarted; i++) {
        if (strcmp(rtu.started[i].peer, peerStr) == 0) {
            rtu.started[i] = rtu.started[--rtu.nStarted];
            break;
        }
    }
    if (rtu.sel.active && strcmp(rtu.sel.peer, peerStr) == 0)
        clear_selection("selecting connection stopped");
    if (rtu.pend.active && strcmp(rtu.pend.peer, peerStr) == 0)
        rtu.pend.conn = NULL; /* still operate the breaker; nobody is left to get ACT-TERM */
}

/*
 * lib60870 raises ACTIVATED and DEACTIVATED while holding the connection's
 * state lock, and IMasterConnection_sendASDU takes that same lock. Taking
 * rtu.lock here would deadlock against the main loop, which holds rtu.lock
 * while it sends. So the handler only records the event; tick() applies it.
 */
static void
connection_event_handler(void* param, IMasterConnection conn, CS104_PeerConnectionEvent event)
{
    (void)param;
    static const char* names[] = {"connection opened", "connection closed", "STARTDT", "STOPDT"};
    char addr[PEER_LEN];
    peer(conn, addr, sizeof(addr));

    rd_log("%s peer=%s", names[event], addr);

    pthread_mutex_lock(&rtu.evLock);
    if (rtu.nEvents < EVENT_QUEUE) {
        ConnEvent* e = &rtu.events[rtu.nEvents++];
        e->conn = conn;
        e->event = event;
        snprintf(e->peer, PEER_LEN, "%s", addr);
    } else {
        rd_log("connection event queue full; event dropped");
    }
    pthread_mutex_unlock(&rtu.evLock);
}

/* Caller holds rtu.lock. */
static void
apply_connection_events(void)
{
    ConnEvent events[EVENT_QUEUE];
    int n;

    pthread_mutex_lock(&rtu.evLock);
    n = rtu.nEvents;
    memcpy(events, rtu.events, sizeof(ConnEvent) * (size_t)n);
    rtu.nEvents = 0;
    pthread_mutex_unlock(&rtu.evLock);

    for (int i = 0; i < n; i++) {
        switch (events[i].event) {
        case CS104_CON_EVENT_ACTIVATED:
            forget_connection(events[i].peer); /* never list a peer twice */
            if (rtu.nStarted < MAX_CONNECTIONS) {
                snprintf(rtu.started[rtu.nStarted].peer, PEER_LEN, "%s", events[i].peer);
                rtu.started[rtu.nStarted].conn = events[i].conn;
                rtu.nStarted++;
            }
            break;
        case CS104_CON_EVENT_DEACTIVATED:
        case CS104_CON_EVENT_CONNECTION_CLOSED:
            forget_connection(events[i].peer);
            break;
        case CS104_CON_EVENT_CONNECTION_OPENED:
            break;
        }
    }
}

/* Main-loop work: operate the breaker after an accepted execute, expire selections, local trips. */
static void
tick(void)
{
    pthread_mutex_lock(&rtu.lock);

    apply_connection_events();

    if (rtu.pend.active) {
        set_breaker((DoublePointValue)rtu.pend.dcs, CS101_COT_RETURN_INFO_REMOTE);
        if (rtu.pend.conn)
            IMasterConnection_sendACT_TERM(rtu.pend.conn, rtu.pend.command);
        CS101_ASDU_destroy(rtu.pend.command);
        rtu.pend = (PendingExecute){0};
    }

    if (rtu.sel.active && Hal_getMonotonicTimeInMs() >= rtu.sel.deadline)
        clear_selection("select timeout elapsed");

    if (tripRequested) {
        tripRequested = 0;
        if (set_breaker(IEC60870_DOUBLE_POINT_OFF, CS101_COT_SPONTANEOUS))
            rd_log("local protection trip: breaker opened");
        else
            rd_log("local protection trip: breaker already open");
    }

    pthread_mutex_unlock(&rtu.lock);
}

static void
usage(void)
{
    fprintf(stderr, "Usage: iec104-rtu [-bind ADDR] [-port N] [-select-timeout MS] [-t3 S]\n"
                    "\n"
                    "IEC 104 server, CA %d and %d. Points: breaker DP ioa %d, current ioa %d, voltage ioa %d,\n"
                    "breaker command C_DC_NA_1 ioa %d (select-before-execute only).\n"
                    "SIGUSR1 trips the breaker locally.\n",
            RD_CA, RD_CA_AUX, RD_IOA_BREAKER, RD_IOA_CURRENT, RD_IOA_VOLTAGE, RD_IOA_BREAKER_CMD);
    exit(2);
}

int
main(int argc, char** argv)
{
    const char* bind = "0.0.0.0";
    long port = IEC_60870_5_104_DEFAULT_PORT;
    long selectTimeout = 10000;
    long t3 = -1;

    for (int i = 1; i < argc; i++) {
        const char* a = argv[i];
        const char* v = (i + 1 < argc) ? argv[i + 1] : NULL;
        if (strcmp(a, "-bind") == 0 && v) {
            bind = v;
            i++;
        } else if (strcmp(a, "-port") == 0 && v && rd_parse_int(v, 1, 65535, &port)) {
            i++;
        } else if (strcmp(a, "-select-timeout") == 0 && v && rd_parse_int(v, 100, 600000, &selectTimeout)) {
            i++;
        } else if (strcmp(a, "-t3") == 0 && v && rd_parse_int(v, 1, 3600, &t3)) {
            i++;
        } else {
            usage();
        }
    }

    rd_log_init("rtu");
    pthread_mutex_init(&rtu.lock, NULL);
    pthread_mutex_init(&rtu.evLock, NULL);
    rtu.selectTimeoutMs = (int)selectTimeout;
    rtu.breaker = IEC60870_DOUBLE_POINT_ON;
    rtu.current = CLOSED_CURRENT_A;
    rtu.voltage = BUS_VOLTAGE_KV;

    struct sigaction sa = {0};
    sa.sa_handler = on_signal;
    sigaction(SIGINT, &sa, NULL);
    sigaction(SIGTERM, &sa, NULL);
    sigaction(SIGUSR1, &sa, NULL);

    /* Queues are sized for the library's own use; spontaneous data bypasses them (see the header). */
    rtu.slave = CS104_Slave_create(16, 64);
    CS104_Slave_setLocalAddress(rtu.slave, bind);
    CS104_Slave_setLocalPort(rtu.slave, (int)port);
    CS104_Slave_setServerMode(rtu.slave, CS104_MODE_CONNECTION_IS_REDUNDANCY_GROUP);
    CS104_Slave_setMaxOpenConnections(rtu.slave, MAX_CONNECTIONS);
    rtu.al = CS104_Slave_getAppLayerParameters(rtu.slave);

    CS104_APCIParameters apci = CS104_Slave_getConnectionParameters(rtu.slave);
    if (t3 > 0)
        apci->t3 = (int)t3;

    CS104_Slave_setInterrogationHandler(rtu.slave, interrogation_handler, NULL);
    CS104_Slave_setASDUHandler(rtu.slave, asdu_handler, NULL);
    CS104_Slave_setAllowedCAHandler(rtu.slave, allowed_ca_handler, NULL);
    CS104_Slave_setConnectionRequestHandler(rtu.slave, connection_request_handler, NULL);
    CS104_Slave_setConnectionEventHandler(rtu.slave, connection_event_handler, NULL);

    CS104_Slave_start(rtu.slave);
    if (!CS104_Slave_isRunning(rtu.slave)) {
        rd_log("cannot listen on %s:%ld", bind, port);
        CS104_Slave_destroy(rtu.slave);
        return 1;
    }

    rd_log("listening %s:%ld ca=%d k=%d w=%d t0=%d t1=%d t2=%d t3=%d select-timeout=%ldms", bind, port, RD_CA,
           apci->k, apci->w, apci->t0, apci->t1, apci->t2, apci->t3, selectTimeout);

    while (!stopRequested) {
        tick();
        Thread_sleep(10);
    }

    rd_log("stopping");
    CS104_Slave_stop(rtu.slave);
    CS104_Slave_destroy(rtu.slave);
    return 0;
}
