package api

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"officerdashboard/internal/purchases"
)

func TestEnforcesRolesValidatesInputsAndProtectsTheLastDirector(t *testing.T) {
	e := newEnv(t)
	expectStatus(t, "officer on admin API", e.get("/api/admin/users", e.officer()), 403)
	expectStatus(t, "officer approving", e.post("/api/requests/nope/approve", map[string]any{}, e.officer()), 403)
	expectStatus(t, "empty event body", e.post("/api/events", map[string]any{}, e.officer()), 400)
	expectStatus(t, "deactivate the last leadership", e.do("PATCH", "/api/admin/users/user-leadership", map[string]any{"active": false}, e.as("user-leadership")), 409)
	expectStatus(t, "demote the last director", e.do("PATCH", "/api/admin/users/user-director", map[string]any{"role": "officer"}, e.director()), 403)
	expectStatus(t, "anonymous", e.get("/api/lookups", anon), 401)
	expectStatus(t, "unknown API path", e.get("/api/nope", e.officer()), 404)
	expectStatus(t, "unknown API path, anonymous", e.get("/api/nope", anon), 401)
	if r := e.get("/api/health", anon); r.Status != 200 || r.at("ok") != true || r.at("db") != true {
		t.Errorf("health %d %s", r.Status, r.Raw)
	}
	bad := e.post("/api/events", map[string]any{}, e.officer())
	if bad.at("error", "code") != "bad_request" || bad.at("error", "details", "fieldErrors", "name") == nil {
		t.Errorf("validation error shape: %s", bad.Raw)
	}
	if r := e.post("/api/events", "not json", e.officer()); r.Status != 400 || r.str("error", "message") != "Expected a JSON body" {
		t.Errorf("non-JSON body: %d %s", r.Status, r.Raw)
	}
}

func TestSettlesActualCostLinksJournalReleasesEncumbranceAndForbidsRepeatSettlement(t *testing.T) {
	e := newEnv(t)
	made := e.mustOK(e.post("/api/requests", purchase, e.officer()))
	id := made.str("request", "id")
	if made.str("request", "status") != "draft" {
		t.Errorf("created status %s", made.str("request", "status"))
	}
	if r := e.mustOK(e.post("/api/requests/"+id+"/submit", nil, e.officer())); r.str("request", "status") != "submitted" {
		t.Errorf("submit → %s", r.str("request", "status"))
	}
	if r := e.mustOK(e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director())); r.str("request", "status") != "approved" {
		t.Errorf("approve → %s", r.str("request", "status"))
	}
	committed := e.get("/api/finance/summary", e.officer())
	if committed.num("totals", "committedCents") < 10000 {
		t.Errorf("committed %d", committed.num("totals", "committedCents"))
	}
	expectStatus(t, "start fulfilling", e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilling"}, e.director()), 200)
	settled := e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilled", "actualTotalCents": 9500}, e.director()))
	if settled.num("request", "actualTotalCents") != 9500 || len(settled.list("request", "journalEntryIds")) != 1 {
		t.Errorf("settled %s", settled.Raw)
	}
	after := e.get("/api/finance/summary", e.officer())
	if after.num("totals", "committedCents") != committed.num("totals", "committedCents")-10000 {
		t.Errorf("committed %d → %d, want a drop of 10000", committed.num("totals", "committedCents"), after.num("totals", "committedCents"))
	}
	if after.num("totals", "spentCents") != committed.num("totals", "spentCents")+9500 {
		t.Errorf("spent %d → %d, want a rise of 9500", committed.num("totals", "spentCents"), after.num("totals", "spentCents"))
	}
	expectStatus(t, "settle twice", e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilled", "actualTotalCents": 9500}, e.director()), 409)
	entry := settled.str("request", "journalEntryIds", 0)
	if d, c := e.scalar(`SELECT sum(debit_cents) FROM journal_lines WHERE entry_id = ?`, entry), e.scalar(`SELECT sum(credit_cents) FROM journal_lines WHERE entry_id = ?`, entry); d != int64(9500) || c != int64(9500) {
		t.Errorf("entry debits %v credits %v", d, c)
	}
}

func TestForbidsSelfApprovalAndDraftEditsAfterSubmission(t *testing.T) {
	e := newEnv(t)
	made := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.director()))
	id := made.str("request", "id")
	expectStatus(t, "self approve", e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director()), 403)
	expectStatus(t, "edit after submit", e.do("PATCH", "/api/requests/"+id, map[string]any{"title": "Edit"}, e.director()), 409)
	expectStatus(t, "non-requester edit", e.do("PATCH", "/api/requests/"+id, map[string]any{"title": "Edit"}, e.officer()), 403)
	// The requester sees no approve button; another director would.
	if got := made.list("request", "allowedTransitions"); fmt.Sprint(got) != "[denied cancelled]" {
		t.Errorf("allowedTransitions for the requester-director: %v", got)
	}
}

func TestHashesAndRevokesScopedTokensIngestsIdempotentlyAndClaimsJobsExclusively(t *testing.T) {
	e := newEnv(t)
	issued := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "test", "scopes": []string{"ingest:luma", "agent:jobs"}}, e.director()))
	token := issued.str("plaintext")
	if !strings.HasPrefix(token, "cseed_") || issued.at("token", "tokenHash") != nil {
		t.Errorf("issued %s", issued.Raw)
	}
	if stored := e.scalar(`SELECT token_hash FROM service_tokens WHERE id = ?`, issued.str("token", "id")); stored == token || len(fmt.Sprint(stored)) != 64 {
		t.Errorf("stored hash %v", stored)
	}
	input := map[string]any{
		"event":  map[string]any{"lumaId": "test-ingest", "name": "Test import", "startsAt": "2026-11-01T10:00:00Z"},
		"guests": []any{map[string]any{"email": "A@EXAMPLE.ORG", "checkedInAt": "2026-11-01T10:00:00Z"}},
	}
	first := e.post("/api/ingest/luma", input, bearer(token))
	if first.Status != 200 || first.num("created") != 1 {
		t.Fatalf("first ingest %d %s", first.Status, first.Raw)
	}
	again := e.post("/api/ingest/luma", input, bearer(token))
	if again.num("created") != 0 || again.num("updated") != 1 {
		t.Errorf("second ingest %s", again.Raw)
	}
	made := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer()))
	id := made.str("request", "id")
	e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director())
	jobs := e.get("/api/agent/jobs", bearer(token))
	jobID := findJob(jobs, id)
	if jobID == "" {
		t.Fatalf("no queued job for %s in %s", id, jobs.Raw)
	}
	expectStatus(t, "claim", e.post("/api/agent/jobs/"+jobID+"/claim", nil, bearer(token)), 200)
	expectStatus(t, "claim again", e.post("/api/agent/jobs/"+jobID+"/claim", nil, bearer(token)), 409)
	done := e.post("/api/agent/jobs/"+jobID+"/complete", map[string]any{"outcome": "done", "actualTotalCents": 9800}, bearer(token))
	if done.Status != 200 || done.num("job", "request", "actualTotalCents") != 9800 || done.str("job", "request", "status") != "fulfilled" {
		t.Errorf("complete %d %s", done.Status, done.Raw)
	}
	expectStatus(t, "revoke", e.do("DELETE", "/api/admin/tokens/"+issued.str("token", "id"), nil, e.director()), 200)
	expectStatus(t, "revoked token", e.get("/api/agent/jobs", bearer(token)), 401)
	expectStatus(t, "revoke twice", e.do("DELETE", "/api/admin/tokens/"+issued.str("token", "id"), nil, e.director()), 409)
	expectStatus(t, "no token", e.get("/api/agent/jobs", anon), 401)
	expectStatus(t, "garbage token", e.get("/api/agent/jobs", bearer("cseed_nope")), 401)
}

func findJob(r reply, requestID string) string {
	for _, j := range r.list("jobs") {
		m := j.(map[string]any)
		if req, ok := m["request"].(map[string]any); ok && req["id"] == requestID {
			return m["id"].(string)
		}
	}
	return ""
}

func TestReadModelsAreContractShapedAndReportsTieTogether(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/api/lookups", "/api/overview", "/api/events", "/api/requests", "/api/finance/summary", "/api/finance/budgets", "/api/finance/periods", "/api/finance/accounts", "/api/admin/users", "/api/admin/tokens", "/api/admin/audit"} {
		if r := e.get(p, e.director()); r.Status != 200 {
			t.Errorf("%s → %d %s", p, r.Status, r.Raw)
		}
	}
	expectStatus(t, "officer on trial balance", e.get("/api/finance/reports/trial-balance", e.officer()), 403)
	expectStatus(t, "officer on fund balances", e.get("/api/finance/reports/fund-balances", e.officer()), 200)
	expectStatus(t, "unknown report", e.get("/api/finance/reports/nope", e.director()), 400)
	tb := e.get("/api/finance/reports/trial-balance", e.director())
	if tb.num("totals", "debitCents") != tb.num("totals", "creditCents") || tb.num("totals", "debitCents") == 0 {
		t.Errorf("trial balance %s", tb.at("totals"))
	}
	pos := e.get("/api/finance/reports/statement-of-financial-position", e.director())
	if pos.at("totals", "balanced") != true {
		t.Errorf("position %v", pos.at("totals"))
	}
	act := e.get("/api/finance/reports/statement-of-activities", e.director())
	if act.num("netAssetsEnding", "totalCents") != pos.num("totals", "netAssetsCents") {
		t.Errorf("activities ending %d vs position %d", act.num("netAssetsEnding", "totalCents"), pos.num("totals", "netAssetsCents"))
	}
	sum := func(rows []any, key string) (n int64) {
		for _, r := range rows {
			n += int64(r.(map[string]any)[key].(float64))
		}
		return
	}
	rev, exp := act.list("revenue"), act.list("expenses")
	rel := act.num("releasesCents")
	if got := sum(rev, "withoutDonorCents") - sum(exp, "withoutDonorCents") + rel; got != act.num("changeInNetAssets", "withoutDonorCents") {
		t.Errorf("unrestricted does not foot: %d vs %d", got, act.num("changeInNetAssets", "withoutDonorCents"))
	}
	if got := sum(rev, "withDonorCents") - sum(exp, "withDonorCents") - rel; got != act.num("changeInNetAssets", "withDonorCents") {
		t.Errorf("restricted does not foot: %d vs %d", got, act.num("changeInNetAssets", "withDonorCents"))
	}
	f990 := e.get("/api/finance/reports/form-990", e.director())
	if len(f990.list("unmappedAccounts")) != 0 || f990.num("totals", "partVIIILine12Cents") != sum(rev, "totalCents") || f990.num("totals", "partIXLine25", "totalCents") != sum(exp, "totalCents") {
		t.Errorf("990 %s", f990.Raw)
	}
	for _, name := range []string{"statement-of-functional-expenses", "fund-balances", "budget-vs-actual", "form-990"} {
		expectStatus(t, name, e.get("/api/finance/reports/"+name, e.director()), 200)
	}
}

// The seeded FY2027 statements foot as documented in the v1 handoff: unrestricted
// 1,800.00 − 591.40 + 186.40 = 1,395.00, net assets ending 11,908.60 = assets.
func TestSeededFY2027StatementsFoot(t *testing.T) {
	e := newEnv(t)
	act := e.get("/api/finance/reports/statement-of-activities?fiscalYear=2027&asOf=2026-10-31", e.director())
	var revWithout, expWithout int64
	for _, r := range act.list("revenue") {
		revWithout += int64(r.(map[string]any)["withoutDonorCents"].(float64))
	}
	for _, r := range act.list("expenses") {
		expWithout += int64(r.(map[string]any)["withoutDonorCents"].(float64))
	}
	if revWithout != 180_000 || expWithout != 59_140 || act.num("releasesCents") != 18_640 || act.num("changeInNetAssets", "withoutDonorCents") != 139_500 {
		t.Errorf("unrestricted: revenue %d expenses %d releases %d change %d", revWithout, expWithout, act.num("releasesCents"), act.num("changeInNetAssets", "withoutDonorCents"))
	}
	if act.num("netAssetsEnding", "totalCents") != 1_190_860 {
		t.Errorf("net assets ending %d", act.num("netAssetsEnding", "totalCents"))
	}
	pos := e.get("/api/finance/reports/statement-of-financial-position?fiscalYear=2027&asOf=2026-10-31", e.director())
	if pos.num("totals", "assetsCents") != 1_190_860 || pos.at("totals", "balanced") != true {
		t.Errorf("assets %d balanced %v", pos.num("totals", "assetsCents"), pos.at("totals", "balanced"))
	}
}

func TestReleasesRestrictedPurchasesPaysReimbursementsOnCloseAndSerializesApprovals(t *testing.T) {
	e := newEnv(t)
	made := e.mustOK(e.post("/api/requests", with(purchase, "type", "reimbursement", "fundId", "fund-buildspace", "submit", true), e.officer()))
	id := made.str("request", "id")
	var wg sync.WaitGroup
	statuses := make([]int, 2)
	for i := range statuses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i] = e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director()).Status
		}()
	}
	wg.Wait()
	sort.Ints(statuses)
	if statuses[0] != 200 || statuses[1] != 409 {
		t.Errorf("concurrent approvals → %v, want [200 409]", statuses)
	}
	expectStatus(t, "over the restricted balance", e.post("/api/requests/"+id+"/transition", map[string]any{"to": "reimbursed", "actualTotalCents": 1_000_000}, e.director()), 409)
	settled := e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "reimbursed", "actualTotalCents": 8000}, e.director()))
	if len(settled.list("request", "journalEntryIds")) != 2 {
		t.Errorf("entries after settle: %v", settled.list("request", "journalEntryIds"))
	}
	if n := e.scalar(`SELECT count(*) FROM journal_entries WHERE purchase_request_id = ? AND source = 'release'`, id); n != int64(1) {
		t.Errorf("releases %v", n)
	}
	act := e.get("/api/finance/reports/statement-of-activities", e.director())
	for _, r := range act.list("expenses") {
		if r.(map[string]any)["withDonorCents"].(float64) != 0 {
			t.Errorf("expense with donor restriction: %v", r)
		}
	}
	closed := e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "closed"}, e.director()))
	if len(closed.list("request", "journalEntryIds")) != 3 {
		t.Errorf("entries after close: %v", closed.list("request", "journalEntryIds"))
	}
	if n := e.scalar(`SELECT coalesce(sum(l.debit_cents - l.credit_cents), 0) FROM journal_lines l JOIN journal_entries en ON en.id = l.entry_id JOIN accounts a ON a.id = l.account_id WHERE en.purchase_request_id = ? AND a.code = '2100'`, id); n != int64(0) {
		t.Errorf("payable not cleared: %v", n)
	}
}

func TestKeepsCheckInsOnSparseReimportValidatesCSVDryRunAndCleansDraftAttachments(t *testing.T) {
	e := newEnv(t)
	issued := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "import only", "scopes": []string{"ingest:luma"}}, e.director()))
	token := issued.str("plaintext")
	expectStatus(t, "wrong scope", e.get("/api/agent/jobs", bearer(token)), 403)
	input := map[string]any{
		"event":  map[string]any{"lumaId": "sparse", "name": "Sparse data", "startsAt": "2026-11-01T10:00:00Z"},
		"guests": []any{map[string]any{"email": "sparse@example.org", "checkedInAt": "2026-11-01T10:00:00Z"}},
	}
	first := e.post("/api/ingest/luma", input, bearer(token))
	second := e.post("/api/ingest/luma", with(input, "guests", []any{map[string]any{"email": "sparse@example.org"}}), bearer(token))
	if second.Status != 200 || second.num("metrics", "checkedIn") != 1 {
		t.Errorf("sparse re-import lost the check-in: %s", second.Raw)
	}
	dry := e.do("POST", "/api/events/import", formBody{
		fields: map[string]string{"eventId": first.str("eventId"), "kind": "reflections", "dryRun": "true"},
		files:  []formFile{{"file", "reflections.csv", "text/csv", "Email,Timestamp\nsparse@example.org,2026-11-01T11:00:00Z"}},
	}, e.officer())
	if dry.Status != 200 || dry.num("metrics", "matchedReflections") != 1 || dry.at("dryRun") != true {
		t.Errorf("dry run %d %s", dry.Status, dry.Raw)
	}
	if n := e.scalar(`SELECT count(*) FROM reflections WHERE event_id = ?`, first.str("eventId")); n != int64(0) {
		t.Errorf("dry run wrote %v reflections", n)
	}
	cleared := e.post("/api/ingest/luma", with(input, "guests", []any{map[string]any{"email": "sparse@example.org", "checkedInAt": nil}}), bearer(token))
	if cleared.Status != 200 || cleared.num("metrics", "checkedIn") != 0 {
		t.Errorf("explicit null should clear the check-in: %s", cleared.Raw)
	}
	draft := e.mustOK(e.post("/api/requests", purchase, e.officer()))
	id := draft.str("request", "id")
	attached := e.do("POST", "/api/requests/"+id+"/attachments", formBody{
		fields: map[string]string{"kind": "receipt"}, files: []formFile{{"file", "test.pdf", "application/pdf", "%PDF-1.4\nTest"}},
	}, e.officer())
	if attached.Status != 200 || attached.str("attachment", "filename") != "test.pdf" || attached.str("attachment", "url") == "" {
		t.Fatalf("attach %d %s", attached.Status, attached.Raw)
	}
	// The receipt downloads through the API (local storage).
	dl := e.get(attached.str("attachment", "url"), e.officer())
	if dl.Status != 200 || string(dl.Raw) != "%PDF-1.4\nTest" || dl.Header.Get("Content-Type") != "application/pdf" {
		t.Errorf("download %d %q %s", dl.Status, dl.Raw, dl.Header.Get("Content-Type"))
	}
	expectStatus(t, "bad MIME", e.do("POST", "/api/requests/"+id+"/attachments", formBody{files: []formFile{{"file", "x.html", "text/html", "<b>"}}}, e.officer()), 400)
	expectStatus(t, "delete draft", e.do("DELETE", "/api/requests/"+id, nil, e.officer()), 200)
	expectStatus(t, "deleted request", e.get("/api/requests/"+id, e.officer()), 404)
	before := e.scalar(`SELECT before_json FROM audit_log WHERE entity_id = ? AND action = 'request.deleted'`, id)
	if !strings.Contains(fmt.Sprint(before), `"attachments":[{`) {
		t.Errorf("audit before-image lacks the attachment: %v", before)
	}
}

func TestDeniesAndCancelsRequestsAndInvalidatesDeactivatedSessions(t *testing.T) {
	e := newEnv(t)
	sub := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer()))
	id := sub.str("request", "id")
	denied := e.post("/api/requests/"+id+"/deny", map[string]any{"comment": "Outside budget"}, e.director())
	if denied.str("request", "status") != "denied" {
		t.Errorf("deny → %s", denied.Raw)
	}
	expectStatus(t, "deny without a comment", e.post("/api/requests/"+id+"/deny", map[string]any{}, e.director()), 400)
	expectStatus(t, "approve a denied request", e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director()), 409)
	if r := e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director()); r.at("error", "code") != "invalid_transition" {
		t.Errorf("error code %v", r.at("error", "code"))
	}
	cancelled := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer()))
	cid := cancelled.str("request", "id")
	e.post("/api/requests/"+cid+"/approve", map[string]any{}, e.director())
	expectStatus(t, "cancel", e.post("/api/requests/"+cid+"/transition", map[string]any{"to": "cancelled"}, e.director()), 200)
	if s := e.scalar(`SELECT status FROM fulfillment_jobs WHERE request_id = ?`, cid); s != "cancelled" {
		t.Errorf("job status %v", s)
	}
	created := e.mustOK(e.post("/api/admin/users", map[string]any{"discordId": "100000000000000099", "name": "Session test"}, e.director()))
	uid := created.str("user", "id")
	victim := e.as(uid)
	expectStatus(t, "new user can read", e.get("/api/me", victim), 200)
	expectStatus(t, "deactivate", e.do("PATCH", "/api/admin/users/"+uid, map[string]any{"active": false}, e.director()), 200)
	if n := e.scalar(`SELECT count(*) FROM sessions WHERE user_id = ?`, uid); n != int64(0) {
		t.Errorf("sessions left: %v", n)
	}
	expectStatus(t, "deactivated user", e.get("/api/me", victim), 401)
	expectStatus(t, "duplicate Discord ID", e.post("/api/admin/users", map[string]any{"discordId": "100000000000000099", "name": "Dup"}, e.director()), 409)
	expectStatus(t, "bad Discord ID", e.post("/api/admin/users", map[string]any{"discordId": "12", "name": "Dup"}, e.director()), 400)
}

func TestUpsertsAFull5000GuestIngestInBoundedTime(t *testing.T) {
	e := newEnv(t)
	issued := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "bulk ingest", "scopes": []string{"ingest:luma"}}, e.director()))
	guests := make([]any, 5000)
	for i := range guests {
		guests[i] = map[string]any{"email": fmt.Sprintf("bulk%d@example.org", i)}
	}
	input := map[string]any{"event": map[string]any{"lumaId": "bulk-5000", "name": "Bulk event", "startsAt": "2026-11-01T10:00:00Z"}, "guests": guests}
	first := e.post("/api/ingest/luma", input, bearer(issued.str("plaintext")))
	if first.Status != 200 || first.num("created") != 5000 {
		t.Fatalf("first %d %.200s", first.Status, first.Raw)
	}
	again := e.post("/api/ingest/luma", input, bearer(issued.str("plaintext")))
	if again.num("created") != 0 || again.num("updated") != 5000 || again.num("metrics", "registered") != 5000 {
		t.Errorf("again %.300s", again.Raw)
	}
	tooMany := make([]any, 5001)
	for i := range tooMany {
		tooMany[i] = map[string]any{"email": fmt.Sprintf("x%d@example.org", i)}
	}
	expectStatus(t, "5001 guests", e.post("/api/ingest/luma", with(input, "guests", tooMany), bearer(issued.str("plaintext"))), 400)
}

func TestRequeuesFailedJobsThroughThreeAttemptsThenLeavesThemForADirector(t *testing.T) {
	e := newEnv(t)
	token := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "retry worker", "scopes": []string{"agent:jobs"}}, e.director())).str("plaintext")
	made := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer()))
	id := made.str("request", "id")
	e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director())
	jobID := findJob(e.get("/api/agent/jobs", bearer(token)), id)
	for attempt := 1; attempt <= 3; attempt++ {
		claimed := e.post("/api/agent/jobs/"+jobID+"/claim", nil, bearer(token))
		if claimed.Status != 200 || claimed.num("job", "attempts") != int64(attempt) {
			t.Fatalf("claim %d: %d %s", attempt, claimed.Status, claimed.Raw)
		}
		failed := e.post("/api/agent/jobs/"+jobID+"/complete", map[string]any{"outcome": "failed", "error": "Vendor unavailable"}, bearer(token))
		want := "queued"
		if attempt == 3 {
			want = "failed"
		}
		if failed.Status != 200 || failed.str("job", "status") != want || failed.str("job", "request", "status") != "approved" || failed.at("job", "claimedAt") != nil {
			t.Errorf("failed attempt %d: %d %s", attempt, failed.Status, failed.Raw)
		}
		queued := e.get("/api/agent/jobs", bearer(token))
		if (findJob(queued, id) != "") != (attempt < 3) {
			t.Errorf("attempt %d: job queued = %v", attempt, findJob(queued, id) != "")
		}
		expectStatus(t, "stale claim", e.post("/api/agent/jobs/"+jobID+"/complete", map[string]any{"outcome": "failed", "error": "stale claim"}, bearer(token)), 409)
	}
	expectStatus(t, "fourth claim", e.post("/api/agent/jobs/"+jobID+"/claim", nil, bearer(token)), 409)
}

func TestHoldsAnAgentSettlementAboveTheEstimatePlusToleranceForDirectorReview(t *testing.T) {
	e := newEnv(t)
	token := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "tolerance", "scopes": []string{"agent:jobs"}}, e.director())).str("plaintext")
	// Estimate $100.00 → default ceiling is max($110.00, $125.00) = $125.00.
	made := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer()))
	id, number := made.str("request", "id"), made.str("request", "number")
	e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director())
	jobID := findJob(e.get("/api/agent/jobs", bearer(token)), id)
	e.post("/api/agent/jobs/"+jobID+"/claim", nil, bearer(token))
	over := e.post("/api/agent/jobs/"+jobID+"/complete", map[string]any{"outcome": "done", "actualTotalCents": 12501}, bearer(token))
	if over.Status != 409 || !strings.Contains(over.str("error", "message"), "director review") {
		t.Fatalf("over tolerance → %d %s", over.Status, over.Raw)
	}
	held := e.get("/api/requests/"+id, e.director())
	if held.str("request", "status") != "fulfilling" || len(held.list("request", "journalEntryIds")) != 0 {
		t.Errorf("held request %s", held.Raw)
	}
	if held.num("request", "settlementHold", "attemptedCents") != 12501 || held.num("request", "settlementHold", "approvedCents") != 10000 ||
		!strings.Contains(held.str("request", "settlementHold", "reason"), "director review") {
		t.Errorf("settlementHold %v", held.at("request", "settlementHold"))
	}
	row := e.scalar(`SELECT status || '|' || coalesce(claimed_by_token_id, 'null') || '|' || last_error FROM fulfillment_jobs WHERE id = ?`, jobID)
	if !strings.HasPrefix(fmt.Sprint(row), "held|null|") || !strings.Contains(fmt.Sprint(row), "$125.01") {
		t.Errorf("job row %v", row)
	}
	// A later failed (or another done) from the agent can't undo the hold or re-queue the job.
	expectStatus(t, "failed after hold", e.post("/api/agent/jobs/"+jobID+"/complete", map[string]any{"outcome": "failed", "error": "complete rejected"}, bearer(token)), 409)
	expectStatus(t, "done after hold", e.post("/api/agent/jobs/"+jobID+"/complete", map[string]any{"outcome": "done", "actualTotalCents": 9000}, bearer(token)), 409)
	expectStatus(t, "claim after hold", e.post("/api/agent/jobs/"+jobID+"/claim", nil, bearer(token)), 409)
	if findJob(e.get("/api/agent/jobs", bearer(token)), id) != "" {
		t.Error("held job must not be queued")
	}
	// Directors are notified with the attempted vs approved amount.
	var note map[string]any
	for _, w := range e.webhooks.all() {
		emb := w["embeds"].([]any)[0].(map[string]any)
		if emb["description"] == "request.held" && strings.HasPrefix(emb["title"].(string), number) {
			note = emb
		}
	}
	if note == nil {
		t.Fatal("no request.held webhook")
	}
	fields := map[string]string{}
	for _, f := range note["fields"].([]any) {
		m := f.(map[string]any)
		fields[m["name"].(string)] = m["value"].(string)
	}
	if fields["Agent reported"] != "$125.01" || fields["Approved"] != "$100.00" {
		t.Errorf("webhook fields %v", fields)
	}
	// The director settles the held request at the real amount, which clears the hold.
	settled := e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilled", "actualTotalCents": 12501}, e.director())
	if settled.Status != 200 || settled.num("request", "actualTotalCents") != 12501 || settled.at("request", "settlementHold") != nil {
		t.Errorf("director settlement %d %s", settled.Status, settled.Raw)
	}
	if s := e.scalar(`SELECT status FROM fulfillment_jobs WHERE id = ?`, jobID); s != "done" {
		t.Errorf("job after settlement %v", s)
	}
	// At the ceiling, the agent may settle on its own.
	made2 := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer()))
	id2 := made2.str("request", "id")
	e.post("/api/requests/"+id2+"/approve", map[string]any{}, e.director())
	job2 := findJob(e.get("/api/agent/jobs", bearer(token)), id2)
	e.post("/api/agent/jobs/"+job2+"/claim", nil, bearer(token))
	expectStatus(t, "at the ceiling", e.post("/api/agent/jobs/"+job2+"/complete", map[string]any{"outcome": "done", "actualTotalCents": 12500}, bearer(token)), 200)
}

func TestCustomToleranceFromConfig(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.Tolerance = purchases.ToleranceFromEnv("5", "1000") })
	token := e.mustOK(e.post("/api/admin/tokens", map[string]any{"name": "tol", "scopes": []string{"agent:jobs"}}, e.director())).str("plaintext")
	made := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer()))
	id := made.str("request", "id")
	e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director())
	jobID := findJob(e.get("/api/agent/jobs", bearer(token)), id)
	e.post("/api/agent/jobs/"+jobID+"/claim", nil, bearer(token))
	// $100 estimate, 5% / $10 → ceiling $110.00.
	expectStatus(t, "over", e.post("/api/agent/jobs/"+jobID+"/complete", map[string]any{"outcome": "done", "actualTotalCents": 11001}, bearer(token)), 409)
}

func TestLetsADirectorCreateTheNextFiscalYearOfPeriodsAndCloseThenLockAPeriod(t *testing.T) {
	e := newEnv(t)
	expectStatus(t, "officer", e.post("/api/finance/periods", map[string]any{"fiscalYear": 2028}, e.officer()), 403)
	made := e.mustOK(e.post("/api/finance/periods", map[string]any{"fiscalYear": 2028}, e.director()))
	var fy28 []map[string]any
	for _, p := range made.list("periods") {
		if m := p.(map[string]any); m["fiscalYear"] == float64(2028) {
			fy28 = append(fy28, m)
		}
	}
	if len(fy28) != 12 || fy28[0]["name"] != "Jul 2027" || fy28[0]["startDate"] != "2027-07-01" || fy28[0]["endDate"] != "2027-07-31" || fy28[0]["status"] != "open" || fy28[11]["endDate"] != "2028-06-30" {
		t.Fatalf("FY2028 periods %v", fy28)
	}
	expectStatus(t, "create twice", e.post("/api/finance/periods", map[string]any{"fiscalYear": 2028}, e.director()), 409)
	expectStatus(t, "seeded year", e.post("/api/finance/periods", map[string]any{"fiscalYear": 2027}, e.director()), 409)
	// A period that hasn't ended (Pacific) can't be closed or locked.
	futureJuly := fy28[0]["id"].(string)
	notEnded := e.post("/api/finance/periods/"+futureJuly+"/close", map[string]any{"to": "closed"}, e.director())
	if notEnded.Status != 409 || !regexp.MustCompile(`hasn.t ended`).MatchString(notEnded.str("error", "message")) {
		t.Errorf("not ended: %d %s", notEnded.Status, notEnded.Raw)
	}
	expectStatus(t, "lock early", e.post("/api/finance/periods/"+futureJuly+"/close", map[string]any{"to": "locked"}, e.director()), 409)
	// A past year closes, then locks.
	past := e.mustOK(e.post("/api/finance/periods", map[string]any{"fiscalYear": 2026}, e.director()))
	var july string
	for _, p := range past.list("periods") {
		if m := p.(map[string]any); m["startDate"] == "2025-07-01" {
			july = m["id"].(string)
		}
	}
	status := func(r reply) string {
		for _, p := range r.list("periods") {
			if m := p.(map[string]any); m["id"] == july {
				return m["status"].(string)
			}
		}
		return ""
	}
	expectStatus(t, "officer closes", e.post("/api/finance/periods/"+july+"/close", map[string]any{"to": "closed"}, e.officer()), 403)
	if got := status(e.mustOK(e.post("/api/finance/periods/"+july+"/close", map[string]any{"to": "closed"}, e.director()))); got != "closed" {
		t.Errorf("after close: %s", got)
	}
	expectStatus(t, "close twice", e.post("/api/finance/periods/"+july+"/close", map[string]any{"to": "closed"}, e.director()), 409)
	if got := status(e.mustOK(e.post("/api/finance/periods/"+july+"/close", map[string]any{"to": "locked"}, e.director()))); got != "locked" {
		t.Errorf("after lock: %s", got)
	}
	expectStatus(t, "reopen", e.post("/api/finance/periods/"+july+"/close", map[string]any{"to": "closed"}, e.director()), 409)
	expectStatus(t, "unknown period", e.post("/api/finance/periods/nope/close", map[string]any{"to": "closed"}, e.director()), 404)
	if n := e.scalar(`SELECT count(*) FROM audit_log WHERE entity_type = 'fiscal_period'`); n.(int64) < 3 {
		t.Errorf("audit rows %v", n)
	}
}

func TestNumbersRequestsFYNNNNSequentiallyAndWithoutCollisionsUnderConcurrentCreates(t *testing.T) {
	e := newEnv(t)
	var wg sync.WaitGroup
	replies := make([]reply, 5)
	for i := range replies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			replies[i] = e.post("/api/requests", with(purchase, "submit", false), e.officer())
		}()
	}
	wg.Wait()
	var seq []int
	re := regexp.MustCompile(`^2027-(\d{4})$`)
	for _, r := range replies {
		if r.Status != 200 {
			t.Fatalf("create → %d %s", r.Status, r.Raw)
		}
		m := re.FindStringSubmatch(r.str("request", "number"))
		if m == nil {
			t.Fatalf("number %q", r.str("request", "number"))
		}
		n, _ := strconv.Atoi(m[1])
		seq = append(seq, n)
	}
	sort.Ints(seq)
	if seq[4]-seq[0] != 4 || seq[0] <= 6 {
		t.Errorf("sequence %v: want five consecutive numbers after the seeded 2027-0006", seq)
	}
	next := e.mustOK(e.post("/api/requests", with(purchase, "submit", false), e.officer()))
	if next.str("request", "number") != fmt.Sprintf("2027-%04d", seq[4]+1) {
		t.Errorf("next number %s after %v", next.str("request", "number"), seq)
	}
}

func TestRequestListFiltersCountsAndPages(t *testing.T) {
	e := newEnv(t)
	all := e.get("/api/requests?scope=all&limit=2", e.officer())
	if len(all.list("items")) != 2 || all.at("nextCursor") == nil {
		t.Fatalf("page 1 %s", all.Raw)
	}
	if all.num("counts", "reimbursed") != 1 || all.num("counts", "draft") != 1 || all.num("counts", "submitted") != 1 {
		t.Errorf("counts %v", all.at("counts"))
	}
	var seen []string
	cursor := ""
	for i := 0; i < 5; i++ {
		r := e.get("/api/requests?scope=all&limit=2"+cursor, e.officer())
		for _, it := range r.list("items") {
			seen = append(seen, it.(map[string]any)["number"].(string))
		}
		if r.at("nextCursor") == nil {
			break
		}
		cursor = "&cursor=" + strings.ReplaceAll(r.str("nextCursor"), "|", "%7C")
	}
	if len(seen) != 6 {
		t.Errorf("paged through %v", seen)
	}
	mine := e.get("/api/requests", e.as("user-marcus"))
	if len(mine.list("items")) != 2 {
		t.Errorf("marcus' requests %s", mine.Raw)
	}
	if p := e.get("/api/requests?scope=all&status=pending", e.officer()); len(p.list("items")) != 1 || p.num("counts", "approved") != 1 {
		t.Errorf("pending filter %s", p.Raw)
	}
	if q := e.get("/api/requests?scope=all&q=PIZZA", e.officer()); len(q.list("items")) != 1 {
		t.Errorf("search filter %s", q.Raw)
	}
	if q := e.get("/api/requests?scope=all&q=%25", e.officer()); len(q.list("items")) != 0 {
		t.Errorf("%% must be literal, got %s", q.Raw)
	}
	expectStatus(t, "bad cursor", e.get("/api/requests?cursor=zzz", e.officer()), 400)
	expectStatus(t, "bad limit", e.get("/api/requests?limit=0", e.officer()), 400)
	expectStatus(t, "bad status", e.get("/api/requests?status=bogus", e.officer()), 400)
}

func TestEventsListDetailMetricsCRUD(t *testing.T) {
	e := newEnv(t)
	list := e.get("/api/events", e.officer())
	if len(list.list("items")) != 6 || list.num("items", 0, "metrics", "registered") == 0 {
		t.Fatalf("events %.300s", list.Raw)
	}
	if first := list.str("items", 0, "name"); first != "Hack Night" { // newest start first
		t.Errorf("first event %s", first)
	}
	upcoming := e.get("/api/events?when=upcoming", e.officer())
	past := e.get("/api/events?when=past", e.officer())
	if len(upcoming.list("items")) != 1 || len(past.list("items")) != 5 {
		t.Errorf("upcoming %d past %d", len(upcoming.list("items")), len(past.list("items")))
	}
	if q := e.get("/api/events?q=resume", e.officer()); len(q.list("items")) != 1 {
		t.Errorf("name search %s", q.Raw)
	}
	if r := e.get("/api/events?from=2026-10-01&to=2026-10-10", e.officer()); len(r.list("items")) != 2 {
		t.Errorf("date range %s", r.Raw)
	}
	detail := e.mustOK(e.get("/api/events/event-buildspace-demo", e.officer()))
	if detail.str("event", "name") != "Buildspace Demo Night" || len(detail.list("requests")) != 2 || detail.num("event", "metrics", "registered") != 92 {
		t.Errorf("detail %.400s", detail.Raw)
	}
	metrics := e.mustOK(e.get("/api/events/event-kickoff/metrics", e.officer()))
	regs := metrics.list("registrationsByDay")
	if len(regs) == 0 || metrics.num("registrationsByDay", len(regs)-1, "cumulative") != metrics.num("totals", "registered") {
		t.Errorf("registration chart ends at %v, registered %d", regs[len(regs)-1], metrics.num("totals", "registered"))
	}
	if metrics.num("totals", "checkedIn") != 50 || metrics.num("totals", "reflections") != 21 || metrics.num("totals", "matchedReflections") != 21 {
		t.Errorf("kickoff totals %v", metrics.at("totals"))
	}
	expectStatus(t, "unknown event", e.get("/api/events/nope", e.officer()), 404)
	created := e.mustOK(e.post("/api/events", map[string]any{"name": " Board social ", "startsAt": "2026-12-01T19:00:00-08:00", "hostUserId": "user-ava"}, e.officer()))
	if created.str("event", "name") != "Board social" || created.str("event", "host", "name") != "Ava Romero" {
		t.Errorf("created %s", created.Raw)
	}
	id := created.str("event", "id")
	expectStatus(t, "ends before start", e.do("PATCH", "/api/events/"+id, map[string]any{"endsAt": "2026-12-01T18:00:00-08:00"}, e.officer()), 400)
	expectStatus(t, "unknown host", e.post("/api/events", map[string]any{"name": "x", "startsAt": "2026-12-01T19:00:00Z", "hostUserId": "nope"}, e.officer()), 400)
	upd := e.mustOK(e.do("PATCH", "/api/events/"+id, map[string]any{"location": "Room 5", "hostUserId": nil}, e.officer()))
	if upd.str("event", "location") != "Room 5" || upd.at("event", "host") != nil || upd.str("event", "name") != "Board social" {
		t.Errorf("patched %s", upd.Raw)
	}
	expectStatus(t, "duplicate Luma id", e.post("/api/events", map[string]any{"name": "x", "startsAt": "2026-12-01T19:00:00Z", "lumaId": "evt-kickoff26"}, e.officer()), 409)
	expectStatus(t, "bad date", e.post("/api/events", map[string]any{"name": "x", "startsAt": "tomorrow"}, e.officer()), 400)
}

func TestCSVGuestImportCreatesAnEventAndReportsRowErrors(t *testing.T) {
	e := newEnv(t)
	csv := "Name,Email,Approval Status,Checked In At\nAnn,ann@example.org,approved,2026-11-05T18:10:00Z\nBob,bad-email,approved,\nCy,cy@example.org,invited,\nAnn,ANN@example.org,approved,2026-11-05T18:12:00Z\n"
	r := e.do("POST", "/api/events/import", formBody{
		fields: map[string]string{"kind": "luma_guests", "lumaEventId": "csv-new", "eventName": "CSV Night", "eventStartsAt": "2026-11-05T18:00:00-08:00"},
		files:  []formFile{{"file", "guests.csv", "text/csv", csv}},
	}, e.officer())
	if r.Status != 200 || r.num("rows") != 4 || r.num("created") != 2 || r.num("skipped") != 2 || len(r.list("errors")) != 1 || r.num("errors", 0, "row") != 3 {
		t.Fatalf("import %d %s", r.Status, r.Raw)
	}
	if r.num("metrics", "registered") != 1 || r.num("metrics", "checkedIn") != 1 {
		t.Errorf("metrics %v", r.at("metrics"))
	}
	if r.str("kind") != "luma_guests" || r.at("dryRun") != false {
		t.Errorf("kind/dryRun %s", r.Raw)
	}
	missing := e.do("POST", "/api/events/import", formBody{fields: map[string]string{"kind": "luma_guests"}, files: []formFile{{"file", "g.csv", "text/csv", csv}}}, e.officer())
	expectStatus(t, "no event given", missing, 400)
	expectStatus(t, "no file", e.do("POST", "/api/events/import", formBody{fields: map[string]string{"kind": "luma_guests", "eventId": "event-kickoff"}}, e.officer()), 400)
	expectStatus(t, "unknown event", e.do("POST", "/api/events/import", formBody{fields: map[string]string{"kind": "luma_guests", "eventId": "nope"}, files: []formFile{{"file", "g.csv", "text/csv", csv}}}, e.officer()), 404)
	bad := e.do("POST", "/api/events/import", formBody{fields: map[string]string{"kind": "reflections", "eventId": "event-kickoff"}, files: []formFile{{"file", "g.csv", "text/csv", "Name\nAnn"}}}, e.officer())
	if bad.Status != 422 || bad.at("error", "code") != "import_failed" {
		t.Errorf("headerless CSV → %d %s", bad.Status, bad.Raw)
	}
}

func TestAdminSurfaceBudgetsLookupsAuditAndNotifications(t *testing.T) {
	e := newEnv(t)
	lookups := e.mustOK(e.get("/api/lookups", e.officer()))
	if len(lookups.list("expenseAccounts")) != 6 || len(lookups.list("funds")) != 3 || lookups.num("currentFiscalYear") != 2027 || len(lookups.list("events")) != 6 {
		t.Errorf("lookups %.300s", lookups.Raw)
	}
	budgets := e.mustOK(e.get("/api/finance/budgets?fiscalYear=2027", e.officer()))
	if len(budgets.list("lines")) != 10 {
		t.Errorf("budget lines %d", len(budgets.list("lines")))
	}
	expectStatus(t, "officer replaces budgets", e.do("PUT", "/api/finance/budgets", map[string]any{"fiscalYear": 2027, "lines": []any{}}, e.officer()), 403)
	expectStatus(t, "overlapping lines", e.do("PUT", "/api/finance/budgets", map[string]any{"fiscalYear": 2027, "lines": []any{
		map[string]any{"accountId": "acct-5100", "fundId": nil, "amountCents": 100},
		map[string]any{"accountId": "acct-5100", "fundId": "fund-general", "amountCents": 100},
	}}, e.director()), 400)
	expectStatus(t, "non-expense account", e.do("PUT", "/api/finance/budgets", map[string]any{"fiscalYear": 2027, "lines": []any{map[string]any{"accountId": "acct-1000", "fundId": nil, "amountCents": 1}}}, e.director()), 400)
	put := e.mustOK(e.do("PUT", "/api/finance/budgets", map[string]any{"fiscalYear": 2027, "lines": []any{
		map[string]any{"accountId": "acct-5100", "fundId": nil, "amountCents": 12345, "notes": "all funds"},
	}}, e.director()))
	if len(put.list("lines")) != 1 || put.num("lines", 0, "amountCents") != 12345 {
		t.Errorf("replaced budgets %s", put.Raw)
	}
	audit := e.mustOK(e.get("/api/admin/audit?entityType=budget", e.director()))
	if len(audit.list("items")) != 1 || audit.str("items", 0, "action") != "budget.replaced" || audit.str("items", 0, "actor", "name") != "Jordan Lee" {
		t.Errorf("audit %s", audit.Raw)
	}
	// A submit notifies Discord with the amount and requester.
	made := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer()))
	var found bool
	for _, w := range e.webhooks.all() {
		emb := w["embeds"].([]any)[0].(map[string]any)
		if emb["description"] == "request.submitted" && strings.HasPrefix(emb["title"].(string), made.str("request", "number")) && strings.HasSuffix(emb["url"].(string), "/requests/"+made.str("request", "id")) {
			found = true
		}
	}
	if !found {
		t.Errorf("no submit webhook among %v", e.webhooks.all())
	}
}

func TestTrailingSlashAndWrongMethod(t *testing.T) {
	e := newEnv(t)
	expectStatus(t, "trailing slash", e.get("/api/me/", e.officer()), 200)
	expectStatus(t, "wrong method", e.do("DELETE", "/api/me", nil, e.officer()), 404)
}
