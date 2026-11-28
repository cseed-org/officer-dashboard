package cfprovision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// No golden file holds a secret: the fake's secrets and the test bearer are built at run
// time, and the Worker upload's golden replaces the bearer by where it came from. This
// checks the shapes they are built from, in every golden of both packages.
func TestGoldensHoldNoSecrets(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "*"))
	more, _ := filepath.Glob(filepath.Join("..", "testdata", "*"))
	files = append(files, more...)
	if len(files) < 6 {
		t.Fatalf("only %d goldens found", len(files))
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"FAKE-CLIENT-SECRET", "FAKE-TUNNEL-SECRET", testBearer[:6], "client_secret\": \"", "\"type\": \"secret_text\",\n          \"text\": \"" + testBearer[:3]} {
			if strings.Contains(string(raw), bad) {
				t.Errorf("%s holds %q", f, bad)
			}
		}
	}
}
