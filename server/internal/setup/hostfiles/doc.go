// Package hostfiles writes the deployment's host files for `dashboard setup`: the three
// env files (.env, dashboard.env, backup.env) and the secret files under secrets/, with
// the owners and modes docs/deploy/fresh-install.md and compose.cloudflare.yml require.
// It has no network code and no CLI.
//
// It never destroys what an admin already has (the adopt rule): an env key with another
// value and any existing secret file are reported and left alone. Work is planned first
// (Planner, Plan.WriteText) and written only by Plan.Apply, so a dry run is a plan that
// is printed and not applied. Every write is atomic (temporary file, fsync, rename).
//
// Secret values never appear in errors, logs or the plan: only paths, modes, owners and
// what happened (create, exists, conflict, missing).
package hostfiles
