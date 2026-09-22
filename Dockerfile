# ---- build stage ----
FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache git

# Resolve deps and build. go.sum is generated here via `go mod tidy`, so it does
# not need to be committed. (Pure Go, no CGO thanks to modernc.org/sqlite.)
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/gateway ./cmd/gateway

# ---- runtime stage ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 app && \
    mkdir -p /data && chown app /data
COPY --from=build /out/gateway /usr/local/bin/gateway

USER app
VOLUME ["/data"]
ENV JEV_DB_PATH=/data/gateway.db \
    JEV_PROXY_ADDR=:8080 \
    JEV_ADMIN_ADDR=:8081
EXPOSE 8080 8081

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/gateway"]
