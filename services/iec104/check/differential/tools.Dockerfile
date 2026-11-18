# SPDX-License-Identifier: Apache-2.0
# Capture-only helper; no production image or host network namespace.
FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
RUN apk add --no-cache tcpdump
ENTRYPOINT ["tcpdump"]
