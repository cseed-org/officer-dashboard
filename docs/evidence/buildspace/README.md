# Buildspace draft evidence

Captured against the seeded Go dev server at `http://127.0.0.1:18145`, branch `v05-buildspace`, 2026-11-03, with system Chromium through Playwright. Refreshed after the V0.5 review polish (distinct blocks icon, partner/activity columns, labeled cards and responsive grouped layout). These are running application captures, with reduced motion enabled. The database was disposable and held only seeded/sample test records.

| Directory | Desktop 1440 × 1000 | Phone 390 × 1000 |
| --- | --- | --- |
| People, light | [Screenshot](people-1440-light.png) | [Screenshot](people-390-light.png) |
| People, dark | [Screenshot](people-1440-dark.png) | [Screenshot](people-390-dark.png) |
| Project cards by track, light | [Screenshot](projects-1440-light.png) | [Screenshot](projects-390-light.png) |
| Project cards by track, dark | [Screenshot](projects-1440-dark.png) | [Screenshot](projects-390-dark.png) |

## Proven browser journeys

- People table, grouped project cards, project drawer, Escape dismissal and stage board at both widths in both themes.
- No horizontal page overflow and no browser script errors.
- People rows show partners, open general requests, open purchases and absences this week; project-wide entries count for each builder and person-specific absences stay scoped to that person.
- Cards label builders and mentors, show “1 open request” for singular counts, and track groups occupy multiple desktop columns.
- Search filters persist in the URL and survive reloads.
- Manual weekly entry saved from a person drawer and read back from the API with that builder's ID.
- Project and builder creation saved and read back through the API.
- Editing the seeded Hardware / Demo-ready project preselects its saved track and stage; changing its stage to Shipped preserves its two mentors and track.

The captures precede the create/edit tests, so the screenshots show the original seeded cohort. All created test records stayed in `/tmp/buildspace-merge.db`. Browser raw evidence and the temporary runner are under ignored `tmp/verify/buildspace/` in the implementation worktree. No live Discord bot, real member credentials, production database or external delivery was used.

API contracts and focused Go tests additionally cover membership/mentor validation, unsafe links, duplicate mentors, scoped authentication, identity spoofing, duplicate message retries, conflicting replays, wrong scopes, token revocation and Finance status ownership. See [the feature guide](../../buildspace.md) for the bot handoff and draft limits.

## Merge-preparation verification

Rebased onto the integrated onboarding, security, dashboard and calendar work. The seeded browser journeys use the security hardening’s explicit dev-login confirmation. Desktop/sidebar screenshot pixels now match the rail background at the bottom of the full page in both themes; the white strip is gone.

[Calendar options with the three optional Buildspace sources](calendar-options-1440-light.png): verified all three begin unchecked, can be enabled, and remain enabled after saving and reloading. The screenshot shows them selected before saving. API tests cover exclusive ranges, DST-stable date-only entries, omitted note/reason contents, weekly completion suppression and shipped-project exclusion. Weekly due dates use the documented draft Sunday convention.
