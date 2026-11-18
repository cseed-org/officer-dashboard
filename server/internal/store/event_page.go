package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// EventFilter keeps list work proportional to the selected page. Time bounds are
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
	conds := []string{"instant_ms(e.starts_at) IS NOT NULL"}
	var args []any
	add := func(cond string, arg any) { conds = append(conds, cond); args = append(args, arg) }
	addTime := func(op string, t time.Time) {
		// The millisecond index narrows candidates; the tuple keeps sub-millisecond
		// comparisons exact, without UnixNano's 1678–2262 range limit.
		coarse := op
		if op == "<" {
			coarse = "<="
		}
		add("instant_ms(e.starts_at) "+coarse+" ?", t.UnixMilli())
		conds = append(conds, "(instant_seconds(e.starts_at),instant_fraction(e.starts_at)) "+op+" (?,?)")
		args = append(args, t.Unix(), t.Nanosecond())
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
		conds = append(conds, "(instant_ms(e.starts_at),e.id) < (?,?)")
		args = append(args, f.CursorMs, f.CursorID)
	}
	if f.CursorRaw != "" {
		add("(strftime('%Y-%m-%dT%H:%M:%fZ',instant_ms(e.starts_at)/1000.0,'unixepoch') || '|' || e.id) < ?", f.CursorRaw)
	}
	query := eventSelect + " WHERE " + strings.Join(conds, " AND ") + " ORDER BY instant_ms(e.starts_at) DESC,e.id DESC"
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
