package api

import (
	"context"
	"officerdashboard/internal/files"
	"officerdashboard/internal/store"
	"os"
	"strings"
	"testing"
)

type budgetRefusalStore struct {
	files.Store
	err error
}

func (b budgetRefusalStore) Put(context.Context, files.PutInput) (files.Stored, error) {
	return files.Stored{}, b.err
}

func TestR2UploadBudgetResponse(t *testing.T) {
	for _, err := range []error{files.ErrBudget, store.ErrR2DailyQuota} {
		e := newEnv(t, func(c *Config) { c.Files = budgetRefusalStore{c.Files, err} })
		id := e.mustOK(e.post("/api/requests", purchase, e.officer())).str("request", "id")
		r := e.do("POST", "/api/requests/"+id+"/attachments", formBody{files: []formFile{{"file", "receipt.pdf", "application/pdf", "%PDF-x"}}}, e.officer())
		if r.Status != 507 || r.str("error", "code") != "storage_budget" || r.str("error", "message") != err.Error() {
			t.Fatalf("upload budget response: %d %s", r.Status, r.Raw)
		}
	}
}

func TestR2ReceiptAuthorizationEndToEnd(t *testing.T) {
	endpoint := os.Getenv("R2_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("local MinIO not requested")
	}
	var r2 *files.R2
	e := newEnv(t, func(c *Config) {
		var err error
		r2, err = files.NewR2(c.DB, strings.Repeat("a", 32), "receipts", "r2-test-access", "r2-test-secret-only")
		if err != nil {
			t.Fatal(err)
		}
		r2.Endpoint = endpoint
		if err := r2.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		c.Files = r2
	})
	id := e.mustOK(e.post("/api/requests", purchase, e.officer())).str("request", "id")
	uploaded := e.mustOK(e.do("POST", "/api/requests/"+id+"/attachments", formBody{files: []formFile{{"file", "receipt.pdf", "application/pdf", "%PDF-1.4\nprivate"}}}, e.officer()))
	url := uploaded.str("attachment", "url")
	before, err := e.db.R2Usage(context.Background(), r2.Now())
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, "anonymous", e.get(url, anon), 401)
	expectStatus(t, "another officer", e.get(url, e.as("user-marcus")), 403)
	after, _ := e.db.R2Usage(context.Background(), r2.Now())
	if after.ClassB != before.ClassB {
		t.Fatal("unauthorized requests reached R2")
	}
	for _, who := range []actor{e.officer(), e.director()} {
		got := e.get(url, who)
		if got.Status != 200 || string(got.Raw) != "%PDF-1.4\nprivate" {
			t.Fatalf("authorized download: %d %s", got.Status, got.Raw)
		}
	}
	var key string
	if err := e.db.R.QueryRow(`SELECT storage_key FROM attachments WHERE id=?`, uploaded.str("attachment", "id")).Scan(&key); err != nil {
		t.Fatal(err)
	}
	e.db.W.Exec(`UPDATE r2_day SET class_b=9000000`)
	blocked := e.get(url, e.officer())
	if blocked.Status != 507 || blocked.str("error", "code") != "storage_budget" {
		t.Fatalf("read ceiling: %d %s", blocked.Status, blocked.Raw)
	}
	if err := r2.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	after, _ = e.db.R2Usage(context.Background(), r2.Now())
	if after.Bytes != before.Bytes-int64(len("%PDF-1.4\nprivate")) || after.ClassB != before.ClassB+2 {
		t.Fatal(after)
	}
}
