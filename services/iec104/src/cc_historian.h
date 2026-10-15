/* SPDX-License-Identifier: GPL-3.0-or-later */
#ifndef RD_CC_HISTORIAN_H
#define RD_CC_HISTORIAN_H
#include <stdbool.h>
#include <stddef.h>
#include <signal.h>

bool cc_historian_start(const char* url, const char* bindAddr, volatile sig_atomic_t* stop,
                        char* err, size_t errLen);
void cc_historian_stop(void);
#endif
