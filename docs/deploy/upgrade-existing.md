# Upgrade an existing deployment

First migrate from the previous Litestream image manually; it lacks `/app/dashboard-backup`,
so the automatic updater cannot safely bootstrap itself. Keep the old image digest and
its encrypted offline DB backup until this transition has passed a restore drill.

1. Complete the approved [D1 sink setup](fresh-install.md). Export the current SQLite
   database with its online backup API, check integrity, gzip and age-encrypt it on the
   recovery workstation. Retain its key off-box. Do not delete the former replica yet.
2. Stop the dashboard, retain the data volume, pull the reviewed new immutable image,
   and initialize `prod-image.env` with that digest. Replace Litestream settings with
   the documented `BACKUP_*` settings. Keep the existing remote tunnel token, routes
   and Access policies. Follow the [fresh install](fresh-install.md) compose commands
   with **both** env files; do not initialize a new DB.
3. Start the dashboard, check `/api/health`, run `/app/dashboard-backup backup` with
   `docker compose … exec -T dashboard`, and do an off-box restore. Then enable the timer.

Every five minutes `scripts/update` compares `:prod` to `prod-image.env`. On change it
pulls the candidate by digest, stops the app to freeze writes, and requires a fresh
encrypted D1 snapshot plus download verification. Snapshot failure restarts the old app.
It keeps a temporary local SQLite checkpoint, applies candidate migrations to a second
copy, starts the candidate, and checks the real `/api/health` via `dashboard healthcheck`.
The persistent image pin changes only after health succeeds. Neither the tunnel nor
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
