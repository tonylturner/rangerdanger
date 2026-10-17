/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 */
#include "pointmap.h"

#include <math.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cJSON.h"

/* ---- error plumbing -------------------------------------------------- */

/*
 * A load fails by writing one message and returning false up the stack. The
 * message buffer belongs to the caller of pointmap_load; every helper takes
 * it so the first failure wins and names the offending key.
 */
typedef struct {
    char* buf;
    size_t len;
    bool failed;
} PmErr;

static bool
fail(PmErr* e, const char* fmt, ...) __attribute__((format(printf, 2, 3)));

static bool
fail(PmErr* e, const char* fmt, ...)
{
    if (!e->failed && e->buf && e->len) {
        va_list ap;
        va_start(ap, fmt);
        vsnprintf(e->buf, e->len, fmt, ap);
        va_end(ap);
    }
    e->failed = true;
    return false;
}

/* ---- strict field access -------------------------------------------- */

/*
 * Rejects any key on `obj` that is not in `allowed` (NULL-terminated). This
 * is what makes the loader strict: a typo or an unexpected field stops the
 * load instead of being ignored.
 */
static bool
only_keys(PmErr* e, const cJSON* obj, const char* where, const char* const* allowed)
{
    for (const cJSON* c = obj->child; c; c = c->next) {
        bool ok = false;
        for (const char* const* a = allowed; *a; a++) {
            if (strcmp(c->string, *a) == 0) {
                ok = true;
                break;
            }
        }
        for (const cJSON* prev = obj->child; prev != c; prev = prev->next)
            if (strcmp(prev->string, c->string) == 0)
                return fail(e, "%s: duplicate key %s", where, c->string);
        if (!ok)
            return fail(e, "%s: unknown key \"%s\"", where, c->string);
    }
    return true;
}

static const cJSON*
required(PmErr* e, const cJSON* obj, const char* where, const char* key)
{
    const cJSON* v = cJSON_GetObjectItemCaseSensitive(obj, key);
    if (!v)
        fail(e, "%s: missing required key \"%s\"", where, key);
    return v;
}

static bool
get_int(PmErr* e, const cJSON* obj, const char* where, const char* key, long min, long max, int* out)
{
    const cJSON* v = required(e, obj, where, key);
    if (!v)
        return false;
    if (!cJSON_IsNumber(v) || !isfinite(v->valuedouble) || v->valuedouble < min || v->valuedouble > max || floor(v->valuedouble) != v->valuedouble)
        return fail(e, "%s: \"%s\" must be an integer", where, key);
    long n = (long)v->valuedouble;
    if (n < min || n > max)
        return fail(e, "%s: \"%s\" = %ld out of range [%ld, %ld]", where, key, n, min, max);
    *out = (int)n;
    return true;
}

static bool
get_bool(PmErr* e, const cJSON* obj, const char* where, const char* key, bool* out)
{
    const cJSON* v = required(e, obj, where, key);
    if (!v)
        return false;
    if (!cJSON_IsBool(v))
        return fail(e, "%s: \"%s\" must be true or false", where, key);
    *out = cJSON_IsTrue(v);
    return true;
}

static bool
get_double(PmErr* e, const cJSON* obj, const char* where, const char* key, double* out)
{
    const cJSON* v = required(e, obj, where, key);
    if (!v)
        return false;
    if (!cJSON_IsNumber(v) || !isfinite(v->valuedouble))
        return fail(e, "%s: \"%s\" must be a number", where, key);
    *out = v->valuedouble;
    return true;
}

static bool
get_string(PmErr* e, const cJSON* obj, const char* where, const char* key, char* out, size_t cap)
{
    const cJSON* v = required(e, obj, where, key);
    if (!v)
        return false;
    if (!cJSON_IsString(v) || !v->valuestring)
        return fail(e, "%s: \"%s\" must be a string", where, key);
    if (strlen(v->valuestring) >= cap)
        return fail(e, "%s: \"%s\" is too long", where, key);
    snprintf(out, cap, "%s", v->valuestring);
    return true;
}

static bool
get_table(PmErr* e, const cJSON* obj, const char* where, PmTable* out)
{
    char s[16];
    if (!get_string(e, obj, where, "table", s, sizeof(s)))
        return false;
    if (strcmp(s, "input") == 0)
        *out = PM_TABLE_INPUT;
    else if (strcmp(s, "holding") == 0)
        *out = PM_TABLE_HOLDING;
    else
        return fail(e, "%s: \"table\" must be \"input\" or \"holding\"", where);
    return true;
}

/* ---- section parsers ------------------------------------------------- */

static bool
parse_timers(PmErr* e, const cJSON* root, PointMap* pm)
{
    const cJSON* t = required(e, root, "root", "timers");
    if (!t)
        return false;
    if (!cJSON_IsObject(t))
        return fail(e, "timers: must be an object");
    static const char* const keys[] = {"t0_s", "t1_s", "t2_s", "t3_s", "k", "w", NULL};
    if (!only_keys(e, t, "timers", keys))
        return false;
    return get_int(e, t, "timers", "t0_s", 1, 255, &pm->timers.t0_s) &&
           get_int(e, t, "timers", "t1_s", 1, 255, &pm->timers.t1_s) &&
           get_int(e, t, "timers", "t2_s", 1, 255, &pm->timers.t2_s) &&
           get_int(e, t, "timers", "t3_s", 1, 255, &pm->timers.t3_s) &&
           get_int(e, t, "timers", "k", 1, 32767, &pm->timers.k) &&
           get_int(e, t, "timers", "w", 1, 32767, &pm->timers.w);
}

static bool
parse_clock(PmErr* e, const cJSON* root, PointMap* pm)
{
    const cJSON* c = required(e, root, "root", "clock");
    if (!c)
        return false;
    if (!cJSON_IsObject(c))
        return fail(e, "clock: must be an object");
    static const char* const keys[] = {"url", "poll_ms", "stale_after_ms", "synced_status", NULL};
    if (!only_keys(e, c, "clock", keys))
        return false;
    if (!get_string(e, c, "clock", "url", pm->clock.url, sizeof(pm->clock.url)) ||
        !get_int(e, c, "clock", "poll_ms", 10, 600000, &pm->clock.poll_ms) ||
        !get_int(e, c, "clock", "stale_after_ms", 10, 600000, &pm->clock.stale_after_ms))
        return false;

    const cJSON* ss = required(e, c, "clock", "synced_status");
    if (!ss)
        return false;
    if (!cJSON_IsArray(ss) || cJSON_GetArraySize(ss) == 0)
        return fail(e, "clock: \"synced_status\" must be a non-empty array");
    const cJSON* it;
    cJSON_ArrayForEach(it, ss)
    {
        if (pm->clock.n_synced >= PM_MAX_SYNC_STATUS)
            return fail(e, "clock: too many synced_status entries (max %d)", PM_MAX_SYNC_STATUS);
        if (!cJSON_IsString(it) || !it->valuestring || strlen(it->valuestring) >= PM_STATUS_LEN)
            return fail(e, "clock: each \"synced_status\" entry must be a short string");
        snprintf(pm->clock.synced_status[pm->clock.n_synced++], PM_STATUS_LEN, "%s", it->valuestring);
    }
    return true;
}

static bool
parse_device(PmErr* e, const cJSON* d, PmDevice* dev)
{
    static const char* const keys[] = {"id",   "host",      "port",           "unit",          "poll_ms",
                                        "timeout_ms", "stale_after_ms", "measurements_valid", "poll", "heartbeat", NULL};
    if (!cJSON_IsObject(d))
        return fail(e, "devices: each entry must be an object");
    if (!only_keys(e, d, "device", keys))
        return false;
    if (!get_string(e, d, "device", "id", dev->id, sizeof(dev->id)) ||
        !get_string(e, d, "device", "host", dev->host, sizeof(dev->host)) ||
        !get_int(e, d, "device", "port", 1, 65535, &dev->port) ||
        !get_int(e, d, "device", "unit", 0, 255, &dev->unit) ||
        !get_int(e, d, "device", "poll_ms", 10, 600000, &dev->poll_ms) ||
        !get_int(e, d, "device", "timeout_ms", 10, 600000, &dev->timeout_ms) ||
        !get_int(e, d, "device", "stale_after_ms", 10, 600000, &dev->stale_after_ms))
        return false;

    const cJSON* poll = required(e, d, "device", "poll");
    static const char* const pk[] = {"table", "start", "count", NULL};
    if (!poll || !cJSON_IsObject(poll) || !only_keys(e, poll, "poll", pk) ||
        !get_table(e, poll, "poll", &dev->poll_table) ||
        !get_int(e, poll, "poll", "start", 0, 65535, &dev->poll_start) ||
        !get_int(e, poll, "poll", "count", 1, 125, &dev->poll_count))
        return fail(e, "device %s: invalid poll block", dev->id);
    if (dev->poll_start + dev->poll_count > 65536)
        return fail(e, "device %s: poll block overflow", dev->id);
    const cJSON* hb = cJSON_GetObjectItemCaseSensitive(d, "heartbeat");
    if (hb) {
        static const char* const hk[] = {"address", "stale_after_ms", NULL};
        if (!cJSON_IsObject(hb) || !only_keys(e, hb, "heartbeat", hk) ||
            !get_int(e, hb, "heartbeat", "address", dev->poll_start,
                     dev->poll_start + dev->poll_count - 1, &dev->heartbeat_address) ||
            !get_int(e, hb, "heartbeat", "stale_after_ms", 10, 600000, &dev->heartbeat_stale_ms))
            return false;
        dev->has_heartbeat = true;
    }
    const cJSON* mv = cJSON_GetObjectItemCaseSensitive(d, "measurements_valid");
    if (mv) {
        static const char* const mvkeys[] = {"table", "address", "bit", NULL};
        if (!cJSON_IsObject(mv))
            return fail(e, "device %s: \"measurements_valid\" must be an object", dev->id);
        if (!only_keys(e, mv, "measurements_valid", mvkeys))
            return false;
        if (!get_table(e, mv, "measurements_valid", &dev->mv_table) ||
            !get_int(e, mv, "measurements_valid", "address", 0, 65535, &dev->mv_address) ||
            !get_int(e, mv, "measurements_valid", "bit", 0, 15, &dev->mv_bit))
            return false;
        if (dev->mv_table != dev->poll_table || dev->mv_address < dev->poll_start ||
            dev->mv_address >= dev->poll_start + dev->poll_count)
            return fail(e, "device %s: measurements_valid outside poll block", dev->id);
        dev->has_measurements_valid = true;
    }
    return true;
}

/* Validates that the configured ASDU string matches the point kind. */
static bool
check_asdu(PmErr* e, const char* where, PmKind kind, const char* asdu)
{
    const char* want = NULL;
    switch (kind) {
    case PM_KIND_DOUBLE_POINT:
        want = "M_DP_TB_1";
        break;
    case PM_KIND_SINGLE_POINT:
        want = "M_SP_TB_1";
        break;
    case PM_KIND_FLOAT:
        want = "M_ME_NC_1";
        break;
    case PM_KIND_STEP_POSITION: want = "M_ST_TB_1"; break;
    case PM_KIND_REGULATING_STEP: want = "C_RC_NA_1"; break;
    case PM_KIND_SINGLE_COMMAND: want = "C_SC_NA_1"; break;
    case PM_KIND_DOUBLE_COMMAND:
        want = "C_DC_NA_1";
        break;
    }
    if (strcmp(asdu, want) != 0)
        return fail(e, "%s: \"asdu\" must be \"%s\" for this kind, not \"%s\"", where, want, asdu);
    return true;
}

static bool
parse_kind(PmErr* e, const cJSON* obj, const char* where, bool command, PmKind* out)
{
    char s[32];
    if (!get_string(e, obj, where, "kind", s, sizeof(s)))
        return false;
    if (!command && strcmp(s, "double-point") == 0)
        *out = PM_KIND_DOUBLE_POINT;
    else if (!command && strcmp(s, "single-point") == 0)
        *out = PM_KIND_SINGLE_POINT;
    else if (!command && strcmp(s, "float") == 0)
        *out = PM_KIND_FLOAT;
    else if (!command && strcmp(s, "step-position") == 0)
        *out = PM_KIND_STEP_POSITION;
    else if (command && strcmp(s, "regulating-step") == 0)
        *out = PM_KIND_REGULATING_STEP;
    else if (command && strcmp(s, "single-command") == 0)
        *out = PM_KIND_SINGLE_COMMAND;
    else if (command && strcmp(s, "double-command") == 0)
        *out = PM_KIND_DOUBLE_COMMAND;
    else
        return fail(e, "%s: unsupported \"kind\" \"%s\"", where, s);
    return true;
}

static bool
parse_point(PmErr* e, const cJSON* p, PmPoint* pt)
{
    static const char* const keys[] = {"id",   "ioa",         "kind", "asdu", "unit",
                                        "deadband", "cyclic_ms", "measurement", "source", NULL};
    static const char* const srckeys[] = {"device", "table", "address", "format", "bit", "scale", "transient", NULL};

    if (!cJSON_IsObject(p))
        return fail(e, "points: each entry must be an object");
    if (!only_keys(e, p, "point", keys))
        return false;

    if (!get_string(e, p, "point", "id", pt->id, sizeof(pt->id)) ||
        !get_int(e, p, "point", "ioa", 1, 0xffffff, &pt->ioa) ||
        !parse_kind(e, p, "point", false, &pt->kind) ||
        !get_string(e, p, "point", "asdu", pt->asdu, sizeof(pt->asdu)))
        return false;
    if (!check_asdu(e, pt->id, pt->kind, pt->asdu))
        return false;

    if (cJSON_GetObjectItemCaseSensitive(p, "unit") &&
        !get_string(e, p, "point", "unit", pt->unit, sizeof(pt->unit)))
        return false;
    if (cJSON_GetObjectItemCaseSensitive(p, "measurement") &&
        !get_bool(e, p, "point", "measurement", &pt->measurement))
        return false;
    if (cJSON_GetObjectItemCaseSensitive(p, "deadband")) {
        if (!get_double(e, p, "point", "deadband", &pt->deadband))
            return false;
        if (pt->deadband < 0)
            return fail(e, "point %s: \"deadband\" must be >= 0", pt->id);
        pt->has_deadband = true;
    }
    if (pt->kind != PM_KIND_FLOAT && (pt->has_deadband || pt->measurement))
        return fail(e, "point %s: \"deadband\"/\"measurement\" apply only to float points", pt->id);

    if (pt->kind == PM_KIND_FLOAT) {
        if (!get_int(e, p, "point", "cyclic_ms", 1000, 3600000, &pt->cyclic_ms))
            return false;
    } else if (cJSON_GetObjectItemCaseSensitive(p, "cyclic_ms"))
        return fail(e, "point %s: \"cyclic_ms\" applies only to float points", pt->id);

    const cJSON* src = required(e, p, "point", "source");
    if (!src)
        return false;
    if (!cJSON_IsObject(src))
        return fail(e, "point %s: \"source\" must be an object", pt->id);
    if (!only_keys(e, src, "source", srckeys))
        return false;

    /* source.device is validated here; the caller resolves it to an index once all devices are known. */
    const cJSON* devItem = required(e, src, "source", "device");
    if (!devItem)
        return false;
    if (!cJSON_IsString(devItem) || !devItem->valuestring)
        return fail(e, "point %s: source.device must be a string", pt->id);

    PmTable table;
    if (!get_table(e, src, "source", &table))
        return false;
    pt->table = table;
    if (!get_int(e, src, "source", "address", 0, 65535, &pt->address))
        return false;

    char fmt[8];
    if (!get_string(e, src, "source", "format", fmt, sizeof(fmt)))
        return false;
    if (strcmp(fmt, "dp") == 0)
        pt->format = PM_FMT_DP;
    else if (strcmp(fmt, "bit") == 0)
        pt->format = PM_FMT_BIT;
    else if (strcmp(fmt, "f32") == 0)
        pt->format = PM_FMT_F32;
    else if (strcmp(fmt, "u16") == 0) pt->format = PM_FMT_U16;
    else if (strcmp(fmt, "i16") == 0) pt->format = PM_FMT_I16;
    else if (strcmp(fmt, "step") == 0) pt->format = PM_FMT_STEP;
    else return fail(e, "point %s: unknown source.format", pt->id);
    pt->scale = 1;
    if (cJSON_GetObjectItemCaseSensitive(src, "scale")) {
        if ((pt->format != PM_FMT_U16 && pt->format != PM_FMT_I16) ||
            !get_double(e, src, "source", "scale", &pt->scale))
            return fail(e, "point %s: scale requires u16/i16", pt->id);
    }
    const cJSON* tr = cJSON_GetObjectItemCaseSensitive(src, "transient");
    if (pt->format == PM_FMT_STEP) {
        static const char* const tk[] = {"address", "bit", NULL};
        if (!tr || !cJSON_IsObject(tr) || !only_keys(e, tr, "transient", tk) ||
            !get_int(e, tr, "transient", "address", 0, 65535, &pt->transient_address) ||
            !get_int(e, tr, "transient", "bit", 0, 15, &pt->transient_bit))
            return fail(e, "point %s: step requires transient address/bit", pt->id);
    } else if (tr) return fail(e, "point %s: transient requires step", pt->id);

    if (pt->format == PM_FMT_BIT) {
        if (!get_int(e, src, "source", "bit", 0, 15, &pt->bit))
            return false;
    } else if (cJSON_GetObjectItemCaseSensitive(src, "bit")) {
        return fail(e, "point %s: \"bit\" is only valid with format \"bit\"", pt->id);
    }

    /* kind/format agreement keeps the wire type and the register reading consistent. */
    if ((pt->kind == PM_KIND_DOUBLE_POINT && pt->format != PM_FMT_DP) ||
        (pt->kind == PM_KIND_SINGLE_POINT && pt->format != PM_FMT_BIT) ||
        (pt->kind == PM_KIND_STEP_POSITION && pt->format != PM_FMT_STEP) ||
        (pt->kind == PM_KIND_FLOAT && pt->format != PM_FMT_F32 && pt->format != PM_FMT_U16 && pt->format != PM_FMT_I16))
        return fail(e, "point %s: kind and source.format disagree", pt->id);

    return true;
}

static int
find_device(const PointMap* pm, const char* id)
{
    for (int i = 0; i < pm->n_devices; i++)
        if (strcmp(pm->devices[i].id, id) == 0)
            return i;
    return -1;
}

static int
find_point(const PointMap* pm, const char* id)
{
    for (int i = 0; i < pm->n_points; i++)
        if (strcmp(pm->points[i].id, id) == 0)
            return i;
    return -1;
}

PointMap*
pointmap_load(const char* path, char* err, size_t errLen)
{
    PmErr e = {err, errLen, false};
    if (err && errLen)
        err[0] = '\0';

    FILE* f = fopen(path, "rb");
    if (!f) {
        fail(&e, "cannot open %s", path);
        return NULL;
    }
    fseek(f, 0, SEEK_END);
    long size = ftell(f);
    fseek(f, 0, SEEK_SET);
    if (size <= 0 || size > 1 << 20) {
        fail(&e, "%s: empty or implausibly large", path);
        fclose(f);
        return NULL;
    }
    char* text = malloc((size_t)size + 1);
    if (!text) {
        fail(&e, "out of memory");
        fclose(f);
        return NULL;
    }
    size_t got = fread(text, 1, (size_t)size, f);
    fclose(f);
    text[got] = '\0';

    cJSON* root = got == (size_t)size && !memchr(text, 0, got) ?
        cJSON_ParseWithLengthOpts(text, got + 1, NULL, true) : NULL;
    free(text);
    if (!root) {
        fail(&e, "%s: invalid JSON", path);
        return NULL;
    }

    PointMap* pm = calloc(1, sizeof(*pm));
    if (!pm) {
        fail(&e, "out of memory");
        cJSON_Delete(root);
        return NULL;
    }

    static const char* const rootkeys[] = {"schema",  "ca",     "timers",   "select_timeout_ms",
                                            "reconnect_interval_ms", "gi_interval_ms", "clock",  "devices",  "points",
                                            "commands", NULL};

    bool ok = cJSON_IsObject(root) && only_keys(&e, root, "root", rootkeys) &&
              get_int(&e, root, "root", "schema", 2, 2, &pm->schema) &&
              get_int(&e, root, "root", "ca", 1, 65535, &pm->ca) && parse_timers(&e, root, pm) &&
              get_int(&e, root, "root", "select_timeout_ms", 1, 600000, &pm->select_timeout_ms) &&
              get_int(&e, root, "root", "reconnect_interval_ms", 1, 600000, &pm->reconnect_interval_ms) &&
              get_int(&e, root, "root", "gi_interval_ms", 60000, 86400000, &pm->gi_interval_ms) &&
              parse_clock(&e, root, pm);

    /* devices */
    if (ok) {
        const cJSON* devs = required(&e, root, "root", "devices");
        if (!devs || !cJSON_IsArray(devs) || cJSON_GetArraySize(devs) == 0)
            ok = fail(&e, "\"devices\" must be a non-empty array");
        else {
            const cJSON* d;
            cJSON_ArrayForEach(d, devs)
            {
                if (pm->n_devices >= PM_MAX_DEVICES) {
                    ok = fail(&e, "too many devices (max %d)", PM_MAX_DEVICES);
                    break;
                }
                if (!parse_device(&e, d, &pm->devices[pm->n_devices])) {
                    ok = false;
                    break;
                }
                pm->n_devices++;
            }
        }
    }

    /* points (with device ids read straight from JSON here, so we avoid scratch fields) */
    if (ok) {
        const cJSON* pts = required(&e, root, "root", "points");
        if (!pts || !cJSON_IsArray(pts) || cJSON_GetArraySize(pts) == 0)
            ok = fail(&e, "\"points\" must be a non-empty array");
        else {
            const cJSON* p;
            cJSON_ArrayForEach(p, pts)
            {
                if (pm->n_points >= PM_MAX_POINTS) {
                    ok = fail(&e, "too many points (max %d)", PM_MAX_POINTS);
                    break;
                }
                PmPoint* pt = &pm->points[pm->n_points];
                if (!parse_point(&e, p, pt)) {
                    ok = false;
                    break;
                }
                const cJSON* src = cJSON_GetObjectItemCaseSensitive(p, "source");
                const cJSON* devItem = cJSON_GetObjectItemCaseSensitive(src, "device");
                pt->device_index = find_device(pm, devItem->valuestring);
                if (pt->device_index < 0) {
                    ok = fail(&e, "point %s: source.device \"%s\" is not a defined device", pt->id,
                              devItem->valuestring);
                    break;
                }
                const PmDevice* dev = &pm->devices[pt->device_index];
                int end = pt->address + (pt->format == PM_FMT_F32 ? 2 : 1);
                if (pt->table != dev->poll_table || pt->address < dev->poll_start ||
                    end > dev->poll_start + dev->poll_count ||
                    (pt->format == PM_FMT_STEP && (pt->transient_address < dev->poll_start ||
                     pt->transient_address >= dev->poll_start + dev->poll_count))) {
                    ok = fail(&e, "point %s: source outside poll block", pt->id);
                    break;
                }
                if (find_point(pm, pt->id) >= 0) {
                    ok = fail(&e, "duplicate point id \"%s\"", pt->id);
                    break;
                }
                pm->n_points++;
            }
        }
    }

    /* commands */
    if (ok) {
        const cJSON* cmds = cJSON_GetObjectItemCaseSensitive(root, "commands");
        if (cmds) {
            if (!cJSON_IsArray(cmds))
                ok = fail(&e, "\"commands\" must be an array");
            else {
                static const char* const keys[] = {"id", "ioa", "kind", "asdu", "sbo", "target", "feedback", NULL};
                static const char* const tkeys[] = {"device", "table", "address", NULL};
                const cJSON* c;
                cJSON_ArrayForEach(c, cmds)
                {
                    if (pm->n_commands >= PM_MAX_COMMANDS) {
                        ok = fail(&e, "too many commands (max %d)", PM_MAX_COMMANDS);
                        break;
                    }
                    PmCommand* cmd = &pm->commands[pm->n_commands];
                    if (!cJSON_IsObject(c)) {
                        ok = fail(&e, "commands: each entry must be an object");
                        break;
                    }
                    if (!only_keys(&e, c, "command", keys) ||
                        !get_string(&e, c, "command", "id", cmd->id, sizeof(cmd->id)) ||
                        !get_int(&e, c, "command", "ioa", 1, 0xffffff, &cmd->ioa) ||
                        !parse_kind(&e, c, "command", true, &cmd->kind) ||
                        !get_string(&e, c, "command", "asdu", cmd->asdu, sizeof(cmd->asdu)) ||
                        !check_asdu(&e, cmd->id, cmd->kind, cmd->asdu) ||
                        !get_bool(&e, c, "command", "sbo", &cmd->sbo) ||
                        !get_string(&e, c, "command", "feedback", cmd->feedback_id, sizeof(cmd->feedback_id))) {
                        ok = false;
                        break;
                    }
                    const cJSON* tgt = required(&e, c, "command", "target");
                    if (!tgt || !cJSON_IsObject(tgt)) {
                        ok = fail(&e, "command %s: \"target\" must be an object", cmd->id);
                        break;
                    }
                    if (!only_keys(&e, tgt, "target", tkeys) || !get_table(&e, tgt, "target", &cmd->table) ||
                        !get_int(&e, tgt, "target", "address", 0, 65535, &cmd->address)) {
                        ok = false;
                        break;
                    }
                    const cJSON* devItem = required(&e, tgt, "target", "device");
                    if (!devItem || !cJSON_IsString(devItem)) {
                        ok = fail(&e, "command %s: target.device must be a string", cmd->id);
                        break;
                    }
                    cmd->device_index = find_device(pm, devItem->valuestring);
                    if (cmd->device_index < 0) {
                        ok = fail(&e, "command %s: target.device \"%s\" is not a defined device", cmd->id,
                                  devItem->valuestring);
                        break;
                    }
                    cmd->feedback_index = find_point(pm, cmd->feedback_id);
                    if (cmd->feedback_index < 0) {
                        ok = fail(&e, "command %s: feedback \"%s\" is not a defined point", cmd->id,
                                  cmd->feedback_id);
                        break;
                    }
                    PmKind expected = cmd->kind == PM_KIND_DOUBLE_COMMAND ? PM_KIND_DOUBLE_POINT :
                        cmd->kind == PM_KIND_REGULATING_STEP ? PM_KIND_STEP_POSITION : PM_KIND_SINGLE_POINT;
                    if (cmd->table != PM_TABLE_HOLDING || !cmd->sbo ||
                        pm->points[cmd->feedback_index].kind != expected ||
                        pm->points[cmd->feedback_index].device_index != cmd->device_index) {
                        ok = fail(&e, "command %s: requires holding/SBO and same-device typed feedback", cmd->id);
                        break;
                    }
                    pm->n_commands++;
                }
            }
        }
    }

    for (int i = 0; ok && i < pm->n_devices; i++)
        for (int j = 0; j < i; j++)
            if (!strcmp(pm->devices[i].id, pm->devices[j].id))
                ok = fail(&e, "duplicate device id");
    for (int i = 0; ok && i < pm->n_points + pm->n_commands; i++) {
        int ioa = i < pm->n_points ? pm->points[i].ioa : pm->commands[i-pm->n_points].ioa;
        const char* id = i < pm->n_points ? pm->points[i].id : pm->commands[i-pm->n_points].id;
        for (int j = 0; j < i; j++) {
            int prev = j < pm->n_points ? pm->points[j].ioa : pm->commands[j-pm->n_points].ioa;
            const char* pid = j < pm->n_points ? pm->points[j].id : pm->commands[j-pm->n_points].id;
            if (ioa == prev || !strcmp(id, pid)) ok = fail(&e, "duplicate IOA or id: %s", id);
        }
    }
    cJSON_Delete(root);
    if (!ok) {
        free(pm);
        return NULL;
    }
    return pm;
}

void
pointmap_free(PointMap* pm)
{
    free(pm);
}

const PmPoint*
pointmap_point_by_ioa(const PointMap* pm, int ioa)
{
    for (int i = 0; i < pm->n_points; i++)
        if (pm->points[i].ioa == ioa)
            return &pm->points[i];
    return NULL;
}

const PmCommand*
pointmap_command_by_ioa(const PointMap* pm, int ioa)
{
    for (int i = 0; i < pm->n_commands; i++)
        if (pm->commands[i].ioa == ioa)
            return &pm->commands[i];
    return NULL;
}

bool
pointmap_status_synced(const PointMap* pm, const char* status)
{
    if (!status)
        return false;
    for (int i = 0; i < pm->clock.n_synced; i++)
        if (strcmp(pm->clock.synced_status[i], status) == 0)
            return true;
    return false;
}
