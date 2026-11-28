package cfprovision

import (
	"context"
	"errors"
	"sync"
)

// SecretName names a secret Cloudflare hands out that setup must keep on the host.
type SecretName string

const (
	// SecretTunnelToken is the token cloudflared runs with. Cloudflare can return it again
	// (GET …/cfd_tunnel/{id}/token), so losing it is recoverable by running setup again.
	SecretTunnelToken SecretName = "tunnel-token"
	// SecretServicesToken is the "officer-dashboard services" service token's client ID
	// and client secret. Cloudflare shows the client secret only in the create response:
	// "This is the only time you can get the Client Secret."
	SecretServicesToken SecretName = "access-service-token/services"
	// SecretBackupsToken is the "dashboard-backups-host" service token the backup service
	// presents to the backup hostname's Access application; shown once, as above.
	SecretBackupsToken SecretName = "access-service-token/backups"
	// SecretBackupBearer is the backup Worker's bearer. `setup host` generates it into
	// secrets/backup/bearer; the apply engine only reads it (Load), to upload it with the
	// Worker it creates, and never stores, prints or regenerates it.
	SecretBackupBearer SecretName = "backup-bearer"
)

// ErrNotKept is Load's answer for a secret the host does not hold.
var ErrNotKept = errors.New("the host does not hold this secret")

// Secret is one secret to keep. ResourceID is the Cloudflare ID it belongs to (the tunnel
// or the service token); ClientID is set for a service token. Value is the secret itself.
// Formatting a Secret with %v, %+v, %s or %#v never prints Value.
type Secret struct {
	Name       SecretName
	ResourceID string
	ClientID   string
	Value      string
}

func (s Secret) String() string {
	return "Secret{" + string(s.Name) + " for " + s.ResourceID + ", value redacted}"
}

func (s Secret) GoString() string { return s.String() }

// SecretSink keeps secrets on the host. The apply engine calls Store immediately after
// the create response that carries a secret, before any other API call; if Store fails
// the run stops, because the secret then exists only in this process's memory.
//
// The real sink comes from the hostfiles package (atomic secret files with owner and
// mode); MemorySink is the test implementation.
type SecretSink interface {
	// Store keeps s durably, or returns an error. It must not include s.Value in the error.
	Store(ctx context.Context, s Secret) error
	// Has reports whether a secret is already kept, so a re-run after a crash can fetch a
	// tunnel token Cloudflare still has, and can say which secrets need rotating.
	Has(ctx context.Context, name SecretName) (bool, error)
	// Load returns a secret the host already holds (only SecretBackupBearer is ever
	// loaded), or ErrNotKept. Its error never includes the value.
	Load(ctx context.Context, name SecretName) (string, error)
}

// MemorySink is a SecretSink in memory, for tests. FailStore makes Store fail for the
// named secret (after the secret was handed to it, as a full disk would).
type MemorySink struct {
	mu        sync.Mutex
	secrets   map[SecretName]Secret
	FailStore map[SecretName]error
}

// ErrSinkFull is a ready-made failure for FailStore.
var ErrSinkFull = errors.New("no space left on device")

func (m *MemorySink) Store(_ context.Context, s Secret) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.FailStore[s.Name]; err != nil {
		return err
	}
	if m.secrets == nil {
		m.secrets = map[SecretName]Secret{}
	}
	m.secrets[s.Name] = s
	return nil
}

func (m *MemorySink) Has(_ context.Context, name SecretName) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.secrets[name]
	return ok, nil
}

func (m *MemorySink) Load(_ context.Context, name SecretName) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.secrets[name]
	if !ok {
		return "", ErrNotKept
	}
	return s.Value, nil
}

// Get returns a stored secret (tests only).
func (m *MemorySink) Get(name SecretName) (Secret, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.secrets[name]
	return s, ok
}
