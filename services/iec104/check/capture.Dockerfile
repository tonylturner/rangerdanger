# Capture helper for check/run.sh only; never shipped in a product image.
# It runs tcpdump in the Docker host network namespace (the Linux VM on
# Docker Desktop), where the check network's bridge lives, so it records
# every container on that bridge. Host tshark decodes the file afterwards.
FROM alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8
RUN apk add --no-cache tcpdump
ENTRYPOINT ["tcpdump"]
