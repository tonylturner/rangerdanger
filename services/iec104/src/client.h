/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * Synchronous wrapper over a lib60870 CS104_Connection for the control
 * centre and iec104cmd: send a request, then wait for the matching
 * confirmation. lib60870 delivers callbacks on its receive thread; this
 * module turns them into waitable state. No lock of ours is held while
 * calling into lib60870, so callbacks can never deadlock against us.
 */
#ifndef RD_IEC104_CLIENT_H
#define RD_IEC104_CLIENT_H

#include <pthread.h>
#include <stdbool.h>

#include "cs104_connection.h"

#define RD_RESPONSES 32

/* A confirmation or termination of a request we sent (COT 7, 9, 10 or 44-47). */
typedef struct {
    TypeID type;
    int cot;
    bool negative;
    int ioa;
} RdResponse;

typedef void (*RdAsduCallback)(void* param, CS101_ASDU asdu);

typedef struct {
    CS104_Connection con;
    RdAsduCallback onAsdu;
    void* onAsduParam;

    pthread_mutex_t lock;
    pthread_cond_t cond;
    bool closed;
    bool startdtCon;
    bool stopdtCon;
    RdResponse responses[RD_RESPONSES];
    unsigned long nResponses; /* total ever received; ring index is n % RD_RESPONSES */
} RdClient;

typedef struct {
    int t0, t1, t2, t3; /* seconds; 0 keeps the lib60870 default */
    int oa;
} RdClientOptions;

/* onAsdu sees every received ASDU, after responses have been recorded. */
void rd_client_init(RdClient* c, const char* host, int port, const RdClientOptions* opt, RdAsduCallback onAsdu,
                    void* param);
void rd_client_destroy(RdClient* c);

bool rd_client_connect(RdClient* c);
bool rd_client_is_closed(RdClient* c);

bool rd_client_startdt(RdClient* c, int timeoutMs);
bool rd_client_stopdt(RdClient* c, int timeoutMs);

/* Cursor for rd_client_wait_response: responses after this point are new. */
unsigned long rd_client_cursor(RdClient* c);

/*
 * Waits for the next response of `type` after *cursor whose COT is one of
 * `cots` (terminated by 0). Advances *cursor past what it consumed. False on
 * timeout or link loss.
 */
bool rd_client_wait_response(RdClient* c, unsigned long* cursor, TypeID type, const int* cots, int timeoutMs,
                             RdResponse* out);

/* COT sets for rd_client_wait_response. */
extern const int RD_COT_CONFIRM[];     /* 7, 9 and the 44-47 rejections */
extern const int RD_COT_TERMINATION[]; /* 10 */

#endif
