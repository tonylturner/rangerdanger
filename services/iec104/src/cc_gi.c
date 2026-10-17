/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 */
#include "cc_state.h"

const char*
cc_gi_tick_locked(uint64_t now)
{
    if (cc.link.state != CC_LINK_STARTED || cc.link.gi == CC_GI_RUNNING)
        return NULL;
    const char* trigger = cc.link.giPending;
    if (!trigger && cc.link.giNextMono && now >= cc.link.giNextMono)
        trigger = "periodic";
    if (!trigger)
        return NULL;
    cc.link.giPending = NULL;
    cc.link.gi = CC_GI_RUNNING;
    cc.link.giFailed = false;
    cc.link.giTrigger = trigger;
    cc.link.giStartedMono = now;
    cc.link.giNextMono = now + (uint64_t)cc.pm->gi_interval_ms;
    cc_stamp_now(cc.link.giStartedAt);
    cc_stamp_after_ms(cc.pm->gi_interval_ms, cc.link.giNextAt);
    cJSON* d = cJSON_CreateObject();
    cJSON_AddNumberToObject(d, "connection_seq", cc.link.connectionSeq);
    cJSON_AddStringToObject(d, "trigger", trigger);
    cc_journal_add("gi_started", d);
    return trigger;
}
