/* SPDX-License-Identifier: GPL-3.0-or-later */
/* Real CC model/API with injected scheduler time; no lib60870 I/O thread. */
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "cc_api.h"
#include "cc_command.h"
#include "cc_link.h"
#include "cc_state.h"

static int sends;

bool
cc_link_send_command(const PmCommand* cmd, int dcs, bool select, int seq)
{
    (void)cmd; (void)dcs; (void)select; (void)seq;
    sends++;
    return false; /* Admission reached the transport; do not wait for a fake reply. */
}

void cc_historian_on_timed_report(void) {}

static cJSON*
member(cJSON* o, const char* key)
{
    cJSON* m = cJSON_GetObjectItemCaseSensitive(o, key);
    assert(m);
    return m;
}

static void*
post_gi(void* arg)
{
    CcHttpRequest r = {.method="POST", .path="/api/gi", .query="", .body="", .bodyLen=0};
    cc_api_handle(&r, arg);
    return NULL;
}

static int
admission(void)
{
    cJSON* body = NULL;
    int code = cc_command_run(0, 2, "close", &body);
    cJSON_Delete(body);
    return code;
}

int
main(int argc, char** argv)
{
    assert(argc == 2);
    char err[256];
    PointMap* pm = pointmap_load(argv[1], err, sizeof(err));
    assert(pm && pm->gi_interval_ms == 60000);
    assert(cc_state_init(pm, "test:2404", err, sizeof(err)));
    cJSON* s = cc_render_state();
    assert(member(s, "schema")->valueint == 3);
    cJSON* gi = member(member(s, "link"), "gi");
    assert(cJSON_GetArraySize(gi) == 5);
    assert(cJSON_IsNull(member(gi, "trigger")) && cJSON_IsNull(member(gi, "next_due_at")));
    cJSON* points = member(s, "points");
    for (int i = 0; i < pm->n_points; i++) {
        cJSON* p = cJSON_GetArrayItem(points, i);
        assert(cJSON_GetArraySize(p) == 14);
        assert(strcmp(member(p, "device")->valuestring, pm->devices[pm->points[i].device_index].id) == 0);
        cJSON* limits = member(p, "limits");
        assert(pm->points[i].has_limits ? cJSON_IsObject(limits) && cJSON_GetArraySize(limits) == 2 : cJSON_IsNull(limits));
        assert(cJSON_IsNull(member(p, "asdu")) && cJSON_IsNull(member(p, "value")));
        assert(cJSON_IsString(member(p, "kind")));
        cJSON* r = member(p, "reporting");
        assert(pm->points[i].kind == PM_KIND_FLOAT ? cJSON_IsObject(r) && cJSON_GetArraySize(r) == 2 : cJSON_IsNull(r));
    }
    cJSON_Delete(s);
    puts("PASS boot schema and nullable received ASDU/reporting");

    CcHttpResponse unavailable = {0};
    post_gi(&unavailable);
    assert(unavailable.status == 503);
    free(unavailable.body);
    cc_on_opened();
    cc_on_started();
    assert(admission() == 409); /* no integrity */
    pthread_mutex_lock(&cc.lock);
    uint64_t start = cc_mono_ms();
    assert(strcmp(cc_gi_tick_locked(start), "connect") == 0);
    assert(!cc_gi_tick_locked(start + 60000)); /* still running */
    pthread_mutex_unlock(&cc.lock);
    cc_on_gi_response(10, false);
    char complete[CC_STAMP_LEN];
    snprintf(complete, sizeof(complete), "%s", cc.link.giCompletedAt);
    assert(complete[0]);

    CcReport status = {.ioa=1001, .type=M_DP_NA_1, .dp=2};
    CcReport analog = {.ioa=2001, .type=M_ME_NC_1, .f=28.9f};
    cc_on_report(&status, 20);
    cc_on_report(&analog, 20);
    int ai = (int)(pointmap_point_by_ioa(pm, 2001) - pm->points);
    uint64_t received = cc.points[ai].receivedMono;
    pthread_mutex_lock(&cc.lock);
    cc_watchdog_locked(received + 3 * (uint64_t)pm->points[ai].cyclic_ms);
    assert(!cc.points[ai].nt);
    cc_watchdog_locked(received + 3 * (uint64_t)pm->points[ai].cyclic_ms + 1);
    assert(cc.points[ai].nt && !cc.points[ai].iv && cc.points[ai].stale == CC_STALE_NOT_UPDATED);
    assert(cc.points[ai].f == analog.f && cc.points[ai].asdu == M_ME_NC_1 && cc.points[ai].cot == 20);
    assert(cc.points[0].stale == CC_STALE_NONE); /* status does not age */
    pthread_mutex_unlock(&cc.lock);
    cc_on_report(&analog, 1);
    assert(!cc.points[ai].nt && cc.points[ai].stale == CC_STALE_NONE);
    analog.q = IEC60870_QUALITY_INVALID;
    cc_on_report(&analog, 3);
    pthread_mutex_lock(&cc.lock);
    cc_watchdog_locked(received + 10000000);
    assert(cc.points[ai].stale == CC_STALE_MEASUREMENT && !cc.points[ai].nt);
    pthread_mutex_unlock(&cc.lock);
    puts("PASS float watchdog boundary, retained fields, recovery and RTU-reason precedence");

    cc.points[0].receivedMono = 1; /* quiet GOOD, arbitrary age */
    assert(admission() == 200 && sends == 1);
    cc.points[0].nt = true;
    assert(admission() == 409 && sends == 1);
    cc.points[0].nt = false;
    cc.points[0].iv = true;
    assert(admission() == 409);
    cc.points[0].iv = false;
    puts("PASS integrity and liveness admission, quiet GOOD accepted, NT/IV refused");

    pthread_mutex_lock(&cc.lock);
    assert(!cc_gi_tick_locked(start + 59999));
    assert(strcmp(cc_gi_tick_locked(start + 60000), "periodic") == 0);
    assert(strcmp(complete, cc.link.giCompletedAt) == 0);
    pthread_mutex_unlock(&cc.lock);
    assert(admission() == 200); /* integrity survives a running GI */
    CcHttpResponse busy = {0};
    post_gi(&busy);
    assert(busy.status == 409);
    free(busy.body);
    cc_on_gi_response(10, false);
    puts("PASS 60000 ms periodic boundary, prior integrity retained, operator GI refused while periodic runs");

    pthread_t threads[2];
    CcHttpResponse responses[2] = {{0}, {0}};
    for (int i = 0; i < 2; i++) assert(!pthread_create(&threads[i], NULL, post_gi, &responses[i]));
    for (int i = 0; i < 2; i++) pthread_join(threads[i], NULL);
    assert((responses[0].status == 202 && responses[1].status == 409) ||
           (responses[0].status == 409 && responses[1].status == 202));
    for (int i = 0; i < 2; i++) {
        cJSON* b = cJSON_Parse(responses[i].body);
        if (responses[i].status == 202) {
            assert(cJSON_GetArraySize(b) == 2);
            assert(!strcmp(member(b, "trigger")->valuestring, "operator"));
            assert(cJSON_IsString(member(b, "requested_at")));
        }
        cJSON_Delete(b);
        free(responses[i].body);
    }
    pthread_mutex_lock(&cc.lock);
    assert(strcmp(cc_gi_tick_locked(start + 60001), "operator") == 0);
    assert(cc.link.giNextMono == start + 120001);
    pthread_mutex_unlock(&cc.lock);
    cc.points[0].sb = true;
    cc_on_gi_response(10, false);
    CcJournalEntry* completion = &cc.journal[(cc.journalNext - 1) % CC_JOURNAL_SIZE];
    assert(member(completion->detail, "points_good")->valueint == 0);
    cc.points[0].sb = false;
    puts("PASS concurrent POSTs reserve exactly one GI; operator restarts interval");

    CcHttpRequest reconnect = {.method="POST", .path="/api/reconnect", .query="", .body="", .bodyLen=0};
    CcHttpResponse rr = {0};
    cc.tracker.active = true;
    cc_api_handle(&reconnect, &rr);
    assert(rr.status == 409 && !cc.link.reconnectPending);
    free(rr.body); rr = (CcHttpResponse){0};
    cc.tracker.active = false;
    cc_api_handle(&reconnect, &rr);
    assert(rr.status == 202 && cc.link.reconnectPending);
    assert(admission() == 409); /* reservation also blocks new commands */
    free(rr.body); rr = (CcHttpResponse){0};
    cc_api_handle(&reconnect, &rr);
    assert(rr.status == 409);
    free(rr.body); rr = (CcHttpResponse){0};
    reconnect.body = "{}"; reconnect.bodyLen = 2;
    cc_api_handle(&reconnect, &rr);
    assert(rr.status == 400);
    free(rr.body);
    cc.link.reconnectPending = false;
    puts("PASS reconnect reservation, in-flight refusal, duplicate/body rejection and command exclusion");

    uint64_t before = cc.journalNext;
    status.type = M_DP_TB_1; status.dp = 1; status.timed = true; status.timeMs = 1792065600000;
    cc_on_report(&status, 3);
    assert(cc.journalNext == before + 1);
    CcJournalEntry* e = &cc.journal[before % CC_JOURNAL_SIZE];
    assert(!strcmp(e->kind, "status_event") && cJSON_GetArraySize(e->detail) == 7);
    assert(!strcmp(member(e->detail, "asdu")->valuestring, "M_DP_TB_1"));
    cc_on_report(&status, 11); /* unchanged */
    assert(cc.journalNext == before + 1);
    cc_on_ended();
    assert(!cc.link.giCompletedAt[0] && !cc.link.giTrigger && !cc.link.giPending);
    pthread_mutex_lock(&cc.lock);
    cc_watchdog_locked(received + 10000000);
    assert(cc.points[ai].stale == CC_STALE_LINK);
    pthread_mutex_unlock(&cc.lock);
    puts("PASS status_event shape/change filtering and link-loss precedence/reset");
    pointmap_free(pm);
    puts("PASS 7 CC model groups");
    return 0;
}
