/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 */
#include "cc_state.h"
#include "cc_historian.h"

#include <errno.h>
#include <fcntl.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#include "common.h"

CcState cc;

/* ---- time ------------------------------------------------------------ */

uint64_t
cc_mono_ms(void)
{
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000u + (uint64_t)ts.tv_nsec / 1000000u;
}

/* RFC3339Nano UTC: fractional digits trimmed of trailing zeros, as Go prints it. */
static void
format_stamp(time_t secs, long nsec, char out[CC_STAMP_LEN])
{
    struct tm tm;
    char base[32];
    gmtime_r(&secs, &tm);
    strftime(base, sizeof(base), "%Y-%m-%dT%H:%M:%S", &tm);

    if (nsec <= 0) {
        snprintf(out, CC_STAMP_LEN, "%sZ", base);
        return;
    }
    char frac[16];
    snprintf(frac, sizeof(frac), "%09ld", nsec);
    for (int i = 8; i > 0 && frac[i] == '0'; i--)
        frac[i] = '\0';
    snprintf(out, CC_STAMP_LEN, "%s.%sZ", base, frac);
}

void
cc_stamp_now(char out[CC_STAMP_LEN])
{
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    format_stamp(ts.tv_sec, ts.tv_nsec, out);
}

static void
stamp_from_ms(uint64_t ms, char out[CC_STAMP_LEN])
{
    format_stamp((time_t)(ms / 1000), (long)(ms % 1000) * 1000000L, out);
}

bool
cc_wait_until(uint64_t deadlineMono)
{
    if (cc_mono_ms() >= deadlineMono)
        return false;
    struct timespec ts = {.tv_sec = (time_t)(deadlineMono / 1000), .tv_nsec = (long)(deadlineMono % 1000) * 1000000L};
    pthread_cond_timedwait(&cc.cond, &cc.lock, &ts);
    return cc_mono_ms() < deadlineMono;
}

/* ---- init ------------------------------------------------------------ */

static bool
random_epoch(char out[CC_EPOCH_LEN])
{
    unsigned char b[8];
    int fd = open("/dev/urandom", O_RDONLY | O_CLOEXEC);
    if (fd < 0)
        return false;
    ssize_t n = read(fd, b, sizeof(b));
    close(fd);
    if (n != (ssize_t)sizeof(b))
        return false;
    for (size_t i = 0; i < sizeof(b); i++)
        snprintf(out + 2 * i, 3, "%02x", b[i]);
    return true;
}

bool
cc_state_init(const PointMap* pm, const char* peer, char* err, size_t errLen)
{
    memset(&cc, 0, sizeof(cc));
    if (!random_epoch(cc.epoch)) {
        snprintf(err, errLen, "cannot read /dev/urandom for the cache epoch: %s", strerror(errno));
        return false;
    }

    pthread_mutex_init(&cc.lock, NULL);
    pthread_condattr_t attr;
    pthread_condattr_init(&attr);
    pthread_condattr_setclock(&attr, CLOCK_MONOTONIC);
    pthread_cond_init(&cc.cond, &attr);
    pthread_condattr_destroy(&attr);

    cc.pm = pm;
    snprintf(cc.peer, sizeof(cc.peer), "%s", peer);
    for (int i = 0; i < pm->n_points; i++) {
        CcPoint* p = &cc.points[i];
        p->iv = true;
        p->nt = true;
        p->stale = CC_STALE_NEVER;
        p->cot = -1;
    }
    cc.link.state = CC_LINK_DISCONNECTED;
    cc_stamp_now(cc.link.since);
    cc.journalNext = 1;
    return true;
}

/* ---- names and JSON helpers ----------------------------------------- */

const char*
cc_dp_name(int dp)
{
    switch (dp) {
    case IEC60870_DOUBLE_POINT_INTERMEDIATE:
        return "intermediate";
    case IEC60870_DOUBLE_POINT_OFF:
        return "open";
    case IEC60870_DOUBLE_POINT_ON:
        return "closed";
    default:
        return "indeterminate";
    }
}

static const char*
stale_name(CcStale s)
{
    switch (s) {
    case CC_STALE_HEARTBEAT: return "heartbeat";
    case CC_STALE_NEVER:
        return "never_received";
    case CC_STALE_LINK:
        return "link_lost";
    case CC_STALE_DEVICE:
        return "device_unreachable";
    case CC_STALE_MEASUREMENT:
        return "measurement_invalid";
    default:
        return NULL;
    }
}

static const char*
link_name(CcLinkState s)
{
    switch (s) {
    case CC_LINK_CONNECTING:
        return "connecting";
    case CC_LINK_STARTED:
        return "started";
    default:
        return "disconnected";
    }
}

static const char*
gi_name(CcGiState s)
{
    switch (s) {
    case CC_GI_RUNNING:
        return "running";
    case CC_GI_COMPLETE:
        return "complete";
    default:
        return "none";
    }
}

/* Adds a string member, or null when s is NULL or empty. */
static void
add_str_or_null(cJSON* obj, const char* key, const char* s)
{
    if (s && s[0])
        cJSON_AddStringToObject(obj, key, s);
    else
        cJSON_AddNullToObject(obj, key);
}

/* Shortest decimal that reads back as the same float, so 28.9f prints as 28.9. */
static cJSON*
float_json(float f)
{
    if (!isfinite(f))
        return cJSON_CreateNull();
    char buf[32];
    for (int prec = 6; prec <= 9; prec++) {
        snprintf(buf, sizeof(buf), "%.*g", prec, (double)f);
        if (strtof(buf, NULL) == f)
            break;
    }
    return cJSON_CreateRaw(buf);
}

cJSON*
cc_point_value_json(int index)
{
    const CcPoint* p = &cc.points[index];
    if (!p->hasValue)
        return cJSON_CreateNull();
    switch (cc.pm->points[index].kind) {
    case PM_KIND_DOUBLE_POINT:
        return cJSON_CreateString(cc_dp_name(p->dp));
    case PM_KIND_SINGLE_POINT:
        return cJSON_CreateBool(p->sp);
    case PM_KIND_STEP_POSITION: {
        cJSON* v = cJSON_CreateObject();
        cJSON_AddNumberToObject(v, "value", p->step);
        cJSON_AddBoolToObject(v, "transient", p->transient);
        return v;
    }
    case PM_KIND_FLOAT:
        return float_json(p->f);
    default:
        return cJSON_CreateNull();
    }
}

/* ---- journal --------------------------------------------------------- */

void
cc_journal_add(const char* kind, cJSON* detail)
{
    uint64_t seq = cc.journalNext++;
    CcJournalEntry* e = &cc.journal[seq % CC_JOURNAL_SIZE];
    if (e->detail)
        cJSON_Delete(e->detail);
    e->seq = seq;
    e->kind = kind;
    e->detail = detail;
    cc_stamp_now(e->at);

    char* text = cJSON_PrintUnformatted(detail);
    rd_log("journal seq=%llu kind=%s detail=%s", (unsigned long long)seq, kind, text ? text : "?");
    free(text);
}

static void
journal_point(int index)
{
    const CcPoint* p = &cc.points[index];
    cJSON* d = cJSON_CreateObject();
    cJSON_AddStringToObject(d, "point", cc.pm->points[index].id);
    if (p->stale == CC_STALE_NONE) {
        cJSON_AddItemToObject(d, "value", cc_point_value_json(index));
        cc_journal_add("point_good", d);
    } else {
        cJSON_AddStringToObject(d, "stale_reason", stale_name(p->stale));
        cJSON_AddItemToObject(d, "value", cc_point_value_json(index));
        cc_journal_add("point_stale", d);
    }
}

static cJSON*
link_detail(void)
{
    cJSON* d = cJSON_CreateObject();
    cJSON_AddStringToObject(d, "peer", cc.peer);
    cJSON_AddNumberToObject(d, "connection_seq", cc.link.connectionSeq);
    return d;
}

static void
journal_link_down(const char* reason)
{
    cJSON* d = link_detail();
    cJSON_AddStringToObject(d, "reason", reason);
    cc_journal_add("link_down", d);
    cc.link.downJournaled = true;
}

/* ---- receive-thread events ------------------------------------------ */

void
cc_on_frame_received(void)
{
    pthread_mutex_lock(&cc.lock);
    cc.link.lastRxMono = cc_mono_ms();
    cc_stamp_now(cc.link.lastRxAt);
    pthread_mutex_unlock(&cc.lock);
}

static bool
kind_accepts(PmKind kind, TypeID type)
{
    switch (kind) {
    case PM_KIND_DOUBLE_POINT:
        return type == M_DP_NA_1 || type == M_DP_TB_1;
    case PM_KIND_SINGLE_POINT:
        return type == M_SP_NA_1 || type == M_SP_TB_1;
    case PM_KIND_STEP_POSITION: return type == M_ST_NA_1 || type == M_ST_TB_1;
    case PM_KIND_FLOAT:
        return type == M_ME_NC_1 || type == M_ME_TF_1;
    default:
        return false;
    }
}

/* Caller holds. Records COT 11 feedback for the in-flight command after its execute ACT-CON. */
static void
track_feedback(int index, int cot)
{
    CcTracker* t = &cc.tracker;
    if (!t->active || cc_mono_ms() >= t->deadlineMono || t->phase != CC_PHASE_EXECUTE || t->executeCon != CC_CON_POSITIVE || t->feedback ||
        index != t->feedbackIndex || cot != CS101_COT_RETURN_INFO_REMOTE)
        return;
    const CcPoint* p = &cc.points[index];
    if (p->iv || p->nt || p->sb || p->bl || p->ov || p->transient) return;
    int actual = cc.pm->points[index].kind == PM_KIND_DOUBLE_POINT ? p->dp :
                 cc.pm->points[index].kind == PM_KIND_SINGLE_POINT ? (int)p->sp : p->step;
    if (actual != t->expected) return;
    t->feedback = true;
    t->feedbackValue = cc_point_value_json(index);
    snprintf(t->feedbackAt, CC_STAMP_LEN, "%s", cc.points[index].receivedAt);
}

void
cc_on_report(const CcReport* r, int cot)
{
    pthread_mutex_lock(&cc.lock);
    const PmPoint* pp = pointmap_point_by_ioa(cc.pm, r->ioa);
    if (!pp || !kind_accepts(pp->kind, r->type)) {
        pthread_mutex_unlock(&cc.lock);
        rd_log("ignored report ioa=%d type=%s: not a monitored point of that type", r->ioa, TypeID_toString(r->type));
        return;
    }
    int index = (int)(pp - cc.pm->points);
    CcPoint* p = &cc.points[index];
    CcStale before = p->stale;

    p->hasValue = true;
    p->dp = r->dp;
    p->sp = r->sp;
    p->f = r->f;
    p->step = r->step;
    p->transient = r->transient;
    p->iv = (r->q & IEC60870_QUALITY_INVALID) != 0;
    p->nt = (r->q & IEC60870_QUALITY_NON_TOPICAL) != 0;
    p->sb = (r->q & IEC60870_QUALITY_SUBSTITUTED) != 0;
    p->bl = (r->q & IEC60870_QUALITY_BLOCKED) != 0;
    p->ov = (r->q & IEC60870_QUALITY_OVERFLOW) != 0;
    if (r->timed) {
        stamp_from_ms(r->timeMs, p->sourceTime);
        p->sourceTimeInvalid = r->timeInvalid;
    } else {
        p->sourceTime[0] = '\0';
        p->sourceTimeInvalid = false;
    }
    cc_stamp_now(p->receivedAt);
    p->receivedMono = cc_mono_ms();
    p->cot = cot;

    /* The RTU encodes the cause: IV+NT = device unreachable, IV only = measurement invalid. */
    p->stale = p->iv && !p->nt && cc.pm->devices[pp->device_index].has_heartbeat ? CC_STALE_HEARTBEAT : p->nt ? CC_STALE_DEVICE : (p->iv ? CC_STALE_MEASUREMENT : CC_STALE_NONE);
    if (p->stale != before)
        journal_point(index);

    track_feedback(index, cot);
    if (r->timed) cc_historian_on_timed_report();
    pthread_cond_broadcast(&cc.cond);
    pthread_mutex_unlock(&cc.lock);
}

static int
points_good(void)
{
    int n = 0;
    for (int i = 0; i < cc.pm->n_points; i++)
        if (cc.points[i].stale == CC_STALE_NONE)
            n++;
    return n;
}

void
cc_on_gi_response(int cot, bool negative)
{
    pthread_mutex_lock(&cc.lock);
    if (cc.link.gi != CC_GI_RUNNING) {
        pthread_mutex_unlock(&cc.lock);
        rd_log("ignored GI response cot=%d: no GI running", cot);
        return;
    }
    if (cot == CS101_COT_ACTIVATION_TERMINATION && !negative) {
        cc.link.gi = CC_GI_COMPLETE;
        cc_stamp_now(cc.link.giCompletedAt);
        cJSON* d = cJSON_CreateObject();
        cJSON_AddNumberToObject(d, "connection_seq", cc.link.connectionSeq);
        cJSON_AddNumberToObject(d, "points_good", points_good());
        cc_journal_add("gi_complete", d);
    } else if (cot != CS101_COT_ACTIVATION_CON || negative) {
        cc.link.giFailed = true;
    }
    pthread_cond_broadcast(&cc.cond);
    pthread_mutex_unlock(&cc.lock);
}

void
cc_on_command_response(int ioa, bool select, int cot, bool negative)
{
    pthread_mutex_lock(&cc.lock);
    CcTracker* t = &cc.tracker;
    bool used = false;
    if (t->active && cc_mono_ms() < t->deadlineMono && ioa == t->ioa) {
        if (cot == CS101_COT_ACTIVATION_TERMINATION) {
            if (!select && t->phase == CC_PHASE_EXECUTE && t->executeCon == CC_CON_POSITIVE && !t->terminated) {
                t->terminated = true;
                t->termNegative = negative;
                cc_stamp_now(t->terminatedAt);
                used = true;
            }
        } else {
            /* ACT-CON, or a COT 44-47 rejection which is negative by definition */
            CcCon con = (negative || cot != CS101_COT_ACTIVATION_CON) ? CC_CON_NEGATIVE : CC_CON_POSITIVE;
            if (select && t->phase == CC_PHASE_SELECT && t->selectCon == CC_CON_NONE) {
                t->selectCon = con;
                cc_stamp_now(t->selectAt);
                used = true;
            } else if (!select && t->phase == CC_PHASE_EXECUTE && t->executeCon == CC_CON_NONE) {
                t->executeCon = con;
                cc_stamp_now(t->executeAt);
                used = true;
            }
        }
    }
    pthread_cond_broadcast(&cc.cond);
    pthread_mutex_unlock(&cc.lock);
    if (!used)
        rd_log("ignored command response ioa=%d cot=%d se=%s: no matching command phase", ioa, cot,
               select ? "select" : "execute");
}

void
cc_on_opened(void)
{
    pthread_mutex_lock(&cc.lock);
    cc.link.attemptOpened = true;
    cc.link.connectionSeq++;
    pthread_cond_broadcast(&cc.cond);
    pthread_mutex_unlock(&cc.lock);
}

void
cc_on_started(void)
{
    pthread_mutex_lock(&cc.lock);
    cc.link.state = CC_LINK_STARTED;
    cc_stamp_now(cc.link.since);
    cc.link.downJournaled = false;
    cc_journal_add("link_up", link_detail());
    pthread_cond_broadcast(&cc.cond);
    pthread_mutex_unlock(&cc.lock);
}

/* Caller holds. Every point keeps value and times and goes IV+NT; received points become link_lost. */
static void
mark_link_lost(void)
{
    for (int i = 0; i < cc.pm->n_points; i++) {
        CcPoint* p = &cc.points[i];
        p->iv = true;
        p->nt = true;
        if (p->hasValue && p->stale != CC_STALE_LINK) {
            p->stale = CC_STALE_LINK;
            journal_point(i);
        }
    }
}

void
cc_on_ended(void)
{
    pthread_mutex_lock(&cc.lock);
    cc.link.attemptEnded = true;
    if (!cc.link.shuttingDown) {
        if (cc.link.state == CC_LINK_STARTED) {
            const char* reason;
            uint64_t t1Ms = (uint64_t)cc.pm->timers.t1_s * 1000u;
            if (cc.link.closeRequested)
                reason = cc.link.closeReason;
            else if (cc.link.lastRxMono == 0 || cc_mono_ms() - cc.link.lastRxMono >= t1Ms)
                reason = "t1_timeout"; /* silent for t1: lib60870 closed on an unconfirmed frame */
            else
                reason = "peer_closed";
            journal_link_down(reason);
            mark_link_lost();
        } else if (!cc.link.downJournaled) {
            journal_link_down("connect_failed");
        }
    }
    cc.link.state = CC_LINK_DISCONNECTED;
    cc_stamp_now(cc.link.since);
    cc.link.gi = CC_GI_NONE;
    cc.link.giStartedAt[0] = '\0';
    cc.link.giCompletedAt[0] = '\0';
    pthread_cond_broadcast(&cc.cond);
    pthread_mutex_unlock(&cc.lock);
}

/* ---- link-thread transitions ---------------------------------------- */

void
cc_link_begin_attempt(void)
{
    pthread_mutex_lock(&cc.lock);
    cc.link.state = CC_LINK_CONNECTING;
    cc_stamp_now(cc.link.since);
    cc.link.attemptOpened = false;
    cc.link.attemptEnded = false;
    cc.link.closeRequested = false;
    cc.link.closeReason = NULL;
    cc.link.giFailed = false;
    pthread_mutex_unlock(&cc.lock);
}

void
cc_gi_begin(void)
{
    pthread_mutex_lock(&cc.lock);
    cc.link.gi = CC_GI_RUNNING;
    cc.link.giFailed = false;
    cc_stamp_now(cc.link.giStartedAt);
    cc.link.giCompletedAt[0] = '\0';
    cJSON* d = cJSON_CreateObject();
    cJSON_AddNumberToObject(d, "connection_seq", cc.link.connectionSeq);
    cc_journal_add("gi_started", d);
    pthread_mutex_unlock(&cc.lock);
}

/* ---- renderers ------------------------------------------------------- */

cJSON*
cc_render_health(void)
{
    cJSON* o = cJSON_CreateObject();
    cJSON_AddStringToObject(o, "status", "ok");
    pthread_mutex_lock(&cc.lock);
    cJSON_AddStringToObject(o, "link", link_name(cc.link.state));
    cJSON* h = cJSON_AddObjectToObject(o, "historian");
    add_str_or_null(h, "last_ok_at", cc.historianLastOk);
    cJSON_AddNumberToObject(h, "failures", (double)cc.historianFailures);
    cJSON_AddNumberToObject(h, "dropped_snapshots", (double)cc.historianDropped);
    pthread_mutex_unlock(&cc.lock);
    return o;
}

static cJSON*
point_json(int index)
{
    const PmPoint* pp = &cc.pm->points[index];
    const CcPoint* p = &cc.points[index];
    cJSON* o = cJSON_CreateObject();
    cJSON_AddStringToObject(o, "id", pp->id);
    cJSON_AddNumberToObject(o, "ioa", pp->ioa);
    cJSON_AddStringToObject(o, "asdu", pp->asdu);
    cJSON_AddItemToObject(o, "value", cc_point_value_json(index));

    cJSON* q = cJSON_AddObjectToObject(o, "quality");
    cJSON_AddBoolToObject(q, "iv", p->iv);
    cJSON_AddBoolToObject(q, "nt", p->nt);
    cJSON_AddBoolToObject(q, "sb", p->sb);
    cJSON_AddBoolToObject(q, "bl", p->bl);
    if (pp->kind == PM_KIND_FLOAT || pp->kind == PM_KIND_STEP_POSITION)
        cJSON_AddBoolToObject(q, "ov", p->ov);
    else
        cJSON_AddNullToObject(q, "ov");

    add_str_or_null(o, "stale_reason", stale_name(p->stale));
    add_str_or_null(o, "source_time", p->sourceTime);
    if (p->sourceTime[0])
        cJSON_AddBoolToObject(o, "source_time_invalid", p->sourceTimeInvalid);
    else
        cJSON_AddNullToObject(o, "source_time_invalid");
    add_str_or_null(o, "received_at", p->receivedAt);
    if (p->cot >= 0)
        cJSON_AddNumberToObject(o, "cot", p->cot);
    else
        cJSON_AddNullToObject(o, "cot");
    return o;
}

cJSON*
cc_render_state(void)
{
    pthread_mutex_lock(&cc.lock);
    cJSON* o = cc_render_state_locked();
    pthread_mutex_unlock(&cc.lock);
    return o;
}

cJSON*
cc_render_state_locked(void)
{
    cJSON* o = cJSON_CreateObject();
    cJSON_AddNumberToObject(o, "schema", 2);
    char seq[32];
    snprintf(seq, sizeof(seq), "%llu", (unsigned long long)++cc.snapshotSeq);
    cJSON_AddRawToObject(o, "snapshot_seq", seq);
    char renderedAt[CC_STAMP_LEN];
    cc_stamp_now(renderedAt);
    cJSON_AddStringToObject(o, "rendered_at", renderedAt);
    cJSON_AddStringToObject(o, "cache_epoch", cc.epoch);

    cJSON* l = cJSON_AddObjectToObject(o, "link");
    cJSON_AddStringToObject(l, "state", link_name(cc.link.state));
    cJSON_AddStringToObject(l, "peer", cc.peer);
    add_str_or_null(l, "since", cc.link.since);
    add_str_or_null(l, "last_rx_at", cc.link.lastRxAt);
    cJSON_AddNumberToObject(l, "connection_seq", cc.link.connectionSeq);
    cJSON* gi = cJSON_AddObjectToObject(l, "gi");
    cJSON_AddStringToObject(gi, "state", gi_name(cc.link.gi));
    add_str_or_null(gi, "started_at", cc.link.giStartedAt);
    add_str_or_null(gi, "completed_at", cc.link.giCompletedAt);

    cJSON* pts = cJSON_AddArrayToObject(o, "points");
    for (int i = 0; i < cc.pm->n_points; i++)
        cJSON_AddItemToArray(pts, point_json(i));
    return o;
}

cJSON*
cc_render_journal(uint64_t after)
{
    cJSON* o = cJSON_CreateObject();
    pthread_mutex_lock(&cc.lock);
    cJSON_AddStringToObject(o, "cache_epoch", cc.epoch);
    cJSON* entries = cJSON_AddArrayToObject(o, "entries");

    uint64_t latest = cc.journalNext - 1;
    uint64_t oldest = cc.journalNext > CC_JOURNAL_SIZE ? cc.journalNext - CC_JOURNAL_SIZE : 1;
    for (uint64_t seq = (after + 1 > oldest ? after + 1 : oldest); seq <= latest; seq++) {
        const CcJournalEntry* e = &cc.journal[seq % CC_JOURNAL_SIZE];
        cJSON* je = cJSON_CreateObject();
        cJSON_AddNumberToObject(je, "seq", (double)e->seq);
        cJSON_AddStringToObject(je, "at", e->at);
        cJSON_AddStringToObject(je, "kind", e->kind);
        cJSON_AddItemToObject(je, "detail", cJSON_Duplicate(e->detail, true));
        cJSON_AddItemToArray(entries, je);
    }
    if (latest == 0) {
        cJSON_AddNullToObject(o, "latest_seq");
        cJSON_AddNullToObject(o, "oldest_seq");
    } else {
        cJSON_AddNumberToObject(o, "latest_seq", (double)latest);
        cJSON_AddNumberToObject(o, "oldest_seq", (double)oldest);
    }
    pthread_mutex_unlock(&cc.lock);
    return o;
}
