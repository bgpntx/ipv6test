# ── Build stage ──
FROM golang:1.22-alpine AS builder
WORKDIR /build
COPY go.mod .
COPY main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /ipv6test .

# ── Run stage ──
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /ipv6test /ipv6test
EXPOSE 8080
ENTRYPOINT ["/ipv6test"]
