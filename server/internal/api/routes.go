package api

import "net/http"

func (s *Server) routes(mux *http.ServeMux) {
	h := func(pattern string, fn handlerFunc) { mux.HandleFunc(pattern, s.wrap(fn)) }

	h("GET /api/health", (*Ctx).health)
	h("GET /api/me", (*Ctx).me)
	h("GET /api/me/prefs", (*Ctx).prefsGet)
	h("PUT /api/me/prefs", (*Ctx).prefsPut)
	h("GET /api/me/calendar-emails", (*Ctx).calendarEmailsList)
	h("POST /api/me/calendar-emails", (*Ctx).calendarEmailCreate)
	h("PATCH /api/me/calendar-emails/{id}", (*Ctx).calendarEmailUpdate)
	h("DELETE /api/me/calendar-emails/{id}", (*Ctx).calendarEmailDelete)
	h("GET /api/lookups", (*Ctx).lookups)
	h("GET /api/overview", (*Ctx).overview)
	h("GET /api/search", (*Ctx).search)

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

	h("GET /api/finance/summary", func(c *Ctx) error { return c.financeSummary() })
	h("GET /api/finance/reports/{report}", func(c *Ctx) error { return c.financeReport() })
	h("GET /api/finance/budgets", (*Ctx).budgetsGet)
	h("PUT /api/finance/budgets", (*Ctx).budgetsPut)
	h("GET /api/finance/periods", (*Ctx).periods)
	h("POST /api/finance/periods", (*Ctx).periodsCreate)
	h("POST /api/finance/periods/{id}/close", (*Ctx).periodClose)
	h("GET /api/finance/accounts", (*Ctx).accounts)

	h("POST /api/invites", (*Ctx).inviteCreate)
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
	mux.Handle("/", s.staticHandler())
}

func (s *Server) staticHandler() http.Handler {
	if s.cfg.Static != nil {
		return s.cfg.Static
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
}
