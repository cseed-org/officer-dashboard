# syntax=docker/dockerfile:1.7
#
# Cloudflare Containers / any OCI host, linux/amd64 and linux/arm64:
#
#   (cd web && npm ci && npm run build)            # produces web/dist, embedded below
#   docker buildx build --platform linux/amd64,linux/arm64 -f server/Dockerfile -t officer-dashboard .
#
# The build context is the repository root so the image can embed web/dist. Building runs
# on the build machine for both targets: the C compiler for go-sqlite3 is cross-installed,
# so there is no emulation and no 15-minute QEMU compile.

ARG GO_VERSION=1.27.4
ARG LITESTREAM_VERSION=0.3.14

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
# The front end: web/dist from the repository root becomes the embedded file system.
COPY web/dist ./internal/web/dist
RUN case "$TARGETARCH" in \
      arm64) export CC=aarch64-linux-gnu-gcc ;; \
      amd64) export CC=x86_64-linux-gnu-gcc ;; \
      *) echo "unsupported TARGETARCH $TARGETARCH" >&2; exit 1 ;; \
    esac \
 && CGO_ENABLED=1 GOOS=linux GOARCH=$TARGETARCH \
    go build -trimpath -tags embedweb -ldflags "-s -w -X main.version=${VERSION}" -o /out/dashboard ./cmd/dashboard

FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 10001 --home-dir /data --shell /usr/sbin/nologin dashboard \
 && mkdir -p /data && chown dashboard:dashboard /data
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
