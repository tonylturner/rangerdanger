/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 */
#include "common.h"

#include <errno.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "cs101_information_objects.h"
#include "hal_time.h"

static const char* logProgram = "iec104";

void
rd_log_init(const char* program)
{
    logProgram = program;
    /* Logs are evidence for the check script; never let them sit in a buffer. */
    setvbuf(stdout, NULL, _IOLBF, 0);
}

void
rd_log(const char* fmt, ...)
{
    uint64_t ms = Hal_getTimeInMs();
    time_t secs = (time_t)(ms / 1000);
    struct tm tm;
    char stamp[32];

    gmtime_r(&secs, &tm);
    strftime(stamp, sizeof(stamp), "%Y-%m-%dT%H:%M:%S", &tm);

    char msg[1024];
    va_list ap;
    va_start(ap, fmt);
    vsnprintf(msg, sizeof(msg), fmt, ap);
    va_end(ap);

    printf("%s.%03uZ %s %s\n", stamp, (unsigned)(ms % 1000), logProgram, msg);
}

const char*
rd_quality_str(QualityDescriptor q, char* buf, size_t len)
{
    static const struct {
        QualityDescriptor bit;
        const char* name;
    } bits[] = {
        {IEC60870_QUALITY_INVALID, "IV"},     {IEC60870_QUALITY_NON_TOPICAL, "NT"},
        {IEC60870_QUALITY_SUBSTITUTED, "SB"}, {IEC60870_QUALITY_BLOCKED, "BL"},
        {IEC60870_QUALITY_OVERFLOW, "OV"},
    };

    if (q == IEC60870_QUALITY_GOOD) {
        snprintf(buf, len, "GOOD");
        return buf;
    }

    buf[0] = '\0';
    for (size_t i = 0; i < sizeof(bits) / sizeof(bits[0]); i++) {
        if (q & bits[i].bit) {
            size_t used = strlen(buf);
            snprintf(buf + used, len - used, "%s%s", used ? "|" : "", bits[i].name);
        }
    }
    return buf;
}

const char*
rd_dpi_str(int dpi)
{
    switch (dpi) {
    case IEC60870_DOUBLE_POINT_INTERMEDIATE:
        return "INTERMEDIATE";
    case IEC60870_DOUBLE_POINT_OFF:
        return "OFF";
    case IEC60870_DOUBLE_POINT_ON:
        return "ON";
    default:
        return "INDETERMINATE";
    }
}

const char*
rd_cp56_str(CP56Time2a t, char* buf, size_t len)
{
    uint64_t ms = CP56Time2a_toMsTimestamp(t);
    time_t secs = (time_t)(ms / 1000);
    struct tm tm;
    char stamp[32];

    gmtime_r(&secs, &tm);
    strftime(stamp, sizeof(stamp), "%Y-%m-%dT%H:%M:%S", &tm);
    snprintf(buf, len, "%s.%03uZ%s", stamp, (unsigned)(ms % 1000), CP56Time2a_isInvalid(t) ? "(IV)" : "");
    return buf;
}

static void
describe_io(TypeID type, InformationObject io, char* buf, size_t len)
{
    char q[32];
    char ts[48];

    switch (type) {
    case M_DP_NA_1: {
        DoublePointInformation dp = (DoublePointInformation)io;
        snprintf(buf, len, "value=%s q=%s", rd_dpi_str(DoublePointInformation_getValue(dp)),
                 rd_quality_str(DoublePointInformation_getQuality(dp), q, sizeof(q)));
        break;
    }
    case M_DP_TB_1: {
        DoublePointWithCP56Time2a dp = (DoublePointWithCP56Time2a)io;
        snprintf(buf, len, "value=%s q=%s time=%s",
                 rd_dpi_str(DoublePointInformation_getValue((DoublePointInformation)dp)),
                 rd_quality_str(DoublePointInformation_getQuality((DoublePointInformation)dp), q, sizeof(q)),
                 rd_cp56_str(DoublePointWithCP56Time2a_getTimestamp(dp), ts, sizeof(ts)));
        break;
    }
    case M_ME_NC_1: {
        MeasuredValueShort mv = (MeasuredValueShort)io;
        snprintf(buf, len, "value=%.3f q=%s", (double)MeasuredValueShort_getValue(mv),
                 rd_quality_str(MeasuredValueShort_getQuality(mv), q, sizeof(q)));
        break;
    }
    case C_DC_NA_1: {
        DoubleCommand dc = (DoubleCommand)io;
        snprintf(buf, len, "dcs=%d(%s) qu=%d se=%s", DoubleCommand_getState(dc),
                 rd_dpi_str(DoubleCommand_getState(dc)), DoubleCommand_getQU(dc),
                 DoubleCommand_isSelect(dc) ? "SELECT" : "EXECUTE");
        break;
    }
    case C_SC_NA_1: {
        SingleCommand sc = (SingleCommand)io;
        snprintf(buf, len, "scs=%s qu=%d se=%s", SingleCommand_getState(sc) ? "ON" : "OFF", SingleCommand_getQU(sc),
                 SingleCommand_isSelect(sc) ? "SELECT" : "EXECUTE");
        break;
    }
    case C_IC_NA_1:
        snprintf(buf, len, "qoi=%d", InterrogationCommand_getQOI((InterrogationCommand)io));
        break;
    default:
        snprintf(buf, len, "(no decoder for this type in the spike)");
        break;
    }
}

void
rd_log_asdu(const char* direction, CS101_ASDU asdu)
{
    TypeID type = CS101_ASDU_getTypeID(asdu);
    CS101_CauseOfTransmission cot = CS101_ASDU_getCOT(asdu);
    int n = CS101_ASDU_getNumberOfElements(asdu);

    rd_log("%s ASDU type=%s(%d) cot=%s(%d) neg=%d test=%d oa=%d ca=%d objects=%d", direction, TypeID_toString(type),
           (int)type, CS101_CauseOfTransmission_toString(cot), (int)cot, CS101_ASDU_isNegative(asdu) ? 1 : 0,
           CS101_ASDU_isTest(asdu) ? 1 : 0, CS101_ASDU_getOA(asdu), CS101_ASDU_getCA(asdu), n);

    for (int i = 0; i < n; i++) {
        InformationObject io = CS101_ASDU_getElement(asdu, i);
        if (io == NULL) {
            rd_log("%s   object %d does not decode", direction, i);
            continue;
        }
        char text[160];
        describe_io(type, io, text, sizeof(text));
        rd_log("%s   ioa=%d %s", direction, InformationObject_getObjectAddress(io), text);
        InformationObject_destroy(io);
    }
}

bool
rd_parse_int(const char* s, long min, long max, long* out)
{
    char* end = NULL;
    errno = 0;
    long v = strtol(s, &end, 10);
    if (errno != 0 || end == s || *end != '\0' || v < min || v > max)
        return false;
    *out = v;
    return true;
}

bool
rd_parse_endpoint(const char* arg, char* host, size_t hostLen, int* port)
{
    const char* colon = strrchr(arg, ':');
    size_t n = colon ? (size_t)(colon - arg) : strlen(arg);

    if (n == 0 || n >= hostLen)
        return false;
    memcpy(host, arg, n);
    host[n] = '\0';

    *port = IEC_60870_5_104_DEFAULT_PORT;
    if (colon) {
        long p;
        if (!rd_parse_int(colon + 1, 1, 65535, &p))
            return false;
        *port = (int)p;
    }
    return true;
}
