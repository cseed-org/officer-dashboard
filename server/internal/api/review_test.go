package api

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"officerdashboard/internal/files"
	"officerdashboard/internal/store"
)

// #1: entries are dated in the organisation's Pacific calendar, not UTC. At 18:00 PDT on
// June 30 it is already July 1 in UTC, which would post FY2027 spending into FY2028.
func TestEntryDatesFollowThePacificCalendar(t *testing.T) {
	e := newEnv(t)
	e.clock = time.Date(2027, 7, 1, 1, 0, 0, 0, time.UTC) // 2027-06-30 18:00 PDT
	made := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer()))
	id := made.str("request", "id")
	e.mustOK(e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director()))
	e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilling"}, e.director()))
	settled := e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilled", "actualTotalCents": 9500}, e.director())
	if settled.Status != 200 {
		t.Fatalf("settle → %d %s", settled.Status, settled.Raw)
	}
	if d := e.scalar(`SELECT entry_date FROM journal_entries WHERE id = ?`, settled.str("request", "journalEntryIds", 0)); d != "2027-06-30" {
		t.Errorf("settlement dated %v, want 2027-06-30", d)
	}

	// Restricted-fund release and reimbursement payout use the same date.
	r := e.mustOK(e.post("/api/requests", with(purchase, "type", "reimbursement", "fundId", "fund-buildspace", "submit", true), e.officer()))
	rid := r.str("request", "id")
	e.mustOK(e.post("/api/requests/"+rid+"/approve", map[string]any{}, e.director()))
	e.mustOK(e.post("/api/requests/"+rid+"/transition", map[string]any{"to": "reimbursed", "actualTotalCents": 8000}, e.director()))
	e.mustOK(e.post("/api/requests/"+rid+"/transition", map[string]any{"to": "closed"}, e.director()))
	if n := e.scalar(`SELECT count(*) FROM journal_entries WHERE purchase_request_id = ? AND entry_date <> '2027-06-30'`, rid); n != int64(0) {
		t.Errorf("%v entries of the reimbursement are not dated 2027-06-30", n)
	}
	if n := e.scalar(`SELECT count(*) FROM journal_entries WHERE purchase_request_id = ?`, rid); n != int64(3) {
		t.Errorf("entries %v, want expense + release + payout", n)
	}
	// "Today" in lookups is the Pacific date too: still FY2027.
	if fy := e.get("/api/lookups", e.officer()).num("currentFiscalYear"); fy != 2027 {
		t.Errorf("currentFiscalYear %d at 18:00 PDT on June 30", fy)
	}
}

// #3: case-insensitive means Unicode, not just ASCII.
func TestSearchAndFiltersFoldUnicodeCase(t *testing.T) {
	e := newEnv(t)
	e.mustOK(e.post("/api/admin/users", map[string]any{"discordId": "100000000000000055", "name": "José Núñez", "title": "Ñandú Lead"}, e.director()))
	if r := e.mustOK(e.get("/api/search?q=JOS%C3%89", e.director())); kinds(r)["user"] != 1 {
		t.Errorf("JOSÉ should find José Núñez: %s", r.Raw)
	}
	if r := e.mustOK(e.get("/api/search?q=n%C3%9A%C3%91ez", e.director())); kinds(r)["user"] != 1 {
		t.Errorf("NÚÑEZ should find Núñez: %s", r.Raw)
	}
	e.mustOK(e.post("/api/requests", with(purchase, "title", "Café supplies"), e.officer()))
	if r := e.mustOK(e.get("/api/search?q=CAF%C3%89", e.officer())); kinds(r)["request"] != 1 {
		t.Errorf("CAFÉ should find the request: %s", r.Raw)
	}
	if r := e.mustOK(e.get("/api/requests?scope=all&q=CAF%C3%89", e.officer())); len(r.list("items")) != 1 {
		t.Errorf("request filter should fold É: %s", r.Raw)
	}
	if r := e.mustOK(e.get("/api/search?q=caf%C3%A9", e.officer())); kinds(r)["request"] != 1 {
		t.Errorf("lowercase é still works: %s", r.Raw)
	}
}

// #7: a blob stored for an upload whose transaction then fails must not be left behind.
type hookStore struct {
	files.Store
	mu      sync.Mutex
	onPut   func()
	deleted []string
}

func (h *hookStore) Put(ctx context.Context, in files.PutInput) (files.Stored, error) {
	s, err := h.Store.Put(ctx, in)
	if h.onPut != nil {
		h.onPut()
	}
	return s, err
}
func (h *hookStore) Delete(ctx context.Context, key string) error {
	h.mu.Lock()
	h.deleted = append(h.deleted, key)
	h.mu.Unlock()
	return h.Store.Delete(ctx, key)
}

func TestUploadBlobIsDeletedWhenTheTransactionFails(t *testing.T) {
	var hs *hookStore
	e := newEnv(t, func(c *Config) {
		hs = &hookStore{Store: c.Files}
		c.Files = hs
	})
	made := e.mustOK(e.post("/api/requests", purchase, e.officer()))
	id := made.str("request", "id")
	// The request is cancelled while the upload is in flight.
	hs.onPut = func() {
		if err := e.db.Write(context.Background(), func(w *store.Queries) error {
			return w.Exec(context.Background(), `UPDATE purchase_requests SET status = 'cancelled' WHERE id = ?`, id)
		}); err != nil {
			t.Error(err)
		}
	}
	r := e.do("POST", "/api/requests/"+id+"/attachments", formBody{files: []formFile{{"file", "r.pdf", "application/pdf", "%PDF-1.4\nx"}}}, e.officer())
	if r.Status != 409 {
		t.Fatalf("upload to a cancelled request → %d %s", r.Status, r.Raw)
	}
	if len(hs.deleted) != 1 {
		t.Errorf("orphaned blob not deleted: %v", hs.deleted)
	}
	if n := e.scalar(`SELECT count(*) FROM attachments`); n != int64(0) {
		t.Errorf("attachment rows %v", n)
	}
}

// #8: a number is never handed out twice, even after the newest draft is deleted.
func TestRequestNumbersAreNeverReused(t *testing.T) {
	e := newEnv(t)
	a := e.mustOK(e.post("/api/requests", purchase, e.officer()))
	b := e.mustOK(e.post("/api/requests", purchase, e.officer()))
	expectStatus(t, "delete newest draft", e.do("DELETE", "/api/requests/"+b.str("request", "id"), nil, e.officer()), 200)
	c := e.mustOK(e.post("/api/requests", purchase, e.officer()))
	var na, nb, nc int
	fmt.Sscanf(strings.TrimPrefix(a.str("request", "number"), "2027-"), "%d", &na)
	fmt.Sscanf(strings.TrimPrefix(b.str("request", "number"), "2027-"), "%d", &nb)
	fmt.Sscanf(strings.TrimPrefix(c.str("request", "number"), "2027-"), "%d", &nc)
	if nb != na+1 || nc != nb+1 {
		t.Errorf("numbers %s, %s, %s: the deleted number was reused", a.str("request", "number"), b.str("request", "number"), c.str("request", "number"))
	}
}

// #6: the bytes must look like what the MIME type claims.
func TestUploadContentMustMatchItsType(t *testing.T) {
	e := newEnv(t)
	id := e.mustOK(e.post("/api/requests", purchase, e.officer())).str("request", "id")
	up := func(mime, content string) int {
		return e.do("POST", "/api/requests/"+id+"/attachments", formBody{files: []formFile{{"file", "x", mime, content}}}, e.officer()).Status
	}
	if got := up("application/pdf", "<html><script>alert(1)</script>"); got != 400 {
		t.Errorf("HTML labelled PDF → %d", got)
	}
	if got := up("image/png", "%PDF-1.4"); got != 400 {
		t.Errorf("PDF labelled PNG → %d", got)
	}
	for mime, content := range map[string]string{
		"application/pdf": "%PDF-1.7\n", "image/png": "\x89PNG\r\n\x1a\nxxxx", "image/jpeg": "\xff\xd8\xff\xe0xxxx",
		"image/webp": "RIFF\x00\x00\x00\x00WEBPVP8 ", "image/heic": "\x00\x00\x00\x18ftypheic\x00\x00",
	} {
		if got := up(mime, content); got != 200 {
			t.Errorf("valid %s → %d", mime, got)
		}
	}
}

// #5: Sec-Fetch-Site: cross-site is a second signal when Origin is absent.
func TestSecFetchSiteCrossSiteIsRefused(t *testing.T) {
	e := newEnv(t)
	w := e.officer()
	w.headers = map[string]string{"Sec-Fetch-Site": "cross-site"}
	expectStatus(t, "cross-site POST", e.post("/api/requests", purchase, w), 403)
	for _, v := range []string{"same-origin", "same-site", "none"} {
		w.headers = map[string]string{"Sec-Fetch-Site": v}
		expectStatus(t, v, e.post("/api/requests", purchase, w), 200)
	}
	w.headers = map[string]string{"Sec-Fetch-Site": "cross-site"}
	expectStatus(t, "cross-site GET is a read", e.get("/api/me", w), 200)
}

// Commitments follow the Pacific day on both sides of midnight UTC: at 02:37 UTC on Nov 1 it
// is still Oct 31 in Pacific, and a request approved "today" must count as committed today.
func TestCommitmentsAreStableAcrossMidnightUTC(t *testing.T) {
	for _, at := range []time.Time{
		time.Date(2026, 10, 31, 18, 0, 0, 0, time.UTC), // 11:00 PDT Oct 31
		time.Date(2026, 11, 1, 2, 37, 0, 0, time.UTC),  // 19:37 PDT Oct 31, after midnight UTC
		time.Date(2026, 11, 1, 6, 59, 0, 0, time.UTC),  // 23:59 PDT Oct 31
		time.Date(2026, 11, 1, 7, 1, 0, 0, time.UTC),   // 00:01 PDT Nov 1
	} {
		t.Run(at.Format(time.RFC3339), func(t *testing.T) {
			e := newEnv(t)
			e.clock = at
			base := e.get("/api/finance/summary", e.officer()).num("totals", "committedCents")
			id := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer())).str("request", "id")
			e.mustOK(e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director()))
			committed := e.get("/api/finance/summary", e.officer()).num("totals", "committedCents")
			if committed != base+10000 {
				t.Fatalf("after approval committed %d, want %d", committed, base+10000)
			}
			e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilling"}, e.director()))
			e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilled", "actualTotalCents": 9500}, e.director()))
			if got := e.get("/api/finance/summary", e.officer()).num("totals", "committedCents"); got != base {
				t.Errorf("after settlement committed %d, want %d", got, base)
			}
			// An explicit asOf of the Pacific day sees the same books as the default.
			day := at.In(pacificForTest).Format("2006-01-02")
			if got := e.get("/api/finance/summary?asOf="+day, e.officer()).num("totals", "committedCents"); got != base {
				t.Errorf("asOf=%s committed %d, want %d", day, got, base)
			}
		})
	}
}

// F1: a client that hangs up mid-request is not a server error and must not log at ERROR.
func TestClientCancellationIsNotAServerError(t *testing.T) {
	var logs bytes.Buffer
	e := newEnv(t, func(c *Config) {
		c.Log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	ck := e.login("user-director")
	for _, path := range []string{"/api/requests?scope=all", "/api/finance/summary", "/api/me"} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := httptest.NewRequest("GET", "http://localhost:8090"+path, nil).WithContext(ctx)
		req.AddCookie(ck)
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		if rec.Code == 500 || rec.Code == 401 {
			t.Errorf("%s with a canceled client → %d, want a non-5xx cancellation status", path, rec.Code)
		}
		if rec.Code != 499 {
			t.Errorf("%s → %d, want 499", path, rec.Code)
		}
	}
	if strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("cancellation logged at ERROR:\n%s", logs.String())
	}
	// A real failure is still a logged 500.
	if got := toAPIError(fmt.Errorf("disk on fire"), slog.New(slog.NewTextHandler(&logs, nil))); got.Status != 500 || !strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("genuine errors must stay 500 + ERROR: %+v", got)
	}
	// A deadline is a timeout, not a 500 either.
	if got := toAPIError(fmt.Errorf("query: %w", context.DeadlineExceeded), slog.Default()); got.Status != 504 {
		t.Errorf("deadline → %d", got.Status)
	}
}
