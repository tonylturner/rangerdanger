# RangerDanger IEC 60870-5-104 image (GPLv3; see services/iec104/README.md).
#
# One image carries the RTU (iec104-rtu, controlled station) and the control
# centre (iec104-cc, controlling station); the compose command selects which
# to run. The entrypoint installs the containd firewall as the default
# gateway first, so a program never starts on an unrouted network.
#
# Build context is the repository root so the image can COPY both its own
# sources (services/iec104/...) and the shared scripts/set-gateway.sh, the
# same way services/Dockerfile does.
#
# Multi-arch: every stage builds its dependencies and the programs from
# source, so `docker buildx build --platform linux/amd64,linux/arm64`
# produces native binaries for both. The base is digest-pinned.
#
# GPLv3 corresponding source: the lib60870 tarball (verified by checksum),
# the cJSON tarball (MIT, verified by checksum) and our own sources are
# copied into /usr/share/src in the final image, alongside the licences, so
# every shipped binary travels with its source.

ARG ALPINE_DIGEST=sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8
FROM alpine:3.22@${ALPINE_DIGEST} AS build

# lib60870-C (MZ Automation, GPLv3), pinned to a release tag and verified.
ARG LIB60870_VERSION=2.4.1
ARG LIB60870_SHA256=d5708b9885c2c068ac9d9bae4696a624bd1d1d6645a1b1a9261c00274bc36c3a
# cJSON (Dave Gamble, MIT), pinned to a release tag and verified. Used by the
# strict point-map loader (RTU and CC) and the CC's HTTP API; a
# GPLv3-compatible, pure-C dependency.
ARG CJSON_VERSION=1.7.19
ARG CJSON_SHA256=7fa616e3046edfa7a28a32d5f9eacfd23f92900fe1f8ccd988c1662f30454562

# linux-headers: lib60870's Linux socket HAL includes <linux/version.h>.
RUN apk add --no-cache build-base curl linux-headers

WORKDIR /src
RUN curl -fsSL -o lib60870.tar.gz \
        "https://github.com/mz-automation/lib60870/archive/refs/tags/v${LIB60870_VERSION}.tar.gz" \
 && echo "${LIB60870_SHA256}  lib60870.tar.gz" | sha256sum -c - \
 && mkdir lib60870 \
 && tar -xzf lib60870.tar.gz -C lib60870 --strip-components=1 \
 && curl -fsSL -o cjson.tar.gz \
        "https://github.com/DaveGamble/cJSON/archive/refs/tags/v${CJSON_VERSION}.tar.gz" \
 && echo "${CJSON_SHA256}  cjson.tar.gz" | sha256sum -c - \
 && mkdir cjson \
 && tar -xzf cjson.tar.gz -C cjson --strip-components=1

# Build the static lib60870 (no TLS: the lab runs plain TCP/2404).
RUN make -C lib60870/lib60870-C -j"$(nproc)" \
 && make -C lib60870/lib60870-C install INSTALL_PREFIX=/opt/lib60870

COPY services/iec104/src/ /src/iec104/src/
WORKDIR /src/iec104
RUN set -eux; \
    mkdir -p /out; \
    CFLAGS="-std=gnu11 -D_GNU_SOURCE -O2 -Wall -Wextra -Werror -Isrc -I/src/cjson -I/opt/lib60870/include"; \
    LIBS="/opt/lib60870/lib/liblib60870.a -lpthread -lm"; \
    cc $CFLAGS -o /out/iec104-rtu \
        src/rtu.c src/common.c src/pointmap.c src/modbus.c src/stationclock.c /src/cjson/cJSON.c $LIBS; \
    cc $CFLAGS -o /out/iec104-cc \
        src/cc.c src/cc_state.c src/cc_link.c src/cc_command.c src/cc_http.c src/cc_api.c \
        src/common.c src/pointmap.c /src/cjson/cJSON.c $LIBS; \
    cc $CFLAGS -o /out/iec104cmd  src/iec104cmd.c src/common.c src/client.c $LIBS

FROM alpine:3.22@${ALPINE_DIGEST}

# iproute2: set-gateway.sh manipulates routes with ip(8). busybox already
# provides nc and wget for the compose healthchecks.
RUN apk add --no-cache iproute2

COPY --from=build /out/iec104-rtu /out/iec104-cc /out/iec104cmd /usr/local/bin/
COPY scripts/set-gateway.sh /usr/local/bin/set-gateway.sh
COPY services/iec104/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/set-gateway.sh /usr/local/bin/entrypoint.sh

# Licences and corresponding source for the GPLv3 binaries in this image.
COPY services/iec104/LICENSE /usr/share/licenses/iec104/LICENSE
COPY services/iec104/README.md /usr/share/licenses/iec104/README.md
COPY --from=build /src/lib60870.tar.gz /usr/share/src/lib60870.tar.gz
COPY --from=build /src/cjson.tar.gz /usr/share/src/cjson.tar.gz
COPY --from=build /src/cjson/LICENSE /usr/share/licenses/iec104/LICENSE.cJSON
COPY services/iec104/src/ /usr/share/src/iec104/src/

EXPOSE 2404
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
# The compose command overrides this; it also supplies the bind address.
CMD ["iec104-rtu", "--config", "/etc/rangerdanger/points.json"]
