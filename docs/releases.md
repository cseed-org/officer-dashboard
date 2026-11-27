# Releases and dependency updates

Every push to `main` runs the pinned release-please CLI with a GitHub App
token, using the root `release-please-config.json` and `.release-please-manifest.json`. It maintains one
release PR containing `CHANGELOG.md`, `version.txt`, the release manifest, and both
Node package versions and lockfiles. Only `feat`,
`fix`, `perf` and `deps` commits propose a release or appear in its notes;
`deps-dev`, `docs`, `chore`, `ci`, `build`, `test`, `refactor`, `style`
and `revert` changes stay hidden and never cut a release on their own. A gate
(`scripts/release-gate.mjs`) enforces this strictly: the workflow skips the
release-pr step unless a commit since the last `vX.Y.Z` tag has one of the four
types, so a breaking marker or `BREAKING CHANGE` note on a hidden type (such as
`ci!: replace the build environment`) cannot open a release PR.

Review the proposed version and notes, wait for all required checks, then squash
merge the release PR with its generated `chore(main): release ...` title. Release
PRs require a person to merge them.

Release-please creates the immutable `vX.Y.Z` tag and GitHub Release. The release
image workflow waits up to 75 minutes — beyond main CI's worst-case critical path
of about 60 minutes — for that commit's successful main CI run and `publish` job,
retrying transient GitHub API errors, then promotes the image digest that exact
run attempt recorded as a CI artifact to semantic version tags.
It does not build another image and never resolves the mutable sha-commit
source tag. Failed or missing publication stops promotion;
the GitHub Release may exist before image aliases or the attestation are ready. A
timeout while main CI is still running is routine, not an incident: reruns are
idempotent, so rerun the failed release-image workflow
from Actions once CI is green; keep the original release/tag. See
[image tags and verification](deployment.md#release-images-and-verification).

Dependabot uses `deps:` / `deps-dev:` titles. The auto-merge workflow runs only
when both the PR author and the event actor are `dependabot[bot]`, parses the
structured `updated-dependencies` YAML trailer in Dependabot's own commit
metadata (grouped updates included), and fails closed: every listed dependency
must carry a recognized semver update type or the PR stays manual. Minor and
patch PRs are queued into GitHub's auto-merge queue pinned to the inspected head
commit; they still need every required check and any required approval
to pass. Major updates receive the
`major-update` label — created on the fly if missing — and require manual review
and merging. PRs with missing, incomplete or unrecognized metadata are left alone. Node upgrades stay on LTS majors; odd-numbered Node majors are excluded from automatic dependency updates.

Maintainers must enable repository auto-merge and require the `pr-title` check
alongside existing checks.
Install a repository-scoped GitHub App with contents and pull requests write
access; save `RELEASE_APP_ID` and `RELEASE_APP_PRIVATE_KEY` as repository secrets.
Allow that App to create `v*` tags through the tag-creation ruleset, while preserving
the separate rules preventing tag updates/deletion. An App token lets release PRs
and GitHub Releases trigger CI; the default `GITHUB_TOKEN` would suppress those runs.
Missing App secrets fail explicitly. Repository settings, secrets, rulesets and
the protected `production` environment require maintainer setup; workflows do not
create them.

## One-time release App setup

**Repository administrators only.** Ordinary contributions need no App or secrets
setup. The upstream App already exists; these instructions are for initial setup
on another repository or recovery by its administrators.

Run this **once per repository, not per server**, from a machine with Node and
authenticated GitHub CLI (`gh auth login`). The person running it needs permission
to create an organization GitHub App, set repository secrets, install that App,
and administer repository rulesets. Preview without making requests or changes:

```sh
node scripts/setup-release-app.mjs --org cseed-org --repo officer-dashboard --dry-run
```

When ready, run the same command without `--dry-run`. Open the printed loopback
URL in a **normal desktop browser on the same machine** and approve GitHub's App
manifest form. If the form cannot be submitted, use the manual fallback below. The helper saves `RELEASE_APP_ID` and
`RELEASE_APP_PRIVATE_KEY` through `gh secret set` using stdin; it never prints or
writes the private key to disk. Follow the printed installation link, select
**Only select repositories**, and select **officer-dashboard** only. The helper
waits up to 10 minutes, verifies that exact repository selection, then adds the
App to **Version tags - owners create** while preserving existing bypass actors.
It leaves **Version tags - immutable** alone.

The default App name is `cseed-release`; change it with `--name`. The server binds
only to `127.0.0.1`, using `--port`, then `PANE_PORT`, then an OS-assigned free port.
If a port is occupied, use `--port 0`. Keep this terminal running during approval
and installation. The loopback callback requires the person to use a browser on
the same machine as the helper.

If both secrets already exist, the helper stops without making changes. `--force`
intentionally creates a replacement App and overwrites the secrets; it does not
remove the old App or its bypass. If only one secret exists, it stops for manual
recovery unless forced. An installation timeout leaves saved secrets in place but
does not add the bypass: finish installing that App and add its creation bypass
manually, rather than creating another App. Check the terminal's completion message
before merging a release PR. This helper does not enable auto-merge, change the
action allowlist, create labels, or configure the production environment.

### Manual fallback

Use this if the manifest form cannot be submitted. If `cseed-release` already
exists, finish or repair its setup instead of creating a duplicate.

1. Open the organization's **Settings > Developer settings > GitHub Apps > New GitHub App**
   ([organization App form](https://github.com/organizations/cseed-org/settings/apps/new))
   in a normal desktop browser. Set the App name to `cseed-release` and the homepage
   to this repository. Disable the webhook. Under repository permissions, grant
   **Contents: Read and write** and **Pull requests: Read and write**; metadata read
   access is automatic. Restrict installation to this organization.
2. Create the App, record its **App ID** (not client ID), and generate a private key.
   Keep the downloaded PEM file private. Install the App using **Only select repositories**,
   selecting **officer-dashboard** only.
3. Save both repository secrets with authenticated GitHub CLI. The first command
   prompts for the App ID; the second reads the PEM file through stdin (Bash):

   ```sh
   gh secret set RELEASE_APP_ID --repo cseed-org/officer-dashboard
   gh secret set RELEASE_APP_PRIVATE_KEY --repo cseed-org/officer-dashboard < /path/to/private-key.pem
   ```

4. In repository **Settings > Rules > Rulesets > Version tags - owners create**,
   add this App to the bypass list with **Always allow**, preserving existing actors.
   Keep **Version tags - immutable** unchanged. Verify both secret names are present
   and the App installation lists only this repository before merging a release PR.
