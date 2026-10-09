/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * cc_link: the control centre's one persistent IEC 104 session to the RTU
 * (contract §6). The link thread connects from the --bind address, sends
 * STARTDT and a station GI after every connect, waits for the session to
 * end and reconnects every reconnect_interval_ms. lib60870's receive thread
 * feeds every frame, report and confirmation into cc_state.
 *
 * All lib60870 sends and the connect/close calls are serialised by one I/O
 * mutex, so a command send never races a reconnect of the shared
 * CS104_Connection; cc.lock is never held across a lib60870 call.
 */
#ifndef RD_IEC104_CC_LINK_H
#define RD_IEC104_CC_LINK_H

#include <signal.h>
#include <stdbool.h>
#include <stddef.h>

/*
 * Creates the connection to host:port from bindAddr (port chosen by the
 * kernel) with the APCI timers of the loaded point map. Fails if bindAddr
 * is not a local IPv4 address.
 */
bool cc_link_init(const char* host, int port, const char* bindAddr, char* err, size_t errLen);

/* Runs the connect / STARTDT / GI / reconnect loop until *stop is set. */
void cc_link_run(volatile sig_atomic_t* stop);

/* Closes the session without journaling a link_down. */
void cc_link_shutdown(void);

/*
 * Sends C_DC_NA_1 (COT 6) with the select or execute bit, only while the
 * link is started on connection connSeq. False if it was not sent.
 */
bool cc_link_send_double_command(int ioa, int dcs, bool select, int connSeq);

#endif
