package files

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// EventsFolder is the top-level folder holding one folder per event.
const EventsFolder = "Events"

// eventZone dates event folders in the club's default timezone.
var eventZone = func() *time.Location {
	if l, err := time.LoadLocation("America/Los_Angeles"); err == nil {
		return l
	}
	return time.UTC
}()

// maxEventName caps the event-name part of a folder name, in characters.
const maxEventName = 80

// SafeName makes s usable as a Drive folder or file name everywhere it may be synced:
// characters Windows and macOS refuse (/\:*?"<>|) and control characters become spaces,
// runs of space collapse, and the result is trimmed.
func SafeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) || r == utf8.RuneError {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// EventFolderName is "<YYYY-MM-DD> <name> [<id>]", dated in the club timezone, or
// "undated <name> [<id>]" for a tentative or undated event (startsAtMs nil). The name is
// sanitised (SafeName) and capped at 80 characters; the trailing [id] keeps the folder
// findable when the event is renamed or moved.
func EventFolderName(startsAtMs *int64, name, id string) string {
	date := "undated"
	if startsAtMs != nil {
		date = time.UnixMilli(*startsAtMs).In(eventZone).Format("2006-01-02")
	}
	n := SafeName(name)
	if r := []rune(n); len(r) > maxEventName {
		n = strings.TrimSpace(string(r[:maxEventName]))
	}
	if n == "" {
		n = "Event"
	}
	return fmt.Sprintf("%s %s [%s]", date, n, SafeName(id))
}

// EventFolder returns the id of the event's folder under Events/, creating it when missing.
// The folder is found by its trailing "[<id>]", so when the event's name or date changes
// the existing folder is renamed in place and keeps its contents.
func EventFolder(ctx context.Context, d *Drive, startsAtMs *int64, name, id string) (string, error) {
	want := EventFolderName(startsAtMs, name, id)
	events, err := d.EnsureFolder(ctx, []string{EventsFolder})
	if err != nil {
		return "", err
	}
	children, err := d.List(ctx, events)
	if err != nil {
		return "", err
	}
	suffix := " [" + SafeName(id) + "]"
	var match *Entry
	for i := range children {
		c := &children[i]
		if !c.IsFolder() || !strings.HasSuffix(c.Name, suffix) {
			continue
		}
		// List sorts by name; keep the oldest so concurrent creators converge.
		if match == nil || c.Created.Before(match.Created) || (c.Created.Equal(match.Created) && c.ID < match.ID) {
			match = c
		}
	}
	if match == nil {
		return d.folder(ctx, want, events)
	}
	if match.Name != want {
		if err := d.RenameFolder(ctx, match.ID, want); err != nil {
			return "", err
		}
	}
	return match.ID, nil
}
