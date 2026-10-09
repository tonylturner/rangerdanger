/*
 * RangerDanger IEC 60870-5-104 programs
 * Copyright (C) 2026 The RangerDanger contributors
 * SPDX-License-Identifier: GPL-3.0-or-later
 */
#include "modbus.h"

#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <netdb.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <poll.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>

#define MBAP_LEN 7
#define MAX_PDU 253
#define MAX_ADU (MBAP_LEN + MAX_PDU)

void
mb_init(MbMaster* m, int unit)
{
    m->fd = -1;
    m->txid = 0;
    m->unit = unit;
}

bool
mb_is_connected(const MbMaster* m)
{
    return m->fd >= 0;
}

void
mb_close(MbMaster* m)
{
    if (m->fd >= 0)
        close(m->fd);
    m->fd = -1;
}

static uint64_t
now_ms(void)
{
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000 + (uint64_t)(ts.tv_nsec / 1000000);
}

/* Blocking connect with a deadline, so a dropped field link cannot hang the poll loop. */
MbStatus
mb_connect(MbMaster* m, const char* host, int port, int timeoutMs)
{
    mb_close(m);

    char portStr[8];
    snprintf(portStr, sizeof(portStr), "%d", port);
    struct addrinfo hints = {0};
    hints.ai_family = AF_UNSPEC;
    hints.ai_socktype = SOCK_STREAM;
    struct addrinfo* res = NULL;
    if (getaddrinfo(host, portStr, &hints, &res) != 0 || !res)
        return MB_IO_ERROR;

    MbStatus st = MB_IO_ERROR;
    for (struct addrinfo* ai = res; ai; ai = ai->ai_next) {
        int fd = socket(ai->ai_family, ai->ai_socktype, ai->ai_protocol);
        if (fd < 0)
            continue;
        int flags = fcntl(fd, F_GETFL, 0);
        fcntl(fd, F_SETFL, flags | O_NONBLOCK);

        int rc = connect(fd, ai->ai_addr, ai->ai_addrlen);
        if (rc == 0) {
            /* connected immediately */
        } else if (errno == EINPROGRESS) {
            struct pollfd pfd = {fd, POLLOUT, 0};
            if (poll(&pfd, 1, timeoutMs) <= 0) {
                close(fd);
                st = MB_TIMEOUT;
                continue;
            }
            int soerr = 0;
            socklen_t slen = sizeof(soerr);
            if (getsockopt(fd, SOL_SOCKET, SO_ERROR, &soerr, &slen) != 0 || soerr != 0) {
                close(fd);
                continue;
            }
        } else {
            close(fd);
            continue;
        }
        fcntl(fd, F_SETFL, flags);
        int one = 1;
        setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &one, sizeof(one));
        m->fd = fd;
        st = MB_OK;
        break;
    }
    freeaddrinfo(res);
    return st;
}

static bool
send_all(int fd, const uint8_t* buf, size_t len, uint64_t deadline)
{
    size_t sent = 0;
    while (sent < len) {
        int budget = (int)(deadline - now_ms());
        if (budget <= 0)
            return false;
        struct pollfd pfd = {fd, POLLOUT, 0};
        if (poll(&pfd, 1, budget) <= 0)
            return false;
        ssize_t n = send(fd, buf + sent, len - sent, 0);
        if (n <= 0) {
            if (n < 0 && (errno == EINTR || errno == EAGAIN))
                continue;
            return false;
        }
        sent += (size_t)n;
    }
    return true;
}

static bool
recv_exact(int fd, uint8_t* buf, size_t len, uint64_t deadline)
{
    size_t got = 0;
    while (got < len) {
        int budget = (int)(deadline - now_ms());
        if (budget <= 0)
            return false;
        struct pollfd pfd = {fd, POLLIN, 0};
        if (poll(&pfd, 1, budget) <= 0)
            return false;
        ssize_t n = recv(fd, buf + got, len - got, 0);
        if (n == 0)
            return false; /* peer closed */
        if (n < 0) {
            if (errno == EINTR || errno == EAGAIN)
                continue;
            return false;
        }
        got += (size_t)n;
    }
    return true;
}

/* Sends one MBAP+PDU request and reads the matching response PDU. */
static MbStatus
transact(MbMaster* m, const uint8_t* pdu, size_t pduLen, uint8_t* respPdu, size_t* respLen, size_t respCap,
         int timeoutMs)
{
    if (m->fd < 0)
        return MB_IO_ERROR;

    uint16_t txid = ++m->txid;
    uint8_t req[MAX_ADU];
    req[0] = (uint8_t)(txid >> 8);
    req[1] = (uint8_t)(txid & 0xff);
    req[2] = 0; /* protocol id hi */
    req[3] = 0; /* protocol id lo */
    uint16_t plen = (uint16_t)(pduLen + 1);
    req[4] = (uint8_t)(plen >> 8);
    req[5] = (uint8_t)(plen & 0xff);
    req[6] = (uint8_t)m->unit;
    memcpy(req + MBAP_LEN, pdu, pduLen);

    uint64_t deadline = now_ms() + (uint64_t)timeoutMs;
    if (!send_all(m->fd, req, MBAP_LEN + pduLen, deadline))
        return MB_TIMEOUT;

    uint8_t hdr[MBAP_LEN];
    if (!recv_exact(m->fd, hdr, MBAP_LEN, deadline))
        return MB_TIMEOUT;

    uint16_t rtx = (uint16_t)((hdr[0] << 8) | hdr[1]);
    uint16_t rproto = (uint16_t)((hdr[2] << 8) | hdr[3]);
    uint16_t rlen = (uint16_t)((hdr[4] << 8) | hdr[5]);
    if (rproto != 0 || rtx != txid || rlen < 2 || rlen > MAX_PDU + 1)
        return MB_IO_ERROR;
    if (hdr[6] != (uint8_t)m->unit)
        return MB_IO_ERROR;

    size_t bodyLen = (size_t)rlen - 1; /* minus unit id, already read */
    if (bodyLen > respCap)
        return MB_IO_ERROR;
    if (!recv_exact(m->fd, respPdu, bodyLen, deadline))
        return MB_TIMEOUT;
    *respLen = bodyLen;
    return MB_OK;
}

MbStatus
mb_read_input(MbMaster* m, int addr, int count, uint16_t* out, int* exceptionCode, int timeoutMs)
{
    if (exceptionCode)
        *exceptionCode = 0;
    if (count < 1 || count > 125)
        return MB_IO_ERROR;

    uint8_t pdu[5] = {0x04, (uint8_t)(addr >> 8), (uint8_t)(addr & 0xff), (uint8_t)(count >> 8),
                      (uint8_t)(count & 0xff)};
    uint8_t resp[MAX_PDU];
    size_t rlen = 0;
    MbStatus st = transact(m, pdu, sizeof(pdu), resp, &rlen, sizeof(resp), timeoutMs);
    if (st != MB_OK)
        return st;

    if (rlen >= 2 && resp[0] == (0x04 | 0x80)) {
        if (exceptionCode)
            *exceptionCode = resp[1];
        return MB_EXCEPTION;
    }
    if (rlen < 2 || resp[0] != 0x04)
        return MB_IO_ERROR;
    uint8_t byteCount = resp[1];
    if (byteCount != count * 2 || rlen != (size_t)(2 + byteCount))
        return MB_IO_ERROR;
    for (int i = 0; i < count; i++)
        out[i] = (uint16_t)((resp[2 + i * 2] << 8) | resp[2 + i * 2 + 1]);
    return MB_OK;
}

MbStatus
mb_write_single(MbMaster* m, int addr, uint16_t value, int* exceptionCode, int timeoutMs)
{
    if (exceptionCode)
        *exceptionCode = 0;

    uint8_t pdu[5] = {0x06, (uint8_t)(addr >> 8), (uint8_t)(addr & 0xff), (uint8_t)(value >> 8),
                      (uint8_t)(value & 0xff)};
    uint8_t resp[MAX_PDU];
    size_t rlen = 0;
    MbStatus st = transact(m, pdu, sizeof(pdu), resp, &rlen, sizeof(resp), timeoutMs);
    if (st != MB_OK)
        return st;

    if (rlen >= 2 && resp[0] == (0x06 | 0x80)) {
        if (exceptionCode)
            *exceptionCode = resp[1];
        return MB_EXCEPTION;
    }
    if (rlen != 5 || resp[0] != 0x06)
        return MB_IO_ERROR;
    uint16_t raddr = (uint16_t)((resp[1] << 8) | resp[2]);
    uint16_t rval = (uint16_t)((resp[3] << 8) | resp[4]);
    if (raddr != (uint16_t)addr || rval != value)
        return MB_IO_ERROR;
    return MB_OK;
}

float
mb_regs_to_f32(uint16_t hi, uint16_t lo)
{
    uint32_t bits = ((uint32_t)hi << 16) | lo;
    float f;
    memcpy(&f, &bits, sizeof(f));
    return f;
}
