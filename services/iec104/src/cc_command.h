/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * cc_command: the control centre's select-before-execute state machine for
 * POST /api/commands (contract §6). One command at a time, synchronous,
 * at most CC_COMMAND_BUDGET_MS, never retried. The outcome is correlated
 * from the select and execute ACT-CONs, the ACT-TERM and the COT 11
 * feedback report the receive thread records in cc.tracker.
 */
#ifndef RD_IEC104_CC_COMMAND_H
#define RD_IEC104_CC_COMMAND_H

#include <stdbool.h>

#include "cJSON.h"

#define CC_COMMAND_BUDGET_MS 10000

/*
 * Runs the typed operation at cc.pm->commands[cmdIndex] with its wire value.
 * Returns the HTTP status (200 with the command result; 409 while another
 * command is in flight or feedback is stale; 503 when the link is not started) and sets *body.
 */
int cc_command_run(int cmdIndex, int value, const char* operation, cJSON** body);

#endif
