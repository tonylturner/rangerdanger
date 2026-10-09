/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * Point map of the spike RTU. Increment 3 moves this into package data
 * (lab-definitions/packages/<id>/protocols/); until then every program
 * reads the same constants so the RTU, control centre and student tool
 * cannot disagree.
 */
#ifndef RD_IEC104_POINTS_H
#define RD_IEC104_POINTS_H

#define RD_CA 1
#define RD_CA_AUX 2

/* Feeder breaker position: M_DP_NA_1 in GI, M_DP_TB_1 when it changes. */
#define RD_IOA_BREAKER 1001
/* Feeder current in A and busbar voltage in kV: M_ME_NC_1. */
#define RD_IOA_CURRENT 2001
#define RD_IOA_VOLTAGE 2002
/* Breaker control: C_DC_NA_1, select-before-execute only. */
#define RD_IOA_BREAKER_CMD 3001

/* Boundary-address test points: M_ME_NC_1 on CA 1. */
#define RD_IOA_EDGE_16BIT_MAX 65535
#define RD_IOA_EDGE_16BIT_OVERFLOW 65536
#define RD_IOA_EDGE_24BIT_MAX 16777215

/* Auxiliary-station test point: M_ME_NC_1 on CA 2. */
#define RD_IOA_AUX_FREQUENCY 4001

#endif
