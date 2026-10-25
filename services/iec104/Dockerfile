# Production Go RTU, control centre and CLI; C is a sealed test reference only.
# Build context: repository root. Native builder per target architecture.
ARG GO_IMAGE=golang:1.26.10-alpine@sha256:27f2d3b83cfc5803c8078f8e730af0f7c431bf067afaec8f60a36fb763187cba
ARG ALPINE_IMAGE=alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8
FROM ${GO_IMAGE} AS rtu-build
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off
WORKDIR /src/services
COPY services/go.mod ./
COPY dnp3go/go.mod /src/dnp3go/go.mod
COPY iec104go/go.mod iec104go/*.go /src/iec104go/
COPY services/iec104/internal/ ./iec104/internal/
COPY services/iec104/cmd/iec104-rtu/ ./iec104/cmd/iec104-rtu/
RUN go build -trimpath -o /out/iec104-rtu ./iec104/cmd/iec104-rtu

COPY services/iec104/cmd/iec104-cc/ ./iec104/cmd/iec104-cc/
RUN go build -trimpath -o /out/iec104-cc ./iec104/cmd/iec104-cc
WORKDIR /src/iec104go
COPY iec104go/cmd/iec104cmd/ ./cmd/iec104cmd/
RUN go build -trimpath -o /out/iec104cmd ./cmd/iec104cmd

FROM ${ALPINE_IMAGE}
RUN apk add --no-cache iproute2
COPY --from=rtu-build /out/iec104-rtu /out/iec104-cc /out/iec104cmd /usr/local/bin/
COPY scripts/set-gateway.sh /usr/local/bin/set-gateway.sh
COPY services/iec104/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/set-gateway.sh /usr/local/bin/entrypoint.sh
COPY iec104go/LICENSE /usr/share/licenses/iec104/LICENSE
EXPOSE 2404
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["iec104-rtu", "--config", "/etc/rangerdanger/points.json"]
