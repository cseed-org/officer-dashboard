//go:build embedsink

package sinkbundle

import "testing"

const embedded = true

// The staleness check, image side: the image build runs this before compiling the
// dashboard, so an image never carries a bundle built from other sources than the binary
// pins.
func TestEmbeddedBundleIsFresh(t *testing.T) {
	b, ok := Embedded()
	if !ok {
		sub, err := Load(distFS())
		if err == nil {
			err = sub.Check()
		}
		t.Fatalf("built with -tags embedsink, but the bundle is missing or stale: %v", err)
	}
	if len(b.Migrations) == 0 || len(b.Script) == 0 {
		t.Fatal("empty bundle")
	}
}
