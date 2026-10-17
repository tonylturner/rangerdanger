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
 *   failed     negative ACT-TERM (the acknowledged operation did not complete).
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
} Outcome;

/* Caller holds. Builds the result and journals it; releases the tracker. */
static cJSON*
finish(const char* id, int cmdIndex, const char* operation, const Outcome* o)
{
    CcTracker* t = &cc.tracker;
    cJSON* result = cJSON_CreateObject();
    cJSON_AddStringToObject(result, "id", id);
    cJSON_AddStringToObject(result, "point", cc.pm->commands[cmdIndex].id);
    cJSON_AddStringToObject(result, "operation", operation);
    cJSON_AddStringToObject(result, "result", o->status);
    if (o->error[0]) cJSON_AddStringToObject(result, "reason", o->error);
    else cJSON_AddNullToObject(result, "reason");
    add_stamp(result, "selected_at", t->selectedAt);
    add_stamp(result, "executed_at", t->executedAt);
    add_stamp(result, "con_at", t->executeAt);
    add_stamp(result, "term_at", t->terminatedAt);
    if (t->feedback) {
        cJSON* fb = cJSON_AddObjectToObject(result, "feedback");
        cJSON_AddNumberToObject(fb, "ioa", cc.pm->points[t->feedbackIndex].ioa);
        cJSON_AddItemToObject(fb, "value", t->feedbackValue);
        t->feedbackValue = NULL;
        add_stamp(fb, "at", t->feedbackAt);
    } else cJSON_AddNullToObject(result, "feedback");
    cc_journal_add("command", cJSON_Duplicate(result, true));
    cJSON_Delete(t->feedbackValue);
    memset(t, 0, sizeof(*t));
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
    if (!cc_link_send_command(c, dcs, true, connSeq)) {
        o->status = "unknown";
        snprintf(o->error, sizeof(o->error), "link lost before the select was sent");
        return;
    }
    uint64_t selectDeadline = cc_mono_ms() + t1Ms;
    w = wait_unlocked(select_answered, connSeq, selectDeadline < deadline ? selectDeadline : deadline);
    if (w == WAIT_LINK_LOST) {
        o->status = "unknown";
        snprintf(o->error, sizeof(o->error), "link lost before the execute was sent");
        return;
    }
    if (w == WAIT_TIMEOUT) {
        o->status = "unknown";
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

    pthread_mutex_lock(&cc.lock);
    cc_stamp_now(cc.tracker.executedAt);
    pthread_mutex_unlock(&cc.lock);
    rd_log("command execute ioa=%d dcs=%d(%s)", c->ioa, dcs, cc_dp_name(dcs));
    if (!cc_link_send_command(c, dcs, false, connSeq)) {
        o->status = "unknown";
        snprintf(o->error, sizeof(o->error), "link lost before the execute was sent");
        return;
    }
    w = wait_unlocked(execute_answered, connSeq, deadline);
    if (w != WAIT_DONE) {
        o->status = "unknown";
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
    bool termNegative = cc.tracker.termNegative;
    pthread_mutex_unlock(&cc.lock);
    const char* fbPoint = cc.pm->points[c->feedback_index].id;
    if (termNegative) {
        o->status = "failed";
        snprintf(o->error, sizeof(o->error), "negative ACT-TERM");
    } else if (!feedback) {
        o->status = "unknown";
        snprintf(o->error, sizeof(o->error), "no good matching COT 11 feedback for %s before ACT-TERM", fbPoint);
    } else {
        o->status = "completed";
    }
}

int
cc_command_run(int cmdIndex, int dcs, const char* operation, cJSON** body)
{
    const PmCommand* c = &cc.pm->commands[cmdIndex];
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
    const CcPoint* fb = &cc.points[c->feedback_index];
    if (!cc.link.giCompletedAt[0] || !fb->hasValue || fb->iv || fb->nt) {
        pthread_mutex_unlock(&cc.lock);
        *body = error_body("feedback is stale or invalid");
        return 409;
    }
    memset(&cc.tracker, 0, sizeof(cc.tracker));
    cc.tracker.deadlineMono = deadline;
    cc.tracker.expected = c->kind == PM_KIND_REGULATING_STEP ? fb->step + (dcs == 1 ? -1 : 1) : dcs;
    cc_stamp_now(cc.tracker.selectedAt);
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
    *body = finish(id, cmdIndex, operation, &o);
    pthread_mutex_unlock(&cc.lock);
    return 200;
}
