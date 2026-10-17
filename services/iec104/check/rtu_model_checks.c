/* SPDX-License-Identifier: GPL-3.0-or-later */
/* Real reporting code/lib60870 encoders, injected monotonic clock and transport. */
#include <assert.h>
#include <stdio.h>
#include <string.h>
#include "rtu_internal.h"

RtuState rtu;
static PointMap pm;
static uint64_t now;
static int frames, objects, lastCot;
static struct sCS101_AppLayerParameters al = {1, 1, 2, 0, 2, 3, 249};

uint64_t __wrap_Hal_getMonotonicTimeInMs(void) { return now; }
bool sc_now(StationClock* clock, uint64_t* ms) { (void)clock; *ms = now; return true; }

bool
IMasterConnection_sendASDU(IMasterConnection conn, CS101_ASDU a)
{
    (void)conn;
    assert(!CS101_ASDU_isSequence(a));
    lastCot = CS101_ASDU_getCOT(a);
    assert(CS101_ASDU_getTypeID(a) == M_ME_NC_1);
    int n = CS101_ASDU_getNumberOfElements(a);
    assert(n > 0 && n <= 30);
    for (int i = 0; i < n; i++) {
        InformationObject io = CS101_ASDU_getElement(a, i);
        assert(io);
        assert(InformationObject_getObjectAddress(io) == objects + i + 1);
        InformationObject_destroy(io);
    }
    frames++;
    objects += n;
    return true;
}

static void clear_capture(void) { frames = objects = lastCot = 0; }

static void
init(int count)
{
    memset(&rtu, 0, sizeof(rtu));
    memset(&pm, 0, sizeof(pm));
    now = 100000;
    pm.ca = 1;
    pm.n_points = count;
    pm.n_devices = 1;
    pm.devices[0].stale_after_ms = 3000;
    rtu.pm = &pm;
    rtu.al = &al;
    rtu.nStarted = 1;
    rtu.started[0].conn = (IMasterConnection)(uintptr_t)1;
    for (int i = 0; i < count; i++) {
        pm.points[i].kind = PM_KIND_FLOAT;
        pm.points[i].ioa = i + 1;
        pm.points[i].cyclic_ms = 60000;
        pm.points[i].deadband = 0.5;
        pm.points[i].measurement = true;
        rtu.points[i].f = rtu.points[i].repF = 10;
        rtu.points[i].nextCyclic = now + 60000;
    }
    clear_capture();
}

static void
tick(uint64_t t)
{
    now = t;
    rtu.devices[0].lastOkMono = t;
    clear_capture();
    rtu_evaluate_and_report();
}

int
main(void)
{
    init(64);
    for (int i = 0; i < 64; i++) rtu.points[i].nextCyclic = now;
    rtu_report_cyclic(now);
    assert(frames == 3 && objects == 64 && lastCot == 1);
    clear_capture();
    rtu_report_gi(rtu.started[0].conn, 0);
    assert(frames == 3 && objects == 64 && lastCot == 20);
    puts("PASS packed cyclic and GI overflow split 64 sparse-capable objects into 30/30/4");

    init(1);
    rtu.points[0].f = 11;
    tick(100000);
    assert(frames == 1 && lastCot == 3);
    rtu.points[0].f = 12;
    tick(100100);
    assert(frames == 0);
    rtu.points[0].f = 11.2f; /* A held value has returned inside the deadband. */
    tick(101000);
    assert(frames == 0 && rtu.points[0].repF == 11);
    rtu.points[0].f = 12;
    tick(101001);
    assert(frames == 1 && rtu.points[0].repF == 12);
    puts("PASS 1000 ms value limiter and pending value re-evaluation at expiry");

    rtu.points[0].measurementInvalid = true;
    tick(101010);
    assert(frames == 1 && (rtu.points[0].repQuality & IEC60870_QUALITY_INVALID));
    rtu.points[0].measurementInvalid = false;
    tick(101020);
    assert(frames == 1 && rtu.points[0].repQuality == IEC60870_QUALITY_GOOD);
    puts("PASS both quality transitions bypass value rate limit");

    rtu.points[0].f = 12.2f;
    tick(101400);
    assert(frames == 0);
    rtu_report_gi(rtu.started[0].conn, 0);
    assert(rtu.points[0].repF == 12);
    rtu.points[0].f = 12.6f;
    tick(102002);
    assert(frames == 1);
    puts("PASS GI does not move spontaneous deadband baseline");

    rtu.points[0].f = 13.2f;
    tick(102010);
    assert(frames == 0);
    rtu.points[0].nextCyclic = 102020;
    tick(102020);
    assert(frames == 1 && lastCot == 1 && rtu.points[0].repF == 13.2f);
    tick(103010);
    assert(frames == 0);
    puts("PASS intervening cyclic report moves baseline and cancels held value event");
    puts("PASS 5 RTU model groups");
    return 0;
}
