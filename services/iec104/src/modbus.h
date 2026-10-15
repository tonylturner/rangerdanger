/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * A minimal, in-tree Modbus TCP master, enough for the RTU's southbound
 * links: read holding/input registers (FC3/FC4) and write a
 * single holding register (FC6). No external Modbus library, so the GPLv3
 * image carries no extra dependency. Big-endian on the wire, as Modbus
 * requires. One request/response per call; the caller owns timing and
 * never retries a write (contract §3.2).
 */
#ifndef RD_IEC104_MODBUS_H
#define RD_IEC104_MODBUS_H

#include <stdbool.h>
#include <stdint.h>

typedef enum {
    MB_OK = 0,
    MB_TIMEOUT,      /* no (complete) reply within the deadline */
    MB_IO_ERROR,     /* connect/send/recv failed or the frame was malformed */
    MB_EXCEPTION     /* the slave returned a Modbus exception; see exceptionCode */
} MbStatus;

struct addrinfo;
typedef struct {
    struct addrinfo* addresses;
    int fd;          /* -1 when not connected */
    uint16_t txid;   /* next MBAP transaction id */
    int unit;
} MbMaster;

/* Initialises to the disconnected state; call mb_connect before use. */
void mb_init(MbMaster* m, int unit);

/* Resolve on the owning worker, separately from the bounded socket connect.
 * DNS cannot delay another device, and reconnect never invokes the resolver. */
MbStatus mb_resolve(MbMaster* m, const char* host, int port);
MbStatus mb_connect(MbMaster* m, int timeoutMs);
void mb_destroy(MbMaster* m);

void mb_close(MbMaster* m);
bool mb_is_connected(const MbMaster* m);

/*
 * FC3/FC4: read `count` holding/input registers starting at `addr` into out[0..count).
 * On MB_EXCEPTION, *exceptionCode holds the slave's exception code.
 */
MbStatus mb_read_holding(MbMaster* m, int addr, int count, uint16_t* out, int* exceptionCode, int timeoutMs);
MbStatus mb_read_input(MbMaster* m, int addr, int count, uint16_t* out, int* exceptionCode, int timeoutMs);

/*
 * FC6: write `value` to holding register `addr`. The slave echoes address
 * and value; a mismatch is reported as MB_IO_ERROR.
 */
MbStatus mb_write_single(MbMaster* m, int addr, uint16_t value, int* exceptionCode, int timeoutMs);

/* Reassembles an IEEE-754 float from two registers, high word first. */
float mb_regs_to_f32(uint16_t hi, uint16_t lo);

#endif
