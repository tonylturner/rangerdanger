/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 */
/* Reporting policy and packed process-image transmission. Callers hold rtu.lock. */
#include <math.h>
#include <string.h>
#include "common.h"
#include "rtu_internal.h"

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
            else if (now - rt->lastValueEvent >= 1000 && rt->f != rt->repF &&
                     fabs((double)rt->f - (double)rt->repF) >= p->deadband)
                report = true;
            break;
        default:
            break;
        }

        if (report) {
            rtu_report_point(i, CS101_COT_SPONTANEOUS);
            if (p->kind == PM_KIND_FLOAT && !qChanged)
                rt->lastValueEvent = now;
        }
    }
    rtu_report_cyclic(now);
}

static void
send_packed(CS101_ASDU a, IMasterConnection conn)
{
    if (conn) {
        rd_log_asdu("tx-gi", a);
        IMasterConnection_sendASDU(conn, a);
    } else
        broadcast(a);
}

/* Group by kind, not configuration order. SQ=0 also supports sparse IOAs. */
static void
report_image(IMasterConnection conn, int oa, bool cyclic, uint64_t now)
{
    const PmKind kinds[] = {PM_KIND_SINGLE_POINT, PM_KIND_DOUBLE_POINT, PM_KIND_STEP_POSITION, PM_KIND_FLOAT};
    for (size_t k = 0; k < sizeof(kinds) / sizeof(kinds[0]); k++) {
        CS101_ASDU a = NULL;
        for (int i = 0; i < rtu.pm->n_points; i++) {
            const PmPoint* p = &rtu.pm->points[i];
            PointRT* rt = &rtu.points[i];
            if (p->kind != kinds[k] || (cyclic && p->kind != PM_KIND_FLOAT))
                continue;
            if (cyclic) {
                if (!rt->nextCyclic)
                    rt->nextCyclic = now + (uint64_t)p->cyclic_ms;
                if (now < rt->nextCyclic)
                    continue;
            }
            QualityDescriptor q = rtu_point_quality(rt, p);
            InformationObject io = build_untimed(p, rt, q);
            CS101_CauseOfTransmission cot = cyclic ? CS101_COT_PERIODIC : CS101_COT_INTERROGATED_BY_STATION;
            if (!a)
                a = CS101_ASDU_create(rtu.al, false, cot, oa, rtu.pm->ca, false, false);
            if (!CS101_ASDU_addInformationObject(a, io)) {
                send_packed(a, conn);
                CS101_ASDU_destroy(a);
                a = CS101_ASDU_create(rtu.al, false, cot, oa, rtu.pm->ca, false, false);
                CS101_ASDU_addInformationObject(a, io);
            }
            InformationObject_destroy(io);
            if (cyclic) {
                rt->repF = rt->f;
                rt->repQuality = q;
                rt->nextCyclic = now + (uint64_t)p->cyclic_ms;
            }
        }
        if (a) {
            send_packed(a, conn);
            CS101_ASDU_destroy(a);
        }
    }
}

void
rtu_report_cyclic(uint64_t now)
{
    report_image(NULL, 0, true, now);
}

void
rtu_report_gi(IMasterConnection conn, int oa)
{
    report_image(conn, oa, false, 0);
}
