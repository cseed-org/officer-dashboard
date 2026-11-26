package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"officerdashboard/internal/api"
	"officerdashboard/internal/driveexport"
	"officerdashboard/internal/files"
	"officerdashboard/internal/notify"
)

func TestExportWithoutRootIsAQuietDisconnectedSkip(t *testing.T) {
	resolve := exportDriveResolver(func(context.Context) (*files.Drive, error) {
		return nil, &api.APIError{Status: 503, Code: "not_configured", Message: "Choose the Google Drive folder for dashboard files in Settings."}
	})
	var logs bytes.Buffer
	s := &driveexport.Service{Drive: resolve, Alerts: notify.NewAlerter("", slog.New(slog.NewTextHandler(&logs, nil)))}
	for range 2 {
		err := s.Run(context.Background())
		if !errors.Is(err, files.ErrNotConnected) {
			t.Fatalf("got %v; want disconnected skip", err)
		}
	}
	if logs.Len() != 0 {
		t.Fatal("unconfigured root sent an alert")
	}
}
