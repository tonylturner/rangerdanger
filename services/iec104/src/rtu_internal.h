/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 */

/* Private RTU model and module interfaces.
 *
 * rtu.lock guards point snapshots, command phases and started connections;
 * shared-state helpers below require it unless documented otherwise. Device
 * workers alone own their Modbus sockets and drop rtu.lock around I/O.
 * Receive callbacks enqueue connection events under evLock, then the main
 * loop applies them under rtu.lock. The station clock owns its own lock.
 */
#ifndef RD_IEC104_RTU_INTERNAL_H
#define RD_IEC104_RTU_INTERNAL_H

#include <pthread.h>
#include <signal.h>
#include <stdbool.h>
#include <stdint.h>

#include "cs104_slave.h"
#include "hal_time.h"
#include "modbus.h"
#include "pointmap.h"
#include "stationclock.h"

#define MAX_CONNECTIONS 8
#define EVENT_QUEUE 64
#define PEER_LEN 64

/* Per-point runtime: raw value, causes, and the last value/quality reported. */
typedef struct {
    DoublePointValue dp;
    bool sp;
    float f;
    bool deviceUnreachable;
    bool measurementInvalid;
    bool valueInvalid, heartbeatStale;
    int step, repStep;
    bool transient, repTransient;

    QualityDescriptor repQuality;
    DoublePointValue repDp;
    bool repSp;
    float repF;
    uint64_t nextCyclic, lastValueEvent;
} PointRT;

typedef struct {
    MbMaster mb;
    uint64_t lastOkMono; /* monotonic ms of the last successful poll; 0 = never */
    uint64_t lastPollStart;
    bool unreachable;
    uint64_t pollSeq;
    uint64_t heartbeatChanged;
    uint16_t heartbeat;
    pthread_t thread;
    int index;
} DeviceRT;

/* Command responses are addressed by peer via rtu_conn_for_peer, not a stored handle. */
typedef struct {
    bool active;
    int cmdIndex;
    char peer[PEER_LEN];
    int dcs;
    int qu;
    uint64_t deadlineMono;
    uint64_t pollSeq, selectedMono;
} Selection;

typedef struct {
    bool active;
    int cmdIndex;
    char peer[PEER_LEN];
    int dcs;
    CS101_ASDU command;
    enum { CMD_START, CMD_WRITE, CMD_WRITING, CMD_FEEDBACK } phase;
    uint64_t afterPoll, afterMono, deadline;
    int expected;
} PendingExecute;

typedef struct {
    IMasterConnection conn;
    char peer[PEER_LEN];
    CS104_PeerConnectionEvent event;
} ConnEvent;

typedef struct {
    char peer[PEER_LEN];
    IMasterConnection conn;
} Started;

typedef struct {
    pthread_mutex_t lock;
    CS104_Slave slave;
    CS101_AppLayerParameters al;
    const PointMap* pm;
    StationClock clock;

    PointRT points[PM_MAX_POINTS];
    DeviceRT devices[PM_MAX_DEVICES];

    Selection sel;
    PendingExecute pend;

    Started started[MAX_CONNECTIONS];
    int nStarted;

    pthread_mutex_t evLock;
    ConnEvent events[EVENT_QUEUE];
    int nEvents;
} RtuState;

extern RtuState rtu;
extern volatile sig_atomic_t rtu_stop_requested;

static inline uint64_t
rtu_mono_ms(void)
{
    return Hal_getMonotonicTimeInMs();
}

/* Server setup, before receive threads start; does not take rtu.lock. */
void rtu_server_init(const char* bind, int port);

/* Uses only the caller's ASDU and connection; does not require rtu.lock. */
void rtu_reply_negative(IMasterConnection conn, CS101_ASDU asdu, CS101_CauseOfTransmission cot);

/* Server/reporting helpers: caller holds rtu.lock. */
QualityDescriptor rtu_point_quality(const PointRT* rt, const PmPoint* p);
IMasterConnection rtu_conn_for_peer(const char* peerStr);
void rtu_report_point(int idx, CS101_CauseOfTransmission cot);
void rtu_evaluate_and_report(void);
void rtu_report_cyclic(uint64_t now);
void rtu_report_gi(IMasterConnection conn, int oa);
void rtu_apply_connection_events(void);

/* Command helpers: caller holds rtu.lock. */
void rtu_clear_selection(const char* why);
void rtu_handle_command(IMasterConnection conn, const char* peerStr, CS101_ASDU asdu,
                       int cmdIndex, int dcs, int qu, bool select);
void rtu_advance_command(void);

/* Target device worker only; takes rtu.lock, releases it around FC6 I/O. */
void rtu_write_command(int d);

/* Thread entry point; owns its socket and locks to publish poll snapshots. */
void* rtu_device_worker(void* arg);

#endif
