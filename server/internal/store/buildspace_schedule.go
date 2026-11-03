package store

import (
	"context"
	"errors"
	"time"
)

// Weekly check-in deadlines are versioned rules. A rule governs every date from its
// EffectiveFrom until the next rule's EffectiveFrom. The first (seed) rule has plain
// weekday-aligned weeks: each week ends on the due weekday and starts six days earlier.
// A later rule starts the day after the previous rule's last deadline; its first week
// runs from EffectiveFrom to the first due weekday at least six days later (7–13 days),
// so changing the due day never shortens a week. Weeks are calendar dates in the club
// time zone; DueTime only marks the deadline within the due day.
type BuildspaceWeekRule struct {
	EffectiveFrom string  `json:"effectiveFrom"`
	DueWeekday    int     `json:"dueWeekday"`
	DueTime       *string `json:"dueTime"`
	UpdatedAt     int64   `json:"updatedAt"`
	UpdatedBy     *string `json:"updatedBy"`
}

// BuildspaceWeek is one check-in week: inclusive Start through the Due date.
type BuildspaceWeek struct {
	Start string `json:"start"`
	Due   string `json:"due"`
}

const isoDate = "2006-01-02"

// DefaultBuildspaceRule is the original convention: Monday–Sunday, due end of Sunday.
var DefaultBuildspaceRule = BuildspaceWeekRule{EffectiveFrom: "0001-01-01", DueWeekday: 0}

func parseDate(s string) time.Time {
	t, err := time.Parse(isoDate, s)
	if err != nil {
		panic("buildspace week: invalid date " + s)
	}
	return t
}

// nextWeekday returns the first date on or after d that falls on weekday wd.
func nextWeekday(d time.Time, wd int) time.Time {
	return d.AddDate(0, 0, (wd-int(d.Weekday())+7)%7)
}

// BuildspaceWeekOf returns the check-in week containing date (YYYY-MM-DD). rules must
// be sorted by EffectiveFrom; an empty list means the default rule.
func BuildspaceWeekOf(rules []BuildspaceWeekRule, date string) BuildspaceWeek {
	if len(rules) == 0 {
		rules = []BuildspaceWeekRule{DefaultBuildspaceRule}
	}
	i := 0
	for j, r := range rules {
		if r.EffectiveFrom <= date {
			i = j
		}
	}
	r, d := rules[i], parseDate(date)
	if i > 0 {
		from := parseDate(r.EffectiveFrom)
		first := nextWeekday(from.AddDate(0, 0, 6), r.DueWeekday)
		if !d.After(first) {
			return BuildspaceWeek{Start: r.EffectiveFrom, Due: first.Format(isoDate)}
		}
	}
	due := nextWeekday(d, r.DueWeekday)
	return BuildspaceWeek{Start: due.AddDate(0, 0, -6).Format(isoDate), Due: due.Format(isoDate)}
}

// BuildspaceWeeksDue lists the weeks whose due date falls in [first, end).
func BuildspaceWeeksDue(rules []BuildspaceWeekRule, first, end string) []BuildspaceWeek {
	out := []BuildspaceWeek{}
	if first >= end {
		return out
	}
	// The week containing `first` may end before `first` only if first is past its due;
	// it never does, since weeks contain their due date. Walk week by week.
	w := BuildspaceWeekOf(rules, first)
	for w.Due < end {
		out = append(out, w)
		w = BuildspaceWeekOf(rules, parseDate(w.Due).AddDate(0, 0, 1).Format(isoDate))
	}
	return out
}

// BuildspaceRuleFor returns the rule governing date.
func BuildspaceRuleFor(rules []BuildspaceWeekRule, date string) BuildspaceWeekRule {
	if len(rules) == 0 {
		return DefaultBuildspaceRule
	}
	r := rules[0]
	for _, v := range rules {
		if v.EffectiveFrom <= date {
			r = v
		}
	}
	return r
}

// BuildspaceDueMoment is the deadline instant of a due date in loc. With no due time the
// deadline is the end of the day (the next midnight). Across DST, a repeated wall time
// resolves to its first occurrence and a skipped one moves forward past the gap.
func BuildspaceDueMoment(due string, dueTime *string, loc *time.Location) time.Time {
	d := parseDate(due)
	if dueTime == nil {
		return time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, loc)
	}
	t, _ := time.Parse("15:04", *dueTime)
	at := time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), 0, 0, loc)
	// A wall time skipped by a DST jump resolves to the moment after the gap (02:30 on a
	// spring-forward night becomes 03:30), never earlier than the chosen time.
	if want, got := t.Hour()*60+t.Minute(), at.Hour()*60+at.Minute(); at.Day() == d.Day() && got < want {
		at = at.Add(time.Duration(want-got) * time.Minute)
	}
	return at
}

func (q *Queries) BuildspaceWeekRules(ctx context.Context) ([]BuildspaceWeekRule, error) {
	rows, err := q.q.QueryContext(ctx, `SELECT effective_from,due_weekday,due_time,updated_at,updated_by FROM buildspace_week_rules ORDER BY effective_from`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BuildspaceWeekRule{}
	for rows.Next() {
		var r BuildspaceWeekRule
		if err := rows.Scan(&r.EffectiveFrom, &r.DueWeekday, &r.DueTime, &r.UpdatedAt, &r.UpdatedBy); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("buildspace_week_rules has no seed rule")
	}
	return out, nil
}

// BuildspaceClock is the club-time-zone view of now used by every week calculation.
type BuildspaceClock struct {
	Loc   *time.Location
	Now   time.Time
	Today string
}

func (q *Queries) BuildspaceClock(ctx context.Context, now time.Time) (BuildspaceClock, error) {
	s, err := q.ClubSettings(ctx)
	if err != nil {
		return BuildspaceClock{}, err
	}
	loc, err := time.LoadLocation(s.ClubTimezone)
	if err != nil {
		return BuildspaceClock{}, err
	}
	local := now.In(loc)
	return BuildspaceClock{Loc: loc, Now: now, Today: local.Format(isoDate)}, nil
}

// BuildspaceSchedule is the setting as leadership sees it: the rule for the current
// week, a pending change (if any) and the current week.
type BuildspaceSchedule struct {
	Timezone         string              `json:"timezone"`
	Today            string              `json:"today"`
	Current          BuildspaceWeekRule  `json:"current"`
	Pending          *BuildspaceWeekRule `json:"pending"`
	Week             BuildspaceWeek      `json:"week"`
	DueAt            string              `json:"dueAt"`
	PastDue          bool                `json:"pastDue"`
	PendingFirstWeek *BuildspaceWeek     `json:"pendingFirstWeek"`
}

func BuildspaceScheduleAt(rules []BuildspaceWeekRule, clock BuildspaceClock) BuildspaceSchedule {
	week := BuildspaceWeekOf(rules, clock.Today)
	cur := BuildspaceRuleFor(rules, clock.Today)
	due := BuildspaceDueMoment(week.Due, cur.DueTime, clock.Loc)
	s := BuildspaceSchedule{Timezone: clock.Loc.String(), Today: clock.Today, Current: cur, Week: week, DueAt: due.UTC().Format(time.RFC3339), PastDue: !clock.Now.Before(due)}
	for i := range rules {
		if rules[i].EffectiveFrom > clock.Today {
			p := rules[i]
			s.Pending = &p
			w := BuildspaceWeekOf(rules, p.EffectiveFrom)
			s.PendingFirstWeek = &w
			break
		}
	}
	return s
}

// SetBuildspaceDue records a new due weekday/time. It never moves the current week's
// deadline: the rule starts the day after the current week's due date. A second change
// before then replaces the pending rule, and choosing the current values again cancels it.
// It reports whether anything changed.
func (q *Queries) SetBuildspaceDue(ctx context.Context, clock BuildspaceClock, weekday int, dueTime *string, by string, now int64) (bool, error) {
	rules, err := q.BuildspaceWeekRules(ctx)
	if err != nil {
		return false, err
	}
	cur := BuildspaceRuleFor(rules, clock.Today)
	from := parseDate(BuildspaceWeekOf(rules, clock.Today).Due).AddDate(0, 0, 1).Format(isoDate)
	same := cur.DueWeekday == weekday && eqPtr(cur.DueTime, dueTime)
	var pending *BuildspaceWeekRule
	for i := range rules {
		if rules[i].EffectiveFrom > clock.Today {
			pending = &rules[i]
		}
	}
	// Anything after the next week boundary is superseded by this choice.
	if err = q.Exec(ctx, `DELETE FROM buildspace_week_rules WHERE effective_from>?`, clock.Today); err != nil {
		return false, err
	}
	if same {
		return pending != nil, nil
	}
	if pending != nil && pending.EffectiveFrom == from && pending.DueWeekday == weekday && eqPtr(pending.DueTime, dueTime) {
		// Unchanged pending rule: restore it as it was.
		return false, q.Exec(ctx, `INSERT INTO buildspace_week_rules(effective_from,due_weekday,due_time,updated_at,updated_by) VALUES(?,?,?,?,?)`, pending.EffectiveFrom, pending.DueWeekday, pending.DueTime, pending.UpdatedAt, pending.UpdatedBy)
	}
	return true, q.Exec(ctx, `INSERT INTO buildspace_week_rules(effective_from,due_weekday,due_time,updated_at,updated_by) VALUES(?,?,?,?,?)`, from, weekday, dueTime, now, by)
}

func eqPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
