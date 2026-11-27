# syntax=docker/dockerfile:1
FROM node:24-bookworm-slim@sha256:873f586f3794f52281bd039bf02c048a0be08fde619338f9941bad1eb6b34530 AS web
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./

FROM web AS web-build
RUN npm run build

FROM golang:1.27.4-bookworm@sha256:ce59b41415e2524ae99926fe1f37ce48ba8c194dcbd2a56a65436cdcefa2bd34 AS go-build
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=1 go build -tags dev -o /out/dashboard ./cmd/dashboard

FROM debian:bookworm-slim@sha256:e67eeead736ff7bbe258ef088bff683635c561b1fcfefad0806c1df4ab49ce0e AS app
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=go-build /out/dashboard ./dashboard
COPY --from=web-build /app/web/dist ./web/dist
RUN mkdir /data
# Inline shell avoids CRLF-sensitive entrypoint scripts on Windows.
CMD ["sh", "-c", "./dashboard seed -reset && exec ./dashboard serve"]
