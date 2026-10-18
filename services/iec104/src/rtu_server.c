/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 */

/* IEC 104 server: ASDU handlers, connection events, quality and reporting. */
#include <math.h>
#include <stdio.h>
#include <string.h>

#include "common.h"
#include "rtu_internal.h"

static const char*
peer(IMasterConnection conn, char* buf, int len)
{
    if (IMasterConnection_getPeerAddress(conn, buf, len) <= 0)
        snprintf(buf, (size_t)len, "?");
    return buf;
}

/* No shared state; also used by receive callbacks before taking rtu.lock. */
void
rtu_reply_negative(IMasterConnection conn, CS101_ASDU asdu, CS101_CauseOfTransmission cot)
{
    CS101_ASDU_setCOT(asdu, cot);
    CS101_ASDU_setNegative(asdu, true);
    IMasterConnection_sendASDU(conn, asdu);
}

/* ---- lib60870 handlers ----------------------------------------------- */

static bool
interrogation_handler(void* param, IMasterConnection conn, CS101_ASDU asdu, uint8_t qoi)
{
    (void)param;
    rd_log_asdu("rx", asdu);

    int ca = CS101_ASDU_getCA(asdu);
    if (ca != rtu.pm->ca) {
        rtu_reply_negative(conn, asdu, CS101_COT_UNKNOWN_CA);
        return true;
    }
    if (CS101_ASDU_getCOT(asdu) == CS101_COT_DEACTIVATION) {
        rtu_reply_negative(conn, asdu, CS101_COT_DEACTIVATION_CON);
        return true;
    }
    if (qoi != IEC60870_QOI_STATION) {
        IMasterConnection_sendACT_CON(conn, asdu, true);
        return true;
    }

    int oa = CS101_ASDU_getOA(asdu);
    pthread_mutex_lock(&rtu.lock);
    IMasterConnection_sendACT_CON(conn, asdu, false);

    rtu_report_gi(conn, oa);

    IMasterConnection_sendACT_TERM(conn, asdu);
    pthread_mutex_unlock(&rtu.lock);
    return true;
}

static bool
asdu_handler(void* param, IMasterConnection conn, CS101_ASDU asdu)
{
    (void)param;
    TypeID type = CS101_ASDU_getTypeID(asdu);
    if (type != C_DC_NA_1 && type != C_RC_NA_1 && type != C_SC_NA_1)
        return false; /* lib60870 answers COT 44, unknown type */

    rd_log_asdu("rx", asdu);

    int ca = CS101_ASDU_getCA(asdu);
    if (ca != rtu.pm->ca) {
        rtu_reply_negative(conn, asdu, CS101_COT_UNKNOWN_CA);
        return true;
    }
    CS101_CauseOfTransmission cot = CS101_ASDU_getCOT(asdu);
    if (cot != CS101_COT_ACTIVATION && cot != CS101_COT_DEACTIVATION) {
        rtu_reply_negative(conn, asdu, CS101_COT_UNKNOWN_COT);
        return true;
    }

    InformationObject io = CS101_ASDU_getElement(asdu, 0);
    int ioa = (io && CS101_ASDU_getNumberOfElements(asdu) == 1) ? InformationObject_getObjectAddress(io) : -1;
    const PmCommand* c = (ioa >= 0) ? pointmap_command_by_ioa(rtu.pm, ioa) : NULL;
    if (!c) {
        if (io)
            InformationObject_destroy(io);
        rtu_reply_negative(conn, asdu, CS101_COT_UNKNOWN_IOA);
        return true;
    }

    int cmdIndex = (int)(c - rtu.pm->commands);
    char peerStr[PEER_LEN];
    peer(conn, peerStr, sizeof(peerStr));

    pthread_mutex_lock(&rtu.lock);
    TypeID expected = c->kind == PM_KIND_DOUBLE_COMMAND ? C_DC_NA_1 :
                      c->kind == PM_KIND_REGULATING_STEP ? C_RC_NA_1 : C_SC_NA_1;
    if (type != expected || CS101_ASDU_isTest(asdu)) rtu_reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON);
    else {
        int value = type == C_DC_NA_1 ? (int)DoubleCommand_getState((DoubleCommand)io) :
                    type == C_RC_NA_1 ? (int)StepCommand_getState((StepCommand)io) : SingleCommand_getState((SingleCommand)io);
        int qu = type == C_DC_NA_1 ? DoubleCommand_getQU((DoubleCommand)io) :
                 type == C_RC_NA_1 ? StepCommand_getQU((StepCommand)io) : SingleCommand_getQU((SingleCommand)io);
        bool select = type == C_DC_NA_1 ? DoubleCommand_isSelect((DoubleCommand)io) :
                      type == C_RC_NA_1 ? StepCommand_isSelect((StepCommand)io) : SingleCommand_isSelect((SingleCommand)io);
        rtu_handle_command(conn, peerStr, asdu, cmdIndex, value, qu, select);
    }
    pthread_mutex_unlock(&rtu.lock);

    InformationObject_destroy(io);
    return true;
}

static bool
allowed_ca_handler(void* param, int ca)
{
    (void)param;
    return ca == rtu.pm->ca;
}

static bool
connection_request_handler(void* param, const char* ip)
{
    (void)param;
    rd_log("connection request from %s", ip);
    return true;
}

static void
connection_event_handler(void* param, IMasterConnection conn, CS104_PeerConnectionEvent event)
{
    (void)param;
    static const char* names[] = {"connection opened", "connection closed", "STARTDT", "STOPDT"};
    char addr[PEER_LEN];
    pthread_mutex_lock(&rtu.lock);

    int index = -1;
    for (int i = 0; i < rtu.nStarted; i++) {
        if (rtu.started[i].conn == conn) {
            index = i;
            break;
        }
    }
    /* lib60870 v2.4.1 emits CLOSED, but getPeerAddress calls getpeername
     * on the socket: after a reset it returns no address. Identity must be
     * the handle, not that fallible diagnostic string. lib60870 reuses
     * handles only after this callback returns, so retirement cannot be
     * deferred to the next main-loop tick. */
    if (index >= 0)
        snprintf(addr, sizeof(addr), "%s", rtu.started[index].peer);
    else
        peer(conn, addr, sizeof(addr));
    rd_log("%s peer=%s", names[event], addr);

    switch (event) {
    case CS104_CON_EVENT_ACTIVATED:
        if (index < 0) {
            if (rtu.nStarted == MAX_CONNECTIONS) {
                rd_log("started connection list full (%d); peer=%s not added", MAX_CONNECTIONS, addr);
                break;
            }
            Started* s = &rtu.started[rtu.nStarted++];
            s->conn = conn;
            snprintf(s->peer, sizeof(s->peer), "%s", addr);
        }
        break;
    case CS104_CON_EVENT_DEACTIVATED:
    case CS104_CON_EVENT_CONNECTION_CLOSED:
        if (index >= 0) {
            if (rtu.sel.active && strcmp(rtu.sel.peer, addr) == 0)
                rtu_clear_selection("selecting connection stopped");
            /* Let an in-flight field write finish, but never deliver its
             * response to a later connection reusing the same peer tuple. */
            if (rtu.pend.active && strcmp(rtu.pend.peer, addr) == 0)
                rtu.pend.peer[0] = '\0';
            rtu.started[index] = rtu.started[--rtu.nStarted];
            rtu.started[rtu.nStarted] = (Started){0};
        }
        break;
    case CS104_CON_EVENT_CONNECTION_OPENED:
        break;
    }
    pthread_mutex_unlock(&rtu.lock);
}

void
rtu_server_init(const char* bind, int port)
{
    const PointMap* pm = rtu.pm;
    rtu.slave = CS104_Slave_create(16, 64);
    CS104_Slave_setLocalAddress(rtu.slave, bind);
    CS104_Slave_setLocalPort(rtu.slave, port);
    CS104_Slave_setServerMode(rtu.slave, CS104_MODE_CONNECTION_IS_REDUNDANCY_GROUP);
    CS104_Slave_setMaxOpenConnections(rtu.slave, MAX_CONNECTIONS);
    rtu.al = CS104_Slave_getAppLayerParameters(rtu.slave);

    CS104_APCIParameters apci = CS104_Slave_getConnectionParameters(rtu.slave);
    apci->k = pm->timers.k;
    apci->w = pm->timers.w;
    apci->t0 = pm->timers.t0_s;
    apci->t1 = pm->timers.t1_s;
    apci->t2 = pm->timers.t2_s;
    apci->t3 = pm->timers.t3_s;

    CS104_Slave_setInterrogationHandler(rtu.slave, interrogation_handler, NULL);
    CS104_Slave_setASDUHandler(rtu.slave, asdu_handler, NULL);
    CS104_Slave_setAllowedCAHandler(rtu.slave, allowed_ca_handler, NULL);
    CS104_Slave_setConnectionRequestHandler(rtu.slave, connection_request_handler, NULL);
    CS104_Slave_setConnectionEventHandler(rtu.slave, connection_event_handler, NULL);
}
