//go:build unix

package hostfiles

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestRealOwnershipAsRoot exercises SystemOwner: real lchown and stat, with the UIDs the
// containers run as. It needs root, so the ordinary CI job skips it; the Docker job in
// .github/workflows/security.yml runs it as root.
func TestRealOwnershipAsRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		if os.Getenv("HOSTFILES_REQUIRE_ROOT") != "" {
			t.Fatal("HOSTFILES_REQUIRE_ROOT is set but the test is not running as root")
		}
		t.Skip("needs root (real chown); run as root, as the security workflow's Docker job does")
	}
	dir := t.TempDir()
	o := SystemOwner()
	build := func() *Plan {
		pl := NewPlanner(dir, o)
		pl.Dir("secrets", 0o711, Root)
		pl.Dir("secrets/app", 0o700, appIDs)
		pl.Env(".env", "", []KV{{"APP_URL", "https://dash.club.example"}})
		pl.Secret(Secret{Path: "secrets/app/discord-client-secret", Value: []byte(sentinel), Owner: appIDs})
		pl.Secret(Secret{Path: "secrets/tunnel-token", Value: []byte(sentinel), Owner: IDs{65532, 65532}})
		p, err := pl.Plan()
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if err := build().Apply(); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]struct {
		mode os.FileMode
		ids  IDs
	}{
		"secrets":                           {0o711, Root},
		"secrets/app":                       {0o700, appIDs},
		"secrets/app/discord-client-secret": {0o400, appIDs},
		"secrets/tunnel-token":              {0o400, IDs{65532, 65532}},
		".env":                              {0o600, Root},
	} {
		info, err := os.Lstat(filepath.Join(dir, path))
		if err != nil {
			t.Fatal(err)
		}
		st := info.Sys().(*syscall.Stat_t)
		if info.Mode().Perm() != want.mode || int(st.Uid) != want.ids.UID || int(st.Gid) != want.ids.GID {
			t.Errorf("%s: mode %04o owner %d:%d, want %04o %+v", path, info.Mode().Perm(), st.Uid, st.Gid, want.mode, want.ids)
		}
	}
	before := snapshot(t, dir, o)
	time.Sleep(20 * time.Millisecond)
	p := build()
	if p.Changes() != 0 || p.Count(Conflict) != 0 {
		t.Fatalf("second run plans changes: %+v", p.Actions)
	}
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, dir, o)
	for path, b := range before {
		if after[path] != b {
			t.Errorf("%s changed on a no-op run", path)
		}
	}
}
