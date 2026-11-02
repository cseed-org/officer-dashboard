## Summary

Describe the problem and resulting behavior.

## Linked issue

Closes #<!-- issue number, or explain why no issue applies -->

## Type

- [ ] Bug fix
- [ ] Feature
- [ ] Documentation
- [ ] Maintenance

## Validation

Run the commands in [CONTRIBUTING.md](../CONTRIBUTING.md). Mark items that apply and explain skipped checks.

- [ ] Go formatting (`gofmt`) is clean.
- [ ] `go vet ./...` and `go vet -tags dev ./...` pass.
- [ ] `go test ./...` and `go test -tags dev ./internal/api` pass.
- [ ] Go release build with cgo and CI flags passes.
- [ ] Root contract check passes (`npm run contract-check`; CI uses `node scripts/contract-check.mjs`).
- [ ] Web `npm ci`, `npm run build` and `npx astro check` pass.
- [ ] Shared zod contracts and domain types are synchronized where changed.
- [ ] New migrations use the next numbered SQL filename; tests use `store.MigrationCount()`.
- [ ] Relevant local Playwright tests pass for UI changes.
- [ ] UI screenshots attached: 1440px and 390px, each in light and dark themes.
- [ ] Brand changes follow [TRADEMARKS.md](../TRADEMARKS.md).

Checks skipped / additional evidence:

## Screenshots

<!-- Attach the four UI screenshots when applicable. -->
