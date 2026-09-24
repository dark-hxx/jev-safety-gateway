# ---- build stage ----
FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache git

# Build identity reported by GET /api/version. .git is not in the build context
# (see .dockerignore), so no VCS stamp is available and COMMIT/BUILD_TIME are
# only ever what the caller passes:
#   docker build --build-arg COMMIT=$(git rev-parse --short HEAD) ...
ARG VERSION=
ARG COMMIT=
ARG BUILD_TIME=

# Resolve deps and build. go.sum is generated here via `go mod tidy`, so it does
# not need to be committed. (Pure Go, no CGO thanks to modernc.org/sqlite.)
COPY . .
RUN go mod tidy

# VERSION is different from the other two: web/package.json is in the context
# and is the same source scripts/build-local-test.ps1 reads, so the image
# derives it itself when no build-arg is given. Without this a plain
# `docker compose up -d --build` reported "dev" while a local Windows build of
# the identical commit reported a real version.
RUN set -eux; \
    ver="${VERSION}"; \
    if [ -z "$ver" ]; then \
        ver="$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' web/package.json | head -n 1)"; \
    fi; \
    [ -n "$ver" ] || ver=dev; \
    echo "building version=$ver commit=${COMMIT}"; \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
        -ldflags="-s -w -X main.version=$ver -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
        -o /out/jev-safety-gateway ./cmd/jev-safety-gateway

# ---- runtime stage ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 app && \
    mkdir -p /data && chown app /data
COPY --from=build /out/jev-safety-gateway /usr/local/bin/jev-safety-gateway

USER app
VOLUME ["/data"]
ENV JEV_DB_PATH=/data/jev-safety-gateway.db \
    JEV_PROXY_ADDR=:8080 \
    JEV_ADMIN_ADDR=:8081
EXPOSE 8080 8081

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/jev-safety-gateway"]
