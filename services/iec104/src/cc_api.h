/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * cc_api: the control centre HTTP routes (contract §6):
 *   GET  /api/health
 *   GET  /api/state
 *   GET  /api/journal?after=<seq>
 *   POST /api/commands {"point","operation"}
 * Unknown paths 404, wrong method 405, unknown query parameters or request
 * fields 400.
 */
#ifndef RD_IEC104_CC_API_H
#define RD_IEC104_CC_API_H

#include "cc_http.h"

void cc_api_handle(const CcHttpRequest* req, CcHttpResponse* resp);

#endif
