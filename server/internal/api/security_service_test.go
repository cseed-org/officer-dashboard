package api

import (
	"net/http"
	"testing"
)

func TestP2ServiceMutationsRevalidateBearerAtCommit(t *testing.T) {
	for _, target := range []string{"calendar", "purchase", "luma", "buildspace"} {
		for _, change := range []string{"revoke", "expire", "deactivate", "demote", "scope"} {
			t.Run(target+"/"+change, func(t *testing.T) {
				e := newEnv(t)
				issued := e.mustOK(e.post("/api/admin/tokens", map[string]any{
					"name": "Commit boundary", "scopes": []string{"agent:jobs", "calendar:write", "ingest:luma", "ingest:buildspace"},
				}, e.director()))
				token, tokenID := issued.str("plaintext"), issued.str("token", "id")
				path := "/api/orgcal/events"
				body := map[string]any{"calendarKey": "internal", "title": "Not committed", "startsAt": "2026-11-24T18:00:00Z", "endsAt": "2026-11-24T19:00:00Z"}
				requestID := ""
				if target == "purchase" {
					requestID = e.mustOK(e.post("/api/requests", with(purchase, "submit", true), e.officer())).str("request", "id")
					e.mustOK(e.post("/api/requests/"+requestID+"/approve", map[string]any{}, e.director()))
					job := e.scalar(`SELECT id FROM fulfillment_jobs WHERE request_id=?`, requestID).(string)
					e.mustOK(e.post("/api/agent/jobs/"+job+"/claim", nil, bearer(token)))
					path, body = "/api/agent/jobs/"+job+"/complete", map[string]any{"outcome": "done", "actualTotalCents": 9500}
				} else if target == "buildspace" {
					path, body = "/api/ingest/buildspace-launchpad", map[string]any{"discordId": "123456789012345699", "externalId": "revocation-fixture", "kind": "weekly", "date": "2026-11-10", "body": "Not committed", "status": "on-track"}
				} else if target == "luma" {
					path, body = "/api/ingest/luma", map[string]any{
						"event":  map[string]any{"lumaId": "revocation-fixture", "name": "Not committed", "startsAt": "2026-11-24T18:00:00Z"},
						"guests": []any{},
					}
				}
				audits := e.scalar(`SELECT count(*) FROM audit_log`)
				code := pausedMutation(t, e, http.MethodPost, path, body, bearer(token), func() {
					switch change {
					case "revoke":
						e.exec(`UPDATE service_tokens SET revoked_at=? WHERE id=?`, e.clock.UnixMilli(), tokenID)
					case "expire":
						e.exec(`UPDATE service_tokens SET expires_at=? WHERE id=?`, e.clock.UnixMilli(), tokenID)
					case "deactivate":
						e.exec(`UPDATE users SET active=0 WHERE id='user-director'`)
					case "demote":
						demoteDirector(e)
					case "scope":
						e.exec(`UPDATE service_tokens SET scopes='[]' WHERE id=?`, tokenID)
					}
				})
				want := http.StatusUnauthorized
				if change == "scope" {
					want = http.StatusForbidden
				}
				if code != want {
					t.Errorf("stale service authority completed %s: %d, want %d", target, code, want)
				}
				if e.scalar(`SELECT count(*) FROM audit_log`) != audits {
					t.Fatal("service mutation committed an audit after revocation")
				}
				if requestID != "" && e.scalar(`SELECT status FROM purchase_requests WHERE id=?`, requestID) != "fulfilling" {
					t.Fatal("revoked token settled a purchase")
				}
			})
		}
	}
}
