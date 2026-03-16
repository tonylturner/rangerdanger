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
RUN CGO_ENABLED=0 go build -o /bin/opendss-sim ./opendss-sim

FROM alpine:3.21 AS relay-sim
COPY --from=builder /bin/relay-sim /usr/local/bin/relay-sim
EXPOSE 8080
CMD ["relay-sim"]

FROM alpine:3.21 AS recloser-sim
COPY --from=builder /bin/recloser-sim /usr/local/bin/recloser-sim
EXPOSE 8080
CMD ["recloser-sim"]

FROM alpine:3.21 AS regulator-sim
COPY --from=builder /bin/regulator-sim /usr/local/bin/regulator-sim
EXPOSE 8080
CMD ["regulator-sim"]

FROM alpine:3.21 AS rtac-sim
COPY --from=builder /bin/rtac-sim /usr/local/bin/rtac-sim
EXPOSE 8080
CMD ["rtac-sim"]

FROM alpine:3.21 AS opendss-sim
COPY --from=builder /bin/opendss-sim /usr/local/bin/opendss-sim
EXPOSE 8080
CMD ["opendss-sim"]
