# Apache-2.0 Go candidate; the C image remains the phase-4 reference only.
# Build context: repository root. Native builder per target architecture.
ARG GO_IMAGE=golang:1.26.9-alpine@sha256:3082400e369fa24d5fc60bca20edab3f6d604e0c5a690ec66b295eff4dd87ade
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

FROM ${ALPINE_IMAGE} AS rtu
RUN apk add --no-cache iproute2
COPY --from=rtu-build /out/iec104-rtu /usr/local/bin/iec104-rtu
COPY scripts/set-gateway.sh /usr/local/bin/set-gateway.sh
COPY services/iec104/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/set-gateway.sh /usr/local/bin/entrypoint.sh
COPY iec104go/LICENSE /usr/share/licenses/iec104/LICENSE
EXPOSE 2404
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["iec104-rtu", "--config", "/etc/rangerdanger/points.json"]

FROM rtu-build AS cc-build
COPY services/iec104/cmd/iec104-cc/ ./iec104/cmd/iec104-cc/
RUN go build -trimpath -o /out/iec104-cc ./iec104/cmd/iec104-cc

FROM rtu-build AS cmd-build
WORKDIR /src/iec104go
COPY iec104go/cmd/iec104cmd/ ./cmd/iec104cmd/
RUN go build -trimpath -o /out/iec104cmd ./cmd/iec104cmd

FROM rtu AS cc
COPY --from=cc-build /out/iec104-cc /usr/local/bin/iec104-cc

FROM cc AS combined
COPY --from=cmd-build /out/iec104cmd /usr/local/bin/iec104cmd
