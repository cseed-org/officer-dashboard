package driveexport

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// These structs are the export schema. Never marshal store rows: new private
// columns must not silently become part of a readable Drive document.
type eventRecord struct {
	Schema         int                      `json:"schema"`
	ExportedAt     int64                    `json:"exported_at"`
	ID             string                   `json:"id"`
	Name           string                   `json:"name"`
	Status         string                   `json:"status"`
	StartsAt       string                   `json:"starts_at,omitempty"`
	EndsAt         *string                  `json:"ends_at,omitempty"`
	TentativeFrom  *string                  `json:"tentative_from,omitempty"`
	TentativeTo    *string                  `json:"tentative_to,omitempty"`
	Location       *string                  `json:"location"`
	URL            *string                  `json:"url"`
	LumaURL        *string                  `json:"luma_url"`
	NotionURL      *string                  `json:"notion_url"`
	Description    *string                  `json:"description"`
	Capacity       *int64                   `json:"capacity"`
	DesignNeed     string                   `json:"design_need"`
	Host           hostRecord               `json:"host"`
	Guests         guestRecord              `json:"guests"`
	DesignRequests []designRecord           `json:"design_requests"`
	Expenses       map[string]expenseRecord `json:"expenses"`
}
type hostRecord struct {
	Name    string `json:"name"`
	Subteam string `json:"subteam"`
}
type guestRecord struct {
	Registered  int `json:"registered"`
	CheckedIn   int `json:"checked_in"`
	Reflections int `json:"reflections"`
}
type designRecord struct {
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Type    string  `json:"type"`
	Stage   string  `json:"stage"`
	DueDate *string `json:"due_date"`
	Folder  string  `json:"folder"`
}
type expenseRecord struct {
	Count      int   `json:"count"`
	TotalCents int64 `json:"total_cents"`
}
type trackRecord struct {
	Schema     int             `json:"schema"`
	ExportedAt int64           `json:"exported_at"`
	ID         string          `json:"id"`
	Key        string          `json:"key"`
	Name       string          `json:"name"`
	Hue        string          `json:"hue"`
	Programs   []programRecord `json:"programs"`
}
type programRecord struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Weekday     int               `json:"weekday"`
	StartTime   string            `json:"start_time"`
	DurationMin int               `json:"duration_min"`
	Location    string            `json:"location"`
	StartsOn    string            `json:"starts_on"`
	EndsOn      string            `json:"ends_on"`
	Description string            `json:"description"`
	Exceptions  []exceptionRecord `json:"exceptions"`
}
type exceptionRecord struct {
	OnDate      string  `json:"on_date"`
	Kind        string  `json:"kind"`
	NewStartsAt *string `json:"new_starts_at"`
	NewEndsAt   *string `json:"new_ends_at"`
	NewLocation *string `json:"new_location"`
}

func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func renderEvent(r eventRecord, name string) ([]byte, error) {
	if name == "event.json" {
		return json.MarshalIndent(r, "", "  ")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nID: %s\nStatus: %s\n", r.Name, r.ID, r.Status)
	if r.StartsAt != "" {
		fmt.Fprintf(&b, "Starts: %s\nEnds: %s\n", r.StartsAt, value(r.EndsAt))
	} else {
		fmt.Fprintf(&b, "Tentative window: %s to %s\n", value(r.TentativeFrom), value(r.TentativeTo))
	}
	fmt.Fprintf(&b, "Location: %s\nURL: %s\nLuma: %s\nNotion: %s\nHost: %s\nSubteam: %s\n", value(r.Location), value(r.URL), value(r.LumaURL), value(r.NotionURL), r.Host.Name, r.Host.Subteam)
	if r.Capacity != nil {
		fmt.Fprintf(&b, "Capacity: %d\n", *r.Capacity)
	}
	fmt.Fprintf(&b, "Design need: %s\n\n%s\n\nGuests: %d registered; %d checked-in; %d reflections.\n\n## Design requests\n", r.DesignNeed, value(r.Description), r.Guests.Registered, r.Guests.CheckedIn, r.Guests.Reflections)
	for _, d := range r.DesignRequests {
		fmt.Fprintf(&b, "- %s (%s): %s; %s; due %s; folder %s\n", d.Title, d.ID, d.Type, d.Stage, value(d.DueDate), d.Folder)
	}
	b.WriteString("\n## Expenses\n")
	statuses := make([]string, 0, len(r.Expenses))
	for k := range r.Expenses {
		statuses = append(statuses, k)
	}
	sort.Strings(statuses)
	for _, status := range statuses {
		e := r.Expenses[status]
		fmt.Fprintf(&b, "- %s: %d requests, %d cents\n", status, e.Count, e.TotalCents)
	}
	fmt.Fprintf(&b, "\nSchema: %d\nExported at (Unix milliseconds): %d\n", r.Schema, r.ExportedAt)
	return []byte(b.String()), nil
}
