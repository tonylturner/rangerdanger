# Capture helper for check/run.sh only; never shipped in a product image.
# It runs tcpdump in the Docker host network namespace (the Linux VM on
# Docker Desktop), where the check network's bridge lives, so it records
# every container on that bridge. Host tshark decodes the file afterwards.
FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a
RUN apk add --no-cache tcpdump
ENTRYPOINT ["tcpdump"]
