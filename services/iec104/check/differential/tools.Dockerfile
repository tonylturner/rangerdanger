# SPDX-License-Identifier: Apache-2.0
# Capture-only helper; no production image or host network namespace.
FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a
RUN apk add --no-cache tcpdump
ENTRYPOINT ["tcpdump"]
