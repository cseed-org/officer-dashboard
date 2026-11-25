# Deployment

For the current $0 Free-plan deployment, start with **[Fresh install](deploy/fresh-install.md)**
or **[Upgrade an existing host](deploy/upgrade-existing.md)**. The original guide's R2 and
Litestream sections describe the previous image. The current image uses encrypted D1 snapshots.

## Measured size and free-tier budget

Measured locally on 2026-11-24 using the release image's `dashboard seed -reset` in an
isolated data directory: **72 tables, 1,060,864 bytes SQLite, 67,507 bytes gzip**. Seed:
6 users, 6 events, 6 purchase requests, 18 journal lines, plus the full schema/indexes.
The original gzip measurement used Python's level 9. The static Go backup helper's
gzip plus age stream measured **72,059 ciphertext bytes** on this seed; plan at
**75,000 ciphertext bytes per seed copy**.
No production database was available to measure. WAL is captured consistently with
SQLite's online backup API, the same API used by `.backup`; integrity is checked first.
Receipts remain outside SQLite (Drive); this backs up their metadata, not Drive contents.

Limits checked against official documentation:
[D1 limits](https://developers.cloudflare.com/d1/platform/limits/) lists 500 MB per free
database, 5 GB per account, 10 databases and 2,000,000 bytes per row;
[D1 pricing](https://developers.cloudflare.com/d1/platform/pricing/) lists 5 million rows
read and 100,000 written daily, including index changes. Enforcement began
[September 1, 2026](https://developers.cloudflare.com/changelog/post/2026-09-01-d1-free-tier-limit-enforcement/).
[Workers limits](https://developers.cloudflare.com/workers/platform/limits/) lists
100,000 requests daily, 10 ms CPU per request, and 50 subrequests per invocation.

Each ciphertext chunk is at most **1,900,000 bytes**. The sink accepts at most 13 chunks
(24,700,000 bytes) per snapshot so commit performs at most 16 D1 operations and uses
one chunk of memory at a time. The final hash covers the complete ordered ciphertext.
Free CPU allowance still needs a real-account rehearsal with the largest expected DB;
local Wrangler does not enforce production CPU limits. Oversize or rejected uploads
alert and stop; they do not fall back to plaintext or another paid service.

Retention is 48 hourly + 30 daily + 4 weekly membership exports: 82 copies. Daily
promotion reclassifies one hourly snapshot rather than copying its BLOBs. Conservative
reserved storage per copy is `2 * (ceil(size/4096)*4096 + chunks*8192 + 16384)`:
page rounding, metadata and twofold overhead. The hard logical reservation ceiling and
current physical-usage guard are 450,000,000 bytes, leaving 50 MB below the D1 cap.
The physical check does not add the new reservation to the file high-water size: deleted
pages can be reused. Actual D1 storage errors still stop and alert for operator recovery.
At 75,000 bytes this reserves 204,800 bytes/copy, or **16,793,600 bytes for 82**.

Growth assumption: 100 officers, 1,000 members, 200 events/year, 20,000 activity/audit
rows/year at 1 KB per row plus 100% index/schema allowance gives roughly 40 MB raw/year.
At an illustrative 10:1 gzip ratio that is 4 MB/copy: full retention reserves about
663 MB and cannot fit; 24 hourly + 30 daily + 4 weekly still needs about 469 MB.
At 3 MB/copy, those figures are about 498 MB and 352 MB respectively. These are
planning scenarios, not measured compression guarantees. The client automatically prunes
to 24 hourly when the full-retention estimate or projected usage is too large. It also
prunes older committed copies by reservation space needed for the next upload,
while preserving the newest committed copy of each kind. Daily/weekly count targets can
therefore shrink under pressure. If protected copies, pending uploads, physical storage
or daily budgets still block an upload, it alerts loudly through `DISCORD_WEBHOOK_URL`.
It cannot promise all requested copies as data grows.

Before every upload the client prunes, checks logical bytes, reserved writes, and upload
count, then obtains an atomic server reservation. Each snapshot reserves `100+20*chunks`
writes including BLOB/index changes, commit, promotion and deletion. One-chunk snapshots
at 24/day cost 2,880 reserved writes/day; one weekly export adds 120 on its day. Requests
are bounded by 100/upload and reads by 3,000/upload for the supplied client, including
usage scans, prune and download verification. Normal one-chunk traffic is about 200
Worker requests and fewer than 72,000 scanned rows/day. Hard limits: 200 uploads/day,
40,000 reserved writes/day, 100 total snapshot records, bounded bodies and immutable chunks.
This allows at most 20,000 planned requests and 600,000 scanned rows/day; 10,000 additional
write allowance covers cleanup/accounting across midnight. UTC resets match Cloudflare.

**Shared-account allocation is a prerequisite:** reserve at most 50,000 writes, 4 million
reads, 80,000 Worker requests and 4.5 GB for membership and other applications. A Worker
bound only to backups cannot observe or constrain the other databases/account workloads.
Therefore no claim of an unconditional account-wide limit guarantee is possible. Check
account metrics before enabling and after each weekly export; unexpected other traffic
can still exhaust the account quota. Cloudflare rejects over-quota queries until UTC reset.
Do not place the broad account API token in this Worker just to measure the account.

Storage alerts start at 350 MB (70% of 500 MB). Write alerts conservatively include the
50,000-write allocation for other workloads, so 20,000 reserved backup writes reaches
70% of the account write budget. The sink separately rejects at its 40,000 reservation
ceiling. Unauthorized floods are controlled by Access; the planned request/read bounds
assume only the supplied trusted client, not an attacker who holds both credentials.

For setup, reviewer settings, off-box recovery and the monthly drill, use the linked
runbooks above. Automatic boot restore stages ciphertext and stops for off-box decryption,
preserving D5. Automatic migration rollback uses a temporary stopped local checkpoint.

The release runtime is `busybox:stable-musl`. Both dashboard and backup helper are
statically linked with `release,osusergo,netgo,sqlite_omit_load_extension` (plus `embedweb`
for the dashboard). SQLite's backup API, gzip and age encryption are compiled into the
backup helper; no Python, Debian runtime packages, Litestream or external age executable
are shipped. Python and the age CLI are needed only on the host/recovery workstation.
Re-scan the combined final image after merging the application security changes: a clean
scan of this branch is not proof for the merged artifact.

| You want | Go to |
| --- | --- |
| a first deployment (Docker, Cloudflare, backups, Google) | [Deploy the dashboard, step by step](deploy/README.md) |
| the Cloudflare setup (tunnel, Access, bot settings, R2) | [Cloudflare setup](deploy/cloudflare.md) |
| to upgrade, roll back, move to a new machine or publish a release | [Upgrade](deploy/upgrade.md) |
| the org Google calendar | [Google calendar setup](deploy/google-calendar.md) |
| a fix for an error | [Troubleshooting](deploy/troubleshooting.md) |
| a Pi under systemd, a self-built image, a quick-tunnel practice run, or what's in the image | [Other setups](deploy/other-setups.md) |
| every setting and its default | [Configuration](configuration.md) |

## Live Free-plan rehearsal

Before enabling the production timer, rehearse against the authenticated custom-domain
Worker on the actual Workers Free account. Local Wrangler does not enforce edge CPU.
Upload the largest expected ciphertext, including a full 1,900,000-byte chunk and a
13-chunk snapshot if that maximum is intended for use. Exercise reservation/prune,
chunk PUT, commit (which reads and hashes every chunk), download and daily promotion.
In the Cloudflare Worker observability metrics/logs, record CPU time for each route,
including cold and repeated requests, and inspect CPU-limit errors. **Workers Free allows
10 ms CPU per invocation.** Record the maximum and distribution with the ciphertext size,
chunk count, date and Worker version; verify headroom below 10 ms before accepting that
snapshot size. Reduce the supported workload and repeat if it fails; do not assume a
paid plan or infer CPU compatibility from a local round trip. Continue the 24-hour
rehearsal while checking account-wide requests, D1 reads/writes/storage and backup alerts.

## Cloudflare Tunnel + Access

`dashboard mcp-check` and some older pages name "docs/deployment.md, Cloudflare Tunnel + Access,
step N". The step numbers are unchanged in [Cloudflare setup](deploy/cloudflare.md):

| Step | Now |
| --- | --- |
| 1 | [Create the tunnel](deploy/cloudflare.md#1-create-the-tunnel) |
| 2 | [One-time PIN and the groups](deploy/cloudflare.md#2-turn-on-one-time-pin-and-create-the-groups) |
| 3 | [The Access application](deploy/cloudflare.md#3-protect-the-whole-site-with-an-access-application) |
| 4 | [Agent paths bypass Access](deploy/cloudflare.md#4-let-ai-agents-through-on-their-own-paths) |
| 5 | [The service token](deploy/cloudflare.md#5-create-a-service-token-for-bots) |
| 6 | [Bot Fight Mode, Browser Integrity Check, WAF](deploy/cloudflare.md#6-turn-off-bot-checks-for-agents) |
| 7 | [Cache rule](deploy/cloudflare.md#7-never-cache-the-agent-paths) |
| 8 | [Rocket Loader and Email Obfuscation](deploy/cloudflare.md#8-turn-off-rocket-loader-and-email-obfuscation) |
| 9 (check from outside) | [Check it from outside](deploy/cloudflare.md#10-check-it-from-outside), now step 10 |


## Private R2 file storage

R2 is optional file storage, enabled with `FILE_STORAGE=r2`. D1 remains the database
backup target. The app proxies private file bytes after the existing requester/director
receipt check. Each authorized download costs one Class B request; a leaked presigned
URL could produce unlimited unmetered reads, so this implementation issues none.
At the read ceiling it returns HTTP 507. Files use server-generated `files/<uuid>`
keys, Standard storage, MIME and magic-byte checks. SVG and HTML are refused.

### Verified allowance and accounting

Verified 2026-11-25 against [Cloudflare R2 pricing](https://developers.cloudflare.com/r2/pricing/):
Standard includes 10 GB-month storage, 1 million Class A operations and 10 million
Class B operations each month, with free egress. Infrequent Access has no free tier.
Storage is based on daily peaks averaged over the billing period. PutObject and
ListObjects/ListMultipartUploads are Class A; GetObject is Class B. DeleteObject
and AbortMultipartUpload are free. Authenticated failures can still count.

The SQLite ledger reserves before sending. Limits are 9,000,000,000 live bytes,
900,000 A and 9,000,000 B per UTC calendar month, leaving 10% margin. A second
guard applies the same ceilings over the current UTC day plus the preceding 31
days, covering billing periods up to 31 days with an extra day for in-flight
requests at midnight. Calendar rollover never grants a second allowance within
a billing cycle. A 10 MiB
upload requires all 10,485,760 bytes of headroom. At most 858 such files fit in
9 GB; 20/user/day permits at most 200 MiB/user/day. A Discord warning is emitted
at 70%; its durable pending flag retries failed Discord delivery until accepted.
After acceptance, that metric does not repeat its warning that month. Failed
delivery is retried no more than once per 15 minutes, including concurrent calls.
The first refusal at 90% of a metric sends its own warning, once per month, even
if the 70% warning was already delivered. Refused calls make no R2 request and
move no counter. A failed inventory scan also sends a warning.
Deletes are tracked separately at zero billable operations. The client performs
no redirects or application retries, disables connection reuse to avoid transport
replay, uses a 20-second timeout, and retains a day of rolling-window margin at month boundaries.

The app scans every object page and checks for multipart uploads in the background
right after startup and every 24 hours, retrying a failed scan every five minutes.
Startup never waits on R2: an outage, a rejected token, an exhausted budget or a
pending usage re-seed leaves the rest of the dashboard running, keeps uploads closed and
sends a Discord warning. Only configuration errors (account or bucket format,
missing secret or webhook, ledger bound to another bucket) stop startup. For 858 objects, that costs two Class A requests per scan (one
object page and one multipart check), about 62/month plus starts. The guard needs
no analytics or account token: the same bucket-scoped Object Read & Write S3 token
can list. An optional read-only analytics token only improves the re-seed after a
restore (below). Uploads close if inventory is older than 25 hours or a scan fails.
Each scan sets the byte total to the listed bytes plus reservations that are
unlisted but less than one hour old. Those may still be a PUT in flight from
another process; the client timeout is 20 seconds and it never retries. An unlisted
reservation older than one hour is released. A timed-out or abandoned upload
(for example, the uploader closed the tab) therefore holds its bytes until the
first scan after one hour, normally the next daily scan. Completed failed PUT
responses trigger a free DELETE and release immediately; rejected credentials
release immediately. Never reset operation counters: listing cannot recover
operations already spent.
SQLite writer connections use FULL synchronous commits. This trades some space
and write throughput for conservative accounting.

[Incomplete multipart parts consume storage until completed or aborted](https://developers.cloudflare.com/r2/objects/upload-objects/).
The app only uses single-part uploads. Any incomplete upload or non-Standard
object in its inventory fails the scan and closes uploads. Configure the
[one-day abort lifecycle](https://developers.cloudflare.com/r2/buckets/object-lifecycles/)
even though the app never initiates multipart uploads. The default is seven days.

### Conditions for staying at $0

R2 allowances are shared across an account. This ledger limits this app's requests;
it cannot impose an account-wide Cloudflare spending cap. Enable only for an
otherwise unused R2 account and a new empty bucket, with no other writer, public
access, Worker bindings, Sippy, Super Slurper, or storage-class transitions. Keep
setup activity below 100 operations (well inside the reserved margin). Do not
reuse an account that already spent its monthly free allowance. Use one dashboard
process and one durable ledger, keep the host clock synchronized, and retain that
ledger across redeploys. Billing notifications are informational, not hard caps.
These operating conditions are required for the $0 budget; the app cannot protect
against a separate client or administrator spending outside its ledger.

Never reset budget tables. A restored database cannot know the R2 operations spent
after its snapshot, and inventory cannot reconstruct them. The official off-box
restore command therefore adds an `r2_reseed` marker to the restored DB (there is
no lock). On its next start the app re-seeds this month's usage counters before it
sends another billable request:

1. With `R2_ANALYTICS_TOKEN_FILE` set, it reads the bucket's Class A and Class B
   operations for the current UTC month and for the guard's rolling 32-day window,
   plus stored bytes, from Cloudflare's free
   [GraphQL Analytics API](https://developers.cloudflare.com/r2/platform/metrics-analytics/)
   (`r2OperationsAdaptiveGroups` and `r2StorageAdaptiveGroups`), within 30 seconds.
2. It merges each counter and each window with the restored ledger by maximum, so
   no counter ever goes down. Unknown operation types count as Class A; deletes
   are free. The next inventory scan replaces the byte figure with listed bytes.
3. If no analytics token is configured, or Cloudflare does not answer, it instead
   assumes 50% of each free-tier allowance is already used in both windows, takes
   the maximum with the ledger and sends one Discord warning. With a token
   configured it retries the measurement hourly, which can still raise the
   counters further.

Uploads and downloads then continue under the normal 70% warning and 90%
refusal. While the first re-seed attempt runs (at most about 30 seconds after
startup), billable R2 requests answer 507 with a retry message; other dashboard
functions are unaffected. A ledger with recorded spending but no
`r2-usage-ledger.json` beside it (for example a database copied to a new host) is
treated the same way. Analytics can lag by a few minutes, so operations spent in
the minutes just before a restore may be missed; the 10% margin below the free
tier absorbs that. Do not remove the marker. Routine image rollback instead merges
the candidate image's R2 counters into the checkpoint before restoring it, so it
preserves spent operations without a re-seed.

Rollback can also run through an older image whose `dashboard-backup` predates R2
and cannot merge, for example the first update to an R2 image, interrupted-update
recovery, or a manual copy of `.update-before.db`. For that case the app keeps
`/data/r2-usage-ledger.json` beside the database. It holds operation counts for the
last 40 days and this and last month. The app rewrites and
fsyncs it inside the same SQLite transaction as every billable reservation, before
the request is sent. At startup the app takes the maximum of the file and the
database, so spending erased from `dashboard.db` is restored; any recovery also
forces a fresh inventory before uploads reopen. The file is never part of a checkpoint or
backup. **Never delete, edit or replace it**, and keep it with the data volume.
If it is unreadable, the app keeps running but refuses billable R2 requests until
an operator restores a valid copy. Losing it together with the volume is the
off-box restore case, which the re-seed covers.
For a manual restore (including manually decrypting an encrypted-only download),
add the marker yourself before starting the app, even if the old snapshot
predates R2's tables:

```sql
CREATE TABLE IF NOT EXISTS r2_reseed(id INTEGER PRIMARY KEY CHECK(id=1), requested_at INTEGER NOT NULL, fallback_at INTEGER NOT NULL DEFAULT 0, alerted INTEGER NOT NULL DEFAULT 0);
INSERT INTO r2_reseed(id,requested_at) VALUES(1,CAST(strftime('%s','now') AS INTEGER))
ON CONFLICT(id) DO UPDATE SET requested_at=excluded.requested_at,fallback_at=0,alerted=0;
```

Keep the original DB/ledger when rolling back an application image. Stored backend
labels are preserved; a local receipt is never looked up as an R2 key. Existing
Drive links retain their authorization check. The optional second R2 backup copy is omitted; no migration or production upload
was run. The operator-only migration command below retains source files.

### Person-only setup

Run these steps yourself after the change is reviewed; agents must not change
Cloudflare settings or deploy it.

1. In R2, create a new default-jurisdiction bucket, e.g. `officer-private-files`,
   with **Standard** as its default storage class. Confirm the account conditions
   above and that this bucket is empty.
2. In the bucket Settings, leave **Public Development URL (r2.dev) disabled** and
   **Custom Domains empty**. Do not add public access or CORS.
3. R2 Overview > Manage API Tokens > Create Account API Token > **Object Read & Write**
   > **Apply to specific buckets only** > select only this bucket. Save Access Key ID
   and Secret Access Key once. See [token scopes](https://developers.cloudflare.com/r2/api/tokens/).
   These are app credentials, separate from D1, backup, Tunnel, Access and membership tokens.
4. Apply the one-day abort rule yourself. Create `r2-lifecycle.json` with the following
   complete lifecycle configuration (new bucket only; it replaces existing rules):

   ```json
   {"rules":[{"id":"abort-incomplete-after-one-day","enabled":true,"conditions":{"prefix":""},"abortMultipartUploadsTransition":{"condition":{"type":"Age","maxAge":86400}}}]}
   ```

   ```sh
   npx wrangler r2 bucket lifecycle set officer-private-files --file r2-lifecycle.json
   npx wrangler r2 bucket lifecycle list officer-private-files
   ```

   Use your interactive administrator login for setup; lifecycle management requires
   Workers R2 Storage Write. Never mount that administrator token in the app. Verify
   the rule covers the whole bucket and has no transition to Infrequent Access.
5. In Cloudflare account **Notifications > Add > Billable Usage**, set R2 usage alerts
   if offered by your plan. Otherwise open **Manage Account > Billing > Billable Usage > Create budget alert**, create an
   account budget alert at the **lowest amount the UI allows**, and confirm delivery
   to the billing email. See [billing notifications](https://developers.cloudflare.com/billing/understand/usage-based-billing/).
   An alert is a backstop, not a provider-side $0 cap; do not enable paid monitoring.
6. Save credentials as host files `secrets/app/r2-access-key-id` and
   `secrets/app/r2-secret-access-key`, owned/readable by the container UID 10001,
   mode 0400. Do not put values into shell history or tracked files. In `dashboard.env`:

   ```dotenv
   FILE_STORAGE=r2
   R2_ACCOUNT_ID=<account-id>
   R2_BUCKET=officer-private-files
   R2_ACCESS_KEY_ID_FILE=/run/secrets/app/r2-access-key-id
   R2_SECRET_ACCESS_KEY_FILE=/run/secrets/app/r2-secret-access-key
   ```

   Keep the existing Discord webhook `_FILE` setting. The hardened compose already
   mounts `secrets/app` read-only only into the app. No backup credentials change.

7. Optional, for accurate counters after a restore: in **My Profile > API Tokens >
   Create Token > Create Custom Token**, grant only **Account > Account Analytics >
   Read**, for this account only, with no other permissions. It is free and
   read-only, and is separate from the R2 S3 token above. Save it as
   `secrets/app/r2-analytics-token` (owner UID 10001, mode 0400) and add
   `R2_ANALYTICS_TOKEN_FILE=/run/secrets/app/r2-analytics-token` to `dashboard.env`.
   Without it, a restore makes the guard assume half of each allowance is used
   (see above). The direct `R2_ANALYTICS_TOKEN` variable is rejected.
   Follow the approved image/update steps. Uploads stay closed until the background
   scan right after startup validates the credentials with a full private inventory.
   Only configuration errors fail startup. A rejected token, a network or inventory
   failure, or an exhausted budget keeps the dashboard running with uploads closed
   and sends a Discord warning; check the app log for `R2 reconciliation failed`.
   Never paste credentials into a PR.


### Optional one-shot file migration

Stop the app first and preserve its DB and original file storage. With the R2
settings above and the old `RECEIPTS_DIR` or Google Drive credential/folder
settings still present, run from the container's `/app` directory:

```sh
./dashboard files-migrate-r2 -source local
# Or -source drive. Default is a read-only plan; it makes no R2 requests.
./dashboard files-migrate-r2 -source local -apply
```

The dry run opens SQLite read-only and prints candidate count, recorded bytes,
and the minimum Class A upload count (inventory requests cost extra). Apply
copies receipts and design files, checks their allowlisted MIME and content,
and updates their existing rows without changing ownership or authorization.
It never deletes a source file. Reruns skip rows already migrated; a durable
journal records server-generated keys before uploading, so an interrupted copy
reuses its reserved key and space. Retried HTTP requests still consume Class A.
The same guard applies, including 20 uploads per UTC day for the migration
operator identity; a large migration pauses safely and can be resumed on the
next day. No migration has been run on real files. Test the isolated fake with
`scripts/test-r2.ps1` (Docker Desktop); it builds pinned MinIO source, uses
synthetic credentials, and verifies private upload/download/authz/delete.
