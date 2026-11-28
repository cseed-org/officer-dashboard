//go:build unix

package setup

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"officerdashboard/internal/setup/cffake"
	"officerdashboard/internal/setup/hostfiles"
)

// TestApplyRealOwnershipAsRoot runs the host phase and then the Cloudflare apply against
// the fake with the real lchown, and checks the secrets Cloudflare handed out landed in
// their files with the owners and modes the containers need. It needs root, so the
// ordinary CI job skips it; the Docker job in .github/workflows/security.yml runs it.
func TestApplyRealOwnershipAsRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		if os.Getenv("HOSTFILES_REQUIRE_ROOT") != "" {
			t.Fatal("HOSTFILES_REQUIRE_ROOT is set but the test is not running as root")
		}
		t.Skip("needs root (real chown); run as root, as the security workflow's Docker job does")
	}
	r := newApplyRigOwned(t, cffake.Fresh(), hostfiles.SystemOwner())
	if res := r.run(append([]string{"--yes"}, groupArgs...)...); res.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	for rel, want := range map[string]struct {
		mode     os.FileMode
		uid, gid uint32
	}{
		"secrets":              {0o711, 0, 0},
		"secrets/tunnel-token": {0o400, 65532, 65532},
		"secrets/admin":        {0o700, 0, 0},
		"secrets/admin/services-access-client-id":     {0o400, 0, 0},
		"secrets/admin/services-access-client-secret": {0o400, 0, 0},
	} {
		info, err := os.Lstat(filepath.Join(r.dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		st := info.Sys().(*syscall.Stat_t)
		if info.Mode().Perm() != want.mode || st.Uid != want.uid || st.Gid != want.gid {
			t.Errorf("%s is %04o %d:%d, want %04o %d:%d", rel, info.Mode().Perm(), st.Uid, st.Gid, want.mode, want.uid, want.gid)
		}
	}
	if !r.isSecretFromCloudflare(r.file("secrets/tunnel-token")) {
		t.Error("secrets/tunnel-token does not hold the token Cloudflare returned")
	}
}
