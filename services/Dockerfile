# Multi-stage Dockerfile for all substation simulator services.
# Usage: docker build --target <service-name> -t <tag> .

FROM golang:1.25-alpine AS builder
WORKDIR /app
# Copy dnp3go library first (standalone module, referenced via replace directive)
COPY dnp3go/ /dnp3go/
COPY services/go.mod ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY services/ .
# All seven sims built in one RUN so the go-build + module caches are
# shared across them, and we end up with one image layer instead of
# seven. Caches persist between CI runs via the workflow's GHA cache.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -o /bin/relay-sim ./relay-sim \
 && CGO_ENABLED=0 go build -o /bin/recloser-sim ./recloser-sim \
 && CGO_ENABLED=0 go build -o /bin/regulator-sim ./regulator-sim \
 && CGO_ENABLED=0 go build -o /bin/rtac-sim ./rtac-sim \
 && CGO_ENABLED=0 go build -o /bin/capbank-sim ./capbank-sim \
 && CGO_ENABLED=0 go build -o /bin/historian-sim ./historian-sim \
 && CGO_ENABLED=0 go build -o /bin/gps-sim ./gps-sim

# Build dnp3poll/dnp3cmd from the dnp3go library so rtac-sim can ship
# them as student-runnable diagnostic tools. The eng-ws Dockerfile
# uses the same dnp3go module via its own builder stage; keeping a
# parallel build here avoids cross-Dockerfile coupling.
FROM golang:1.25-alpine AS dnp3-builder
WORKDIR /src
COPY dnp3go/ .
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -o /bin/dnp3poll ./cmd/dnp3poll \
 && CGO_ENABLED=0 go build -o /bin/dnp3cmd  ./cmd/dnp3cmd

# mbpoll isn't in any alpine repo (and the debian package would drag
# in debian's CVE backlog — we want to stay on alpine). Build from
# source against alpine's libmodbus. Resulting binary links against
# libmodbus.so dynamically, so the final rtac-sim stage installs the
# libmodbus runtime package alongside it.
FROM alpine:3.21 AS mbpoll-builder
RUN apk add --no-cache git cmake make gcc musl-dev libmodbus-dev pkgconfig
RUN git clone --depth 1 https://github.com/epsilonrt/mbpoll /src
WORKDIR /src
RUN cmake -B build -DCMAKE_BUILD_TYPE=Release \
 && cmake --build build

FROM alpine:3.21 AS sim-base

# iproute2: ip(8) for route manipulation in harden scripts
# iptables: FORWARD DROP policy and per-interface INPUT filtering in rtac-harden.sh
# bash:     student-facing terminal consistency (~2MB per image)
# nmap:     Lab 2.4 validation report runs reachability probes from
#           lan1-only OT hosts (historian-sim) to confirm the
#           firewall enforces non-RTAC → field denies. Without nmap
#           in sim-base, the only lan1-only-with-probes host would
#           be openplc — which is multi-homed and bypasses the
#           firewall, so a probe from there proves nothing about
#           segmentation enforcement.
RUN apk add --no-cache iproute2 iptables bash nmap
COPY scripts/set-gateway.sh /usr/local/bin/set-gateway.sh
COPY scripts/rtac-harden.sh /usr/local/bin/rtac-harden.sh
COPY scripts/rtac-route-monitor.sh /usr/local/bin/rtac-route-monitor.sh
RUN chmod +x /usr/local/bin/set-gateway.sh \
              /usr/local/bin/rtac-harden.sh \
              /usr/local/bin/rtac-route-monitor.sh

FROM sim-base AS relay-sim
COPY --from=builder /bin/relay-sim /usr/local/bin/relay-sim
EXPOSE 8080 502 20000
CMD ["sh", "-c", "set-gateway.sh && relay-sim"]

FROM sim-base AS recloser-sim
COPY --from=builder /bin/recloser-sim /usr/local/bin/recloser-sim
EXPOSE 8080 502 20000
CMD ["sh", "-c", "set-gateway.sh && recloser-sim"]

FROM sim-base AS regulator-sim
COPY --from=builder /bin/regulator-sim /usr/local/bin/regulator-sim
EXPOSE 8080 502 20000
CMD ["sh", "-c", "set-gateway.sh && regulator-sim"]

# rtac-sim is a special case: the validation-evidence and
# hardening-configurations labs document "from RTAC" diagnostic
# commands students are expected to run — mbpoll, dnp3poll, curl,
# python3 to pretty-print API state. The improved firewall policy
# also allows vendor (DMZ) → OT (lan1) on tcp/22 + tcp/443, so
# this stage hosts an sshd + nginx-on-443 management surface for
# Lab 2.4 to probe against. We stay on alpine (low CVE surface)
# and pull in:
#   - libmodbus runtime + the mbpoll binary built in mbpoll-builder
#     (mbpoll isn't in any alpine repo, see comment on that stage)
#   - dnp3poll/dnp3cmd built from our dnp3go module
#   - curl, python3 for the documented API-poking commands
#   - openssh-server, openssh-keygen for the :22 management listener
#   - nginx, openssl for the :443 management portal listener
FROM sim-base AS rtac-sim
RUN apk add --no-cache libmodbus curl python3 openssh-server openssh-keygen nginx openssl shadow
COPY --from=mbpoll-builder /src/build/bin/mbpoll /usr/local/bin/mbpoll
COPY --from=builder        /bin/rtac-sim         /usr/local/bin/rtac-sim
COPY --from=dnp3-builder   /bin/dnp3poll         /usr/local/bin/dnp3poll
COPY --from=dnp3-builder   /bin/dnp3cmd          /usr/local/bin/dnp3cmd
COPY scripts/rtac-mgmt-init.sh /usr/local/bin/rtac-mgmt-init.sh
RUN chmod +x /usr/local/bin/rtac-mgmt-init.sh
EXPOSE 22 443 8080 502 20000
# set-gateway.sh: installs the OT Ops firewall as the default route
# rtac-harden.sh: disables forwarding, sets FORWARD DROP, and routes
# field_net traffic through the firewall so the RTAC cannot bypass it
# rtac-route-monitor.sh: re-installs the default + indirect field
# route if docker network reconciliation wipes them. Without this,
# RTAC silently loses cross-zone reachability when peers restart.
# rtac-mgmt-init.sh: starts sshd on :22 and nginx on :443 so the
# vendor-to-ot-restricted firewall rule has real listeners to probe.
CMD ["sh", "-c", "set-gateway.sh && rtac-harden.sh && rtac-mgmt-init.sh && rtac-route-monitor.sh & exec rtac-sim"]

FROM sim-base AS capbank-sim
COPY --from=builder /bin/capbank-sim /usr/local/bin/capbank-sim
EXPOSE 8080 502
CMD ["sh", "-c", "set-gateway.sh && capbank-sim"]

FROM sim-base AS historian-sim
COPY --from=builder /bin/historian-sim /usr/local/bin/historian-sim
EXPOSE 8080 502
CMD ["sh", "-c", "set-gateway.sh && historian-sim"]

FROM sim-base AS gps-sim
COPY --from=builder /bin/gps-sim /usr/local/bin/gps-sim
EXPOSE 8080 502
CMD ["sh", "-c", "set-gateway.sh && gps-sim"]

