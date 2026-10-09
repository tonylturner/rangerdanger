/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * Status rules (contract §6):
 *   completed  select and execute confirmed, ACT-TERM received, and a COT 11
 *              feedback report of the feedback point after the execute
 *              ACT-CON carrying the commanded value.
 *   rejected   negative select or execute ACT-CON.
 *   failed     the execute was never sent: the link was lost first, or the
 *              select went unanswered (the RTU acted on nothing).
 *   unknown    the execute was sent and the outcome is ambiguous: no
 *              execute ACT-CON, no ACT-TERM, link lost after the execute,
 *              or feedback missing or not the commanded value.
 */
#include "cc_command.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cc_link.h"
#include "cc_state.h"
#include "common.h"

typedef enum { WAIT_DONE, WAIT_LINK_LOST, WAIT_TIMEOUT } WaitResult;

typedef bool (*TrackerDone)(const CcTracker*);

static bool
select_answered(const CcTracker* t)
{
    return t->selectCon != CC_CON_NONE;
}

static bool
execute_answered(const CcTracker* t)
{
    return t->executeCon != CC_CON_NONE;
}

static bool
terminated(const CcTracker* t)
{
    return t->terminated;
}

/* Caller holds. Waits until done, the link leaves connection connSeq, or the deadline. */
static WaitResult
wait_tracker(TrackerDone done, int connSeq, uint64_t deadline)
{
    for (;;) {
        if (done(&cc.tracker))
            return WAIT_DONE;
        if (cc.link.state != CC_LINK_STARTED || cc.link.connectionSeq != connSeq)
            return WAIT_LINK_LOST;
        if (cc_mono_ms() >= deadline)
            return WAIT_TIMEOUT;
        cc_wait_until(deadline);
    }
}

static WaitResult
wait_unlocked(TrackerDone done, int connSeq, uint64_t deadline)
{
    pthread_mutex_lock(&cc.lock);
    WaitResult w = wait_tracker(done, connSeq, deadline);
    pthread_mutex_unlock(&cc.lock);
    return w;
}

static cJSON*
error_body(const char* text)
{
    cJSON* o = cJSON_CreateObject();
    cJSON_AddStringToObject(o, "error", text);
    return o;
}

/* Phase status of one confirmation; timedOut wins only when nothing arrived. */
static void
add_phase_status(cJSON* o, CcCon con, bool timedOut)
{
    if (con == CC_CON_POSITIVE)
        cJSON_AddStringToObject(o, "status", "confirmed");
    else if (con == CC_CON_NEGATIVE)
        cJSON_AddStringToObject(o, "status", "negative");
    else if (timedOut)
        cJSON_AddStringToObject(o, "status", "timeout");
    else
        cJSON_AddNullToObject(o, "status");
}

static void
add_stamp(cJSON* o, const char* key, const char* stamp)
{
    if (stamp[0])
        cJSON_AddStringToObject(o, key, stamp);
    else
        cJSON_AddNullToObject(o, key);
}

typedef struct {
    const char* status;
    char error[160];
    bool selectTimedOut;
    bool executeTimedOut;
} Outcome;

/* Caller holds. Builds the result and journals it; releases the tracker. */
static cJSON*
finish(const char* id, int cmdIndex, bool closeBreaker, int dcs, const Outcome* o)
{
    const CcTracker* t = &cc.tracker;
    cJSON* sel = cJSON_CreateObject();
    add_phase_status(sel, t->selectCon, o->selectTimedOut);
    add_stamp(sel, "at", t->selectAt);

    cJSON* exe = cJSON_CreateObject();
    add_phase_status(exe, t->executeCon, o->executeTimedOut);
    add_stamp(exe, "at", t->executeAt);
    add_stamp(exe, "terminated_at", t->terminatedAt);

    cJSON* fb = cJSON_CreateObject();
    if (t->feedback) {
        cJSON_AddStringToObject(fb, "status", t->feedbackDp == dcs ? "matched" : "mismatched");
        cJSON_AddStringToObject(fb, "value", cc_dp_name(t->feedbackDp));
    } else {
        cJSON_AddStringToObject(fb, "status", "missing");
        cJSON_AddNullToObject(fb, "value");
    }
    add_stamp(fb, "received_at", t->feedbackAt);

    const char* point = cc.pm->commands[cmdIndex].id;
    const char* operation = closeBreaker ? "close" : "open";

    cJSON* detail = cJSON_CreateObject();
    cJSON_AddStringToObject(detail, "id", id);
    cJSON_AddStringToObject(detail, "point", point);
    cJSON_AddStringToObject(detail, "operation", operation);
    cJSON_AddStringToObject(detail, "status", o->status);
    cJSON_AddItemToObject(detail, "select", cJSON_Duplicate(sel, true));
    cJSON_AddItemToObject(detail, "execute", cJSON_Duplicate(exe, true));
    cJSON_AddItemToObject(detail, "feedback", cJSON_Duplicate(fb, true));
    cc_journal_add("command", detail);

    cJSON* result = cJSON_CreateObject();
    cJSON_AddStringToObject(result, "id", id);
    cJSON_AddStringToObject(result, "status", o->status);
    cJSON_AddItemToObject(result, "select", sel);
    cJSON_AddItemToObject(result, "execute", exe);
    cJSON_AddItemToObject(result, "feedback", fb);
    if (o->error[0])
        cJSON_AddStringToObject(result, "error", o->error);
    else
        cJSON_AddNullToObject(result, "error");

    rd_log("command %s %s %s: %s%s%s", id, point, operation, o->status, o->error[0] ? " - " : "", o->error);
    memset(&cc.tracker, 0, sizeof(cc.tracker));
    pthread_cond_broadcast(&cc.cond);
    return result;
}

/* Drives select, execute, termination and feedback; fills the outcome. */
static void
run_phases(const PmCommand* c, int dcs, int connSeq, uint64_t deadline, Outcome* o)
{
    uint64_t t1Ms = (uint64_t)cc.pm->timers.t1_s * 1000u;
    WaitResult w;

    rd_log("command select ioa=%d dcs=%d(%s)", c->ioa, dcs, cc_dp_name(dcs));
    if (!cc_link_send_double_command(c->ioa, dcs, true, connSeq)) {
        o->status = "failed";
        snprintf(o->error, sizeof(o->error), "link lost before the select was sent");
        return;
    }
    uint64_t selectDeadline = cc_mono_ms() + t1Ms;
    w = wait_unlocked(select_answered, connSeq, selectDeadline < deadline ? selectDeadline : deadline);
    if (w == WAIT_LINK_LOST) {
        o->status = "failed";
        snprintf(o->error, sizeof(o->error), "link lost before the execute was sent");
        return;
    }
    if (w == WAIT_TIMEOUT) {
        o->status = "failed";
        o->selectTimedOut = true;
        snprintf(o->error, sizeof(o->error), "no select confirmation; execute not sent");
        return;
    }

    pthread_mutex_lock(&cc.lock);
    bool selectOk = cc.tracker.selectCon == CC_CON_POSITIVE;
    if (selectOk)
        cc.tracker.phase = CC_PHASE_EXECUTE; /* before the send, so the ACT-CON cannot race it */
    pthread_mutex_unlock(&cc.lock);
    if (!selectOk) {
        o->status = "rejected";
        snprintf(o->error, sizeof(o->error), "select rejected (negative ACT-CON)");
        return;
    }

    rd_log("command execute ioa=%d dcs=%d(%s)", c->ioa, dcs, cc_dp_name(dcs));
    if (!cc_link_send_double_command(c->ioa, dcs, false, connSeq)) {
        o->status = "failed";
        snprintf(o->error, sizeof(o->error), "link lost before the execute was sent");
        return;
    }
    w = wait_unlocked(execute_answered, connSeq, deadline);
    if (w != WAIT_DONE) {
        o->status = "unknown";
        o->executeTimedOut = w == WAIT_TIMEOUT;
        snprintf(o->error, sizeof(o->error), "%s",
                 w == WAIT_TIMEOUT ? "no execute confirmation within the command budget"
                                   : "link lost after the execute was sent");
        return;
    }

    pthread_mutex_lock(&cc.lock);
    bool executeOk = cc.tracker.executeCon == CC_CON_POSITIVE;
    pthread_mutex_unlock(&cc.lock);
    if (!executeOk) {
        o->status = "rejected";
        snprintf(o->error, sizeof(o->error), "execute rejected (negative ACT-CON)");
        return;
    }

    /* the RTU sends the COT 11 feedback before ACT-TERM on the same stream */
    w = wait_unlocked(terminated, connSeq, deadline);
    if (w != WAIT_DONE) {
        o->status = "unknown";
        snprintf(o->error, sizeof(o->error), "%s",
                 w == WAIT_TIMEOUT ? "no ACT-TERM within the command budget" : "link lost before ACT-TERM");
        return;
    }

    pthread_mutex_lock(&cc.lock);
    bool feedback = cc.tracker.feedback;
    int feedbackDp = cc.tracker.feedbackDp;
    pthread_mutex_unlock(&cc.lock);
    const char* fbPoint = cc.pm->points[c->feedback_index].id;
    if (!feedback) {
        o->status = "unknown";
        snprintf(o->error, sizeof(o->error), "no COT 11 feedback for %s before ACT-TERM", fbPoint);
    } else if (feedbackDp != dcs) {
        o->status = "unknown";
        snprintf(o->error, sizeof(o->error), "feedback %s is %s, commanded %s", fbPoint, cc_dp_name(feedbackDp),
                 cc_dp_name(dcs));
    } else {
        o->status = "completed";
    }
}

int
cc_command_run(int cmdIndex, bool closeBreaker, cJSON** body)
{
    const PmCommand* c = &cc.pm->commands[cmdIndex];
    int dcs = closeBreaker ? IEC60870_DOUBLE_POINT_ON : IEC60870_DOUBLE_POINT_OFF;
    uint64_t deadline = cc_mono_ms() + CC_COMMAND_BUDGET_MS;

    pthread_mutex_lock(&cc.lock);
    if (cc.tracker.active) {
        pthread_mutex_unlock(&cc.lock);
        *body = error_body("a command is in flight");
        return 409;
    }
    if (cc.link.state != CC_LINK_STARTED) {
        pthread_mutex_unlock(&cc.lock);
        *body = error_body("link not started");
        return 503;
    }
    memset(&cc.tracker, 0, sizeof(cc.tracker));
    cc.tracker.active = true;
    cc.tracker.ioa = c->ioa;
    cc.tracker.feedbackIndex = c->feedback_index;
    cc.tracker.connSeq = cc.link.connectionSeq;
    cc.tracker.phase = CC_PHASE_SELECT;
    int connSeq = cc.tracker.connSeq;
    char id[32];
    snprintf(id, sizeof(id), "cmd-%llu", (unsigned long long)++cc.commandSeq);
    pthread_mutex_unlock(&cc.lock);

    Outcome o = {0};
    run_phases(c, dcs, connSeq, deadline, &o);

    pthread_mutex_lock(&cc.lock);
    *body = finish(id, cmdIndex, closeBreaker, dcs, &o);
    pthread_mutex_unlock(&cc.lock);
    return 200;
}
