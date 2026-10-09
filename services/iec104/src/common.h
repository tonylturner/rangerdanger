/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * Helpers shared by the RTU, the control centre and iec104cmd: one log
 * format and one way of printing ASDUs, so the check script and students
 * read the same text from every program.
 */
#ifndef RD_IEC104_COMMON_H
#define RD_IEC104_COMMON_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "iec60870_common.h"

/* Program name used as the log prefix; set once in main. */
void rd_log_init(const char* program);

/* One line on stdout: "<UTC ISO-8601 ms> <program> <message>". */
void rd_log(const char* fmt, ...) __attribute__((format(printf, 1, 2)));

/* "GOOD" or the set quality bits joined with '|', e.g. "IV|NT". */
const char* rd_quality_str(QualityDescriptor q, char* buf, size_t len);

const char* rd_dpi_str(int dpi);

/* UTC ISO-8601 with milliseconds; CP56Time2a carries no zone. */
const char* rd_cp56_str(CP56Time2a t, char* buf, size_t len);

/* Logs the ASDU header and one line per information object. */
void rd_log_asdu(const char* direction, CS101_ASDU asdu);

/* Parses "host" or "host:port"; the port defaults to 2404. */
bool rd_parse_endpoint(const char* arg, char* host, size_t hostLen, int* port);

/* Parses a decimal integer within [min, max]; false on any junk. */
bool rd_parse_int(const char* s, long min, long max, long* out);

#endif
