# RangerDanger IEC 60870-5-104 image (GPLv3; see services/iec104/README.md).
#
# One image carries all three programs: iec104-rtu (server),
# iec104-cc (control-centre client) and iec104cmd (student CLI). The
# compose service or `docker run` command picks which one to run. This is
# a spike image; increment 3 may split the control centre into its own
# image behind the firewall.
#
# Multi-arch: every stage builds lib60870 and the programs from source, so
# `docker buildx build --platform linux/amd64,linux/arm64` produces native
# binaries for both. Pin the base by digest for reproducibility.
#
# GPLv3 corresponding source: the lib60870 tarball (verified by checksum)
# and our own sources are copied into /usr/share/src in the final image,
# alongside the licence, so every shipped binary travels with its source.

ARG ALPINE_DIGEST=sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8
FROM alpine:3.22@${ALPINE_DIGEST} AS build

# lib60870-C (MZ Automation, GPLv3), pinned to a release tag and verified.
ARG LIB60870_VERSION=2.4.1
ARG LIB60870_SHA256=d5708b9885c2c068ac9d9bae4696a624bd1d1d6645a1b1a9261c00274bc36c3a

# linux-headers: lib60870's Linux socket HAL includes <linux/version.h>.
RUN apk add --no-cache build-base curl linux-headers

WORKDIR /src
RUN curl -fsSL -o lib60870.tar.gz \
        "https://github.com/mz-automation/lib60870/archive/refs/tags/v${LIB60870_VERSION}.tar.gz" \
 && echo "${LIB60870_SHA256}  lib60870.tar.gz" | sha256sum -c - \
 && mkdir lib60870 \
 && tar -xzf lib60870.tar.gz -C lib60870 --strip-components=1

# Build the static library (no TLS: the spike runs plain TCP/2404).
RUN make -C lib60870/lib60870-C -j"$(nproc)" \
 && make -C lib60870/lib60870-C install INSTALL_PREFIX=/opt/lib60870

COPY src/ /src/iec104/src/
WORKDIR /src/iec104
RUN set -eux; \
    mkdir -p /out; \
    CFLAGS="-std=gnu11 -O2 -Wall -Wextra -Werror -Isrc -I/opt/lib60870/include"; \
    LIBS="/opt/lib60870/lib/liblib60870.a -lpthread"; \
    cc $CFLAGS -o /out/iec104-rtu src/rtu.c      src/common.c              $LIBS; \
    cc $CFLAGS -o /out/iec104-cc  src/cc.c       src/common.c src/client.c $LIBS; \
    cc $CFLAGS -o /out/iec104cmd  src/iec104cmd.c src/common.c src/client.c $LIBS

FROM alpine:3.22@${ALPINE_DIGEST}
COPY --from=build /out/iec104-rtu /out/iec104-cc /out/iec104cmd /usr/local/bin/
COPY LICENSE /usr/share/licenses/iec104/LICENSE
COPY README.md /usr/share/licenses/iec104/README.md
# Corresponding source for the GPLv3 binaries in this image.
COPY --from=build /src/lib60870.tar.gz /usr/share/src/lib60870.tar.gz
COPY src/ /usr/share/src/iec104/src/

EXPOSE 2404
# Defaults to the RTU; pass iec104-cc or iec104cmd as the command to run the others.
CMD ["iec104-rtu"]
