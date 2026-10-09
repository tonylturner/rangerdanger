/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 */
#include "cc_api.h"

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cJSON.h"
#include "cc_command.h"
#include "cc_state.h"

static void
respond_json(CcHttpResponse* resp, int status, cJSON* body)
{
    resp->status = status;
    resp->body = cJSON_PrintUnformatted(body);
    cJSON_Delete(body);
}

static bool
no_query(const CcHttpRequest* req, CcHttpResponse* resp)
{
    if (req->query[0] == '\0')
        return true;
    cc_http_error(resp, 400, "this endpoint takes no query parameters");
    return false;
}

static void
route_health(const CcHttpRequest* req, CcHttpResponse* resp)
{
    if (no_query(req, resp))
        respond_json(resp, 200, cc_render_health());
}

static void
route_state(const CcHttpRequest* req, CcHttpResponse* resp)
{
    if (no_query(req, resp))
        respond_json(resp, 200, cc_render_state());
}

static void
route_journal(const CcHttpRequest* req, CcHttpResponse* resp)
{
    uint64_t after = 0;
    if (req->query[0]) {
        const char* v = strncmp(req->query, "after=", 6) == 0 ? req->query + 6 : NULL;
        bool ok = v && *v && strlen(v) <= 19;
        for (const char* p = v; ok && *p; p++)
            ok = *p >= '0' && *p <= '9';
        if (!ok) {
            cc_http_error(resp, 400, "the only query parameter is after=<non-negative integer>");
            return;
        }
        after = strtoull(v, NULL, 10);
    }
    respond_json(resp, 200, cc_render_journal(after));
}

/* Strict body: exactly {"point": <command id>, "operation": "open"|"close"}. */
static void
route_commands(const CcHttpRequest* req, CcHttpResponse* resp)
{
    if (!no_query(req, resp))
        return;
    if (strlen(req->body) != req->bodyLen) {
        cc_http_error(resp, 400, "request body contains a NUL byte");
        return;
    }
    cJSON* root = cJSON_ParseWithLengthOpts(req->body, req->bodyLen + 1, NULL, true);
    if (!root || !cJSON_IsObject(root)) {
        cJSON_Delete(root);
        cc_http_error(resp, 400, "request body must be a JSON object");
        return;
    }

    const cJSON* point = NULL;
    const cJSON* operation = NULL;
    char msg[160] = "";
    for (const cJSON* c = root->child; c && !msg[0]; c = c->next) {
        const cJSON** slot = strcmp(c->string, "point") == 0 ? &point
                             : strcmp(c->string, "operation") == 0 ? &operation
                                                                   : NULL;
        if (!slot)
            snprintf(msg, sizeof(msg), "unknown field \"%.64s\"", c->string);
        else if (*slot)
            snprintf(msg, sizeof(msg), "duplicate field \"%s\"", c->string);
        else if (!cJSON_IsString(c))
            snprintf(msg, sizeof(msg), "\"%s\" must be a string", c->string);
        else
            *slot = c;
    }
    if (!msg[0] && (!point || !operation))
        snprintf(msg, sizeof(msg), "\"point\" and \"operation\" are required");

    int cmdIndex = -1;
    if (!msg[0]) {
        for (int i = 0; i < cc.pm->n_commands; i++)
            if (strcmp(cc.pm->commands[i].id, point->valuestring) == 0)
                cmdIndex = i;
        if (cmdIndex < 0)
            snprintf(msg, sizeof(msg), "unknown command point \"%.64s\"", point->valuestring);
    }
    bool closeBreaker = false;
    if (!msg[0]) {
        if (strcmp(operation->valuestring, "close") == 0)
            closeBreaker = true;
        else if (strcmp(operation->valuestring, "open") != 0)
            snprintf(msg, sizeof(msg), "\"operation\" must be \"open\" or \"close\"");
    }
    cJSON_Delete(root);
    if (msg[0]) {
        cc_http_error(resp, 400, msg);
        return;
    }

    cJSON* body = NULL;
    int status = cc_command_run(cmdIndex, closeBreaker, &body);
    respond_json(resp, status, body);
}

typedef struct {
    const char* path;
    const char* method;
    void (*fn)(const CcHttpRequest*, CcHttpResponse*);
} Route;

static const Route routes[] = {
    {"/api/health", "GET", route_health},
    {"/api/state", "GET", route_state},
    {"/api/journal", "GET", route_journal},
    {"/api/commands", "POST", route_commands},
};

void
cc_api_handle(const CcHttpRequest* req, CcHttpResponse* resp)
{
    for (size_t i = 0; i < sizeof(routes) / sizeof(routes[0]); i++) {
        if (strcmp(req->path, routes[i].path) != 0)
            continue;
        if (strcmp(req->method, routes[i].method) != 0) {
            resp->allow = routes[i].method;
            cc_http_error(resp, 405, "method not allowed");
            return;
        }
        routes[i].fn(req, resp);
        return;
    }
    cc_http_error(resp, 404, "not found");
}
