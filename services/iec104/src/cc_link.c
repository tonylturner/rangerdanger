/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 */
#include "cc_link.h"

#include <arpa/inet.h>
#include <errno.h>
#include <netinet/in.h>
#include <pthread.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

#include "cc_state.h"
#include "common.h"
#include "cs101_information_objects.h"
#include "cs104_connection.h"

static CS104_Connection con;
static pthread_mutex_t ioLock = PTHREAD_MUTEX_INITIALIZER;

/* ---- lib60870 receive-thread handlers -------------------------------- */

static void
on_raw_message(void* param, uint8_t* msg, int msgSize, bool sent)
{
    (void)param;
    (void)msg;
    (void)msgSize;
    if (!sent)
        cc_on_frame_received();
}

static void
on_connection_event(void* param, CS104_Connection connection, CS104_ConnectionEvent event)
{
    (void)param;
    (void)connection;
    static const char* names[] = {"opened", "closed", "startdt-con", "stopdt-con", "failed"};
    rd_log("link %s", names[event]);

    switch (event) {
    case CS104_CONNECTION_OPENED:
        cc_on_opened();
        break;
    case CS104_CONNECTION_STARTDT_CON_RECEIVED:
        cc_on_started();
        break;
    case CS104_CONNECTION_STOPDT_CON_RECEIVED:
        break; /* the CC never sends STOPDT */
    case CS104_CONNECTION_CLOSED:
    case CS104_CONNECTION_FAILED:
        cc_on_ended();
        break;
    }
}

/* Decodes one monitoring object; false for a type the CC does not cache. */
static bool
decode_report(TypeID type, InformationObject io, CcReport* r)
{
    memset(r, 0, sizeof(*r));
    r->ioa = InformationObject_getObjectAddress(io);
    r->type = type;
    CP56Time2a t = NULL;

    switch (type) {
    case M_DP_NA_1:
    case M_DP_TB_1:
        r->dp = DoublePointInformation_getValue((DoublePointInformation)io);
        r->q = DoublePointInformation_getQuality((DoublePointInformation)io);
        if (type == M_DP_TB_1)
            t = DoublePointWithCP56Time2a_getTimestamp((DoublePointWithCP56Time2a)io);
        break;
    case M_SP_NA_1:
    case M_SP_TB_1:
        r->sp = SinglePointInformation_getValue((SinglePointInformation)io);
        r->q = SinglePointInformation_getQuality((SinglePointInformation)io);
        if (type == M_SP_TB_1)
            t = SinglePointWithCP56Time2a_getTimestamp((SinglePointWithCP56Time2a)io);
        break;
    case M_ST_NA_1:
    case M_ST_TB_1:
        r->step = StepPositionInformation_getValue((StepPositionInformation)io);
        r->transient = StepPositionInformation_isTransient((StepPositionInformation)io);
        r->q = StepPositionInformation_getQuality((StepPositionInformation)io);
        if (type == M_ST_TB_1) t = StepPositionWithCP56Time2a_getTimestamp((StepPositionWithCP56Time2a)io);
        break;
    case M_ME_NC_1:
    case M_ME_TF_1:
        r->f = MeasuredValueShort_getValue((MeasuredValueShort)io);
        r->q = MeasuredValueShort_getQuality((MeasuredValueShort)io);
        if (type == M_ME_TF_1)
            t = MeasuredValueShortWithCP56Time2a_getTimestamp((MeasuredValueShortWithCP56Time2a)io);
        break;
    default:
        return false;
    }
    if (t) {
        r->timed = true;
        r->timeMs = CP56Time2a_toMsTimestamp(t);
        r->timeInvalid = CP56Time2a_isInvalid(t);
    }
    return true;
}

static bool
on_asdu(void* param, int address, CS101_ASDU asdu)
{
    (void)param;
    (void)address; /* CS104 carries no link address */
    rd_log_asdu("rx", asdu);

    TypeID type = CS101_ASDU_getTypeID(asdu);
    int cot = CS101_ASDU_getCOT(asdu);
    bool negative = CS101_ASDU_isNegative(asdu);
    if (CS101_ASDU_getCA(asdu) != cc.pm->ca) {
        rd_log("ignored ASDU for ca=%d (configured ca=%d)", CS101_ASDU_getCA(asdu), cc.pm->ca);
        return true;
    }

    if (type == C_IC_NA_1) {
        cc_on_gi_response(cot, negative);
        return true;
    }
    if (type == C_DC_NA_1 || type == C_RC_NA_1 || type == C_SC_NA_1) {
        InformationObject io = CS101_ASDU_getElement(asdu, 0);
        if (io) {
            int ioa = InformationObject_getObjectAddress(io);
            bool select = type == C_DC_NA_1 ? DoubleCommand_isSelect((DoubleCommand)io) :
                type == C_RC_NA_1 ? StepCommand_isSelect((StepCommand)io) : SingleCommand_isSelect((SingleCommand)io);
            InformationObject_destroy(io);
            cc_on_command_response(ioa, select, cot, negative);
        }
        return true;
    }

    int n = CS101_ASDU_getNumberOfElements(asdu);
    for (int i = 0; i < n; i++) {
        InformationObject io = CS101_ASDU_getElement(asdu, i);
        if (!io)
            continue;
        CcReport r;
        bool ok = decode_report(type, io, &r);
        InformationObject_destroy(io);
        if (!ok) {
            rd_log("ignored ASDU type=%s: not a monitored type", TypeID_toString(type));
            break;
        }
        cc_on_report(&r, cot);
    }
    return true;
}

/* ---- setup ----------------------------------------------------------- */

/* A connect from an address that is not ours would silently use another one; refuse it up front. */
static bool
check_local_address(const char* bindAddr, char* err, size_t errLen)
{
    struct sockaddr_in sa = {.sin_family = AF_INET, .sin_port = 0};
    if (inet_pton(AF_INET, bindAddr, &sa.sin_addr) != 1) {
        snprintf(err, errLen, "--bind %s is not an IPv4 address", bindAddr);
        return false;
    }
    int fd = socket(AF_INET, SOCK_STREAM | SOCK_CLOEXEC, 0);
    if (fd < 0) {
        snprintf(err, errLen, "socket: %s", strerror(errno));
        return false;
    }
    int rc = bind(fd, (struct sockaddr*)&sa, sizeof(sa));
    int saved = errno;
    close(fd);
    if (rc != 0) {
        snprintf(err, errLen, "--bind %s: %s", bindAddr, strerror(saved));
        return false;
    }
    return true;
}

bool
cc_link_init(const char* host, int port, const char* bindAddr, char* err, size_t errLen)
{
    if (!check_local_address(bindAddr, err, errLen))
        return false;

    con = CS104_Connection_create(host, port);
    if (!con) {
        snprintf(err, errLen, "cannot create the IEC 104 connection");
        return false;
    }
    CS104_Connection_setLocalAddress(con, bindAddr, 0);

    CS104_APCIParameters apci = CS104_Connection_getAPCIParameters(con);
    apci->k = cc.pm->timers.k;
    apci->w = cc.pm->timers.w;
    apci->t0 = cc.pm->timers.t0_s;
    apci->t1 = cc.pm->timers.t1_s;
    apci->t2 = cc.pm->timers.t2_s;
    apci->t3 = cc.pm->timers.t3_s;
    CS104_Connection_setAPCIParameters(con, apci);

    CS104_Connection_setConnectionHandler(con, on_connection_event, NULL);
    CS104_Connection_setASDUReceivedHandler(con, on_asdu, NULL);
    CS104_Connection_setRawMessageHandler(con, on_raw_message, NULL);
    return true;
}

/* ---- link thread ----------------------------------------------------- */

typedef bool (*LinkPredicate)(void);

static bool
attempt_settled(void)
{
    return cc.link.attemptOpened || cc.link.attemptEnded;
}

static bool
opened_alive(void)
{
    return cc.link.attemptOpened && !cc.link.attemptEnded;
}

static bool
started_or_ended(void)
{
    return cc.link.state == CC_LINK_STARTED || cc.link.attemptEnded;
}

static bool
started_alive(void)
{
    return cc.link.state == CC_LINK_STARTED && !cc.link.attemptEnded;
}

static bool
gi_settled(void)
{
    return cc.link.gi == CC_GI_COMPLETE || cc.link.giFailed || cc.link.attemptEnded;
}

static bool
ended(void)
{
    return cc.link.attemptEnded;
}

/* Evaluates pred under the lock. */
static bool
holds(LinkPredicate pred)
{
    pthread_mutex_lock(&cc.lock);
    bool v = pred();
    pthread_mutex_unlock(&cc.lock);
    return v;
}

/* Waits (lock taken here) until pred holds, timeoutMs passes or *stop is set; returns pred. */
static bool
wait_for(LinkPredicate pred, uint64_t timeoutMs, volatile sig_atomic_t* stop)
{
    uint64_t deadline = cc_mono_ms() + timeoutMs;
    pthread_mutex_lock(&cc.lock);
    while (!pred() && !*stop) {
        uint64_t now = cc_mono_ms();
        if (now >= deadline)
            break;
        uint64_t slice = now + 100; /* poll *stop, which a signal handler sets */
        cc_wait_until(slice < deadline ? slice : deadline);
    }
    bool ok = pred();
    pthread_mutex_unlock(&cc.lock);
    return ok;
}

/* Closes the session (joins lib60870's thread); reason is journaled when it was started. */
static void
close_session(const char* reason)
{
    if (reason) {
        pthread_mutex_lock(&cc.lock);
        if (!cc.link.attemptEnded) {
            cc.link.closeRequested = true;
            cc.link.closeReason = reason;
        }
        pthread_mutex_unlock(&cc.lock);
    }
    pthread_mutex_lock(&ioLock);
    CS104_Connection_close(con);
    pthread_mutex_unlock(&ioLock);
}

static void
sleep_interruptible(uint64_t ms, volatile sig_atomic_t* stop)
{
    uint64_t deadline = cc_mono_ms() + ms;
    while (!*stop && cc_mono_ms() < deadline)
        usleep(50 * 1000);
}

/* One connection attempt: connect, STARTDT, GI, then hold the session until it ends. */
static void
run_session(volatile sig_atomic_t* stop)
{
    const PointMap* pm = cc.pm;
    uint64_t t0Ms = (uint64_t)pm->timers.t0_s * 1000u;
    uint64_t t1Ms = (uint64_t)pm->timers.t1_s * 1000u;

    cc_link_begin_attempt();
    pthread_mutex_lock(&ioLock);
    CS104_Connection_connectAsync(con);
    pthread_mutex_unlock(&ioLock);

    /* lib60870's connect timeout is t0 */
    wait_for(attempt_settled, t0Ms + 1000, stop);
    if (*stop || !holds(opened_alive)) {
        close_session(NULL);
        return;
    }

    pthread_mutex_lock(&ioLock);
    CS104_Connection_sendStartDT(con);
    pthread_mutex_unlock(&ioLock);
    /* lib60870 closes the session itself if STARTDT_CON misses t1 */
    wait_for(started_or_ended, t1Ms + 1000, stop);
    if (*stop || !holds(started_alive)) {
        close_session(NULL);
        return;
    }

    cc_gi_begin();
    pthread_mutex_lock(&ioLock);
    bool sent = CS104_Connection_sendInterrogationCommand(con, CS101_COT_ACTIVATION, pm->ca, IEC60870_QOI_STATION);
    pthread_mutex_unlock(&ioLock);
    rd_log("sent station GI ca=%d", pm->ca);

    /* GI ACT-CON and ACT-TERM each within t1 */
    bool settled = sent && wait_for(gi_settled, 2 * t1Ms, stop);
    if (*stop)
        return;
    pthread_mutex_lock(&cc.lock);
    bool giOk = cc.link.gi == CC_GI_COMPLETE;
    bool gone = cc.link.attemptEnded;
    pthread_mutex_unlock(&cc.lock);
    if (!giOk && !gone) {
        rd_log("GI %s; closing the session", settled ? "rejected" : "not completed within 2*t1");
        close_session("io_error");
        return;
    }

    uint64_t refreshMs = 1000;
    for (int i = 0; i < pm->n_devices; i++) {
        uint64_t half = (uint64_t)pm->devices[i].stale_after_ms / 2;
        if (half < refreshMs) refreshMs = half;
    }
    uint64_t lastGi = cc_mono_ms();
    while (!*stop && !wait_for(ended, refreshMs, stop)) {
        if (cc_mono_ms() - lastGi >= refreshMs) {
            pthread_mutex_lock(&cc.lock);
            bool refresh = cc.link.gi == CC_GI_COMPLETE;
            bool failed = cc.link.giFailed || (cc.link.gi == CC_GI_RUNNING && cc_mono_ms() - lastGi >= 2 * t1Ms);
            pthread_mutex_unlock(&cc.lock);
            if (failed) {
                close_session("io_error");
                return;
            }
            if (refresh) {
                cc_gi_begin();
                pthread_mutex_lock(&ioLock);
                CS104_Connection_sendInterrogationCommand(con, CS101_COT_ACTIVATION, pm->ca, IEC60870_QOI_STATION);
                pthread_mutex_unlock(&ioLock);
                lastGi = cc_mono_ms();
            }
        }
    }
    if (!*stop)
        close_session(NULL);
}

void
cc_link_run(volatile sig_atomic_t* stop)
{
    while (!*stop) {
        run_session(stop);
        if (*stop)
            break;
        sleep_interruptible((uint64_t)cc.pm->reconnect_interval_ms, stop);
    }
}

void
cc_link_shutdown(void)
{
    pthread_mutex_lock(&cc.lock);
    cc.link.shuttingDown = true;
    pthread_mutex_unlock(&cc.lock);
    pthread_mutex_lock(&ioLock);
    CS104_Connection_destroy(con);
    con = NULL;
    pthread_mutex_unlock(&ioLock);
}

bool
cc_link_send_command(const PmCommand* cmd, int dcs, bool select, int connSeq)
{
    pthread_mutex_lock(&ioLock);
    pthread_mutex_lock(&cc.lock);
    bool ok = con && cc.link.state == CC_LINK_STARTED && cc.link.connectionSeq == connSeq &&
        cc_mono_ms() < cc.tracker.deadlineMono;
    pthread_mutex_unlock(&cc.lock);
    if (ok) {
        InformationObject io = cmd->kind == PM_KIND_DOUBLE_COMMAND ?
            (InformationObject)DoubleCommand_create(NULL, cmd->ioa, dcs, select, 0) :
            cmd->kind == PM_KIND_REGULATING_STEP ?
            (InformationObject)StepCommand_create(NULL, cmd->ioa, dcs, select, 0) :
            (InformationObject)SingleCommand_create(NULL, cmd->ioa, dcs != 0, select, 0);
        ok = CS104_Connection_sendProcessCommandEx(con, CS101_COT_ACTIVATION, cc.pm->ca, io);
        InformationObject_destroy(io);
    }
    pthread_mutex_unlock(&ioLock);
    return ok;
}
