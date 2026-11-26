# Deployment

For the current $0 Free-plan deployment, start with **[Fresh install](deploy/fresh-install.md)**
or **[Upgrade an existing host](deploy/upgrade-existing.md)**. The original guide's R2 and
Litestream sections describe the previous image. The current image uses encrypted D1 snapshots,
plus an independent daily/monthly copy in Google Drive ([below](#a-second-independent-copy-in-google-drive)).

## Release images and verification

CI on `main` builds `ghcr.io/cseed-org/officer-dashboard:main` and
`:sha-<7-character commit>` with SBOM and build provenance, and records the
pushed digest as a run-scoped CI artifact. A published stable
GitHub Release such as `v0.15.0` promotes that recorded multi-platform
digest to `:0.15.0`, `:0.15` and `:latest`. Releases at major version 1 and later
also update `:1` (or the corresponding major); there is no `:0` alias. Prereleases
are excluded. The exact version always (re)points at the recorded digest —
promotion fails rather than replace a different build — while `latest` and the
minor/major aliases advance only when the release is the highest published
stable version in their scope, so rerunning an older release leaves newer
aliases alone. Version aliases and `latest` can move: pin deployments by digest.
The image's binary version remains the source commit's short SHA because release
promotion never rebuilds it.

The release workflow requires a successful `main` CI run, including its `publish`
job, for the tag's resolved commit; it waits up to 75 minutes for that run, then
redeems the digest that exact run attempt recorded — never the mutable
`:sha-<commit>` source tag, which a rerun could have replaced. It
checks that the image revision matches the
full commit and that all promoted tags keep the same digest. It adds a GitHub
artifact attestation (Sigstore-backed) for the promoted digest using GitHub's
OIDC identity.
The release attestation records promotion; the original build's provenance and
SBOM remain attached to the image. Tags never authorize a build.

The GHCR package is private: authenticate with a `read:packages` token and sign
in to the registry (for example `gh auth login` plus
`echo "$(gh auth token)" | docker login ghcr.io -u <user> --password-stdin`)
before inspecting or verifying. With the GitHub CLI installed, resolve
the release digest and verify the promotion attestation before deploying:

```sh
IMAGE=ghcr.io/cseed-org/officer-dashboard
DIGEST=$(docker buildx imagetools inspect "$IMAGE:0.15.0" --format '{{.Manifest.Digest}}')
gh attestation verify "oci://$IMAGE@$DIGEST" --owner cseed-org
```

Change the version when verifying another release. Add
`--signer-workflow cseed-org/officer-dashboard/.github/workflows/release-publish.yml`
to also require the promotion workflow as the attestation signer.

Semantic release publication does not deploy production. Continue using the
separate `promote-production.yml` workflow and protected `production` environment
for production promotion. Maintainers must create that environment and its
protection rules before using it.

## Measured size and free-tier budget

Measured locally on 2026-11-24 using the release image's `dashboard seed -reset` in an
isolated data directory: **72 tables, 1,060,864 bytes SQLite, 67,507 bytes gzip**. Seed:
6 users, 6 events, 6 purchase requests, 18 journal lines, plus the full schema/indexes.
The original gzip measurement used Python's level 9. The static Go backup helper's
gzip plus age stream measured **72,059 ciphertext bytes** on this seed; plan at
**75,000 ciphertext bytes per seed copy**.
No production database was available to measure. WAL is captured consistently with
SQLite's online backup API, the same API used by `.backup`; integrity is checked first.
Receipts and design files remain outside SQLite (in the Shared Drive); this backs up their metadata, not Drive contents.

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

## A second, independent copy in Google Drive

The D1 snapshots above are one off-box copy; the shared Google Drive holds another, on a
separate path with no shared credential. Once per UTC day the backup sidecar also hands
that hour's ciphertext (the same age-over-gzip bytes, not a second database copy) to the
dashboard through `/data/drive-outbox/`: `dashboard-<YYYY-MM-DD>.db.gz.age` plus a sha256
manifest, at most three days waiting. The dashboard, which holds the Drive sign-in,
uploads each waiting file to `Backups/daily/`, adds a first-of-month copy to
`Backups/monthly/dashboard-<YYYY-MM>.db.gz.age`, verifies Drive's stored size and sha256,
and only then deletes its local copy. Uploads are idempotent (a file already in Drive
under the same name and sha is only deleted locally) and retried hourly after quota,
rate-limit or network failures; failures and any snapshot waiting over three days alert
on Discord. After each verified upload the uploader keeps the newest 30 daily and
12 monthly copies. `entrypoint.sh` keeps every `BACKUP_*` variable out of the app, both
services mount the same `dashboard-data` volume, and the age identity stays offline.

Restore from Drive (the dashboard never decrypts). `drive-backup` reads the stored Drive
sign-in from the live database, so run it only while `/data/dashboard.db` exists — it
refuses to create one. When the database itself is gone (total loss), skip the CLI:
open the shared drive in the Google Drive web UI, download the newest file from
`Backups/daily` (or `Backups/monthly`) by hand, and finish with
`backup.py restore --input` below.

    cd server/deploy
    dc="docker compose --env-file .env --env-file prod-image.env -f compose.cloudflare.yml"
    $dc run --rm --no-deps --entrypoint /app/dashboard dashboard drive-backup list
    $dc run --rm --no-deps --entrypoint /app/dashboard dashboard drive-backup restore \
      --latest --output /data/drive-restore.db.gz.age

`drive-backup restore` verifies the ciphertext against Drive's stored sha256 and never
overwrites an existing file. Copy it off-box (or use the hand-downloaded file) and finish
with the age identity:

    python3 backup.py restore --input drive-restore.db.gz.age \
      --identity <identity file> --output restored.db

`--input` also works for a file downloaded by hand from the Drive web UI: it decrypts,
gunzips, checks SQLite integrity, and only then places the database.

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

## File storage: the Google Shared Drive (person-only setup)

Receipts and design files go to one Google Shared Drive, which the dashboard reaches as a
member Google account (see [Integrations](integrations.md#files-in-a-google-shared-drive)).
The Drive API's standard quota costs nothing and needs no billing account; over its limits
Google answers 403 or 429, never a bill. These steps need a person; nothing in the
repository can do them:

1. **The Shared Drive.** A UW advisor creates it (for example "CSEED dashboard"), turns
   off sharing outside the organisation, and creates the root folder in it.
2. **A Google Cloud project with no billing.** Use the project that holds
   `GOOGLE_OAUTH_CLIENT_ID` (see [Google calendar setup](deploy/google-calendar.md)), or a
   new one. Enable the **Google Drive API** and don't attach a billing account.
3. **The OAuth redirect URI.** In that OAuth client, add the authorized redirect URI
   `${APP_URL}/auth/drive-google/callback`.
4. **Publish the OAuth consent screen to Production.** In Testing mode Google expires
   refresh tokens after 7 days, and file storage would stop every week. Add the
   `https://www.googleapis.com/auth/drive` scope to the consent screen.
5. **The account.** Choose the Google account the dashboard stores files as (a UW account
   that outlives one officer's term is best) and give it **Content manager** on the
   Shared Drive. Officers need no Drive access; they open files through the dashboard.
6. **Connect it.** Signed in to the dashboard as an owner, open Settings → Google Drive file
   storage, choose **Connect Google Drive**, sign in as that account and allow Drive access.
   Then paste the root folder's link and choose **Use this folder**. The status shows the
   account, the folder, and the storage used.
7. **Alerts.** Set `DISCORD_WEBHOOK_URL` so owners hear when the drive is 70 % full
   (`DRIVE_QUOTA_BYTES`, default 100 GB), full, or the account's access was revoked.

**Alternative: a service account.** Instead of steps 4 to 6, create a service account in
the project, add it to the Shared Drive as a Content manager (and to nothing else), save
its key as `secrets/app/google-service-account-json`, and set
`GOOGLE_SERVICE_ACCOUNT_JSON_FILE` and `DRIVE_ROOT_FOLDER_ID`. A direct
`GOOGLE_SERVICE_ACCOUNT_JSON` value is refused at startup. A connected account wins over
the service account.
