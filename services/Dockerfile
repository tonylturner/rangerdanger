# Multi-stage Dockerfile for all substation simulator services.
# Usage: docker build --target <service-name> -t <tag> .

FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/relay-sim ./relay-sim
RUN CGO_ENABLED=0 go build -o /bin/recloser-sim ./recloser-sim
RUN CGO_ENABLED=0 go build -o /bin/regulator-sim ./regulator-sim
RUN CGO_ENABLED=0 go build -o /bin/rtac-sim ./rtac-sim
RUN CGO_ENABLED=0 go build -o /bin/capbank-sim ./capbank-sim
RUN CGO_ENABLED=0 go build -o /bin/historian-sim ./historian-sim
RUN CGO_ENABLED=0 go build -o /bin/gps-sim ./gps-sim

FROM alpine:3.21 AS sim-base

RUN apk add --no-cache iproute2
COPY scripts/set-gateway.sh /usr/local/bin/set-gateway.sh
RUN chmod +x /usr/local/bin/set-gateway.sh

FROM sim-base AS relay-sim
COPY --from=builder /bin/relay-sim /usr/local/bin/relay-sim
EXPOSE 8080 502
CMD ["sh", "-c", "set-gateway.sh && relay-sim"]

FROM sim-base AS recloser-sim
COPY --from=builder /bin/recloser-sim /usr/local/bin/recloser-sim
EXPOSE 8080 502
CMD ["sh", "-c", "set-gateway.sh && recloser-sim"]

FROM sim-base AS regulator-sim
COPY --from=builder /bin/regulator-sim /usr/local/bin/regulator-sim
EXPOSE 8080 502
CMD ["sh", "-c", "set-gateway.sh && regulator-sim"]

FROM sim-base AS rtac-sim
COPY --from=builder /bin/rtac-sim /usr/local/bin/rtac-sim
EXPOSE 8080
CMD ["sh", "-c", "set-gateway.sh && rtac-sim"]

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

