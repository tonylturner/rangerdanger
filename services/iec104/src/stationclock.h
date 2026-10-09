/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * stationclock: the RTU's station clock, fed by polling gps-sim over HTTP
 * (contract §0, §3.2). A background thread polls `clock.url` every
 * `poll_ms`; the station time is the last GPS reported_time advanced by the
 * monotonic clock, so a paused or lost GPS does not rewind time. The clock
 * is "synced" only while the last poll is fresher than `stale_after_ms` and
 * the reported sync_status is in `synced_status`. CP56Time2a tags carry the
 * invalid (IV) bit whenever the clock is not synced.
 */
#ifndef RD_IEC104_STATIONCLOCK_H
#define RD_IEC104_STATIONCLOCK_H

#include <pthread.h>
#include <stdbool.h>
#include <stdint.h>

#include "pointmap.h"

typedef struct {
    char url[PM_URL_LEN];
    int pollMs;
    int staleAfterMs;
    const PointMap* pm; /* for synced_status membership */

    pthread_mutex_t lock;
    pthread_t thread;
    bool running;

    bool haveBase;           /* at least one successful poll since boot */
    uint64_t baseReportedMs; /* GPS reported_time of the last good poll, UTC ms */
    uint64_t baseMonoMs;     /* monotonic clock at that poll */
    uint64_t lastOkMonoMs;   /* monotonic clock of the last successful poll */
    bool lastStatusSynced;   /* sync_status of the last poll was in synced_status */
    char lastStatus[PM_STATUS_LEN];
} StationClock;

void sc_init(StationClock* sc, const PointMap* pm);
void sc_start(StationClock* sc);
void sc_stop(StationClock* sc);

/*
 * Writes the current station time (UTC ms) to *utcMs and returns true when
 * the clock is synced. When not synced it still returns a best-effort time
 * (last base advanced monotonically, or host time if GPS was never reached)
 * so a time tag can be formed with the IV bit set.
 */
bool sc_now(StationClock* sc, uint64_t* utcMs);

#endif
