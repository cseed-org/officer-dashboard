package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// EventFilter keeps metrics enrichment proportional to the selected page. Time bounds are
// instants, resolved in the viewer's zone by the API, including DST transitions.
type EventFilter struct {
	When, Search string
	Now          time.Time
	From, To     *time.Time
	HasCursor    bool
	CursorMs     int64
	CursorID     string
	CursorRaw    string
	Limit        int
}

func (q *Queries) EventPage(ctx context.Context, f EventFilter) ([]Event, error) {
	conds := []string{"e.starts_at_ms IS NOT NULL"}
	var args []any
	add := func(cond string, arg any) { conds = append(conds, cond); args = append(args, arg) }
	addTime := func(op string, t time.Time) {
		// The ordinary-column index handles millisecond-aligned bounds by itself.
		if t.Nanosecond()%1_000_000 == 0 {
			add("e.starts_at_ms "+op+" ?", t.UnixMilli())
			return
		}
		// Only rows in the boundary millisecond need the exact tuple comparison,
		// without UnixNano's 1678–2262 range limit.
		coarse := op
		if op == "<" {
			coarse = "<="
		}
		add("e.starts_at_ms "+coarse+" ?", t.UnixMilli())
		conds = append(conds, "(e.starts_at_ms != ? OR (instant_seconds(e.starts_at),instant_fraction(e.starts_at)) "+op+" (?,?))")
		args = append(args, t.UnixMilli(), t.Unix(), t.Nanosecond())
	}
	switch f.When {
	case "past":
		addTime("<", f.Now)
	case "upcoming":
		addTime(">=", f.Now)
	}
	if f.From != nil {
		addTime(">=", *f.From)
	}
	if f.To != nil {
		addTime("<", *f.To)
	}
	if f.Search != "" {
		conds = append(conds, "(instr(fold(e.name),?)>0 OR instr(fold(coalesce(e.location,'')),?)>0)")
		args = append(args, Fold(f.Search), Fold(f.Search))
	}
	if f.HasCursor {
		conds = append(conds, "(e.starts_at_ms,e.id) < (?,?)")
		args = append(args, f.CursorMs, f.CursorID)
	}
	if f.CursorRaw != "" {
		add("(strftime('%Y-%m-%dT%H:%M:%fZ',e.starts_at_ms/1000.0,'unixepoch') || '|' || e.id) < ?", f.CursorRaw)
	}
	query := eventSelect + " WHERE " + strings.Join(conds, " AND ") + " ORDER BY e.starts_at_ms DESC,e.id DESC"
	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := q.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// EventTotals is the four UI-wide counters, independent of list pagination.
type EventTotals struct {
	Upcoming, ThisMonth, Attendees, Drafts int
}

// EventSummary supplies the SQL aggregate for the Events summary. The UI's
// pagination/counter change (M-014) can consume this separately from EventPage.
// Month bounds are exclusive at the end and resolved in the viewer's zone by
// the caller. Millisecond instants match JavaScript Date comparisons.
func (q *Queries) EventSummary(ctx context.Context, now, monthStart, monthEnd Ms) (EventTotals, error) {
	var out EventTotals
	err := q.q.QueryRowContext(ctx, `SELECT
		coalesce(sum(status NOT IN ('draft','cancelled') AND starts_at IS NOT NULL AND starts_at_ms >= ?),0),
		coalesce(sum(status NOT IN ('draft','cancelled') AND starts_at IS NOT NULL AND starts_at_ms >= ? AND starts_at_ms < ?),0),
		(SELECT count(*) FROM event_guests WHERE checked_in_at IS NOT NULL),
		coalesce(sum(status = 'draft'),0)
		FROM events`, now, monthStart, monthEnd).Scan(&out.Upcoming, &out.ThisMonth, &out.Attendees, &out.Drafts)
	return out, err
}
