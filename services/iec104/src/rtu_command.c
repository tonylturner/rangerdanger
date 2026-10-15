/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 */

/* SBO validation, command execution and fresh-feedback completion. */
#include <stdio.h>
#include <string.h>

#include "common.h"
#include "rtu_internal.h"

/* ---- commands -------------------------------------------------------- */

void
rtu_clear_selection(const char* why)
{
    if (rtu.sel.active) {
        const PmCommand* c = &rtu.pm->commands[rtu.sel.cmdIndex];
        rd_log("command ioa=%d peer=%s phase=select result=cleared reason=\"%s\"", c->ioa, rtu.sel.peer, why);
    }
    rtu.sel = (Selection){0};
}

/* Caller holds rtu.lock. Validates SBO; a valid execute is queued, not confirmed yet. */
void
rtu_handle_command(IMasterConnection conn, const char* peerStr, CS101_ASDU asdu, int cmdIndex, int dcs, int qu, bool select)
{
    const PmCommand* c = &rtu.pm->commands[cmdIndex];
    CS101_CauseOfTransmission cot = CS101_ASDU_getCOT(asdu);
    uint64_t now = rtu_mono_ms();

    if (rtu.sel.active && now >= rtu.sel.deadlineMono)
        rtu_clear_selection("select timeout elapsed");

    bool ownsSelection = rtu.sel.active && strcmp(rtu.sel.peer, peerStr) == 0 && rtu.sel.cmdIndex == cmdIndex;

    if (cot == CS101_COT_DEACTIVATION) {
        if (ownsSelection) {
            rtu_clear_selection("deactivated by the selecting client");
            CS101_ASDU_setCOT(asdu, CS101_COT_DEACTIVATION_CON);
            IMasterConnection_sendASDU(conn, asdu);
        } else {
            rtu_reply_negative(conn, asdu, CS101_COT_DEACTIVATION_CON);
        }
        return;
    }

    if (qu != 0 || (c->kind != PM_KIND_SINGLE_COMMAND && dcs != 1 && dcs != 2)) {
        rd_log("command ioa=%d peer=%s phase=%s result=rejected reason=\"invalid state or qualifier\"", c->ioa, peerStr,
               select ? "select" : "execute");
        rtu_reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON);
        return;
    }

    if (select) {
        if (!c->sbo) {
            rd_log("command ioa=%d peer=%s phase=select result=rejected reason=\"not an SBO command\"", c->ioa,
                   peerStr);
            rtu_reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON);
            return;
        }
        if (rtu.pend.active) {
            rtu_reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON);
            return;
        }
        if (rtu.sel.active && !ownsSelection) {
            rd_log("command ioa=%d peer=%s phase=select result=rejected reason=\"selected by another peer\"", c->ioa,
                   peerStr);
            rtu_reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON);
            return;
        }
        rtu.sel.active = true;
        rtu.sel.cmdIndex = cmdIndex;
        snprintf(rtu.sel.peer, PEER_LEN, "%s", peerStr);
        rtu.sel.dcs = dcs;
        rtu.sel.qu = qu;
        rtu.sel.deadlineMono = now + (uint64_t)rtu.pm->select_timeout_ms;
        rtu.sel.pollSeq = rtu.devices[c->device_index].pollSeq;
        rtu.sel.selectedMono = now;
        rd_log("command ioa=%d peer=%s phase=select result=accepted dcs=%d", c->ioa, peerStr, dcs);
        IMasterConnection_sendACT_CON(conn, asdu, false);
        return;
    }

    /* execute */
    if (c->sbo && !ownsSelection) {
        rd_log("command ioa=%d peer=%s phase=execute result=rejected reason=\"%s\"", c->ioa, peerStr,
               rtu.sel.active ? "selected by another peer" : "execute without a valid selection");
        rtu_reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON);
        return;
    }
    if (c->sbo && (dcs != rtu.sel.dcs || qu != rtu.sel.qu)) {
        rtu_clear_selection("execute does not match the selection");
        rd_log("command ioa=%d peer=%s phase=execute result=rejected reason=\"value differs from selection\"", c->ioa,
               peerStr);
        rtu_reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON);
        return;
    }
    if (rtu.pend.active) {
        rtu_reply_negative(conn, asdu, CS101_COT_ACTIVATION_CON);
        return;
    }

    uint64_t afterPoll = rtu.sel.pollSeq, afterMono = rtu.sel.selectedMono;
    rtu_clear_selection("executed");
    rtu.pend.active = true;
    rtu.pend.cmdIndex = cmdIndex;
    snprintf(rtu.pend.peer, PEER_LEN, "%s", peerStr);
    rtu.pend.dcs = dcs;
    rtu.pend.phase = CMD_START;
    rtu.pend.afterPoll = afterPoll;
    rtu.pend.afterMono = afterMono;
    rtu.pend.deadline = now + (uint64_t)rtu.pm->devices[c->device_index].timeout_ms;
    rtu.pend.command = CS101_ASDU_clone(asdu, NULL);
    rd_log("command ioa=%d peer=%s phase=execute result=accepted dcs=%d", c->ioa, peerStr, dcs);
}

/* Caller holds. A single owner retains the slot until a terminal outcome. */
static void
finish_command(const char* result)
{
    PendingExecute* p = &rtu.pend;
    rd_log("command ioa=%d peer=%s phase=execute result=%s", rtu.pm->commands[p->cmdIndex].ioa, p->peer, result);
    CS101_ASDU_destroy(p->command);
    *p = (PendingExecute){0};
}

/* Main-loop state machine: never performs network I/O or waits for motion. */
void
rtu_advance_command(void)
{
    PendingExecute* p = &rtu.pend;
    if (!p->active) return;
    const PmCommand* c = &rtu.pm->commands[p->cmdIndex];
    const PmPoint* fb = &rtu.pm->points[c->feedback_index];
    const PmDevice* dev = &rtu.pm->devices[c->device_index];
    DeviceRT* drt = &rtu.devices[c->device_index];
    PointRT* value = &rtu.points[c->feedback_index];
    IMasterConnection conn = rtu_conn_for_peer(p->peer);
    bool fresh = drt->pollSeq > p->afterPoll && drt->lastPollStart > p->afterMono && drt->lastOkMono &&
        rtu_mono_ms() - drt->lastOkMono < (uint64_t)dev->stale_after_ms && rtu_point_quality(value, fb) == 0;
    if ((p->phase == CMD_START || p->phase == CMD_WRITE) && rtu_mono_ms() >= p->deadline) {
        if (conn) IMasterConnection_sendACT_CON(conn, p->command, true);
        finish_command("rejected");
    } else if (p->phase == CMD_START && fresh) {
        p->expected = c->kind == PM_KIND_REGULATING_STEP ? value->step + (p->dcs == 1 ? -1 : 1) : p->dcs;
        if (c->kind == PM_KIND_REGULATING_STEP && value->transient) {
            if (conn) IMasterConnection_sendACT_CON(conn, p->command, true);
            finish_command("rejected");
        } else p->phase = CMD_WRITE;
    } else if (p->phase == CMD_FEEDBACK) {
        /* Deadline wins over even freshly matching feedback received too late. */
        if (rtu_mono_ms() >= p->deadline) {
            if (conn) rtu_reply_negative(conn, p->command, CS101_COT_ACTIVATION_TERMINATION);
            finish_command("failed");
        } else if (fresh && (c->kind == PM_KIND_DOUBLE_COMMAND ? (int)value->dp == p->expected :
                   c->kind == PM_KIND_SINGLE_COMMAND ? (int)value->sp == p->expected :
                   value->step == p->expected && !value->transient)) {
            rtu_report_point(c->feedback_index, CS101_COT_RETURN_INFO_REMOTE);
            if (conn) IMasterConnection_sendACT_TERM(conn, p->command);
            finish_command("completed");
        }
    }
}

/* Only the target device's worker may take CMD_WRITE. Never retry an FC6. */
void
rtu_write_command(int d)
{
    pthread_mutex_lock(&rtu.lock);
    PendingExecute* p = &rtu.pend;
    if (!p->active || p->phase != CMD_WRITE || rtu.pm->commands[p->cmdIndex].device_index != d) {
        pthread_mutex_unlock(&rtu.lock);
        return;
    }
    if (rtu_mono_ms() >= p->deadline) {
        IMasterConnection conn = rtu_conn_for_peer(p->peer);
        if (conn) IMasterConnection_sendACT_CON(conn, p->command, true);
        finish_command("rejected");
        pthread_mutex_unlock(&rtu.lock);
        return;
    }
    p->phase = CMD_WRITING;
    const PmCommand* c = &rtu.pm->commands[p->cmdIndex];
    const PmDevice* dev = &rtu.pm->devices[d];
    DeviceRT* drt = &rtu.devices[d];
    int val = c->kind == PM_KIND_SINGLE_COMMAND ? (p->dcs ? 2 : 1) : p->dcs;
    pthread_mutex_unlock(&rtu.lock);

    int exc = 0;
    MbStatus st = mb_write_single(&drt->mb, c->address, (uint16_t)val, &exc, dev->timeout_ms);
    if (st != MB_OK) mb_close(&drt->mb);
    pthread_mutex_lock(&rtu.lock);
    IMasterConnection conn = rtu_conn_for_peer(p->peer);
    if (st == MB_OK) {
        if (conn) IMasterConnection_sendACT_CON(conn, p->command, false);
        p->phase = CMD_FEEDBACK;
        p->afterPoll = drt->pollSeq;
        p->afterMono = rtu_mono_ms();
        p->deadline = rtu_mono_ms() + (c->kind == PM_KIND_REGULATING_STEP ? 5000u : (uint64_t)dev->timeout_ms);
    } else {
        /* A lost echo is ambiguous even when the socket has closed. */
        if (st == MB_EXCEPTION && conn) IMasterConnection_sendACT_CON(conn, p->command, true);
        finish_command(st == MB_EXCEPTION ? "rejected" : "unknown");
    }
    pthread_mutex_unlock(&rtu.lock);
}
