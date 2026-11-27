# Updates and recovery

Use this for the current Docker deployment. Complete [Fresh install](fresh-install.md)
and verify an off-host restore before enabling automatic updates. Host Compose commands
run from `server/deploy`; every production Compose command needs
`--env-file .env --env-file prod-image.env -f compose.cloudflare.yml`.

## Approved updates and rollback

Every five minutes `scripts/update` compares `:prod` to `prod-image.env`. On change it
pulls the candidate by digest, stops the app and backup service to freeze writes, and requires a fresh
encrypted D1 snapshot plus download verification. Snapshot failure restarts the old app.
It keeps a temporary local SQLite checkpoint, applies candidate migrations to a second
copy, starts the candidate, and checks the real `/api/health` via `dashboard healthcheck`.
The backup service starts on the candidate only after app health succeeds, then the persistent image pin changes. Neither the tunnel nor
cloudflared is replaced by this command.

If startup or health fails after migrations could run, it stops the candidate, removes
the stopped database's WAL/SHM, restores the pre-update local checkpoint, starts the
previous digest and checks health. Keeping the decryption key off-host prevents decrypting a D1
snapshot automatically on the host, so immediate rollback uses the stopped local
checkpoint. A host/disk loss still requires the off-box restore. The checkpoint and
migration copy have the data volume's existing access controls and are removed after
success; these are temporary local working databases, not unencrypted remote backups.

Discord receives success, rollback or rollback-failed status without credentials or
subprocess logs. After successful rollback the rejected digest is recorded; the timer
skips it until `:prod` changes, protecting hourly backups from repeated failed-update
snapshots. Use `scripts/update --retry` only after resolving the cause. A failed rollback
retains the checkpoint for operator recovery. A host
crash halfway through an update needs operator inspection before restarting the timer:
keep the original pin and `.update-before.db`, compare the running digest, then restore
or complete the operation. The updater takes an exclusive host lock to prevent overlap.

Rehearse updates in an isolated compose project first. Approve A, then a good B, then a
deliberately unhealthy C. B must become healthy; C must return to B with the pre-C data
and pin intact. Never push a deliberately broken image to production's `:prod` tag.

## Monthly restore drill

On an isolated recovery workstation, check out this repository and start at its
root. Install Python 3 and the age CLI. Configure `BACKUP_SINK_URL` and the sink's
Access/bearer credentials using `BACKUP_ACCESS_CLIENT_ID_FILE`,
`BACKUP_ACCESS_CLIENT_SECRET_FILE`, and `BACKUP_BEARER_SECRET_FILE`. These paths
must point to protected files on the recovery workstation, not the container paths
from production `backup.env`. Obtain the offline age identity. Run:

```sh
DB_PATH=./drill-lock.db scripts/restore-from-d1 --snapshot latest \
  --identity /offline/dashboard.agekey --output ./restored.db
```

The command refuses to overwrite, verifies ciphertext SHA-256 and length before age
decryption, gunzips, and requires `PRAGMA integrity_check = ok` before installing the file.
Use an isolated dashboard instance with no production integrations to check representative
records. Record snapshot ID/date, hash/integrity result and elapsed recovery time; delete
the drill's plaintext copy and unmount the offline identity. Separately decrypt a weekly
membership SQL export and import it into a local D1 database, never into the live mirror.

The alternative systemd backup script now requires an absolute `BACKUP_DIR`, a public
`BACKUP_AGE_RECIPIENT` and the age CLI. It sets umask 0077, verifies a private temporary
SQLite snapshot, and keeps/transmits only `.db.gz.age` artifacts. `BACKUP_REMOTE` is an
executable hook receiving the ciphertext filename as one argument. Migrate pre-existing
plaintext backups off-box; the script neither sends nor silently deletes them. See [systemd setup](other-setups.md#pi-under-systemd-without-docker)
for that alternative deployment.

## Interrupted update recovery

`scripts/update` durably writes `update-recovery-required.json` before attempting a stop.
If the host or rollback fails, every later poll refuses to update, including `--retry`.
Keep this marker and `/data/.update-before.db`; checkpoint refuses to overwrite an
existing checkpoint and rollback preserves it until successful recovery or commit.

Disable the update timer and stop both dashboard and backup services. Read the marker's
previous/candidate pins and the actual `prod-image.env`. If the candidate pin was committed,
verify its DB and health before deciding to retain it. Otherwise restore the original
checkpoint using the previous image's `dashboard-backup rollback` with both services
stopped, or restore the verified D1 ciphertext off-box if the checkpoint is unavailable.
Start the chosen pinned services, check health and data, and persist the matching
`prod-image.env`. Only after recovery is verified, run `dashboard-backup cleanup` against
the stopped maintenance files, remove `update-recovery-required.json`, then re-enable
the timer. Never delete the marker just to make another poll proceed. A pre-swap sink
outage that recovers normally is retried automatically; candidate migration/startup
failures are rejected after successful rollback.

Complete the [live Free-plan CPU and usage rehearsal](../deployment.md#live-free-plan-rehearsal)
before enabling unattended backups and updates.
