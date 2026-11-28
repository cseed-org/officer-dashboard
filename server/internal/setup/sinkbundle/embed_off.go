//go:build !embedsink

package sinkbundle

// Embedded returns the backup Worker bundle compiled into the binary. Only the release
// image's build (`-tags embedsink`, after the Dockerfile's sink stage) has one.
func Embedded() (Bundle, bool) { return Bundle{}, false }
