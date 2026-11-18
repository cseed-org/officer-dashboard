package cfsync

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// M-111: a failed run (including the startup catch-up) retries with backoff, not after 24 h;
// a success returns to the daily cadence and resets the backoff; ErrBusy stays at 1 minute.
func TestDailyRetriesFailuresWithBackoff(t *testing.T) {
	down := errors.New("dial tcp: lookup api.cloudflare.com: no such host")
	results := []error{down, down, ErrBusy, down, down, down, nil, down}
	var waits []time.Duration
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runs := 0
	s := &Service{}
	s.daily(ctx, func(context.Context) error {
		err := results[runs]
		runs++
		return err
	}, func(_ context.Context, d time.Duration) bool {
		waits = append(waits, d)
		return len(waits) < len(results)
	})
	want := []time.Duration{5 * time.Minute, 15 * time.Minute, time.Minute, time.Hour, 6 * time.Hour, 6 * time.Hour, 24 * time.Hour, 5 * time.Minute}
	if !reflect.DeepEqual(waits, want) {
		t.Fatalf("waits = %v, want %v", waits, want)
	}
	if waits[0] >= 24*time.Hour {
		t.Fatal("failed startup catch-up waits a full day")
	}
}
