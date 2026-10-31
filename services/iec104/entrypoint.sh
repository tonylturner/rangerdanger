#!/bin/sh
# RangerDanger IEC 60870-5-104 image entrypoint.
# Copyright (C) 2026 The RangerDanger contributors
# SPDX-License-Identifier: Apache-2.0
#
# Route cross-zone traffic through the containd firewall, then exec the
# program the compose command selected (iec104-rtu or iec104-cc). set -e so
# a failed gateway install stops the container rather than starting a
# program on an unrouted network.
set -e
set-gateway.sh
exec "$@"
