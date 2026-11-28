package store

import (
	"errors"

	"github.com/mattn/go-sqlite3"
)

// IsCheckViolation reports a CHECK constraint failure, by the driver's extended error code
// rather than its text. IsConflict deliberately treats these as server faults; the API uses
// this to answer a refused access email with its own message instead.
func IsCheckViolation(err error) bool {
	var se sqlite3.Error
	return errors.As(err, &se) && se.Code == sqlite3.ErrConstraint && se.ExtendedCode == sqlite3.ErrConstraintCheck
}
