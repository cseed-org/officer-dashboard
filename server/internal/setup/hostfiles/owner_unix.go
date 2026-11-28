//go:build unix

package hostfiles

import (
	"fmt"
	"os"
	"syscall"
)

type systemOwner struct{}

// SystemOwner changes ownership with lchown(2) and reads it with lstat(2). Changing an
// owner needs root (CAP_CHOWN); the error says which path failed, never its contents.
func SystemOwner() Owner { return systemOwner{} }

func (systemOwner) Chown(path string, ids IDs) error {
	// Lchown: a symlink planted in place of a file must not move ownership elsewhere.
	return os.Lchown(path, ids.UID, ids.GID)
}

func (systemOwner) Owner(path string) (IDs, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return IDs{}, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return IDs{}, fmt.Errorf("%s: no owner information on this system", path)
	}
	return IDs{int(st.Uid), int(st.Gid)}, nil
}

// syncDir makes a rename or link in dir durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
