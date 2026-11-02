// Package notion will create a Notion page for each event. It is a stub for now: see
// docs/notion-integration.md for what the real client must do.
//
// TODO(notion): add an API client (integration token, events database id) behind NotionSync
// and select it in cmd/dashboard when NOTION_TOKEN is set.
package notion

import (
	"context"
	"strings"
)

// Event is what a Notion page draft is built from.
type Event struct {
	ID          string
	Name        string
	StartsAt    string
	EndsAt      *string
	Location    *string
	Description *string
	LumaURL     *string
}

// Page is the result of drafting a page.
type Page struct {
	URL string
	// Placeholder is true when no page exists yet (the stub).
	Placeholder bool
}

// NotionSync drafts and updates the Notion page that mirrors an event.
//
// TODO(notion): grow this with UpdateEventPage and ArchiveEventPage once the real client exists.
type NotionSync interface {
	DraftEventPage(ctx context.Context, e Event) (Page, error)
}

// PlaceholderPrefix starts every link the stub hands out. The UI never treats it as a real
// page: it shows the value in a disabled field next to "Coming soon".
const PlaceholderPrefix = "https://www.notion.so/placeholder/cseed-event-"

// Stub is the no-op NotionSync: it calls nothing and returns a stable placeholder link.
//
// TODO(notion): replace with the API client.
type Stub struct{}

// DraftEventPage returns the placeholder link for the event.
func (Stub) DraftEventPage(_ context.Context, e Event) (Page, error) {
	return Page{URL: PlaceholderPrefix + e.ID, Placeholder: true}, nil
}

// IsPlaceholder reports whether a stored link came from the stub.
func IsPlaceholder(url string) bool { return strings.HasPrefix(url, PlaceholderPrefix) }
