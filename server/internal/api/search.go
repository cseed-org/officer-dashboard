package api

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/fiscal"
	"officerdashboard/internal/luma"
	"officerdashboard/internal/money"
	"officerdashboard/internal/validate"
)

type searchResult struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Href     string `json:"href"`
}

var kindOrder = map[string]int{"event": 0, "request": 1, "user": 2, "account": 3}

// search is the sidebar search: GET /api/search?q=&limit=. Everyone signed in finds events
// and purchase requests; users and chart-of-accounts rows appear only for directors.
func (c *Ctx) search() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	var text string
	var limit int64
	if err := c.query(func(r *validate.Reader) {
		text = r.Str("q", validate.S{Req: true, Trim: true, Min: 1, Max: 100}).V
		limit = r.Int("limit", validate.I{Min: validate.Lo(1), Max: validate.Lo(50), HasDef: true, Def: 20}).V
	}); err != nil {
		return err
	}
	hits, err := c.S.db.Read().Search(c.Context(), text, int(limit), u.Role == auth.RoleDirector)
	if err != nil {
		return err
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.Score != b.Score {
			return a.Score < b.Score
		}
		if kindOrder[a.Kind] != kindOrder[b.Kind] {
			return kindOrder[a.Kind] < kindOrder[b.Kind]
		}
		return a.Sort > b.Sort
	})
	if len(hits) > int(limit) {
		hits = hits[:limit]
	}
	out := make([]searchResult, 0, len(hits))
	for _, h := range hits {
		r := searchResult{Kind: h.Kind, ID: h.ID}
		switch h.Kind {
		case "event":
			r.Title, r.Href = h.Primary, "/events?id="+h.ID
			r.Subtitle = joinNonEmpty(" · ", eventDay(h.A), h.B)
		case "request":
			r.Title, r.Href = h.Primary, "/finance?id="+h.ID
			r.Subtitle = joinNonEmpty(" · ", h.A, h.B, h.C)
		case "user":
			r.Title, r.Href = h.Primary, "/admin"
			r.Subtitle = joinNonEmpty(" · ", h.A, h.B, h.C)
		case "account":
			r.Title, r.Href = fmt.Sprintf("%s %s", h.A, h.Primary), "/finance?tab=reports&report=trial-balance"
			r.Subtitle = strings.ReplaceAll(h.B, "_", " ")
		}
		out = append(out, r)
	}
	writeJSON(c.W, 200, map[string]any{"results": out})
	return nil
}

func eventDay(startsAt string) string {
	t, err := luma.ParseInstant(startsAt)
	if err != nil {
		return ""
	}
	return fiscal.PacificDate(t)
}

func joinNonEmpty(sep string, parts ...string) string {
	var keep []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, sep)
}

var _ = money.Format
var _ = time.Now
