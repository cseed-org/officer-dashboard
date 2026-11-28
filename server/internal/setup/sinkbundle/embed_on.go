//go:build embedsink

package sinkbundle

import (
	"embed"
	"io/fs"
)

// dist is filled by the Dockerfile's sink stage: worker.js (the esbuild bundle), the
// Worker's sources and migrations/*.sql.
//
//go:embed all:dist
var dist embed.FS

func distFS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return dist
	}
	return sub
}

// Embedded returns the compiled-in bundle, or false when it is missing or does not match
// the pinned source (the image build's test refuses such a build first).
func Embedded() (Bundle, bool) {
	b, err := Load(distFS())
	if err != nil || b.Check() != nil {
		return Bundle{}, false
	}
	return b, true
}
