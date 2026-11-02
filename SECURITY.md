# Security policy

## Supported versions

Only the `main` branch receives security fixes. Forks are not supported.

## Report privately

Use [GitHub private vulnerability reporting](https://github.com/cseed-org/officer-dashboard/security/advisories/new).
Do not publish exploit details, credentials or personal data in public issues.
If private reporting is unavailable, ask a maintainer for a private channel
without disclosing the vulnerability publicly.

Include the affected commit/release, impact, reproduction steps, a minimal proof
of concept and any relevant redacted logs. Share secrets only through the private
report, and avoid collecting other people's data.

We aim to acknowledge reports within **3 business days**, provide an initial
assessment within **7 business days**, and update confirmed reports at least every
**14 days** until resolution. These are targets, not guarantees. We will agree on
remediation and coordinated disclosure timing with the reporter based on severity.

## Scope

Report vulnerabilities in the dashboard and its supported deployment paths, including:

- Discord OAuth, redirects, session cookies, session expiry and revocation.
- Authorization for leadership, director and officer roles, plus owner privileges.
- Cloudflare D1 member write-back, credentials and data boundaries.
- Invites, invite acceptance and onboarding.
- CSV import validation and handling of untrusted data.
- File uploads, downloads, design files and financial receipts.
- Production exposure of development authentication. `/auth/dev` is compiled out
  of builds without the `dev` tag: see [release registration](server/internal/api/devauth_off.go)
  and its [regression test](server/internal/api/devauth_off_test.go).
  The [Docker build](server/Dockerfile) and [release target](server/Makefile)
  use `embedweb`, not `dev`. Never deploy a build made with `-tags dev`.

## Out of scope

General support and feature requests, vulnerabilities solely in third-party
services (report those to their owners), and unsupported forks are outside this
project's response scope. Missing optional hardening without demonstrated impact
may be treated as a suggestion. Do not perform denial-of-service, social engineering,
physical attacks, credential stuffing or tests against other people's accounts.
Test locally or in an environment where you have explicit authorization.

## Safe harbor

CSEED will not pursue legal action for good-faith research consistent with this
policy: minimize access, stop if sensitive data is encountered, keep findings
private while coordinating a fix, and report promptly. This commitment covers
only CSEED's rights; it cannot authorize testing of third-party systems or waive
others' rights. There is no promised bounty.
