/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * iec104-rtu: the substation RTU / gateway, an IEC 104 controlled station
 * (server) built on lib60870-C. Unlike the increment-2b spike, this RTU has
 * no fake physics: every value comes from a real Modbus TCP poll of the
 * feeder breaker, every time tag comes from the GPS-fed station clock, and
 * every command is executed as a Modbus write whose readback produces the
 * COT 11 feedback report. The point set, CA, timers and command mapping are
 * loaded from the package point map (contract §3.1); nothing is hard-coded.
 *
 * Threads:
 *   - lib60870's own receive threads call interrogation_handler,
 *     asdu_handler and connection_event_handler.
 *   - the station clock polls gps-sim on its own thread (stationclock.c).
 *   - the main loop owns the Modbus socket: it polls the device, applies
 *     quality rules, reports spontaneous changes, and executes commands.
 *     Modbus I/O happens with rtu.lock released; shared state and all ASDU
 *     sends happen under rtu.lock.
 *
 * Quality and reporting rules are contract §3.2:
 *   - device_unreachable (no successful poll within stale_after_ms) -> IV+NT.
 *   - measurement_invalid (measurement point while the device valid bit is 0)
 *     -> IV, floats only.
 *   - every quality change is reported spontaneously with the last value;
 *     the float deadband suppresses value-only float changes.
 *   - status spontaneous events use the timed type (CP56Time2a, UTC, IV when
 *     the clock is not synced); a command-caused feedback report is COT 11.
 */
#include <math.h>
#include <pthread.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "common.h"
#include "cs104_slave.h"
#include "hal_thread.h"
#include "hal_time.h"
#include "modbus.h"
#include "pointmap.h"
#include "stationclock.h"

#define MAX_CONNECTIONS 8
#define EVENT_QUEUE 64
#define PEER_LEN 64

/* Per-point runtime: raw value, causes, and the last value/quality reported. */
typedef struct {
    DoublePointValue dp;
    bool sp;
    float f;
    bool deviceUnreachable;
    bool measurementInvalid;

    QualityDescriptor repQuality;
    DoublePointValue repDp;
    bool repSp;
    float repF;
} PointRT;

typedef struct {
    MbMaster mb;
    uint64_t lastOkMono; /* monotonic ms of the last successful poll; 0 = never */
    uint64_t lastTryMono;
    uint64_t lastConnectTryMono;
    bool unreachable;
    int regCount; /* number of input registers to read in one FC4 */
} DeviceRT;

/* Command responses are addressed by peer via conn_for_peer, not a stored handle. */
typedef struct {
    bool active;
    int cmdIndex;
    char peer[PEER_LEN];
    int dcs;
    int qu;
    uint64_t deadlineMono;
} Selection;

typedef struct {
    bool active;
    int cmdIndex;
    char peer[PEER_LEN];
    int dcs;
    CS101_ASDU command;
} PendingExecute;

typedef struct {
    IMasterConnection conn;
    char peer[PEER_LEN];
    CS104_PeerConnectionEvent event;
} ConnEvent;

typedef struct {
    char peer[PEER_LEN];
    IMasterConnection conn;
} Started;

static struct {
    pthread_mutex_t lock;
    CS104_Slave slave;
    CS101_AppLayerParameters al;
    const PointMap* pm;
    StationClock clock;

    PointRT points[PM_MAX_POINTS];
    DeviceRT devices[PM_MAX_DEVICES];

    Selection sel;
    PendingExecute pend;

    Started started[MAX_CONNECTIONS];
    int nStarted;

    pthread_mutex_t evLock;
    ConnEvent events[EVENT_QUEUE];
    int nEvents;
} rtu;

static volatile sig_atomic_t stopRequested = 0;

static void
on_signal(int sig)
{
    (void)sig;
    stopRequested = 1;
}

static uint64_t
mono_ms(void)
{
    return Hal_getMonotonicTimeInMs();
}

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

static QualityDescriptor
point_quality(const PointRT* rt, const PmPoint* p)
{
    QualityDescriptor q = IEC60870_QUALITY_GOOD;
    if (rt->deviceUnreachable)
        q = (QualityDescriptor)(q | IEC60870_QUALITY_INVALID | IEC60870_QUALITY_NON_TOPICAL);
    if (p->measurement && rt->measurementInvalid)
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
    case PM_KIND_FLOAT:
        return (InformationObject)MeasuredValueShort_create(NULL, p->ioa, rt->f, q);
    default:
        return NULL;
    }
}

/* Caller holds rtu.lock. Returns the started connection for a peer, or NULL. */
static IMasterConnection
conn_for_peer(const char* peerStr)
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
static void
report_point(int idx, CS101_CauseOfTransmission cot)
{
    const PmPoint* p = &rtu.pm->points[idx];
    PointRT* rt = &rtu.points[idx];
    QualityDescriptor q = point_quality(rt, p);
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
}

/* ---- Modbus polling -------------------------------------------------- */

/* Extract one point's value from an input-register snapshot. */
static void
apply_point(int idx, const uint16_t* regs, int regCount)
{
    const PmPoint* p = &rtu.pm->points[idx];
    PointRT* rt = &rtu.points[idx];
    if (p->table != PM_TABLE_INPUT)
        return;
    switch (p->format) {
    case PM_FMT_DP:
        if (p->address < regCount) {
            int v = regs[p->address] & 0x3;
            rt->dp = (DoublePointValue)v;
        }
        break;
    case PM_FMT_BIT:
        if (p->address < regCount)
            rt->sp = ((regs[p->address] >> p->bit) & 1) != 0;
        break;
    case PM_FMT_F32:
        if (p->address + 1 < regCount)
            rt->f = mb_regs_to_f32(regs[p->address], regs[p->address + 1]);
        break;
    }
}

/*
 * Reads one device's input block (FC4) and applies it to that device's
 * points under the lock. Returns the Modbus status. On MB_OK it also sets
 * measurement_invalid from the device valid bit and records the poll time.
 */
static MbStatus
poll_device(int devIdx)
{
    const PmDevice* dev = &rtu.pm->devices[devIdx];
    DeviceRT* drt = &rtu.devices[devIdx];
    uint16_t regs[128];
    int exc = 0;

    MbStatus st = mb_read_input(&drt->mb, 0, drt->regCount, regs, &exc, dev->timeout_ms);
    if (st != MB_OK) {
        if (st == MB_IO_ERROR)
            mb_close(&drt->mb);
        return st;
    }

    pthread_mutex_lock(&rtu.lock);
    drt->lastOkMono = mono_ms();
    bool mvValid = true;
    if (dev->has_measurements_valid && dev->mv_address < drt->regCount)
        mvValid = ((regs[dev->mv_address] >> dev->mv_bit) & 1) != 0;

    for (int i = 0; i < rtu.pm->n_points; i++) {
        if (rtu.pm->points[i].device_index != devIdx)
            continue;
        apply_point(i, regs, drt->regCount);
        if (rtu.pm->points[i].measurement)
            rtu.points[i].measurementInvalid = !mvValid;
    }
    pthread_mutex_unlock(&rtu.lock);
    return st;
}

/* Caller holds rtu.lock. Recomputes causes and reports every changed point. */
static void
evaluate_and_report(void)
{
    uint64_t now = mono_ms();
    for (int d = 0; d < rtu.pm->n_devices; d++) {
        const PmDevice* dev = &rtu.pm->devices[d];
        DeviceRT* drt = &rtu.devices[d];
        drt->unreachable = drt->lastOkMono == 0 || (now - drt->lastOkMono) > (uint64_t)dev->stale_after_ms;
    }

    for (int i = 0; i < rtu.pm->n_points; i++) {
        const PmPoint* p = &rtu.pm->points[i];
        PointRT* rt = &rtu.points[i];
        rt->deviceUnreachable = rtu.devices[p->device_index].unreachable;

        QualityDescriptor newQ = point_quality(rt, p);
        bool qChanged = newQ != rt->repQuality;
        bool report = false;

        switch (p->kind) {
        case PM_KIND_DOUBLE_POINT:
            report = qChanged || rt->dp != rt->repDp;
            break;
        case PM_KIND_SINGLE_POINT:
            report = qChanged || rt->sp != rt->repSp;
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
            report_point(i, CS101_COT_SPONTANEOUS);
    }
}

/* ---- commands -------------------------------------------------------- */

static void
clear_selection(const char* why)
{
    if (rtu.sel.active) {
        const PmCommand* c = &rtu.pm->commands[rtu.sel.cmdIndex];
        rd_log("command ioa=%d peer=%s phase=select result=cleared reason=\"%s\"", c->ioa, rtu.sel.peer, why);
    }
    rtu.sel = (Selection){0};
}

/* Caller holds rtu.lock. */
static void
reply_negative(IMasterConnection conn, CS101_ASDU asdu, CS101_CauseOfTransmission cot)
{
    CS101_ASDU_setCOT(asdu, cot);
    CS101_ASDU_setNegative(asdu, true);
    IMasterConnection_sendASDU(conn, asdu);
}

/* Caller holds rtu.lock. Validates SBO; a valid execute is queued, not confirmed yet. */
static void
handle_double_command(IMasterConnection conn, const char* peerStr, CS101_ASDU asdu, int cmdIndex, DoubleCommand dc)
{
    const PmCommand* c = &rtu.pm->commands[cmdIndex];
    CS101_CauseOfTransmission cot = CS101_ASDU_getCOT(asdu);
    int dcs = DoubleCommand_getState(dc);
    int qu = DoubleCommand_getQU(dc);
    uint64_t now = mono_ms();

    if (rtu.sel.active && now >= rtu.sel.deadlineMono)
        clear_selection("select timeout elapsed");

    bool ownsSelection = rtu.sel.active && strcmp(rtu.sel.peer, peerStr) == 0 && rtu.sel.cmdIndex == cmdIndex;

    if (cot == CS101_COT_DEACTIVATION) {
        if (ownsSelection) {
            clear_selection("deactivated by the selecting client");
            CS101_ASDU_setCOT(asdu, CS101_COT_DEACTIVATION_CON);
            IMasterConnection_sendASDU(conn, asdu);
        } else {
            reply_negative(conn, asdu, CS101_COT_DEACTIVATION_CON);
        }
        return;
    }

    if (dcs != IEC60870_DOUBLE_POINT_OFF && dcs != IEC60870_DOUBLE_POINT_ON) {
        rd_log("command ioa=%d peer=%s phase=%s result=rejected reason=\"DCS 0/3 not permitted\"", c->ioa, peerStr,
               DoubleCommand_isSelect(dc) ? "select" : "execute");
        reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON);
        return;
    }

    if (DoubleCommand_isSelect(dc)) {
        if (!c->sbo) {
            rd_log("command ioa=%d peer=%s phase=select result=rejected reason=\"not an SBO command\"", c->ioa,
                   peerStr);
            reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON);
            return;
        }
        if (rtu.pend.active) {
            reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON);
            return;
        }
        if (rtu.sel.active && !ownsSelection) {
            rd_log("command ioa=%d peer=%s phase=select result=rejected reason=\"selected by another peer\"", c->ioa,
                   peerStr);
            reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON);
            return;
        }
        rtu.sel.active = true;
        rtu.sel.cmdIndex = cmdIndex;
        snprintf(rtu.sel.peer, PEER_LEN, "%s", peerStr);
        rtu.sel.dcs = dcs;
        rtu.sel.qu = qu;
        rtu.sel.deadlineMono = now + (uint64_t)rtu.pm->select_timeout_ms;
        rd_log("command ioa=%d peer=%s phase=select result=accepted dcs=%d", c->ioa, peerStr, dcs);
        IMasterConnection_sendACT_CON(conn, asdu, false);
        return;
    }

    /* execute */
    if (c->sbo && !ownsSelection) {
        rd_log("command ioa=%d peer=%s phase=execute result=rejected reason=\"%s\"", c->ioa, peerStr,
               rtu.sel.active ? "selected by another peer" : "execute without a valid selection");
        reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON);
        return;
    }
    if (c->sbo && (dcs != rtu.sel.dcs || qu != rtu.sel.qu)) {
        clear_selection("execute does not match the selection");
        rd_log("command ioa=%d peer=%s phase=execute result=rejected reason=\"value differs from selection\"", c->ioa,
               peerStr);
        reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON);
        return;
    }
    if (rtu.pend.active) {
        reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON);
        return;
    }

    clear_selection("executed");
    rtu.pend.active = true;
    rtu.pend.cmdIndex = cmdIndex;
    snprintf(rtu.pend.peer, PEER_LEN, "%s", peerStr);
    rtu.pend.dcs = dcs;
    rtu.pend.command = CS101_ASDU_clone(asdu, NULL);
    rd_log("command ioa=%d peer=%s phase=execute result=accepted dcs=%d", c->ioa, peerStr, dcs);
}

/*
 * Executes a queued command: FC6 write, deferred ACT-CON from the write
 * result, readback of the feedback point, COT 11 feedback report, ACT-TERM.
 * Never retries the write. Modbus I/O runs with rtu.lock released.
 */
static void
execute_command(const PendingExecute* p)
{
    const PmCommand* c = &rtu.pm->commands[p->cmdIndex];
    int devIdx = c->device_index;
    const PmDevice* dev = &rtu.pm->devices[devIdx];
    DeviceRT* drt = &rtu.devices[devIdx];

    if (!mb_is_connected(&drt->mb))
        mb_connect(&drt->mb, dev->host, dev->port, dev->timeout_ms);

    int exc = 0;
    MbStatus st = mb_write_single(&drt->mb, c->address, (uint16_t)p->dcs, &exc, dev->timeout_ms);
    rd_log("command ioa=%d peer=%s phase=write result=%s exc=%d", c->ioa, p->peer,
           st == MB_OK ? "ok" : (st == MB_EXCEPTION ? "exception" : (st == MB_TIMEOUT ? "timeout" : "io_error")), exc);

    /*
     * Command responses go to the commanding peer's *started* connection,
     * resolved from the connection list under the lock — the same object
     * broadcast() uses. Sending to the IMasterConnection captured in the ASDU
     * callback does not reach the client from the main-loop thread.
     */
    if (st != MB_OK) {
        if (st == MB_IO_ERROR)
            mb_close(&drt->mb);
        pthread_mutex_lock(&rtu.lock);
        IMasterConnection conn = conn_for_peer(p->peer);
        if (conn)
            IMasterConnection_sendACT_CON(conn, p->command, true);
        pthread_mutex_unlock(&rtu.lock);
        rd_log("command ioa=%d peer=%s phase=execute result=%s", c->ioa, p->peer,
               st == MB_EXCEPTION ? "rejected" : "failed");
        return;
    }

    /* positive ACT-CON once the write echoed */
    pthread_mutex_lock(&rtu.lock);
    IMasterConnection conn = conn_for_peer(p->peer);
    if (conn)
        IMasterConnection_sendACT_CON(conn, p->command, false);
    pthread_mutex_unlock(&rtu.lock);

    /* readback until the feedback point matches the commanded value or the device timeout passes */
    uint64_t deadline = mono_ms() + (uint64_t)dev->timeout_ms;
    int fb = c->feedback_index;
    for (;;) {
        poll_device(devIdx);
        pthread_mutex_lock(&rtu.lock);
        bool matched = rtu.points[fb].dp == (DoublePointValue)p->dcs;
        pthread_mutex_unlock(&rtu.lock);
        if (matched || mono_ms() >= deadline)
            break;
        Thread_sleep(20);
    }

    /* feedback report (COT 11) for the feedback point, then ACT-TERM */
    pthread_mutex_lock(&rtu.lock);
    report_point(fb, CS101_COT_RETURN_INFO_REMOTE);
    conn = conn_for_peer(p->peer);
    if (conn)
        IMasterConnection_sendACT_TERM(conn, p->command);
    DoublePointValue fbv = rtu.points[fb].dp;
    pthread_mutex_unlock(&rtu.lock);
    rd_log("command ioa=%d peer=%s phase=execute result=completed feedback=%s", c->ioa, p->peer, rd_dpi_str(fbv));
}

/* ---- lib60870 handlers ----------------------------------------------- */

static bool
interrogation_handler(void* param, IMasterConnection conn, CS101_ASDU asdu, uint8_t qoi)
{
    (void)param;
    rd_log_asdu("rx", asdu);

    int ca = CS101_ASDU_getCA(asdu);
    if (ca != rtu.pm->ca) {
        reply_negative(conn, asdu, CS101_COT_UNKNOWN_CA);
        return true;
    }
    if (CS101_ASDU_getCOT(asdu) == CS101_COT_DEACTIVATION) {
        reply_negative(conn, asdu, CS101_COT_DEACTIVATION_CON);
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
        QualityDescriptor q = point_quality(rt, p);
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
    if (CS101_ASDU_getTypeID(asdu) != C_DC_NA_1)
        return false; /* lib60870 answers COT 44, unknown type */

    rd_log_asdu("rx", asdu);

    int ca = CS101_ASDU_getCA(asdu);
    if (ca != rtu.pm->ca) {
        reply_negative(conn, asdu, CS101_COT_UNKNOWN_CA);
        return true;
    }
    CS101_CauseOfTransmission cot = CS101_ASDU_getCOT(asdu);
    if (cot != CS101_COT_ACTIVATION && cot != CS101_COT_DEACTIVATION) {
        reply_negative(conn, asdu, CS101_COT_UNKNOWN_COT);
        return true;
    }

    InformationObject io = CS101_ASDU_getElement(asdu, 0);
    int ioa = (io && CS101_ASDU_getNumberOfElements(asdu) == 1) ? InformationObject_getObjectAddress(io) : -1;
    const PmCommand* c = (ioa >= 0) ? pointmap_command_by_ioa(rtu.pm, ioa) : NULL;
    if (!c) {
        if (io)
            InformationObject_destroy(io);
        reply_negative(conn, asdu, CS101_COT_UNKNOWN_IOA);
        return true;
    }

    int cmdIndex = (int)(c - rtu.pm->commands);
    char peerStr[PEER_LEN];
    peer(conn, peerStr, sizeof(peerStr));

    pthread_mutex_lock(&rtu.lock);
    handle_double_command(conn, peerStr, asdu, cmdIndex, (DoubleCommand)io);
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
            forget_connection(events[i].peer);
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

/* ---- main loop ------------------------------------------------------- */

static void
ensure_modbus(void)
{
    uint64_t now = mono_ms();
    for (int d = 0; d < rtu.pm->n_devices; d++) {
        const PmDevice* dev = &rtu.pm->devices[d];
        DeviceRT* drt = &rtu.devices[d];
        if (mb_is_connected(&drt->mb))
            continue;
        if (drt->lastConnectTryMono != 0 && (now - drt->lastConnectTryMono) < (uint64_t)dev->timeout_ms)
            continue;
        drt->lastConnectTryMono = now;
        mb_connect(&drt->mb, dev->host, dev->port, dev->timeout_ms);
    }
}

static void
tick(void)
{
    /* 1. connection events, selection expiry, take ownership of any pending command */
    PendingExecute p = {0};
    pthread_mutex_lock(&rtu.lock);
    apply_connection_events();
    if (rtu.sel.active && mono_ms() >= rtu.sel.deadlineMono)
        clear_selection("select timeout elapsed");
    if (rtu.pend.active) {
        p = rtu.pend;
        rtu.pend = (PendingExecute){0};
    }
    pthread_mutex_unlock(&rtu.lock);

    /* 2. execute a queued command (blocking Modbus I/O without the lock) */
    if (p.active) {
        execute_command(&p);
        if (p.command)
            CS101_ASDU_destroy(p.command);
    }

    /* 3. poll the devices that are due */
    ensure_modbus();
    uint64_t now = mono_ms();
    for (int d = 0; d < rtu.pm->n_devices; d++) {
        const PmDevice* dev = &rtu.pm->devices[d];
        DeviceRT* drt = &rtu.devices[d];
        if (drt->lastTryMono != 0 && (now - drt->lastTryMono) < (uint64_t)dev->poll_ms)
            continue;
        drt->lastTryMono = now;
        if (mb_is_connected(&drt->mb))
            poll_device(d);
    }

    /* 4. report changed points (value changes from the poll, quality changes from staleness) */
    pthread_mutex_lock(&rtu.lock);
    evaluate_and_report();
    pthread_mutex_unlock(&rtu.lock);
}

static void
usage(void)
{
    fprintf(stderr, "Usage: iec104-rtu --config PATH [--bind ADDR] [--port N]\n"
                    "\n"
                    "IEC 104 controlled station (server). The point map at --config supplies\n"
                    "CA, timers, the Modbus device, the monitored points and the command set.\n");
    exit(2);
}

int
main(int argc, char** argv)
{
    const char* config = NULL;
    const char* bind = "0.0.0.0";
    long port = IEC_60870_5_104_DEFAULT_PORT;

    for (int i = 1; i < argc; i++) {
        const char* a = argv[i];
        const char* v = (i + 1 < argc) ? argv[i + 1] : NULL;
        if (strcmp(a, "--config") == 0 && v) {
            config = v;
            i++;
        } else if (strcmp(a, "--bind") == 0 && v) {
            bind = v;
            i++;
        } else if (strcmp(a, "--port") == 0 && v && rd_parse_int(v, 1, 65535, &port)) {
            i++;
        } else {
            usage();
        }
    }
    if (!config)
        usage();

    rd_log_init("rtu");

    char err[256];
    PointMap* pm = pointmap_load(config, err, sizeof(err));
    if (!pm) {
        rd_log("config error: %s", err);
        return 1;
    }
    rtu.pm = pm;

    pthread_mutex_init(&rtu.lock, NULL);
    pthread_mutex_init(&rtu.evLock, NULL);

    /* seed per-point reported state so the first good poll reports a GOOD transition */
    for (int i = 0; i < pm->n_points; i++) {
        PointRT* rt = &rtu.points[i];
        rt->deviceUnreachable = true;
        rt->repQuality = IEC60870_QUALITY_INVALID | IEC60870_QUALITY_NON_TOPICAL;
        rt->dp = IEC60870_DOUBLE_POINT_INDETERMINATE;
        rt->repDp = IEC60870_DOUBLE_POINT_INDETERMINATE;
    }
    for (int d = 0; d < pm->n_devices; d++) {
        DeviceRT* drt = &rtu.devices[d];
        mb_init(&drt->mb, pm->devices[d].unit);
        int maxReg = 0;
        for (int i = 0; i < pm->n_points; i++) {
            const PmPoint* pt = &pm->points[i];
            if (pt->device_index != d || pt->table != PM_TABLE_INPUT)
                continue;
            int top = pt->address + (pt->format == PM_FMT_F32 ? 1 : 0);
            if (top > maxReg)
                maxReg = top;
        }
        if (pm->devices[d].has_measurements_valid && pm->devices[d].mv_address > maxReg)
            maxReg = pm->devices[d].mv_address;
        drt->regCount = maxReg + 1;
        if (drt->regCount > 125)
            drt->regCount = 125;
    }

    sc_init(&rtu.clock, pm);
    sc_start(&rtu.clock);

    struct sigaction sa = {0};
    sa.sa_handler = on_signal;
    sigaction(SIGINT, &sa, NULL);
    sigaction(SIGTERM, &sa, NULL);

    rtu.slave = CS104_Slave_create(16, 64);
    CS104_Slave_setLocalAddress(rtu.slave, bind);
    CS104_Slave_setLocalPort(rtu.slave, (int)port);
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

    CS104_Slave_start(rtu.slave);
    if (!CS104_Slave_isRunning(rtu.slave)) {
        rd_log("cannot listen on %s:%ld", bind, port);
        CS104_Slave_destroy(rtu.slave);
        sc_stop(&rtu.clock);
        pointmap_free(pm);
        return 1;
    }

    rd_log("listening %s:%ld ca=%d points=%d devices=%d select-timeout=%dms", bind, port, pm->ca, pm->n_points,
           pm->n_devices, pm->select_timeout_ms);

    while (!stopRequested) {
        tick();
        Thread_sleep(20);
    }

    rd_log("stopping");
    CS104_Slave_stop(rtu.slave);
    CS104_Slave_destroy(rtu.slave);
    sc_stop(&rtu.clock);
    for (int d = 0; d < pm->n_devices; d++)
        mb_close(&rtu.devices[d].mb);
    pointmap_free(pm);
    return 0;
}
