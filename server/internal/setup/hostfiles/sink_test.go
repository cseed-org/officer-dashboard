package hostfiles

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSinkStoresKnownSecretsWhereTheContainersReadThem(t *testing.T) {
	dir := t.TempDir()
	o := newFakeOwner()
	sink := &HostSink{Root: dir, Owner: o}
	for name, want := range map[string]struct {
		path string
		ids  IDs
	}{
		"tunnel-token":                {"secrets/tunnel-token", IDs{65532, 65532}},
		"backup-access-client-id":     {"secrets/backup/access-client-id", appIDs},
		"backup-access-client-secret": {"secrets/backup/access-client-secret", appIDs},
		"discord-client-secret":       {"secrets/app/discord-client-secret", appIDs},
		// No host file reads the services token: it goes to a file only root can read,
		// for the admin to copy into the bots and agents that use it.
		"services-access-client-secret": {"secrets/admin/services-access-client-secret", Root},
	} {
		if err := sink.StoreSecret(name, sentinel+name); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		path := filepath.Join(dir, want.path)
		if got := read(t, path); string(got) != sentinel+name {
			t.Fatalf("%s: wrote %q", name, got)
		}
		if ids, _ := o.Owner(path); ids != want.ids {
			t.Fatalf("%s: owner %+v", name, ids)
		}
	}
	info, _ := os.Lstat(filepath.Join(dir, "secrets/admin"))
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("admin dir mode %04o", info.Mode().Perm())
	}
}

func TestSinkNeverOverwritesAndNeverLosesTheNewValue(t *testing.T) {
	dir := t.TempDir()
	sink := &HostSink{Root: dir, Owner: newFakeOwner()}
	if err := sink.StoreSecret("tunnel-token", "first"); err != nil {
		t.Fatal(err)
	}
	err := sink.StoreSecret("tunnel-token", sentinel)
	var aside *StoredAsideError
	if !errors.As(err, &aside) {
		t.Fatalf("want StoredAsideError, got %v", err)
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatal("error shows the value")
	}
	if got := read(t, filepath.Join(dir, "secrets/tunnel-token")); string(got) != "first" {
		t.Fatal("existing secret overwritten")
	}
	if got := read(t, aside.Path); string(got) != sentinel {
		t.Fatalf("value kept aside as %q", got)
	}
	if !strings.HasPrefix(aside.Path, filepath.Join(dir, "secrets", "admin")+string(filepath.Separator)) {
		t.Fatalf("kept aside at %s", aside.Path)
	}
	rot := &HostSink{Root: dir, Owner: newFakeOwner(), Rotate: true}
	if err := rot.StoreSecret("tunnel-token", "second"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, "secrets/tunnel-token")); string(got) != "second" {
		t.Fatal("rotate did not replace")
	}
}

func TestSinkRefusesBadNamesAndEmptyValues(t *testing.T) {
	sink := &HostSink{Root: t.TempDir(), Owner: newFakeOwner()}
	for _, name := range []string{"", "../escape", "a/b", "UPPER", ".hidden"} {
		if err := sink.StoreSecret(name, "v"); err == nil {
			t.Errorf("name %q accepted", name)
		}
	}
	if err := sink.StoreSecret("tunnel-token", ""); err == nil {
		t.Error("empty value accepted")
	}
}
