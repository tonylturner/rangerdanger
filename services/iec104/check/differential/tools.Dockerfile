# SPDX-License-Identifier: Apache-2.0
# Capture-only helper; no production image or host network namespace.
FROM alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8
RUN apk add --no-cache tcpdump
ENTRYPOINT ["tcpdump"]
