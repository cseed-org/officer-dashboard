package api

import (
	"testing"
)

func kinds(r reply) map[string]int {
	out := map[string]int{}
	for _, it := range r.list("results") {
		out[it.(map[string]any)["kind"].(string)]++
	}
	return out
}

func TestSearchFindsEventsAndRequestsForOfficers(t *testing.T) {
	e := newEnv(t)
	r := e.mustOK(e.get("/api/search?q=demo", e.officer()))
	k := kinds(r)
	if k["event"] != 1 || k["request"] != 1 || k["user"] != 0 || k["account"] != 0 {
		t.Fatalf("kinds %v in %s", k, r.Raw)
	}
	var ev, rq map[string]any
	for _, it := range r.list("results") {
		m := it.(map[string]any)
		if m["kind"] == "event" {
			ev = m
		} else {
			rq = m
		}
	}
	if ev["id"] != "event-buildspace-demo" || ev["title"] != "Buildspace Demo Night" || ev["href"] != "/events?id=event-buildspace-demo" || ev["subtitle"] != "2026-10-09 · Engineering Atrium" {
		t.Errorf("event hit %v", ev)
	}
	if rq["id"] != "req-0005" || rq["title"] != "Demo night banner printing" || rq["href"] != "/finance?id=req-0005" || rq["subtitle"] != "2027-0005 · FedEx Office · submitted" {
		t.Errorf("request hit %v", rq)
	}
	for _, key := range []string{"kind", "id", "title", "subtitle", "href"} {
		if _, ok := ev[key].(string); !ok {
			t.Errorf("event hit lacks string %q: %v", key, ev)
		}
	}
}

func TestSearchMatchesNumbersVendorsAndIgnoresCase(t *testing.T) {
	e := newEnv(t)
	if r := e.mustOK(e.get("/api/search?q=2027-0003", e.officer())); kinds(r)["request"] != 1 || r.str("results", 0, "id") != "req-0003" {
		t.Errorf("by number: %s", r.Raw)
	}
	if r := e.mustOK(e.get("/api/search?q=ADAFRUIT", e.officer())); r.str("results", 0, "id") != "req-0002" {
		t.Errorf("by vendor: %s", r.Raw)
	}
	if r := e.mustOK(e.get("/api/search?q=%20pizza%20", e.officer())); r.str("results", 0, "id") != "req-0001" {
		t.Errorf("trimmed query: %s", r.Raw)
	}
	if r := e.mustOK(e.get("/api/search?q=zzzz-nothing", e.officer())); len(r.list("results")) != 0 || r.at("results") == nil {
		t.Errorf("no match must be an empty array: %s", r.Raw)
	}
	// LIKE wildcards in the query are literal.
	if r := e.mustOK(e.get("/api/search?q=%25", e.officer())); len(r.list("results")) != 0 {
		t.Errorf("%% matched everything: %s", r.Raw)
	}
	if r := e.mustOK(e.get("/api/search?q=_", e.officer())); len(r.list("results")) != 0 {
		t.Errorf("_ matched everything: %s", r.Raw)
	}
}

func TestSearchShowsUsersAndAccountsOnlyToDirectors(t *testing.T) {
	e := newEnv(t)
	if r := e.mustOK(e.get("/api/search?q=priya", e.officer())); kinds(r)["user"] != 0 {
		t.Errorf("officer saw users: %s", r.Raw)
	}
	r := e.mustOK(e.get("/api/search?q=priya", e.director()))
	if kinds(r)["user"] != 1 {
		t.Fatalf("director should see Priya: %s", r.Raw)
	}
	var user map[string]any
	for _, it := range r.list("results") {
		if m := it.(map[string]any); m["kind"] == "user" {
			user = m
		}
	}
	if user["title"] != "Priya Shah" || user["subtitle"] != "VP Events · officer · priya@example.org" || user["href"] != "/admin" {
		t.Errorf("user hit %v", user)
	}
	// By email, case-insensitively.
	if r := e.mustOK(e.get("/api/search?q=MARCUS@EXAMPLE", e.director())); r.str("results", 0, "title") != "Marcus Chen" {
		t.Errorf("by email: %s", r.Raw)
	}
	acc := e.mustOK(e.get("/api/search?q=checking", e.director()))
	if acc.str("results", 0, "kind") != "account" || acc.str("results", 0, "title") != "1000 Checking" || acc.str("results", 0, "subtitle") != "asset" || acc.str("results", 0, "href") != "/finance?tab=reports&report=trial-balance" {
		t.Errorf("account hit: %s", acc.Raw)
	}
	if r := e.mustOK(e.get("/api/search?q=1000", e.director())); r.str("results", 0, "id") != "acct-1000" {
		t.Errorf("by account code: %s", r.Raw)
	}
	if r := e.mustOK(e.get("/api/search?q=checking", e.officer())); len(r.list("results")) != 0 {
		t.Errorf("officer saw accounts: %s", r.Raw)
	}
}

func TestSearchRanksPrefixMatchesFirstAndHonoursLimit(t *testing.T) {
	e := newEnv(t)
	// "hack": the event "Hack Night" starts with it; the request "Hack Night prizes" starts with it too,
	// while a request merely mentioning it in the vendor would rank lower.
	r := e.mustOK(e.get("/api/search?q=hack", e.officer()))
	if len(r.list("results")) < 2 || r.str("results", 0, "kind") != "event" || r.str("results", 0, "title") != "Hack Night" {
		t.Errorf("ranking: %s", r.Raw)
	}
	if r := e.mustOK(e.get("/api/search?q=a&limit=3", e.director())); len(r.list("results")) != 3 {
		t.Errorf("limit 3 returned %d", len(r.list("results")))
	}
	if r := e.mustOK(e.get("/api/search?q=a", e.director())); len(r.list("results")) > 20 {
		t.Errorf("default limit is 20, got %d", len(r.list("results")))
	}
}

func TestSearchValidatesAndRequiresSignIn(t *testing.T) {
	e := newEnv(t)
	expectStatus(t, "anonymous", e.get("/api/search?q=demo", anon), 401)
	expectStatus(t, "missing q", e.get("/api/search", e.officer()), 400)
	expectStatus(t, "blank q", e.get("/api/search?q=%20%20", e.officer()), 400)
	expectStatus(t, "limit 0", e.get("/api/search?q=a&limit=0", e.officer()), 400)
	expectStatus(t, "limit 51", e.get("/api/search?q=a&limit=51", e.officer()), 400)
	expectStatus(t, "limit 50", e.get("/api/search?q=a&limit=50", e.officer()), 200)
}
