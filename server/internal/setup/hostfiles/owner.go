package hostfiles

// IDs is a numeric owner: the containers run as fixed UIDs, so names would be wrong on
// the host.
type IDs struct{ UID, GID int }

// Root is the host's root user, which owns the env files and the secrets/ parent.
var Root = IDs{0, 0}

// Owner changes and reads file ownership. It is an interface so the package's tests run
// without root and on any OS; SystemOwner is the real one.
type Owner interface {
	Chown(path string, ids IDs) error
	Owner(path string) (IDs, error)
}
