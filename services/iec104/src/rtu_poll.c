/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 */

/* Independent southbound Modbus poll workers and register decoding. */
#include <math.h>

#include "hal_thread.h"
#include "rtu_internal.h"

/* ---- Modbus polling -------------------------------------------------- */

/* Extract one point's value from a register snapshot. */
static void
apply_point(int idx, const uint16_t* regs)
{
    const PmPoint* p = &rtu.pm->points[idx];
    PointRT* rt = &rtu.points[idx];
    /* The loader proves the entire source is in this block. */
    int addr = p->address - rtu.pm->devices[p->device_index].poll_start;
    rt->valueInvalid = false;
    switch (p->format) {
    case PM_FMT_DP: rt->dp = (DoublePointValue)(regs[addr] & 3); break;
    case PM_FMT_BIT: rt->sp = ((regs[addr] >> p->bit) & 1) != 0; break;
    case PM_FMT_F32:
        rt->f = mb_regs_to_f32(regs[addr], regs[addr+1]);
        rt->valueInvalid = !isfinite(rt->f);
        if (rt->valueInvalid) rt->f = 0.0f;
        break;
    case PM_FMT_U16: rt->f = (float)(regs[addr] * p->scale); break;
    case PM_FMT_I16: rt->f = (float)((int16_t)regs[addr] * p->scale); break;
    case PM_FMT_STEP: {
        int value = (int16_t)regs[addr];
        rt->valueInvalid = value < -64 || value > 63;
        if (!rt->valueInvalid) rt->step = value;
        rt->transient = ((regs[p->transient_address - rtu.pm->devices[p->device_index].poll_start] >>
                          p->transient_bit) & 1) != 0;
        break;
    }
    }
    if (p->kind == PM_KIND_FLOAT && !isfinite(rt->f)) {
        rt->valueInvalid = true;
        rt->f = 0.0f;
    }
}

/*
 * Reads one device's configured block (FC3/FC4) and applies it to that device's
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

    uint64_t started = rtu_mono_ms();
    MbStatus st = (dev->poll_table == PM_TABLE_INPUT ? mb_read_input : mb_read_holding)(
        &drt->mb, dev->poll_start, dev->poll_count, regs, &exc, dev->timeout_ms);
    if (st != MB_OK) {
        if (st == MB_TIMEOUT || st == MB_IO_ERROR) mb_close(&drt->mb);
        return st;
    }

    pthread_mutex_lock(&rtu.lock);
    drt->lastOkMono = rtu_mono_ms();
    drt->pollSeq++;
    drt->lastPollStart = started;
    if (dev->has_heartbeat) {
        uint16_t hb = regs[dev->heartbeat_address - dev->poll_start];
        if (drt->heartbeatChanged == 0 || hb != drt->heartbeat) {
            drt->heartbeat = hb;
            drt->heartbeatChanged = drt->lastOkMono;
        }
    }
    bool mvValid = true;
    if (dev->has_measurements_valid)
        mvValid = ((regs[dev->mv_address - dev->poll_start] >> dev->mv_bit) & 1) != 0;

    for (int i = 0; i < rtu.pm->n_points; i++) {
        if (rtu.pm->points[i].device_index != devIdx)
            continue;
        apply_point(i, regs);
        if (rtu.pm->points[i].measurement)
            rtu.points[i].measurementInvalid = !mvValid;
    }
    pthread_mutex_unlock(&rtu.lock);
    return st;
}

/* Independent I/O owners: even a DROPped TCP connect/read cannot starve a peer.
 * Connects have a 250 ms total socket budget; failed connections back off. */
void*
rtu_device_worker(void* arg)
{
    DeviceRT* drt = arg;
    int d = drt->index;
    const PmDevice* dev = &rtu.pm->devices[d];
    uint64_t nextPoll = 0, nextConnect = 0;
    int initialBackoff = rtu.pm->reconnect_interval_ms < 250 ? rtu.pm->reconnect_interval_ms : 250;
    int backoff = initialBackoff;
    while (!rtu_stop_requested) {
        uint64_t now = rtu_mono_ms();
        if (!mb_is_connected(&drt->mb) && now >= nextConnect) {
            MbStatus st = MB_OK;
            if (!drt->mb.addresses) st = mb_resolve(&drt->mb, dev->host, dev->port);
            if (st == MB_OK) st = mb_connect(&drt->mb, 250);
            if (st == MB_OK) backoff = initialBackoff;
            else {
                nextConnect = rtu_mono_ms() + (uint64_t)backoff;
                backoff = backoff < rtu.pm->reconnect_interval_ms / 2 ? backoff * 2 : rtu.pm->reconnect_interval_ms;
            }
        }
        if (mb_is_connected(&drt->mb)) {
            rtu_write_command(d);
            if (rtu_mono_ms() >= nextPoll) {
                uint64_t start = rtu_mono_ms();
                if (poll_device(d) != MB_OK) nextConnect = rtu_mono_ms() + (uint64_t)backoff;
                nextPoll = start + (uint64_t)dev->poll_ms;
            }
        }
        Thread_sleep(10);
    }
    mb_destroy(&drt->mb);
    return NULL;
}
