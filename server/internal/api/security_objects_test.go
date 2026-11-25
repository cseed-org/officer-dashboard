package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestP2EventPurchaseDraftPrivacyHTTPAndMCP(t *testing.T) {
	e := newEnv(t)
	event := e.mustOK(e.post("/api/events", map[string]any{"name": "Draft boundary", "startsAt": "2026-11-07T18:00:00-08:00"}, e.officer())).str("event", "id")
	a := e.mustOK(e.post("/api/requests", with(purchase, "eventId", event, "title", "private-draft-alpha"), e.officer())).str("request", "id")
	b := e.mustOK(e.post("/api/requests", with(purchase, "eventId", event, "title", "private-draft-beta"), e.as("user-marcus"))).str("request", "id")
	public := e.mustOK(e.post("/api/requests", with(purchase, "eventId", event, "submit", true), e.officer())).str("request", "id")
	for _, user := range []string{"user-priya", "user-marcus", "user-director"} {
		t.Run(user, func(t *testing.T) {
			httpReply := e.mustOK(e.get("/api/events/"+event, e.as(user)))
			_, tokens := connectAgent(t, e, user, "Event privacy regression")
			text, isErr := toolCall(e, tokens.str("access_token"), "get_event", map[string]any{"id": event})
			if isErr {
				t.Fatal("event tool failed")
			}
			for name, raw := range map[string]string{"HTTP": string(httpReply.Raw), "MCP": text} {
				for id, want := range map[string]bool{a: user != "user-marcus", b: user != "user-priya", public: true} {
					if strings.Contains(raw, id) != want {
						t.Errorf("%s exposed or omitted a purchase against draft policy", name)
					}
				}
			}
		})
	}
}

func TestP2PurchaseLifecycleReloadsAuthority(t *testing.T) {
	for _, action := range []string{"approve", "deny", "transition"} {
		for _, change := range []string{"demote", "deactivate"} {
			t.Run(action+"/"+change, func(t *testing.T) {
				e := newEnv(t)
				id := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer())).str("request", "id")
				before := "submitted"
				if action == "transition" {
					e.mustOK(e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director()))
					before = "approved"
				}
				approvals := e.scalar(`SELECT count(*) FROM approvals WHERE request_id=?`, id)
				jobs := e.scalar(`SELECT count(*) FROM fulfillment_jobs WHERE request_id=?`, id)
				audits := e.scalar(`SELECT count(*) FROM audit_log WHERE entity_id=?`, id)
				code := pausedMutation(t, e, http.MethodPost, "/api/requests/"+id+"/"+action,
					map[string]any{"comment": "Declined", "to": "fulfilling"}, e.director(), func() {
						if change == "demote" {
							demoteDirector(e)
						} else {
							e.exec(`UPDATE users SET active=0 WHERE id='user-director'`)
						}
					})
				if code != http.StatusForbidden {
					t.Errorf("revoked financial authority completed a mutation: %d", code)
				}
				if e.scalar(`SELECT status FROM purchase_requests WHERE id=?`, id) != before ||
					e.scalar(`SELECT count(*) FROM approvals WHERE request_id=?`, id) != approvals ||
					e.scalar(`SELECT count(*) FROM fulfillment_jobs WHERE request_id=?`, id) != jobs ||
					e.scalar(`SELECT count(*) FROM audit_log WHERE entity_id=?`, id) != audits {
					t.Fatal("revoked financial mutation left side effects")
				}
			})
		}
	}
}

func TestP2SettlementUsesLiveLeadershipRole(t *testing.T) {
	e := newEnv(t)
	id := e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer())).str("request", "id")
	e.mustOK(e.post("/api/requests/"+id+"/approve", map[string]any{}, e.director()))
	e.mustOK(e.post("/api/requests/"+id+"/transition", map[string]any{"to": "fulfilling"}, e.director()))
	// Retain another leadership account so the last-leadership invariant is not involved.
	e.exec(`UPDATE users SET role='leadership', is_owner=1 WHERE id='user-director'`)
	code := pausedMutation(t, e, http.MethodPost, "/api/requests/"+id+"/transition",
		map[string]any{"to": "fulfilled", "actualTotalCents": 12501}, e.as("user-leadership"), func() {
			e.exec(`UPDATE users SET role='director', is_owner=0 WHERE id='user-leadership'`)
		})
	if code != http.StatusConflict || e.scalar(`SELECT status FROM purchase_requests WHERE id=?`, id) != "fulfilling" {
		t.Fatalf("stale leadership authority bypassed settlement tolerance: %d", code)
	}
}

func TestP2OrgCalendarFailureLogsOmitRosterAddresses(t *testing.T) {
	e, fake := orgGoogleEnv(t)
	var out bytes.Buffer
	e.srv.log = slog.New(slog.NewTextHandler(&out, nil))
	fake.failACLInsert = true
	e.mustOK(e.post("/api/orgcal/events", map[string]any{
		"calendarKey": "internal", "title": "Privacy regression",
		"startsAt": "2026-11-24T18:00:00Z", "endsAt": "2026-11-24T19:00:00Z",
	}, e.officer()))
	e.srv.WaitBackground()
	if !strings.Contains(out.String(), "org calendar ACL entry failed") {
		t.Fatal("ACL failure was not exercised")
	}
	if strings.Contains(out.String(), "@") {
		t.Fatal("operational logs contain a roster address")
	}
}
