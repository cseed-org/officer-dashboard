# Upgrade an existing deployment

First migrate from the previous Litestream image manually; it lacks `/app/dashboard-backup`,
so the automatic updater cannot safely bootstrap itself. Keep the old image digest and
its encrypted offline DB backup until this transition has passed a restore drill.

1. Complete the approved [D1 sink setup](fresh-install.md). Export the current SQLite
   database with its online backup API, check integrity, gzip and age-encrypt it on the
   recovery workstation. Retain its key off-box. Do not delete the former replica yet.
2. Stop the dashboard, retain the data volume, pull the reviewed new immutable image,
   and initialize `prod-image.env` with that digest. Replace Litestream settings with
   the documented `BACKUP_*` settings in the separate `backup.env`. Move credentials to the app/backup/tunnel secret files and remove plaintext and backup credentials from `dashboard.env`. Keep the existing remote tunnel token, routes
   and Access policies. Follow the [fresh install](fresh-install.md) compose commands
   with **both** env files; do not initialize a new DB.
3. Start the dashboard, check `/api/health`, run `/app/dashboard-backup backup` with
   `docker compose ... run --rm --no-deps backup backup`, and do an off-box restore. Then enable the timer.

Every five minutes `scripts/update` compares `:prod` to `prod-image.env`. On change it
pulls the candidate by digest, stops the app and backup service to freeze writes, and requires a fresh
encrypted D1 snapshot plus download verification. Snapshot failure restarts the old app.
It keeps a temporary local SQLite checkpoint, applies candidate migrations to a second
copy, starts the candidate, and checks the real `/api/health` via `dashboard healthcheck`.
The backup service starts on the candidate only after app health succeeds, then the persistent image pin changes. Neither the tunnel nor
cloudflared is replaced by this command.

If startup or health fails after migrations could run, it stops the candidate, removes
the stopped database's WAL/SHM, restores the pre-update local checkpoint, starts the
previous digest and checks health. The off-box-key rule prevents decrypting a D1
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

On an isolated recovery workstation, load only the backup sink's Access/bearer credentials
and obtain the offline age identity. Run:

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

The legacy systemd backup script now requires an absolute `BACKUP_DIR`, a public
`BACKUP_AGE_RECIPIENT` and the age CLI. It sets umask 0077, verifies a private temporary
SQLite snapshot, and keeps/transmits only `.db.gz.age` artifacts. `BACKUP_REMOTE` is an
executable hook receiving the ciphertext filename as one argument. Migrate pre-existing
plaintext backups off-box; the script neither sends nor silently deletes them. P1 owns
the systemd unit's UMask hardening; this change does not edit that unit.

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
Leave `/data/r2-usage-ledger.json` in place. Even a pre-R2 image's rollback erases
R2 spending only from the database, and the next R2-aware start restores it from that
file ([details](../deployment.md#conditions-for-staying-at-0)).
Start the chosen pinned services, check health and data, and persist the matching
`prod-image.env`. Only after recovery is verified, run `dashboard-backup cleanup` against
the stopped maintenance files, remove `update-recovery-required.json`, then re-enable
the timer. Never delete the marker just to make another poll proceed. A pre-swap sink
outage that recovers normally is retried automatically; candidate migration/startup
failures are rejected after successful rollback.

Complete the [live Free-plan CPU and usage rehearsal](../deployment.md#live-free-plan-rehearsal)
before enabling unattended backups and updates.


## Optional private R2 file storage (person-only)

Complete every step in [Private R2 file storage](../deployment.md#private-r2-file-storage)
before selecting `FILE_STORAGE=r2`: create an empty Standard bucket, disable r2.dev
and public domains, issue an Object Read & Write token scoped to that bucket only,
apply the one-day abort lifecycle with the documented Wrangler command, and set a
Cloudflare Billable Usage notification or the lowest available billing budget
alert. Save separate app credential files, then use the documented `dashboard.env`
entries and the existing read-only `secrets/app` mount. Review the exclusive-account
budget and restored-ledger constraints there. These are person-only settings and
startup actions; no agent should run the setup or migration against production.

Switching backends does not copy old local files. Keep the old deployment/storage
available until an explicitly planned migration is complete. Drive receipt links
keep the existing requester/director check. D1 database backups are unchanged;
the optional R2 backup copy is not included. For existing local/Drive files,
review the `files-migrate-r2` dry-run and explicit `-apply` steps in that guide.
