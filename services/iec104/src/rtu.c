/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * iec104-rtu: the substation RTU / gateway, an IEC 104 controlled station
 * (server) built on lib60870-C. Unlike the increment-2b spike, this RTU has
 * no fake physics: every value comes from a real Modbus TCP poll of the
 * configured field devices, every time tag comes from the GPS-fed station clock, and
 * every command is executed as a Modbus write whose readback produces the
 * COT 11 feedback report. The point set, CA, timers and command mapping are
 * loaded from the package point map (contract §3.1); nothing is hard-coded.
 *
 * Threads:
 *   - lib60870's own receive threads call the handlers in
 *     rtu_server.c (interrogation, commands and connection events).
 *   - the station clock polls gps-sim on its own thread (stationclock.c).
 *   - independent device workers own their Modbus sockets and publish poll
 *     snapshots/write outcomes under rtu.lock (rtu_poll.c/rtu_command.c).
 *     The main loop drives command phases, quality and reporting without
 *     waiting for network I/O or motion. Shared state and ASDU sends happen
 *     under rtu.lock.
 *
 * Quality and reporting rules are contract §3.2:
 *   - device_unreachable (no successful poll within stale_after_ms) -> IV+NT.
 *   - measurement_invalid (measurement point while the device valid bit is 0, or register NaN)
 *     -> IV, floats only.
 *   - every quality change is reported spontaneously with the last value;
 *     the float deadband suppresses value-only float changes.
 *   - status spontaneous events use the timed type (CP56Time2a, UTC, IV when
 *     the clock is not synced); a command-caused feedback report is COT 11.
 */
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "common.h"
#include "hal_thread.h"
#include "rtu_internal.h"

RtuState rtu;
volatile sig_atomic_t rtu_stop_requested = 0;

static void
on_signal(int sig)
{
    (void)sig;
    rtu_stop_requested = 1;
}

static void
tick(void)
{
    pthread_mutex_lock(&rtu.lock);
    if (rtu.sel.active && rtu_mono_ms() >= rtu.sel.deadlineMono) rtu_clear_selection("select timeout elapsed");
    rtu_evaluate_and_report();
    rtu_advance_command();
    pthread_mutex_unlock(&rtu.lock);
}

static void
usage(void)
{
    fprintf(stderr, "Usage: iec104-rtu --config PATH [--bind ADDR] [--port N]\n"
                    "\n"
                    "IEC 104 controlled station (server). The point map at --config supplies\n"
                    "CA, timers, the Modbus device, the monitored points and the command set.\n");
    exit(2);
}

int
main(int argc, char** argv)
{
    const char* config = NULL;
    const char* bind = "0.0.0.0";
    long port = IEC_60870_5_104_DEFAULT_PORT;

    for (int i = 1; i < argc; i++) {
        const char* a = argv[i];
        const char* v = (i + 1 < argc) ? argv[i + 1] : NULL;
        if (strcmp(a, "--config") == 0 && v) {
            config = v;
            i++;
        } else if (strcmp(a, "--bind") == 0 && v) {
            bind = v;
            i++;
        } else if (strcmp(a, "--port") == 0 && v && rd_parse_int(v, 1, 65535, &port)) {
            i++;
        } else {
            usage();
        }
    }
    if (!config)
        usage();

    rd_log_init("rtu");

    char err[256];
    PointMap* pm = pointmap_load(config, err, sizeof(err));
    if (!pm) {
        rd_log("config error: %s", err);
        return 1;
    }
    rtu.pm = pm;

    pthread_mutex_init(&rtu.lock, NULL);

    /* seed per-point reported state so the first good poll reports a GOOD transition */
    for (int i = 0; i < pm->n_points; i++) {
        PointRT* rt = &rtu.points[i];
        rt->deviceUnreachable = true;
        rt->repQuality = IEC60870_QUALITY_INVALID | IEC60870_QUALITY_NON_TOPICAL;
        rt->dp = IEC60870_DOUBLE_POINT_INDETERMINATE;
        rt->repDp = IEC60870_DOUBLE_POINT_INDETERMINATE;
    }
    for (int d = 0; d < pm->n_devices; d++) {
        DeviceRT* drt = &rtu.devices[d];
        mb_init(&drt->mb, pm->devices[d].unit);
        drt->index = d;
    }

    sc_init(&rtu.clock, pm);
    sc_start(&rtu.clock);

    struct sigaction sa = {0};
    sa.sa_handler = on_signal;
    sigaction(SIGINT, &sa, NULL);
    sigaction(SIGTERM, &sa, NULL);

    rtu_server_init(bind, (int)port);

    CS104_Slave_start(rtu.slave);
    if (!CS104_Slave_isRunning(rtu.slave)) {
        rd_log("cannot listen on %s:%ld", bind, port);
        CS104_Slave_destroy(rtu.slave);
        sc_stop(&rtu.clock);
        pointmap_free(pm);
        return 1;
    }

    signal(SIGPIPE, SIG_IGN);
    for (int d = 0; d < pm->n_devices; d++) {
        if (pthread_create(&rtu.devices[d].thread, NULL, rtu_device_worker, &rtu.devices[d]) != 0) {
            rd_log("cannot start device worker %d", d);
            rtu_stop_requested = 1;
            for (int j = 0; j < d; j++) pthread_join(rtu.devices[j].thread, NULL);
            CS104_Slave_stop(rtu.slave);
            sc_stop(&rtu.clock);
            return 1;
        }
    }
    rd_log("listening %s:%ld ca=%d points=%d devices=%d select-timeout=%dms", bind, port, pm->ca, pm->n_points,
           pm->n_devices, pm->select_timeout_ms);

    while (!rtu_stop_requested) {
        tick();
        Thread_sleep(20);
    }

    rd_log("stopping");
    for (int d = 0; d < pm->n_devices; d++) pthread_join(rtu.devices[d].thread, NULL);
    CS104_Slave_stop(rtu.slave);
    CS104_Slave_destroy(rtu.slave);
    sc_stop(&rtu.clock);
    pointmap_free(pm);
    return 0;
}
