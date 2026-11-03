# Deployment

## Raspberry Pi 5 (systemd)

Run Go build commands from `server/`.

```sh
make build-release           # needs ../web/dist; embeds it. `make build` for an API-only binary
```
then follow the header of `deploy/dashboard.service` (user, env file `deploy/dashboard.env.example`,
install, enable). Put it behind a Cloudflare Tunnel or Caddy for HTTPS; set `APP_URL` to the public
URL. Nightly backups: `deploy/dashboard-backup.{service,timer}` run `scripts/backup`.

**Backups.** `scripts/backup` uses `sqlite3 .backup` (safe on a live WAL database; never copy the
`.db` file alone), checks `PRAGMA integrity_check`, gzips, prunes to the newest 30 and can push
off-box via `BACKUP_REMOTE` (`rclone`/`aws s3 cp`). Restore steps are in its header. Receipts are
not in the database: Google Drive holds them (or `RECEIPTS_DIR`, which you must back up too).

## Cloudflare Containers / Docker

Run the following from the repository root. The multi-platform build needs a builder capable of running both target architectures (native nodes or QEMU/binfmt); cgo compilation uses cross-gcc, but runtime package installation executes target binaries.

```sh
(cd web && npm ci && npm run build)
docker buildx build --platform linux/amd64,linux/arm64 -f server/Dockerfile -t officer-dashboard .
```
Multi-stage; cross-compiles cgo with an installed cross-gcc; the runtime is
`debian:bookworm-slim` plus the `litestream` binary, running as a non-root user with a
`HEALTHCHECK` (`dashboard healthcheck`). The image listens on `:8090`, so it **refuses to start
without `APP_URL`** (the public https origin). When the TLS proxy (Caddy, cloudflared) reaches the
container over the Docker network rather than loopback, set `TRUSTED_PROXIES` to that network
(for example `172.16.0.0/12`, or the proxy's address): otherwise every user shares the proxy's
rate-limit budget for sign-in and uploads. **Containers have ephemeral disks**, so the image
replicates the SQLite file with Litestream when `LITESTREAM_BUCKET` is set:

| Variable | Example |
| --- | --- |
| `LITESTREAM_BUCKET` | `cseed-dashboard-db` |
| `LITESTREAM_ENDPOINT` | `https://<account-id>.r2.cloudflarestorage.com` (R2; empty for AWS S3) |
| `LITESTREAM_REGION` | `auto` (R2) or `us-west-2` |
| `LITESTREAM_PATH` | `dashboard` (key prefix; default) |
| `LITESTREAM_ACCESS_KEY_ID`, `LITESTREAM_SECRET_ACCESS_KEY` | an R2/S3 token scoped to the bucket |

On boot `deploy/entrypoint.sh` runs `litestream restore -if-db-not-exists -if-replica-exists`, then
`litestream replicate -exec "dashboard serve"`: up to ~1 s of writes can be lost if the container
dies hard, and a replaced container comes back with the data. `deploy/litestream.yml` keeps 72 h of
WAL and a snapshot every 6 h. Receipts must use Google Drive in a container (`RECEIPTS_DIR` would
be wiped). Run **one** instance: SQLite plus Litestream is single-writer.

> [!NOTE]
> The original Go-port branch did not build the image because `web/dist` did not yet exist. During this documentation update, the multi-platform command reached runtime setup but failed with `exec format error` on the amd64 stage of this ARM host. Configure a multi-platform builder before using that command. Live Litestream restore/replication still needs deployment credentials and verification.

## HTTPS and restore

Terminate HTTPS at a Cloudflare Tunnel or Caddy and forward to the server's loopback listener (`127.0.0.1:8090` in the systemd unit). Set `APP_URL` to the public HTTPS origin so OAuth callbacks, Secure cookies and HSTS agree with the public site.

Install the service following [the unit header](../server/deploy/dashboard.service), with secrets based on [the env example](../server/deploy/dashboard.env.example). The example does not enumerate every optional V0.3 integration; use [configuration](configuration.md).

For a local backup restore: stop the service, decompress the chosen `.db.gz` into the configured database path, remove the matching stale `-wal` and `-shm` files, and restart (migrations are reapplied as needed). See [backup script instructions](../server/scripts/backup). Container recovery uses Litestream restore instead. Back up local receipts separately.
