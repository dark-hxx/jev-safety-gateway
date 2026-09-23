# ---- build stage ----
FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache git

# Build identity reported by GET /api/version. .git is not in the build context
# (see .dockerignore), so the linker flags are the only source of this in the
# image — pass them from CI/compose:
#   docker build --build-arg VERSION=$(git describe --tags --always) ...
ARG VERSION=dev
ARG COMMIT=
ARG BUILD_TIME=

# Resolve deps and build. go.sum is generated here via `go mod tidy`, so it does
# not need to be committed. (Pure Go, no CGO thanks to modernc.org/sqlite.)
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
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
