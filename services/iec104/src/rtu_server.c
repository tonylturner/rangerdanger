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

/* ---- time tags ------------------------------------------------------- */

static void
fill_cp56(struct sCP56Time2a* t)
{
    uint64_t ms;
    bool synced = sc_now(&rtu.clock, &ms);
    CP56Time2a_createFromMsTimestamp(t, ms);
    CP56Time2a_setInvalid(t, !synced);
}

/* ---- quality --------------------------------------------------------- */

QualityDescriptor
rtu_point_quality(const PointRT* rt, const PmPoint* p)
{
    QualityDescriptor q = IEC60870_QUALITY_GOOD;
    if (rt->deviceUnreachable)
        q = (QualityDescriptor)(q | IEC60870_QUALITY_INVALID | IEC60870_QUALITY_NON_TOPICAL);
    if (rt->valueInvalid || rt->heartbeatStale || (p->measurement && rt->measurementInvalid))
        q = (QualityDescriptor)(q | IEC60870_QUALITY_INVALID);
    return q;
}

/* ---- information-object builders ------------------------------------- */

/* Caller holds rtu.lock. Builds the untimed GI form of a point. */
static InformationObject
build_untimed(const PmPoint* p, const PointRT* rt, QualityDescriptor q)
{
    switch (p->kind) {
    case PM_KIND_DOUBLE_POINT:
        return (InformationObject)DoublePointInformation_create(NULL, p->ioa, rt->dp, q);
    case PM_KIND_SINGLE_POINT:
        return (InformationObject)SinglePointInformation_create(NULL, p->ioa, rt->sp, q);
    case PM_KIND_STEP_POSITION:
        return (InformationObject)StepPositionInformation_create(NULL, p->ioa, rt->step, rt->transient, q);
    case PM_KIND_FLOAT:
        return (InformationObject)MeasuredValueShort_create(NULL, p->ioa, rt->f, q);
    default:
        return NULL;
    }
}

/* Caller holds rtu.lock. Builds the spontaneous form (timed for status). */
static InformationObject
build_spontaneous(const PmPoint* p, const PointRT* rt, QualityDescriptor q, struct sCP56Time2a* t)
{
    switch (p->kind) {
    case PM_KIND_DOUBLE_POINT:
        fill_cp56(t);
        return (InformationObject)DoublePointWithCP56Time2a_create(NULL, p->ioa, rt->dp, q, t);
    case PM_KIND_SINGLE_POINT:
        fill_cp56(t);
        return (InformationObject)SinglePointWithCP56Time2a_create(NULL, p->ioa, rt->sp, q, t);
    case PM_KIND_STEP_POSITION:
        fill_cp56(t);
        return (InformationObject)StepPositionWithCP56Time2a_create(NULL, p->ioa, rt->step, rt->transient, q, t);
    case PM_KIND_FLOAT:
        return (InformationObject)MeasuredValueShort_create(NULL, p->ioa, rt->f, q);
    default:
        return NULL;
    }
}

/* Caller holds rtu.lock. Returns the started connection for a peer, or NULL. */
IMasterConnection
rtu_conn_for_peer(const char* peerStr)
{
    for (int i = 0; i < rtu.nStarted; i++)
        if (strcmp(rtu.started[i].peer, peerStr) == 0)
            return rtu.started[i].conn;
    return NULL;
}

/* Caller holds rtu.lock. */
static void
broadcast(CS101_ASDU asdu)
{
    rd_log_asdu("tx-all", asdu);
    for (int i = 0; i < rtu.nStarted; i++)
        IMasterConnection_sendASDU(rtu.started[i].conn, asdu);
}

/* Caller holds rtu.lock. Sends one point as a spontaneous (or COT 11) report. */
void
rtu_report_point(int idx, CS101_CauseOfTransmission cot)
{
    const PmPoint* p = &rtu.pm->points[idx];
    PointRT* rt = &rtu.points[idx];
    QualityDescriptor q = rtu_point_quality(rt, p);
    struct sCP56Time2a t;

    CS101_ASDU asdu = CS101_ASDU_create(rtu.al, false, cot, 0, rtu.pm->ca, false, false);
    InformationObject io = build_spontaneous(p, rt, q, &t);
    CS101_ASDU_addInformationObject(asdu, io);
    InformationObject_destroy(io);
    broadcast(asdu);
    CS101_ASDU_destroy(asdu);

    rt->repQuality = q;
    rt->repDp = rt->dp;
    rt->repSp = rt->sp;
    rt->repF = rt->f;
    rt->repStep = rt->step;
    rt->repTransient = rt->transient;
}

/* Caller holds rtu.lock. Recomputes causes and reports every changed point. */
void
rtu_evaluate_and_report(void)
{
    uint64_t now = rtu_mono_ms();
    for (int d = 0; d < rtu.pm->n_devices; d++) {
        const PmDevice* dev = &rtu.pm->devices[d];
        DeviceRT* drt = &rtu.devices[d];
        drt->unreachable = drt->lastOkMono == 0 || (now - drt->lastOkMono) > (uint64_t)dev->stale_after_ms;
    }

    for (int i = 0; i < rtu.pm->n_points; i++) {
        const PmPoint* p = &rtu.pm->points[i];
        PointRT* rt = &rtu.points[i];
        DeviceRT* drt = &rtu.devices[p->device_index];
        const PmDevice* dev = &rtu.pm->devices[p->device_index];
        rt->deviceUnreachable = drt->unreachable;
        rt->heartbeatStale = dev->has_heartbeat && (drt->heartbeatChanged == 0 ||
            now - drt->heartbeatChanged >= (uint64_t)dev->heartbeat_stale_ms);

        QualityDescriptor newQ = rtu_point_quality(rt, p);
        bool qChanged = newQ != rt->repQuality;
        bool report = false;

        switch (p->kind) {
        case PM_KIND_DOUBLE_POINT:
            report = qChanged || rt->dp != rt->repDp;
            break;
        case PM_KIND_SINGLE_POINT:
            report = qChanged || rt->sp != rt->repSp;
            break;
        case PM_KIND_STEP_POSITION:
            report = qChanged || rt->step != rt->repStep || rt->transient != rt->repTransient;
            break;
        case PM_KIND_FLOAT:
            if (qChanged)
                report = true;
            else if (rt->f != rt->repF && fabs((double)rt->f - (double)rt->repF) >= p->deadband)
                report = true;
            break;
        default:
            break;
        }

        if (report)
            rtu_report_point(i, CS101_COT_SPONTANEOUS);
    }
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

    for (int i = 0; i < rtu.pm->n_points; i++) {
        const PmPoint* p = &rtu.pm->points[i];
        PointRT* rt = &rtu.points[i];
        QualityDescriptor q = rtu_point_quality(rt, p);
        CS101_ASDU a =
            CS101_ASDU_create(rtu.al, false, CS101_COT_INTERROGATED_BY_STATION, oa, rtu.pm->ca, false, false);
        InformationObject io = build_untimed(p, rt, q);
        CS101_ASDU_addInformationObject(a, io);
        InformationObject_destroy(io);
        IMasterConnection_sendASDU(conn, a);
        CS101_ASDU_destroy(a);
    }

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

/* Caller holds rtu.lock. */
static void
forget_started(const char* peerStr)
{
    for (int i = 0; i < rtu.nStarted; i++) {
        if (strcmp(rtu.started[i].peer, peerStr) == 0) {
            rtu.started[i] = rtu.started[--rtu.nStarted];
            break;
        }
    }
}

/* Caller holds rtu.lock. */
static void
forget_connection(const char* peerStr)
{
    forget_started(peerStr);
    if (rtu.sel.active && strcmp(rtu.sel.peer, peerStr) == 0)
        rtu_clear_selection("selecting connection stopped");
}

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
void
rtu_apply_connection_events(void)
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
            /* Events apply late: a select may already have arrived on this
             * started connection, so STARTDT must not clear it. */
            forget_started(events[i].peer);
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
