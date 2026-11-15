# syntax=docker/dockerfile:1
FROM node:26-bookworm-slim AS web
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./

FROM web AS web-build
RUN npm run build

FROM golang:1.24-bookworm AS go-build
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=1 go build -tags dev -o /out/dashboard ./cmd/dashboard

FROM debian:bookworm-slim AS app
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=go-build /out/dashboard ./dashboard
COPY --from=web-build /app/web/dist ./web/dist
RUN mkdir /data
# Inline shell avoids CRLF-sensitive entrypoint scripts on Windows.
CMD ["sh", "-c", "./dashboard seed -reset && exec ./dashboard serve"]
