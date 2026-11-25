//go:build release

package main

import (
	"errors"
	"io"
	"log/slog"
)

func seedCmd(_ []string, _ func(string) string, _ io.Writer, _ *slog.Logger) error {
	return errors.New("sample data is not included in release builds; use a local development build")
}
