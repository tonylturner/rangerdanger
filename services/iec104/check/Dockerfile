ARG BASE_IMAGE=alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8
FROM ${BASE_IMAGE}
# Test-only tooling; never shipped in the service image.
RUN apk add --no-cache python3 iptables
