package api

import "net/http"

func (s *Server) routes(mux *http.ServeMux) {
	h := func(pattern string, fn handlerFunc) { mux.HandleFunc(pattern, s.wrap(fn)) }

	h("GET /api/buildspace-launchpad", (*Ctx).buildspaceList)
	h("PUT /api/buildspace-launchpad/projects/{id}", (*Ctx).buildspaceProjectSave)
	h("PUT /api/buildspace-launchpad/people/{id}", (*Ctx).buildspacePersonSave)
	h("POST /api/buildspace-launchpad/activity", (*Ctx).buildspaceActivityCreate)
	h("POST /api/buildspace-launchpad/activity/{id}/resolve", (*Ctx).buildspaceActivityResolve)
	h("POST /api/ingest/buildspace-launchpad", (*Ctx).buildspaceIngest)
	h("PUT /api/buildspace-launchpad/schedule", (*Ctx).buildspaceSchedulePut)

	// Legacy API aliases preserve request bodies and existing integrations.
	h("GET /api/buildspace", (*Ctx).buildspaceList)
	h("PUT /api/buildspace/projects/{id}", (*Ctx).buildspaceProjectSave)
	h("PUT /api/buildspace/people/{id}", (*Ctx).buildspacePersonSave)
	h("POST /api/buildspace/activity", (*Ctx).buildspaceActivityCreate)
	h("POST /api/buildspace/activity/{id}/resolve", (*Ctx).buildspaceActivityResolve)
	h("POST /api/ingest/buildspace", (*Ctx).buildspaceIngest)
	h("PUT /api/buildspace/schedule", (*Ctx).buildspaceSchedulePut)

	h("GET /api/members", (*Ctx).membersList)
	h("GET /api/members/export", (*Ctx).membersExport)
	h("GET /api/members/{id}", (*Ctx).memberDetail)
	h("PATCH /api/members/{id}", (*Ctx).memberUpdate)
	h("POST /api/cloudflare/sync", (*Ctx).cloudflareSync)
	h("GET /api/cloudflare/widgets", (*Ctx).cloudflareWidgets)
	h("GET /api/health", (*Ctx).health)
	h("GET /api/me", (*Ctx).me)
	h("GET /api/me/stats", (*Ctx).meStats)
	h("GET /api/club/settings", (*Ctx).clubSettingsGet)
	h("PUT /api/club/settings", (*Ctx).clubSettingsPut)
	h("GET /api/calendar/feed", (*Ctx).calendarFeed)
	h("GET /api/me/calendar-widget", (*Ctx).calendarWidgetGet)
	h("PUT /api/me/calendar-widget", (*Ctx).calendarWidgetPut)
	h("GET /api/me/prefs", (*Ctx).prefsGet)
	h("PUT /api/me/prefs", (*Ctx).prefsPut)
	h("GET /api/me/calendar-emails", (*Ctx).calendarEmailsList)
	h("POST /api/me/calendar-emails", (*Ctx).calendarEmailCreate)
	h("PATCH /api/me/calendar-emails/{id}", (*Ctx).calendarEmailUpdate)
	h("DELETE /api/me/calendar-emails/{id}", (*Ctx).calendarEmailDelete)
	h("GET /api/lookups", (*Ctx).lookups)
	h("GET /api/overview", (*Ctx).overview)
	h("GET /api/search", (*Ctx).search)

	h("GET /api/contacts", (*Ctx).contactsList)
	h("POST /api/contacts", (*Ctx).contactCreate)
	h("GET /api/contacts/{id}", (*Ctx).contactDetail)
	h("PATCH /api/contacts/{id}", (*Ctx).contactUpdate)
	h("DELETE /api/contacts/{id}", (*Ctx).contactDelete)
	h("POST /api/contacts/{id}/outreach", (*Ctx).contactOutreachCreate)

	h("GET /api/events", (*Ctx).eventsList)
	h("POST /api/events", (*Ctx).eventCreate)
	h("POST /api/events/import", (*Ctx).eventsImport)
	h("GET /api/events/{id}", (*Ctx).eventDetail)
	h("PATCH /api/events/{id}", (*Ctx).eventUpdate)
	h("DELETE /api/events/{id}", (*Ctx).eventDelete)
	h("GET /api/events/{id}/cover", (*Ctx).eventCoverGet)
	h("PUT /api/events/{id}/cover", (*Ctx).eventCoverPut)
	h("DELETE /api/events/{id}/cover", (*Ctx).eventCoverDelete)
	h("GET /api/events/{id}/metrics", (*Ctx).eventMetrics)
	h("POST /api/ingest/luma", (*Ctx).ingestLuma)

	h("GET /api/requests", (*Ctx).requestsList)
	h("POST /api/requests", (*Ctx).requestCreate)
	h("GET /api/requests/{id}", (*Ctx).requestDetail)
	h("PATCH /api/requests/{id}", (*Ctx).requestUpdate)
	h("DELETE /api/requests/{id}", (*Ctx).requestDelete)
	h("POST /api/requests/{id}/submit", func(c *Ctx) error { return c.lifecycle("submit") })
	h("POST /api/requests/{id}/approve", func(c *Ctx) error { return c.lifecycle("approve") })
	h("POST /api/requests/{id}/deny", func(c *Ctx) error { return c.lifecycle("deny") })
	h("POST /api/requests/{id}/transition", func(c *Ctx) error { return c.lifecycle("transition") })
	h("POST /api/requests/{id}/attachments", (*Ctx).requestAttach)
	h("GET /api/attachments/{id}", (*Ctx).attachmentDownload)

	h("GET /api/design-requests", (*Ctx).designList)
	h("POST /api/design-requests", (*Ctx).designCreate)
	h("GET /api/design-requests/{id}", (*Ctx).designDetail)
	h("PATCH /api/design-requests/{id}", (*Ctx).designUpdate)
	h("DELETE /api/design-requests/{id}", (*Ctx).designDelete)
	h("POST /api/design-requests/{id}/assign", (*Ctx).designAssign)
	h("POST /api/design-requests/{id}/move", (*Ctx).designMove)
	h("POST /api/design-requests/{id}/approve", (*Ctx).designApprove)
	h("POST /api/design-requests/{id}/comments", (*Ctx).designComment)
	h("POST /api/design-requests/{id}/files", (*Ctx).designUpload)
	h("GET /api/design-files/{id}", (*Ctx).designFileDownload)
	h("GET /api/events/{id}/design-request-draft", (*Ctx).designDraftFromEvent)
	h("POST /api/events/{id}/design-requests", (*Ctx).designCreateFromEvent)
	h("PUT /api/events/{id}/design-need", (*Ctx).eventDesignNeed)

	h("GET /api/finance/summary", func(c *Ctx) error { return c.financeSummary() })
	h("GET /api/finance/reports/{report}", func(c *Ctx) error { return c.financeReport() })
	h("GET /api/finance/budgets", (*Ctx).budgetsGet)
	h("PUT /api/finance/budgets", (*Ctx).budgetsPut)
	h("GET /api/finance/periods", (*Ctx).periods)
	h("POST /api/finance/periods", (*Ctx).periodsCreate)
	h("POST /api/finance/periods/{id}/close", (*Ctx).periodClose)
	h("GET /api/finance/accounts", (*Ctx).accounts)

	h("GET /api/invites", (*Ctx).invitesList)
	h("POST /api/invites", (*Ctx).inviteCreate)
	h("PATCH /api/invites/{id}", (*Ctx).inviteUpdate)
	h("DELETE /api/invites/{id}", (*Ctx).inviteRevoke)
	h("GET /api/officers", (*Ctx).officersList)
	h("GET /api/officers/{id}", (*Ctx).officerDetail)
	h("GET /api/officers/{id}/activity", (*Ctx).officerActivity)
	h("PATCH /api/officers/{id}", (*Ctx).officerUpdate)
	h("GET /api/me/profile", (*Ctx).profileGet)
	h("PUT /api/me/profile", (*Ctx).profilePut)
	h("PUT /api/me/onboarding", (*Ctx).onboardingSave)
	h("POST /api/me/onboarding/complete", (*Ctx).onboardingComplete)
	h("GET /api/me/calendar", (*Ctx).calendarGet)
	h("PUT /api/me/calendar", (*Ctx).calendarPut)
	h("POST /api/me/calendar/test", (*Ctx).calendarTest)
	h("GET /api/me/calendar/google/connect", (*Ctx).googleConnect)
	h("DELETE /api/me/calendar/google", (*Ctx).googleDisconnect)
	h("GET /auth/google/callback", (*Ctx).googleCallback)
	h("POST /api/admin/owners/{userId}", (*Ctx).ownerAdd)
	h("DELETE /api/admin/owners/{userId}", (*Ctx).ownerRemove)
	h("POST /api/admin/owners/transfer", (*Ctx).ownerTransfer)
	h("GET /api/admin/users", (*Ctx).adminUsers)
	h("POST /api/admin/users", (*Ctx).adminUserCreate)
	h("PATCH /api/admin/users/{id}", (*Ctx).adminUserUpdate)
	h("GET /api/admin/tokens", (*Ctx).adminTokens)
	h("POST /api/admin/tokens", (*Ctx).adminTokenCreate)
	h("DELETE /api/admin/tokens/{id}", (*Ctx).adminTokenRevoke)
	h("GET /api/admin/audit", (*Ctx).adminAudit)

	h("GET /api/agent/jobs", (*Ctx).agentJobs)
	h("POST /api/agent/jobs/{id}/claim", (*Ctx).agentJobClaim)
	h("POST /api/agent/jobs/{id}/complete", (*Ctx).agentJobComplete)

	h("GET /auth/login", (*Ctx).authLogin)
	h("GET /auth/callback", (*Ctx).authCallback)
	h("POST /auth/logout", (*Ctx).authLogout)
	s.registerDev(mux, h)

	// Anything else under /api is a 404 in the ApiError shape; everything else is the front end.
	h("/api/", func(c *Ctx) error { return notFoundErr("Endpoint not found") })
	mux.HandleFunc("GET /speakers", func(w http.ResponseWriter, r *http.Request) {
		target := "/contacts"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
	})
	mux.HandleFunc("GET /speakers/", func(w http.ResponseWriter, r *http.Request) {
		target := "/contacts"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
	})
	mux.Handle("/", s.staticHandler())
}

func (s *Server) staticHandler() http.Handler {
	if s.cfg.Static != nil {
		return s.cfg.Static
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
}
