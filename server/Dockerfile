# syntax=docker/dockerfile:1.7
#
# The release image, linux/amd64 and linux/arm64. CI publishes it to
# ghcr.io/cseed-org/officer-dashboard (.github/workflows/ci.yml, jobs image and publish), so a
# deploy only pulls it. To build it yourself, from the repository root, with nothing built first:
#
#   docker build -f server/Dockerfile -t officer-dashboard .
#   docker buildx build --platform linux/amd64,linux/arm64 -f server/Dockerfile -t officer-dashboard .
#
# Every stage that compiles or installs runs on the build machine: the front end is built with
# Node there (its output is the same for every platform), the C compiler for go-sqlite3 is
# cross-installed, and the runtime's system files are prepared there too. The target-platform
# stage only copies files, so a multi-platform build needs no emulation (no QEMU).

ARG GO_VERSION=1.27.4
ARG LITESTREAM_VERSION=0.3.14

# The front end (web/dist): static files, embedded into the server binary below. Node 24, as in
# CI's web job; a literal tag (no ARG) so Dependabot's docker updates see it.
FROM --platform=$BUILDPLATFORM node:24-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS toolchain
RUN apt-get update \
 && apt-get install -y --no-install-recommends gcc-aarch64-linux-gnu gcc-x86-64-linux-gnu libc6-dev-arm64-cross libc6-dev-amd64-cross \
 && rm -rf /var/lib/apt/lists/*

# Keep the existing replica format, but rebuild its runtime with the same Go as dashboard.
# Match v0.3.14's upstream Dockerfile tags and static-link flags.
FROM toolchain AS litestream
ARG LITESTREAM_VERSION
WORKDIR /src/litestream
RUN go mod download github.com/benbjohnson/litestream@v${LITESTREAM_VERSION} \
 && cp -a /go/pkg/mod/github.com/benbjohnson/litestream@v${LITESTREAM_VERSION}/. ./
# Minimum fixed versions; x/text and x/oauth2 are raised to the minimum required by
# the fixed x/crypto, x/net and grpc versions. Protobuf's fix is selected transitively.
# The OTEL SDK pin fixes GO-2026-6505 in grpc's updated dependency graph.
# AWS SDK v1.44.318 has no fixed v1 release for GO-2022-0635 / GO-2022-0646;
# moving to SDK v2 would require changes to Litestream's own code.
RUN go get golang.org/x/crypto@v0.58.1-0.20261009181718-2f1f834330b2 \
      golang.org/x/net@v0.62.0 \
      golang.org/x/text@v0.43.0 \
      golang.org/x/oauth2@v0.36.0 \
      google.golang.org/grpc@v1.83.2 \
      filippo.io/age@v1.2.1 \
      go.opentelemetry.io/otel/sdk@v1.45.0 \
 && go mod tidy
# Run the upstream SQLite, S3 and CLI tests on the builder's native architecture.
# TARGETARCH is introduced afterwards so this layer is shared by both release targets.
RUN CGO_ENABLED=1 go test -p 2 -tags osusergo,netgo,sqlite_omit_load_extension ./ ./s3/... ./cmd/litestream/...
ARG TARGETARCH
RUN case "$TARGETARCH" in \
      arm64) export CC=aarch64-linux-gnu-gcc ;; \
      amd64) export CC=x86_64-linux-gnu-gcc ;; \
      *) echo "unsupported TARGETARCH $TARGETARCH" >&2; exit 1 ;; \
    esac \
 && CGO_ENABLED=1 GOOS=linux GOARCH=$TARGETARCH \
    go build -trimpath -tags osusergo,netgo,sqlite_omit_load_extension \
    -ldflags "-s -w -X main.Version=${LITESTREAM_VERSION} -extldflags '-static'" \
    -o /out/litestream ./cmd/litestream

FROM toolchain AS build
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
# The front end built above becomes the embedded file system.
COPY --from=web /src/web/dist ./internal/web/dist
RUN case "$TARGETARCH" in \
      arm64) export CC=aarch64-linux-gnu-gcc ;; \
      amd64) export CC=x86_64-linux-gnu-gcc ;; \
      *) echo "unsupported TARGETARCH $TARGETARCH" >&2; exit 1 ;; \
    esac \
 && CGO_ENABLED=1 GOOS=linux GOARCH=$TARGETARCH \
    go build -trimpath -tags embedweb -ldflags "-s -w -X main.version=${VERSION}" -o /out/dashboard ./cmd/dashboard

# The runtime's system files: CA certificates and the dashboard user (uid 10001, home /data).
# Both are the same on every architecture, so they are prepared on the build machine.
FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS rootfs
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 10001 --home-dir /data --shell /usr/sbin/nologin dashboard \
 && mkdir -p /data && chown dashboard:dashboard /data

FROM debian:bookworm-slim
COPY --from=rootfs /etc/passwd /etc/group /etc/shadow /etc/gshadow /etc/
COPY --from=rootfs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=rootfs --chown=dashboard:dashboard /data /data
COPY --from=litestream /out/litestream /usr/local/bin/litestream
COPY --from=build /out/dashboard /app/dashboard
COPY server/deploy/litestream.yml /etc/litestream.yml
COPY server/deploy/entrypoint.sh /app/entrypoint.sh
USER dashboard
# Listening on all interfaces, the server refuses to start without APP_URL (the public https
# origin). Behind a proxy on the Docker network, set TRUSTED_PROXIES (e.g. 172.16.0.0/12) so
# rate limits key each client by X-Forwarded-For instead of by the proxy's address.
ENV ADDR=:8090 DB_PATH=/data/dashboard.db
EXPOSE 8090
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s CMD ["/app/dashboard", "healthcheck"]
ENTRYPOINT ["/app/entrypoint.sh"]
