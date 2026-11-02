package store

import "io/fs"

// MigrationCount reports how many embedded migrations exist, so tests do not
// hard-code a number that changes whenever another branch adds a migration.
func MigrationCount() int {
	names, _ := fs.Glob(migrationFS, "migrations/*.sql")
	return len(names)
}
