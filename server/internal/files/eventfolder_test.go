package files

import (
	"context"
	"strings"
	"testing"
	"time"

	"officerdashboard/internal/files/drivefake"
)

func ms(t time.Time) *int64 { v := t.UnixMilli(); return &v }

func TestEventFolderNameIsDatedSanitisedAndCapped(t *testing.T) {
	// 2026-11-21 03:00 UTC is still the 20th in Los Angeles.
	evening := ms(time.Date(2026, 11, 21, 3, 0, 0, 0, time.UTC))
	cases := []struct {
		at         *int64
		name, want string
	}{
		{evening, "Demo Day", "2026-11-20 Demo Day [ev1]"},
		{nil, "Tentative mixer", "undated Tentative mixer [ev1]"},
		{evening, `a/b\c:d*e?f"g<h>i|j`, "2026-11-20 a b c d e f g h i j [ev1]"},
		{evening, "tab\there\nnew   line\x00", "2026-11-20 tab here new line [ev1]"},
		{evening, "  ", "2026-11-20 Event [ev1]"},
	}
	for _, c := range cases {
		if got := EventFolderName(c.at, c.name, "ev1"); got != c.want {
			t.Errorf("EventFolderName(%q) = %q; want %q", c.name, got, c.want)
		}
	}
	long := EventFolderName(nil, strings.Repeat("é", 200), "ev1")
	if got := []rune(strings.TrimSuffix(strings.TrimPrefix(long, "undated "), " [ev1]")); len(got) != 80 {
		t.Errorf("long name kept %d characters; want 80", len(got))
	}
}

func TestEventFolderIsCreatedOnceAndRenamedInPlace(t *testing.T) {
	d, fake, _ := onDrive(t)
	ctx := context.Background()
	first, err := EventFolder(ctx, d, nil, "Mixer", "ev1")
	if err != nil {
		t.Fatal(err)
	}
	fake.Add(drivefake.File{Name: "event.json", Parent: first, Body: []byte("{}")})
	again, err := EventFolder(ctx, d, nil, "Mixer", "ev1")
	if err != nil || again != first {
		t.Fatalf("second lookup %q, %v; want %q", again, err, first)
	}
	// The event gets a date and a new name: same folder, renamed, contents kept.
	moved, err := EventFolder(ctx, d, ms(time.Date(2026, 12, 1, 20, 0, 0, 0, time.UTC)), "Winter Mixer", "ev1")
	if err != nil || moved != first {
		t.Fatalf("after rename %q, %v; want %q", moved, err, first)
	}
	f := fake.Path("Events", "2026-12-01 Winter Mixer [ev1]")
	if f == nil || f.ID != first || len(fake.Children(first)) != 1 {
		t.Errorf("renamed folder %+v", f)
	}
	other, _ := EventFolder(ctx, d, nil, "Mixer", "ev10")
	if other == first {
		t.Error("ev10 reused ev1's folder")
	}
	if n := len(fake.Children(fake.Path("Events").ID)); n != 2 {
		t.Errorf("%d event folders; want 2", n)
	}
}
