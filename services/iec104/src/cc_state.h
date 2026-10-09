/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * cc_state: the control centre's shared model (contract §6). One mutex
 * guards everything the IEC 104 receive thread, the link thread, the HTTP
 * threads and the command state machine share: the per-point cache, the
 * link and GI status, the journal ring and the in-flight command tracker.
 *
 * Locking rule: nothing calls into lib60870 while holding cc.lock. lib60870
 * invokes our ASDU handler with its own connection lock held, and that
 * handler takes cc.lock; holding cc.lock across a lib60870 call could
 * therefore deadlock. Every function in this header that is documented as
 * "locks" takes and releases cc.lock itself; "caller holds" functions must
 * be called with it held.
 */
#ifndef RD_IEC104_CC_STATE_H
#define RD_IEC104_CC_STATE_H

#include <pthread.h>
#include <stdbool.h>
#include <stdint.h>

#include "cJSON.h"
#include "iec60870_common.h"
#include "pointmap.h"

#define CC_STAMP_LEN 40 /* RFC3339Nano UTC; "" means null */
#define CC_JOURNAL_SIZE 500
#define CC_PEER_LEN 64
#define CC_EPOCH_LEN 17 /* 16 hex digits + NUL */

typedef enum { CC_STALE_NONE, CC_STALE_NEVER, CC_STALE_LINK, CC_STALE_DEVICE, CC_STALE_MEASUREMENT } CcStale;

typedef enum { CC_LINK_DISCONNECTED, CC_LINK_CONNECTING, CC_LINK_STARTED } CcLinkState;

typedef enum { CC_GI_NONE, CC_GI_RUNNING, CC_GI_COMPLETE } CcGiState;

/* Cached state of one monitored point. */
typedef struct {
    bool hasValue;
    int dp; /* double point value (DPI 0-3) */
    bool sp;
    float f;
    bool iv, nt, sb, bl, ov;
    CcStale stale;
    char sourceTime[CC_STAMP_LEN]; /* "" for untimed reports */
    bool sourceTimeInvalid;
    char receivedAt[CC_STAMP_LEN];
    int cot; /* -1 until received */
} CcPoint;

/* One decoded monitoring information object. */
typedef struct {
    int ioa;
    TypeID type;
    int dp;
    bool sp;
    float f;
    QualityDescriptor q;
    bool timed;
    uint64_t timeMs;
    bool timeInvalid;
} CcReport;

typedef enum { CC_PHASE_IDLE, CC_PHASE_SELECT, CC_PHASE_EXECUTE } CcPhase;

/* Result of one confirmation: none yet, positive, negative. */
typedef enum { CC_CON_NONE, CC_CON_POSITIVE, CC_CON_NEGATIVE } CcCon;

/* The single in-flight command; filled by the receive thread. */
typedef struct {
    bool active;
    int ioa;
    int feedbackIndex;
    int connSeq;
    CcPhase phase;
    CcCon selectCon;
    char selectAt[CC_STAMP_LEN];
    CcCon executeCon;
    char executeAt[CC_STAMP_LEN];
    bool terminated;
    char terminatedAt[CC_STAMP_LEN];
    bool feedback;
    int feedbackDp;
    char feedbackAt[CC_STAMP_LEN];
} CcTracker;

typedef struct {
    uint64_t seq;
    char at[CC_STAMP_LEN];
    const char* kind;
    cJSON* detail;
} CcJournalEntry;

typedef struct {
    CcLinkState state;
    char since[CC_STAMP_LEN];
    char lastRxAt[CC_STAMP_LEN];
    uint64_t lastRxMono;
    int connectionSeq;
    CcGiState gi;
    char giStartedAt[CC_STAMP_LEN];
    char giCompletedAt[CC_STAMP_LEN];
    bool giFailed;

    /* per-attempt outcome flags, reset by the link thread before each attempt */
    bool attemptOpened;
    bool attemptEnded;

    /* set by the link thread when it closes the session itself */
    bool closeRequested;
    const char* closeReason;
    /* the last journaled link event is a link_down: suppresses repeated connect_failed */
    bool downJournaled;
    bool shuttingDown;
} CcLink;

typedef struct {
    pthread_mutex_t lock;
    pthread_cond_t cond; /* CLOCK_MONOTONIC; broadcast on every change */

    const PointMap* pm;
    char peer[CC_PEER_LEN];
    char epoch[CC_EPOCH_LEN];

    CcPoint points[PM_MAX_POINTS];
    CcLink link;
    CcTracker tracker;
    uint64_t commandSeq;

    CcJournalEntry journal[CC_JOURNAL_SIZE];
    uint64_t journalNext; /* seq of the next entry; entries 1..journalNext-1 were written */
} CcState;

extern CcState cc;

/* Initialises cc; false (with err) if the boot epoch cannot be generated. */
bool cc_state_init(const PointMap* pm, const char* peer, char* err, size_t errLen);

uint64_t cc_mono_ms(void);
void cc_stamp_now(char out[CC_STAMP_LEN]);

/* Caller holds. Waits on cc.cond until woken or deadlineMono passes; false on timeout. */
bool cc_wait_until(uint64_t deadlineMono);

/* Caller holds. Appends a journal entry, taking ownership of detail. */
void cc_journal_add(const char* kind, cJSON* detail);

/* Value of a cached point as JSON (null when never received). Caller holds. */
cJSON* cc_point_value_json(int index);

/* Wire name of a double-point value. */
const char* cc_dp_name(int dp);

/* ---- receive-thread events (lock) ---------------------------------- */

void cc_on_frame_received(void);
void cc_on_report(const CcReport* r, int cot);
void cc_on_gi_response(int cot, bool negative);
void cc_on_command_response(int ioa, bool select, int cot, bool negative);
void cc_on_opened(void);
void cc_on_started(void);
void cc_on_ended(void);

/* ---- link-thread transitions (lock) -------------------------------- */

void cc_link_begin_attempt(void);
void cc_gi_begin(void);

/* ---- HTTP renderers (lock) ----------------------------------------- */

cJSON* cc_render_health(void);
cJSON* cc_render_state(void);
cJSON* cc_render_journal(uint64_t after);

#endif
