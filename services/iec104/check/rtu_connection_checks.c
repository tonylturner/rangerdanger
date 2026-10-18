/* SPDX-License-Identifier: GPL-3.0-or-later */
/* Exercise the real server callback, including reset/lifetime boundaries. */
#include <assert.h>
#include <stdatomic.h>
#include <stdio.h>
#include <string.h>
#include "rtu_internal.h"

RtuState rtu;
static CS104_ConnectionEventHandler handler;
static void* handlerParam;
static int handles[MAX_CONNECTIONS + 1];
static bool addressAvailable = true;
static int generation, frames;
static atomic_bool closing, closed;

void
__wrap_CS104_Slave_setConnectionEventHandler(CS104_Slave slave,
                                            CS104_ConnectionEventHandler fn, void* param)
{
    (void)slave;
    handler = fn;
    handlerParam = param;
}

int
__wrap_IMasterConnection_getPeerAddress(IMasterConnection conn, char* buf, int size)
{
    (void)conn;
    if (!addressAvailable) return 0; /* getpeername after TCP RST */
    return snprintf(buf, (size_t)size, "192.0.2.1:%d", 10000 + generation);
}

bool
__wrap_IMasterConnection_sendASDU(IMasterConnection conn, CS101_ASDU asdu)
{
    (void)asdu;
    assert(conn == (IMasterConnection)&handles[0]);
    assert(!atomic_load(&closed)); /* no send after CLOSED returns */
    frames++;
    return true;
}

bool sc_now(StationClock* clock, uint64_t* ms) { (void)clock; *ms = 0; return true; }
void rtu_clear_selection(const char* why) { (void)why; rtu.sel = (Selection){0}; }
void
rtu_handle_command(IMasterConnection conn, const char* peer, CS101_ASDU asdu,
                   int index, int dcs, int qu, bool select)
{
    (void)conn; (void)peer; (void)asdu; (void)index; (void)dcs; (void)qu; (void)select;
    assert(false); /* these checks never inject commands */
}

static void
event(int slot, CS104_PeerConnectionEvent kind)
{
    handler(handlerParam, (IMasterConnection)&handles[slot], kind);
}

static void*
close_connection(void* unused)
{
    (void)unused;
    atomic_store(&closing, true);
    event(0, CS104_CON_EVENT_CONNECTION_CLOSED);
    atomic_store(&closed, true);
    return NULL;
}

int
main(void)
{
    static PointMap pm;
    pm.ca = 1;
    pm.n_points = 1;
    pm.points[0].kind = PM_KIND_FLOAT;
    pm.points[0].ioa = 1;
    rtu.pm = &pm;
    pthread_mutex_init(&rtu.lock, NULL);
    rtu_server_init("127.0.0.1", 2404);

    for (generation = 0; generation < 32; generation++) {
        addressAvailable = true;
        atomic_store(&closed, false);
        event(0, CS104_CON_EVENT_CONNECTION_OPENED);
        event(0, CS104_CON_EVENT_ACTIVATED);
        event(0, CS104_CON_EVENT_ACTIVATED); /* idempotent STARTDT */
        assert(rtu.nStarted == 1);
        char peer[PEER_LEN];
        snprintf(peer, sizeof(peer), "%s", rtu.started[0].peer);
        assert(rtu_conn_for_peer(peer) == (IMasterConnection)&handles[0]);
        frames = 0;
        rtu_report_point(0, CS101_COT_SPONTANEOUS);
        assert(frames == 1);
        rtu.sel.active = rtu.pend.active = true;
        snprintf(rtu.sel.peer, PEER_LEN, "%s", peer);
        snprintf(rtu.pend.peer, PEER_LEN, "%s", peer);
        addressAvailable = false;
        event(0, CS104_CON_EVENT_CONNECTION_CLOSED);
        atomic_store(&closed, true);
        assert(rtu.nStarted == 0 && rtu.started[0].conn == NULL);
        assert(!rtu.sel.active && rtu.pend.peer[0] == '\0');
        assert(rtu_conn_for_peer(peer) == NULL);
        frames = 0;
        rtu_report_point(0, CS101_COT_SPONTANEOUS);
        assert(frames == 0);
    }
    puts("PASS 32 resets with lost peer addresses and recycled handles: one report, synchronous retirement");

    addressAvailable = true;
    event(0, CS104_CON_EVENT_ACTIVATED);
    addressAvailable = false;
    event(0, CS104_CON_EVENT_DEACTIVATED);
    assert(rtu.nStarted == 0);
    event(0, CS104_CON_EVENT_CONNECTION_CLOSED);
    assert(rtu.nStarted == 0);
    puts("PASS STOPDT retires by identity even without a peer address; subsequent CLOSED is harmless");

    addressAvailable = true;
    atomic_store(&closed, false);
    event(0, CS104_CON_EVENT_ACTIVATED);
    pthread_mutex_lock(&rtu.lock);
    pthread_t thread;
    assert(pthread_create(&thread, NULL, close_connection, NULL) == 0);
    while (!atomic_load(&closing)) {}
    assert(!atomic_load(&closed));
    frames = 0;
    rtu_report_point(0, CS101_COT_SPONTANEOUS);
    assert(frames == 1);
    pthread_mutex_unlock(&rtu.lock);
    assert(pthread_join(thread, NULL) == 0);
    assert(atomic_load(&closed) && rtu.nStarted == 0);
    puts("PASS CLOSED cannot return/recycle a handle while a locked sender is using it");

    for (int i = 0; i <= MAX_CONNECTIONS; i++) {
        generation++;
        event(i, CS104_CON_EVENT_ACTIVATED);
    }
    assert(rtu.nStarted == MAX_CONNECTIONS);
    for (int i = 0; i <= MAX_CONNECTIONS; i++)
        event(i, CS104_CON_EVENT_CONNECTION_CLOSED);
    assert(rtu.nStarted == 0);
    puts("PASS full started list is logged and never overflows");
    CS104_Slave_destroy(rtu.slave);
    pthread_mutex_destroy(&rtu.lock);
    return 0;
}
