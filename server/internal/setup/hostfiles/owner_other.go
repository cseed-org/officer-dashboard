//go:build !unix

package hostfiles

import (
	"errors"
	"runtime"
)

type systemOwner struct{}

// SystemOwner on this OS refuses every call: the deployment files' owners are Linux
// UIDs, and `dashboard setup host` runs on the Linux host (or in the image) only.
func SystemOwner() Owner { return systemOwner{} }

var errNoOwners = errors.New("file ownership is not supported on " + runtime.GOOS + "; run setup on the Linux host")

func (systemOwner) Chown(string, IDs) error   { return errNoOwners }
func (systemOwner) Owner(string) (IDs, error) { return IDs{}, errNoOwners }

// syncDir is a no-op: Windows cannot open a directory to flush it, and renames there
// are already durable once the file itself is flushed.
func syncDir(string) error { return nil }
