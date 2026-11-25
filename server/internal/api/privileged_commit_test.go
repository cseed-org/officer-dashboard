package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

// Snapshot every affected table, including tokens, portal sessions and success audits.
func privilegedSnapshot(t *testing.T, e *testEnv) string {
	t.Helper()
	names, err := e.db.W.Query(`SELECT name FROM sqlite_master WHERE type='table' AND (name LIKE 'buildspace_launchpad_%' OR name IN ('budgets','fiscal_periods','audit_log')) ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for names.Next() {
		var name string
		if err := names.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := names.Err(); err != nil {
		t.Fatal(err)
	}
	names.Close()
	data := map[string][][]any{}
	for _, table := range tables {
		rows, err := e.db.W.Query(`SELECT * FROM "` + table + `" ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(cols))
			dest := make([]any, len(cols))
			for i := range values {
				dest[i] = &values[i]
			}
			if err := rows.Scan(dest...); err != nil {
				t.Fatal(err)
			}
			data[table] = append(data[table], values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestP2PrivilegedMutationsReloadAuthorityAtCommit(t *testing.T) {
	const lp = "/api/buildspace-launchpad"
	for _, change := range []string{"demote", "deactivate"} {
		for _, name := range []string{
			"budgets", "periods_create", "period_close", "cohort_create", "cohort_update",
			"cohort_delete", "mentor_invite", "builder_disable", "builder_enable",
			"mentor_resend", "mentor_revoke", "project_delete", "person_delete",
		} {
			t.Run(change+"/"+name, func(t *testing.T) {
				mailer := &fakeMailer{}
				e := newEnv(t, func(c *Config) { c.Mailer = mailer })
				method, path := http.MethodPost, ""
				var body any
				switch name {
				case "budgets":
					method, path = http.MethodPut, "/api/finance/budgets"
					body = map[string]any{"fiscalYear": 2030, "lines": []any{}}
				case "periods_create":
					path, body = "/api/finance/periods", map[string]any{"fiscalYear": 2030}
				case "period_close":
					e.exec(`INSERT INTO fiscal_periods(id,fiscal_year,name,start_date,end_date,status) VALUES('p2-old',2020,'Old','2020-01-01','2020-01-31','open')`)
					path, body = "/api/finance/periods/p2-old/close", map[string]any{"to": "closed"}
				case "cohort_create":
					path, body = lp+"/cohorts", map[string]any{"name": "P2 cohort", "startsOn": "2030-01-01"}
				case "cohort_update":
					method, path, body = http.MethodPatch, lp+"/cohorts/cohort-fall-2026", map[string]any{"name": "Stale edit"}
				case "cohort_delete":
					id := e.mustOK(e.post(lp+"/cohorts", map[string]any{"name": "P2 empty", "startsOn": "2030-01-01"}, e.director())).str("cohort", "id")
					method, path = http.MethodDelete, lp+"/cohorts/"+id
				case "mentor_invite":
					path, body = lp+"/mentors/bs-mentor-jordan/invite", map[string]any{"method": "email"}
				case "builder_disable":
					e.portalLogin("lp-account-alex")
					path = lp + "/accounts/lp-account-alex/disable"
				case "builder_enable":
					e.exec(`UPDATE buildspace_launchpad_accounts SET status='disabled' WHERE id='lp-account-alex'`)
					path = lp + "/accounts/lp-account-alex/enable"
				case "mentor_resend", "mentor_revoke":
					r := e.mustOK(e.post(lp+"/mentors/bs-mentor-jordan/invite", map[string]any{"method": "link"}, e.director()))
					path = lp + "/accounts/" + r.str("account", "id")
					if name == "mentor_resend" {
						path += "/resend"
					} else {
						method = http.MethodDelete
					}
				case "project_delete":
					e.mustOK(e.do(http.MethodPut, lp+"/projects/p2-empty", map[string]any{"name": "Empty", "description": "", "track": "Software", "stage": "Ideating", "links": []string{}, "mentorIds": []string{}, "cohortId": "cohort-fall-2026"}, e.officer()))
					method, path = http.MethodDelete, lp+"/projects/p2-empty"
				case "person_delete":
					// Alex has an account/session but no person-linked activity.
					e.exec(`DELETE FROM buildspace_launchpad_activity WHERE person_id='bs-person-01'`)
					e.portalLogin("lp-account-alex")
					method, path = http.MethodDelete, lp+"/people/bs-person-01"
				}
				e.srv.WaitBackground()
				before, mails := privilegedSnapshot(t, e), mailer.all()
				revoke := func() {
					if change == "demote" {
						demoteDirector(e)
					} else {
						e.exec(`UPDATE users SET active=0 WHERE id='user-director'`)
					}
				}
				var status int
				if body != nil {
					status = pausedMutation(t, e, method, path, body, e.director(), revoke)
				} else {
					status = pausedDirectorRequest(t, e, method, path, nil, revoke).Status
				}
				e.srv.WaitBackground()
				if status != http.StatusForbidden {
					t.Errorf("stale privileged mutation returned %d, want 403", status)
				}
				if after := privilegedSnapshot(t, e); after != before {
					t.Error("refused mutation changed domain data, credentials, sessions or success audits")
				}
				if !reflect.DeepEqual(mailer.all(), mails) {
					t.Error("refused mutation delivered invite email")
				}
			})
		}
	}
}

func TestP2PrivilegedConditionalAccountsRetainOfficerPolicy(t *testing.T) {
	for _, action := range []string{"resend", "revoke"} {
		t.Run(action, func(t *testing.T) {
			e := newEnv(t)
			method, path := http.MethodPost, "/api/buildspace-launchpad/accounts/lp-invite-sky/resend"
			if action == "revoke" {
				method, path = http.MethodDelete, "/api/buildspace-launchpad/accounts/lp-invite-sky"
			}
			// A demotion to officer still allows these builder-account operations,
			// unlike identical operations targeting mentor accounts.
			r := pausedDirectorRequest(t, e, method, path, nil, func() { demoteDirector(e) })
			expectStatus(t, "builder operation remains officer-accessible", r, http.StatusOK)
			if action == "resend" {
				if r.str("link", "url") == "" {
					t.Fatal("allowed builder resend did not return its invite link")
				}
			} else if e.scalar(`SELECT count(*) FROM buildspace_launchpad_accounts WHERE id='lp-invite-sky'`) != int64(0) {
				t.Fatal("allowed builder revoke did not delete its account")
			}
		})
	}
}
