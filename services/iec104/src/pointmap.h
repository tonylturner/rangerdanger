/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * pointmap: the shared, strict loader for the package point map
 * (lab-definitions/packages/<id>/protocols/points.json, schema in the
 * schema-2 contract). The RTU drives its Modbus client, GI and
 * command handling from it; the control centre reuses the same structs so
 * both stations agree on IOAs, CA, timers and quality rules without a
 * second copy of the schema.
 *
 * The loader is deliberately strict: an unknown key, a wrong type, or an
 * out-of-range value is an error, never a silent default. null is not
 * accepted where a value is required. pointmap_load returns NULL and fills
 * `err` on any such problem so a program can exit non-zero with a clear
 * message instead of booting on a half-understood configuration.
 */
#ifndef RD_IEC104_POINTMAP_H
#define RD_IEC104_POINTMAP_H

#include <stdbool.h>
#include <stddef.h>

#define PM_MAX_DEVICES 8
#define PM_MAX_POINTS 64
#define PM_MAX_COMMANDS 16
#define PM_MAX_SYNC_STATUS 8
#define PM_ID_LEN 64
#define PM_ASDU_LEN 16
#define PM_UNIT_LEN 16
#define PM_URL_LEN 256
#define PM_STATUS_LEN 16

typedef enum { PM_TABLE_INPUT, PM_TABLE_HOLDING } PmTable;

/* How to read a Modbus register (or bit of it) into a point value. */
typedef enum {
    PM_FMT_DP,  /* register holds a double-point code: 0 intermediate, 1 open, 2 closed, 3 indeterminate */
    PM_FMT_BIT, /* one bit of the register, selected by `bit` */
    PM_FMT_U16, /* one unsigned register, optionally scaled into a float */
    PM_FMT_I16, /* one signed register, optionally scaled into a float */
    PM_FMT_STEP, /* signed VTI plus a separate transient bit */
    PM_FMT_F32  /* two registers, IEEE-754 float, high word first */
} PmFormat;

typedef enum {
    PM_KIND_DOUBLE_POINT, PM_KIND_SINGLE_POINT, PM_KIND_FLOAT,
    PM_KIND_DOUBLE_COMMAND, PM_KIND_STEP_POSITION, PM_KIND_REGULATING_STEP, PM_KIND_SINGLE_COMMAND
} PmKind;

typedef struct {
    char id[PM_ID_LEN];
    char host[PM_ID_LEN];
    int port;
    int unit;
    int poll_ms;
    int timeout_ms;
    int stale_after_ms;
    PmTable poll_table;
    int poll_start, poll_count;
    bool has_heartbeat;
    int heartbeat_address, heartbeat_stale_ms;
    bool has_measurements_valid;
    PmTable mv_table;
    int mv_address;
    int mv_bit;
} PmDevice;

typedef struct {
    char id[PM_ID_LEN];
    int ioa;
    PmKind kind;
    char asdu[PM_ASDU_LEN];
    bool measurement; /* measurement:true points go IV when the device valid bit is 0 */
    char unit[PM_UNIT_LEN];
    int cyclic_ms; /* required for float points */
    double deadband; /* floats only: value-only changes smaller than this are suppressed */
    bool has_deadband;

    int device_index; /* resolved index into PointMap.devices */
    PmTable table;
    int address;
    PmFormat format;
    int bit; /* for PM_FMT_BIT */
    double scale;
    int transient_address, transient_bit;
} PmPoint;

typedef struct {
    char id[PM_ID_LEN];
    int ioa;
    PmKind kind;
    char asdu[PM_ASDU_LEN];
    bool sbo;

    int device_index; /* resolved index into PointMap.devices */
    PmTable table;
    int address;

    char feedback_id[PM_ID_LEN];
    int feedback_index; /* resolved index into PointMap.points */
} PmCommand;

typedef struct {
    int schema;
    int ca;

    struct {
        int t0_s, t1_s, t2_s, t3_s, k, w;
    } timers;

    int select_timeout_ms;
    int reconnect_interval_ms;
    int gi_interval_ms;

    struct {
        char url[PM_URL_LEN];
        int poll_ms;
        int stale_after_ms;
        char synced_status[PM_MAX_SYNC_STATUS][PM_STATUS_LEN];
        int n_synced;
    } clock;

    PmDevice devices[PM_MAX_DEVICES];
    int n_devices;
    PmPoint points[PM_MAX_POINTS];
    int n_points;
    PmCommand commands[PM_MAX_COMMANDS];
    int n_commands;
} PointMap;

/*
 * Loads and validates a point map from `path`. On success returns a heap
 * PointMap the caller frees with pointmap_free. On any error returns NULL
 * and writes a human-readable reason (with the offending key where known)
 * into err[0..errLen).
 */
PointMap* pointmap_load(const char* path, char* err, size_t errLen);

void pointmap_free(PointMap* pm);

/* Lookups by index resolution; return NULL if not found. */
const PmPoint* pointmap_point_by_ioa(const PointMap* pm, int ioa);
const PmCommand* pointmap_command_by_ioa(const PointMap* pm, int ioa);

/* True if the clock status string counts as synced for this map. */
bool pointmap_status_synced(const PointMap* pm, const char* status);

#endif
