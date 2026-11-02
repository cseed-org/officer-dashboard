package store

import (
	"context"
	"encoding/json"
	"officerdashboard/internal/cloudflare"
	"strings"
)

type MemberFilter struct {
	Search, Campus, GradYear, Officer string
	Limit, Offset                     int
}

func memberWhere(f MemberFilter) (string, []any) {
	where := " WHERE 1=1"
	args := []any{}
	if f.Search != "" {
		where += " AND instr(fold(first_name||' '||last_name||' '||coalesce(uw_email,'')||' '||coalesce(json_extract(record_json,'$.cs_email'),'')||' '||coalesce(json_extract(record_json,'$.personal_email'),'')||' '||coalesce(json_extract(record_json,'$.preferred_name'),'')),?)>0"
		args = append(args, Fold(f.Search))
	}
	for _, v := range []struct{ k, v string }{{"campus", f.Campus}, {"grad_year", f.GradYear}} {
		if v.v != "" {
			where += " AND " + v.k + "=?"
			args = append(args, v.v)
		}
	}
	if f.Officer != "" {
		where += " AND has_been_officer=?"
		args = append(args, f.Officer == "yes")
	}
	return where, args
}
func (q *Queries) Members(ctx context.Context, f MemberFilter) ([]cloudflare.Person, int, error) {
	where, args := memberWhere(f)
	var total int
	if err := q.q.QueryRowContext(ctx, "SELECT count(*) FROM members_mirror"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := q.q.QueryContext(ctx, "SELECT record_json FROM members_mirror"+where+" ORDER BY join_date DESC,person_id LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []cloudflare.Person{}
	for rows.Next() {
		var raw string
		var p cloudflare.Person
		if err = rows.Scan(&raw); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}
func (q *Queries) Member(ctx context.Context, id string) (cloudflare.Person, error) {
	var p cloudflare.Person
	var raw string
	err := q.q.QueryRowContext(ctx, "SELECT record_json FROM members_mirror WHERE person_id=?", id).Scan(&raw)
	if err != nil {
		return p, notFound(err)
	}
	err = json.Unmarshal([]byte(raw), &p)
	return p, err
}
func (q *Queries) SaveMember(ctx context.Context, p cloudflare.Person) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return q.Exec(ctx, `INSERT INTO members_mirror(person_id,first_name,last_name,uw_email,major,grad_year,campus,join_date,has_been_officer,record_json) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(person_id) DO UPDATE SET first_name=excluded.first_name,last_name=excluded.last_name,uw_email=excluded.uw_email,major=excluded.major,grad_year=excluded.grad_year,campus=excluded.campus,join_date=excluded.join_date,has_been_officer=excluded.has_been_officer,record_json=excluded.record_json`, p.ID, p.FirstName, p.LastName, p.UWEmail, p.Major, p.GradYear, p.Campus, p.JoinDate, p.HasBeenOfficer, string(raw))
}

// ReplaceMembers runs inside one write transaction, so failed pulls never erase the mirror.
func (q *Queries) ReplaceMembers(ctx context.Context, people []cloudflare.Person, submissions []cloudflare.Submission, now int64) error {
	if err := q.Exec(ctx, "DELETE FROM members_submissions_mirror"); err != nil {
		return err
	}
	if err := q.Exec(ctx, "DELETE FROM members_mirror"); err != nil {
		return err
	}
	for _, p := range people {
		if err := q.SaveMember(ctx, p); err != nil {
			return err
		}
	}
	for _, s := range submissions {
		if err := q.Exec(ctx, "INSERT INTO members_submissions_mirror VALUES(?,?,?,?)", s.ID, s.PersonID, s.Payload, s.ReceivedAt); err != nil {
			return err
		}
	}
	return q.SyncState(ctx, "members", &now, "")
}
func (q *Queries) SyncState(ctx context.Context, kind string, success *int64, message string) error {
	return q.Exec(ctx, `INSERT INTO cloudflare_sync_state(kind,last_success,last_error) VALUES(?,?,?) ON CONFLICT(kind) DO UPDATE SET last_success=coalesce(excluded.last_success,cloudflare_sync_state.last_success),last_error=excluded.last_error`, kind, success, message)
}
func (q *Queries) SyncStatus(ctx context.Context, kind string) (*int64, string, error) {
	var last *int64
	var msg *string
	err := q.q.QueryRowContext(ctx, "SELECT last_success,last_error FROM cloudflare_sync_state WHERE kind=?", kind).Scan(&last, &msg)
	if err != nil {
		return nil, "", notFound(err)
	}
	if msg == nil {
		return last, "", nil
	}
	return last, *msg, nil
}
func (q *Queries) SaveSnapshot(ctx context.Context, kind, day string, raw json.RawMessage, now int64) error {
	if err := q.Exec(ctx, `INSERT INTO cloudflare_snapshots VALUES(?,?,?,?) ON CONFLICT(kind,day) DO UPDATE SET data_json=excluded.data_json,synced_at=excluded.synced_at`, kind, day, string(raw), now); err != nil {
		return err
	}
	return q.SyncState(ctx, kind, &now, "")
}
func (q *Queries) Snapshot(ctx context.Context, kind string) (json.RawMessage, error) {
	var raw string
	err := q.q.QueryRowContext(ctx, "SELECT data_json FROM cloudflare_snapshots WHERE kind=? ORDER BY day DESC LIMIT 1", kind).Scan(&raw)
	return json.RawMessage(raw), notFound(err)
}
func (q *Queries) MemberFacets(ctx context.Context) (map[string][]string, error) {
	out := map[string][]string{}
	for _, col := range []string{"campus", "grad_year"} {
		rows, err := q.q.QueryContext(ctx, "SELECT DISTINCT "+col+" FROM members_mirror WHERE "+col+" IS NOT NULL ORDER BY "+col)
		if err != nil {
			return nil, err
		}
		out[col] = []string{}
		for rows.Next() {
			var v string
			if err = rows.Scan(&v); err != nil {
				rows.Close()
				return nil, err
			}
			if strings.TrimSpace(v) != "" {
				out[col] = append(out[col], v)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// MemberSignupCounts uses the last successful sync as the reporting boundary.
func (q *Queries) MemberSignupCounts(ctx context.Context, at int64) (map[string]int, error) {
	counts := map[string]int{}
	var total, week, month, today int
	err := q.q.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(date(join_date) >= date(?/1000,'unixepoch','-6 days') AND datetime(join_date) <= datetime(?/1000,'unixepoch')),0),coalesce(sum(date(join_date) >= date(?/1000,'unixepoch','-29 days') AND datetime(join_date) <= datetime(?/1000,'unixepoch')),0)  ,coalesce(sum(date(join_date)=date(?/1000,'unixepoch') AND datetime(join_date)<=datetime(?/1000,'unixepoch')),0) FROM members_mirror`, at, at, at, at, at, at).Scan(&total, &week, &month, &today)
	counts["today"] = today
	counts["members"] = total
	counts["last7Days"] = week
	counts["last30Days"] = month
	return counts, err
}
