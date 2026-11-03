Design requests QA

The before images were supplied by the orchestrator from the deployed V0.4 test app. The after images are from this branch's isolated, seeded dev server on port 18094.

Coverage at 1440×900 and 390×844, in light and dark:

| Viewer | Create, edit, withdraw, reference upload, comment | Event prefill, Request another, Mark not needed | Filters, Board/List/My work | Assign, review, request changes, approve, final upload, reopen |
| --- | --- | --- | --- | --- |
| Leadership | Covered | Covered | Covered | Covered |
| Director | Covered | Covered | Covered | Covered |
| Officer (Ava) | Covered | Covered | Covered | Permission messaging; no worker controls |
| Designer (Riley) | Covered | Covered | Covered | Covered |
| Requester (Priya) | Covered | Covered | Covered | Cross-user review lifecycle in design.spec.ts |

Additional regression checks cover leadership/director moves of unassigned requests through the drawer and menu, pointer and keyboard moves, native desktop card dragging, restricted drop targets, confirmed/cancelled leadership delivery overrides, and a failed optimistic move rolling back with a visible error. API tests assert permissions, override confirmation, audit entries, and assignment after delivery. The migration test upgrades existing requests with files/comments and verifies both preservation and deletion cascades.

Validation commands:

```sh
cd server && go vet ./... && go test ./... && go test -tags dev ./internal/api
# from repo root
npm run contract-check
cd web && npm run build && npx astro check
WEB_ORIGIN=http://127.0.0.1:18094 npx playwright test tests/design-actions.spec.ts tests/design.spec.ts tests/events.spec.ts --project=phone
WEB_ORIGIN=http://127.0.0.1:18094 npx playwright test tests/design-actions.spec.ts tests/design.spec.ts tests/events.spec.ts --project=desktop
```

The optional Events review screenshot tests are disabled by default in the existing Events spec. Design screenshots are generated for every role, theme and viewport by design-actions.spec.ts; selected evidence is kept here.

Results: Go vet/tests (including dev-auth build), API contract check, production web build and Astro check passed. Each viewport batch passed 20 browser tests; each skipped the two existing optional Events screenshot cases. The cross-user requester/designer lifecycle was additionally checked in both themes at both widths. Astro reported no errors or warnings (two pre-existing hints).

After merging main's developer-mode login changes (#40), the Design/Events suites were rerun against a fresh seeded server with DEV_AUTH_KEY required and matching E2E_DEV_AUTH_KEY: 42 passed, with the four existing optional Events screenshot cases skipped. The new design-actions login helper uses the shared devKeyParam. Go vet/tests, dev-build API tests, contract validation (199 responses across 97 of 104 endpoints), production web build and Astro check also passed on the merged branch.
