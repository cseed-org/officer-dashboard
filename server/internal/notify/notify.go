// Package notify tells directors and requesters what happened to a purchase request.
// Delivery is best effort: failures are logged and never fail the API call.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"officerdashboard/internal/money"
)

// Event is one notification. Zero values mean "absent".
type Event struct {
	Type      string // request.submitted | request.approved | request.denied | request.status | request.held
	RequestID string
	Number    string
	Title     string

	TotalCents     *int64
	AttemptedCents *int64
	ApprovedCents  *int64
	Requester      string
	Director       string
	Status         string
	Comment        string
	Reason         string
}

// Notifier delivers events.
type Notifier interface {
	Notify(ctx context.Context, e Event, link string)
}

// Cents is a helper for the optional money fields.
func Cents(n int64) *int64 { return &n }

// Console logs instead of posting (no webhook configured).
type Console struct{ Log *slog.Logger }

func (c Console) Notify(_ context.Context, e Event, link string) {
	l := c.Log
	if l == nil {
		l = slog.Default()
	}
	raw, _ := json.Marshal(e)
	l.Info("notify", "type", e.Type, "event", string(raw), "link", link)
}

// Discord posts an embed to a webhook.
type Discord struct {
	URL  string
	HTTP *http.Client
	Log  *slog.Logger
}

// NewDiscord builds a webhook notifier with a 5-second delivery timeout.
func NewDiscord(url string) *Discord {
	return &Discord{URL: url, HTTP: &http.Client{Timeout: 5 * time.Second}}
}

type embedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

func trunc(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// Fields are the embed fields for an event, in a fixed order, skipping blanks.
func Fields(e Event) []embedField {
	var out []embedField
	add := func(name, value string) {
		if strings.TrimSpace(value) != "" {
			out = append(out, embedField{Name: name, Value: trunc(value, 1024), Inline: true})
		}
	}
	cents := func(n *int64) string {
		if n == nil {
			return ""
		}
		return money.Format(*n)
	}
	add("Amount", cents(e.TotalCents))
	add("Agent reported", cents(e.AttemptedCents))
	add("Approved", cents(e.ApprovedCents))
	add("requester", e.Requester)
	add("director", e.Director)
	add("status", e.Status)
	add("comment", e.Comment)
	add("reason", e.Reason)
	return out
}

func (d *Discord) Notify(ctx context.Context, e Event, link string) {
	log := d.Log
	if log == nil {
		log = slog.Default()
	}
	fields := Fields(e)
	if fields == nil {
		fields = []embedField{}
	}
	payload, _ := json.Marshal(map[string]any{
		"allowed_mentions": map[string]any{"parse": []string{}},
		"embeds": []map[string]any{{
			"title":       trunc(fmt.Sprintf("%s: %s", e.Number, e.Title), 256),
			"url":         link,
			"description": e.Type,
			"fields":      fields,
		}},
	})
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(payload))
	if err != nil {
		log.Error("Discord notification delivery failed")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := d.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		log.Error("Discord notification delivery failed")
		return
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		log.Error("Discord notification failed", "status", res.StatusCode)
	}
}
