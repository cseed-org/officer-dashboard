package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

type recordingQ struct {
	Q
	statements []string
	arguments  [][]any
}

func (r *recordingQ) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	r.statements = append(r.statements, query)
	r.arguments = append(r.arguments, append([]any(nil), args...))
	return r.Q.QueryContext(ctx, query, args...)
}

func explain(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(details, " | ")
}

func TestPerformanceQueryPlans(t *testing.T) {
	x := newTDB(t)
	ctx := context.Background()
	rec := &recordingQ{Q: x.db}
	q := On(rec)
	if _, err := q.ReportLines(ctx); err != nil {
		t.Fatal(err)
	}
	if plan := explain(t, x.db, rec.statements[0]); !strings.Contains(plan, "contributions_journal_entry_idx") {
		t.Error(plan)
	}
	for _, f := range []AuditFilter{{Limit: 51}, {EntityType: "purchase_request", Limit: 51}} {
		f.HasCursor, f.CursorCreatedAt, f.CursorID = true, 100, "cursor"
		rec.statements = nil
		rec.arguments = nil
		if _, err := q.Audit(ctx, f); err != nil {
			t.Fatal(err)
		}
		plan := explain(t, x.db, rec.statements[0], rec.arguments[0]...)
		if strings.Contains(plan, "TEMP B-TREE") || strings.Contains(plan, "MULTI-INDEX OR") {
			t.Error(plan)
		}
	}
	for _, tc := range []struct{ query, index string }{
		{"SELECT id FROM fulfillment_jobs WHERE request_id=? AND status='held'", "fulfillment_jobs_request_idx"},
		{"SELECT id FROM oauth_grants WHERE client_id=?", "oauth_grants_client"},
		{"SELECT code_hash FROM oauth_codes WHERE client_id=?", "oauth_codes_client"},
		{"SELECT id FROM buildspace_launchpad_activity WHERE purchase_id=?", "buildspace_activity_purchase_idx"},
	} {
		if plan := explain(t, x.db, tc.query, "x"); !strings.Contains(plan, tc.index) {
			t.Error(plan)
		}
	}
}

func TestAuditCursorParityAndFinanceHistory(t *testing.T) {
	x := newTDB(t)
	ctx := context.Background()
	q := On(x.db)
	for i := 0; i < 150; i++ {
		id := fmt.Sprintf("audit-%03d", i)
		kind := "purchase_request"
		if i%3 == 0 {
			kind = "profile"
		}
		before, after := `{"status":"approved"}`, `{"large":"unused"}`
		if err := q.InsertAudit(ctx, AuditEntry{ID: id, EntityType: kind, EntityID: &id, BeforeJSON: &before, AfterJSON: &after, CreatedAt: Ms(10 + i/40)}); err != nil {
			t.Fatal(err)
		}
	}
	f := AuditFilter{Limit: 50}
	var got []string
	for page := 0; page < 3; page++ {
		rows, err := q.Audit(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 50 {
			t.Fatalf("page %d: %d", page, len(rows))
		}
		for _, a := range rows {
			got = append(got, a.ID)
		}
		last := rows[len(rows)-1]
		f.HasCursor, f.CursorCreatedAt, f.CursorID = true, int64(last.CreatedAt), last.ID
	}
	rows, err := x.db.Query(`SELECT id FROM audit_log WHERE (created_at < ? OR (created_at = ? AND id < ?)) ORDER BY created_at DESC,id DESC`, 100, 100, "z")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		if got[i] != id {
			t.Fatalf("%d: %s != %s", i, got[i], id)
		}
		i++
	}
	if i != len(got) {
		t.Fatal(i)
	}
	history, err := q.AuditAfter(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 100 {
		t.Fatalf("unrelated history loaded: %d", len(history))
	}
	for _, a := range history {
		if a.AfterJSON != nil || a.BeforeJSON == nil {
			t.Fatalf("report projection: %+v", a)
		}
	}
}

func TestRequestCursorParityAndPlan(t *testing.T) {
	x := newTDB(t)
	ctx := context.Background()
	q := On(x.db)
	for i := 0; i < 150; i++ {
		id := fmt.Sprintf("cursor-%03d", i)
		x.must(`INSERT INTO purchase_requests(id,number,requester_id,type,title,vendor,quantity,unit_cost_cents,total_cents,expense_account_id,fund_id,justification,created_at,updated_at) VALUES(?,?,'u1','club_purchase','Purchase','Store',1,10,10,'cash','gen','Test',?,1)`, id, id, 10+i/40)
	}
	f := RequestFilter{Limit: 50}
	var got []string
	for i := 0; i < 3; i++ {
		rows, err := q.Requests(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 50 {
			t.Fatal(len(rows))
		}
		for _, r := range rows {
			got = append(got, r.ID)
		}
		last := rows[len(rows)-1]
		f.HasCursor, f.CursorCreatedAt, f.CursorID = true, int64(last.CreatedAt), last.ID
	}
	rows, err := x.db.Query(`SELECT id FROM purchase_requests WHERE created_at<? OR (created_at=? AND id<?) ORDER BY created_at DESC,id DESC`, 100, 100, "z")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		if got[n] != id {
			t.Fatal(n, got[n], id)
		}
		n++
	}
	if n != 150 {
		t.Fatal(n)
	}
	rec := &recordingQ{Q: x.db}
	if _, err := On(rec).Requests(ctx, f); err != nil {
		t.Fatal(err)
	}
	plan := explain(t, x.db, rec.statements[0], rec.arguments[0]...)
	if strings.Contains(plan, "TEMP B-TREE") || strings.Contains(plan, "MULTI-INDEX OR") {
		t.Fatal(plan)
	}
}
