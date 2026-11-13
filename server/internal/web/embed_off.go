//go:build !embedweb

package web

import "io/fs"

// Embedded returns the front end compiled into the binary; release builds
// (`go build -tags embedweb`) embed server/internal/web/dist, which the Dockerfile fills
// from web/dist. A development build has none and serves from a directory instead.
func Embedded() (fs.FS, bool) { return nil, false }
