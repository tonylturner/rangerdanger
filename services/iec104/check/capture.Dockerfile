# Capture helper for check/run.sh only; never shipped in a product image.
# It runs tcpdump in the Docker host network namespace (the Linux VM on
# Docker Desktop), where the check network's bridge lives, so it records
# every container on that bridge. Host tshark decodes the file afterwards.
FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
RUN apk add --no-cache tcpdump
ENTRYPOINT ["tcpdump"]
