# syntax=docker/dockerfile:1.7
#
# The release image, linux/amd64 and linux/arm64. CI publishes it to
# ghcr.io/cseed-org/officer-dashboard (.github/workflows/ci.yml, jobs image and publish), so a
# deploy only pulls it. To build it yourself, from the repository root, with nothing built first:
#
#   docker build -f server/Dockerfile -t officer-dashboard .
#   docker buildx build --platform linux/amd64,linux/arm64 -f server/Dockerfile -t officer-dashboard .
#
# The front end and static Go/C cross compilation run on the build machine. The final
# musl BusyBox runtime has /bin/sh, CA certificates and static app/backup binaries only.

# The front end (web/dist): static files, embedded into the server binary below. Node 24, as in
# CI's web job; a literal tag (no ARG) so Dependabot's docker updates see it.
FROM --platform=$BUILDPLATFORM node:24-bookworm-slim@sha256:873f586f3794f52281bd039bf02c048a0be08fde619338f9941bad1eb6b34530 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.4-bookworm@sha256:ce59b41415e2524ae99926fe1f37ce48ba8c194dcbd2a56a65436cdcefa2bd34 AS toolchain
RUN apt-get update \
 && apt-get install -y --no-install-recommends gcc-aarch64-linux-gnu gcc-x86-64-linux-gnu libc6-dev-arm64-cross libc6-dev-amd64-cross \
 && rm -rf /var/lib/apt/lists/*

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
    go build -trimpath -tags embedweb,release,osusergo,netgo,sqlite_omit_load_extension \
    -ldflags "-s -w -X main.version=${VERSION} -extldflags '-static'" -o /out/dashboard ./cmd/dashboard \
 && CGO_ENABLED=1 GOOS=linux GOARCH=$TARGETARCH \
    go build -trimpath -tags release,osusergo,netgo,sqlite_omit_load_extension \
    -ldflags "-s -w -extldflags '-static'" -o /out/dashboard-backup ./cmd/dashboard-backup

# Test binary belongs only to the isolated rehearsal target, never the release image.
FROM build AS backup-test
RUN CGO_ENABLED=1 go test -c -tags osusergo,netgo,sqlite_omit_load_extension \
    -ldflags "-s -w -extldflags '-static'" -o /out/backup-test ./cmd/dashboard-backup

# The runtime's system files: CA certificates and the dashboard user (uid 10001, home /data).
# Both are the same on every architecture, so they are prepared on the build machine.
FROM --platform=$BUILDPLATFORM debian:bookworm-slim@sha256:e67eeead736ff7bbe258ef088bff683635c561b1fcfefad0806c1df4ab49ce0e AS rootfs
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 10001 --home-dir /data --shell /usr/sbin/nologin dashboard \
 && mkdir -p /data && chown dashboard:dashboard /data

# Minimal static musl BusyBox supports the entrypoint without Debian runtime
# packages or dynamic system libraries. Pin its multi-architecture manifest.
FROM busybox:stable-musl@sha256:3c6ae8008e2c2eedd141725c30b20d9c36b026eb796688f88205845ef17aa213 AS runtime
COPY --from=rootfs /etc/passwd /etc/group /etc/
COPY --from=rootfs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=rootfs --chown=dashboard:dashboard /data /data
COPY --from=build /out/dashboard /app/dashboard
COPY --from=build /out/dashboard-backup /app/dashboard-backup
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

FROM runtime AS backup-test-image
COPY --from=backup-test /out/backup-test /app/backup-test

FROM runtime AS release
