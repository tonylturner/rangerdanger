/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * cc_http: a minimal in-tree HTTP/1.1 server for the control centre API.
 * GET and POST only, Content-Length bodies only (no chunked encoding), one
 * request per connection (Connection: close), bounded header and body
 * sizes, read/write timeouts, and a cap on concurrent connections. Each
 * accepted connection runs on its own detached thread so a synchronous
 * command (up to 10 s) never blocks state reads.
 */
#ifndef RD_IEC104_CC_HTTP_H
#define RD_IEC104_CC_HTTP_H

#include <stdbool.h>
#include <stddef.h>

#define CC_HTTP_MAX_HEADER 8192
#define CC_HTTP_MAX_BODY 4096
#define CC_HTTP_MAX_CONNECTIONS 32
#define CC_HTTP_IO_TIMEOUT_S 5

typedef struct {
    const char* method; /* "GET" or "POST" */
    const char* path;   /* without the query */
    const char* query;  /* after '?', "" when absent */
    const char* body;   /* NUL-terminated; "" when absent */
    size_t bodyLen;
} CcHttpRequest;

typedef struct {
    int status;
    char* body;        /* malloc'd JSON text, freed by the server */
    const char* allow; /* Allow header for 405, or NULL */
} CcHttpResponse;

typedef void (*CcHttpHandler)(const CcHttpRequest* req, CcHttpResponse* resp);

/*
 * Binds host:port (IPv4 literal) and starts the accept thread. A failed
 * bind returns false with err set; the caller treats it as fatal.
 */
bool cc_http_start(const char* host, int port, CcHttpHandler handler, char* err, size_t errLen);

/* Sets resp to {"error": text} with the status. */
void cc_http_error(CcHttpResponse* resp, int status, const char* text);

#endif
