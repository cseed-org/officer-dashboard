package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/calendar"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

// CalendarSettings is the signed-in officer's calendar delivery state.
type CalendarSettings struct {
	Mode        string  `json:"mode"` // ics | google
	HasProfile  bool    `json:"hasProfile"`
	ActiveEmail *string `json:"activeEmail"`
	ICS         struct {
		Configured bool `json:"configured"`
	} `json:"ics"`
	Google struct {
		Configured   bool    `json:"configured"`
		Connected    bool    `json:"connected"`
		AccountEmail *string `json:"accountEmail"`
		ConnectedAt  *string `json:"connectedAt"`
	} `json:"google"`
}

func (c *Ctx) calendarSettings(ctx context.Context, q *store.Queries, userID string) (*CalendarSettings, error) {
	out := &CalendarSettings{Mode: "ics"}
	out.ICS.Configured = c.S.cfg.Mailer != nil
	out.Google.Configured = c.S.cfg.Google != nil
	if p, err := q.ProfileByUser(ctx, userID); err == nil {
		out.Mode, out.HasProfile = p.DeliveryMode, true
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	email, err := q.ActiveCalendarEmail(ctx, userID)
	if err != nil {
		return nil, err
	}
	out.ActiveEmail = email
	if t, err := q.GoogleTokenFor(ctx, userID); err == nil {
		out.Google.Connected = true
		out.Google.AccountEmail = t.AccountEmail
		out.Google.ConnectedAt = isoPtr(&t.ConnectedAt)
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	return out, nil
}

func (c *Ctx) calendarGet() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	out, err := c.calendarSettings(c.Context(), c.S.db.Read(), u.ID)
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, out)
	return nil
}

func (c *Ctx) calendarPut() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	var mode string
	if err := c.jsonBody(func(r *validate.Reader) {
		mode = r.Str("mode", validate.S{Req: true, Enum: []string{"ics", "google"}}).V
	}); err != nil {
		return err
	}
	ctx := c.Context()
	var out *CalendarSettings
	changed := false
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		before, err := q.ProfileByUser(ctx, u.ID)
		if err != nil {
			return conflict("Save your profile (phone and birthday) before choosing calendar delivery")
		}
		if mode == "google" {
			if c.S.cfg.Google == nil {
				return conflict("Google Calendar sync is not configured on this dashboard")
			}
			if _, err := q.GoogleTokenFor(ctx, u.ID); err != nil {
				return conflict("Connect Google Calendar before choosing Google sync")
			}
		}
		if before.DeliveryMode != mode {
			changed = true
			if err := q.SetDeliveryMode(ctx, u.ID, mode); err != nil {
				return err
			}
			if err := c.audit(ctx, q, "calendar.mode", "officer_profile", u.ID, map[string]any{"mode": before.DeliveryMode}, map[string]any{"mode": mode}); err != nil {
				return err
			}
		}
		out, err = c.calendarSettings(ctx, q, u.ID)
		return err
	})
	if err != nil {
		return err
	}
	if changed {
		c.S.backfillCalendar(u.ID)
	}
	writeJSON(c.W, 200, out)
	return nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// googleConnect starts the Google consent screen. It is a navigation, not a fetch.
func (c *Ctx) googleConnect() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	g := c.S.cfg.Google
	if g == nil {
		return fail(503, "not_configured", "Google Calendar sync is not configured (GOOGLE_OAUTH_CLIENT_ID / GOOGLE_OAUTH_CLIENT_SECRET)")
	}
	state, verifier := auth.RandomToken(32), auth.RandomToken(32)
	next := auth.SafeNext(c.R.URL.Query().Get("next"))
	if next == "/" {
		next = "/settings"
	}
	for name, v := range map[string]string{googleStateName: state, googleVerifier: verifier, googleNextName: next} {
		c.S.setCookie(c.W, c.S.hostPrefixed(name), v, c.S.now().Add(10*time.Minute), 600)
	}
	http.Redirect(c.W, c.R, g.ConsentURL(state, pkceChallenge(verifier)), http.StatusFound)
	return nil
}

func withQuery(path, key, value string) string {
	frag := ""
	if i := strings.IndexByte(path, '#'); i >= 0 {
		path, frag = path[:i], path[i:]
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + key + "=" + value + frag
}

// googleCallback stores the grant for the signed-in officer and switches them to Google
// sync. Errors land back on the page that started it with ?google=<reason>.
func (c *Ctx) googleCallback() error {
	q := c.R.URL.Query()
	state, code := q.Get("state"), q.Get("code")
	stored, verifier := c.cookie(c.S.hostPrefixed(googleStateName)), c.cookie(c.S.hostPrefixed(googleVerifier))
	next := auth.SafeNext(c.cookie(c.S.hostPrefixed(googleNextName)))
	for _, n := range []string{googleStateName, googleVerifier, googleNextName} {
		c.S.clearCookie(c.W, c.S.hostPrefixed(n))
	}
	back := func(reason string) error {
		http.Redirect(c.W, c.R, withQuery(next, "google", reason), http.StatusFound)
		return nil
	}
	if c.User == nil {
		http.Redirect(c.W, c.R, "/login?next=/settings", http.StatusFound)
		return nil
	}
	g := c.S.cfg.Google
	if g == nil {
		return back("not_configured")
	}
	if q.Get("error") == "access_denied" {
		return back("cancelled")
	}
	if code == "" || state == "" || stored == "" || state != stored || verifier == "" {
		return back("failed")
	}
	ctx := c.Context()
	tok, err := g.Exchange(ctx, code, verifier)
	if err != nil {
		c.S.log.Warn("google oauth exchange failed", "error", err)
		return back("failed")
	}
	if tok.RefreshToken == "" {
		return back("failed")
	}
	now := c.S.nowMs()
	exp := now + tok.ExpiresIn*1000
	sealedAccess := c.S.sealer.Seal(tok.AccessToken)
	t := store.GoogleToken{UserID: c.User.ID, SealedRefresh: c.S.sealer.Seal(tok.RefreshToken), SealedAccess: &sealedAccess, AccessExpires: &exp, ConnectedAt: now}
	if e := calendar.EmailFromIDToken(tok.IDToken); e != "" {
		t.AccountEmail = &e
	}
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		if err := w.SaveGoogleToken(ctx, t); err != nil {
			return err
		}
		if _, err := w.ProfileByUser(ctx, c.User.ID); err == nil {
			if err := w.SetDeliveryMode(ctx, c.User.ID, "google"); err != nil {
				return err
			}
		}
		return c.audit(ctx, w, "calendar.google_connected", "user", c.User.ID, nil, map[string]any{"accountEmail": t.AccountEmail})
	})
	if err != nil {
		c.S.log.Error("save google token failed", "error", err)
		return back("failed")
	}
	c.S.backfillCalendar(c.User.ID)
	return back("connected")
}

func (c *Ctx) googleDisconnect() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	ctx := c.Context()
	var refresh string
	var out *CalendarSettings
	err = c.S.db.Write(ctx, func(q *store.Queries) error {
		t, err := q.GoogleTokenFor(ctx, u.ID)
		if errors.Is(err, store.ErrNotFound) {
			out, err = c.calendarSettings(ctx, q, u.ID)
			return err
		}
		if err != nil {
			return err
		}
		if c.S.sealer != nil {
			refresh, _ = c.S.sealer.Open(t.SealedRefresh)
		}
		if err := q.DeleteGoogleToken(ctx, u.ID); err != nil {
			return err
		}
		if p, err := q.ProfileByUser(ctx, u.ID); err == nil && p.DeliveryMode == "google" {
			if err := q.SetDeliveryMode(ctx, u.ID, "ics"); err != nil {
				return err
			}
		}
		if err := c.audit(ctx, q, "calendar.google_disconnected", "user", u.ID, map[string]any{"accountEmail": t.AccountEmail}, nil); err != nil {
			return err
		}
		out, err = c.calendarSettings(ctx, q, u.ID)
		return err
	})
	if err != nil {
		return err
	}
	if refresh != "" && c.S.cfg.Google != nil {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		c.S.cfg.Google.Revoke(rctx, refresh)
		cancel()
	}
	writeJSON(c.W, 200, out)
	return nil
}

// calendarTest sends a sample invite through the officer's current mode, synchronously,
// so Settings can show whether delivery actually works.
func (c *Ctx) calendarTest() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	ctx := c.Context()
	st, err := c.calendarSettings(ctx, c.S.db.Read(), u.ID)
	if err != nil {
		return err
	}
	start := c.S.now().Add(24 * time.Hour).Truncate(time.Hour)
	ev := calendar.Event{UID: "test-" + auth.UUID() + "@cseed-dashboard", Summary: "CSEED calendar test", Description: "If this is on your calendar, dashboard events will be too. You can delete it.",
		Start: start, End: start.Add(30 * time.Minute), URL: c.S.link("/settings", c.R)}
	r := recipient{UserID: u.ID, Name: u.Name, Mode: st.Mode, Email: st.ActiveEmail}
	res := c.S.deliverOne(ctx, r, ev, nil)
	writeJSON(c.W, 200, map[string]any{"status": res.Status, "message": res.Message})
	return nil
}

// ─── delivery ───

type recipient struct {
	UserID, Name, Mode string
	Email              *string
}

type deliveryResult struct {
	Status        string // sent | failed | skipped | not_configured
	Message       string
	GoogleEventID string
}

// deliverOne sends ev to one person. prior is the last delivery of the same event, which
// carries the Google event id to update.
func (s *Server) deliverOne(ctx context.Context, r recipient, ev calendar.Event, prior *store.Delivery) deliveryResult {
	res := func(status, msg string) deliveryResult { return deliveryResult{Status: status, Message: msg} }
	switch r.Mode {
	case "google":
		if s.cfg.Google == nil {
			return res("not_configured", "Google Calendar sync is not configured on this dashboard.")
		}
		access, err := s.googleAccess(ctx, r.UserID)
		if errors.Is(err, store.ErrNotFound) {
			return res("skipped", "Google Calendar isn't connected.")
		}
		if err != nil {
			return res("failed", googleMessage(err))
		}
		gid := ""
		if prior != nil && prior.GoogleEventID != nil {
			gid = *prior.GoogleEventID
		}
		id, err := s.cfg.Google.UpsertEvent(ctx, access, gid, ev)
		if err != nil {
			return res("failed", googleMessage(err))
		}
		return deliveryResult{Status: "sent", Message: "Added to Google Calendar.", GoogleEventID: id}
	default:
		if s.cfg.Mailer == nil {
			return res("not_configured", ".ics email invites are not configured (SMTP_HOST / SMTP_FROM).")
		}
		if r.Email == nil {
			return res("skipped", "No calendar email selected.")
		}
		method := "REQUEST"
		subject := "Invitation: " + ev.Summary
		if ev.Cancelled {
			method, subject = "CANCEL", "Cancelled: "+ev.Summary
		} else if ev.Sequence > 0 {
			subject = "Updated invitation: " + ev.Summary
		}
		zone := s.effectiveZone(ctx, r.UserID)
		ev.Timezone = zone.String()
		when := ev.Start.In(zone).Format("Mon Jan 2, 3:04 PM MST") + " · " + zone.String()
		text := fmt.Sprintf("%s\n%s\n%s\n\nOpen the attached invite to add it to your calendar.\n%s", ev.Summary, when, ev.Location, ev.URL)
		err := s.cfg.Mailer.Send(ctx, calendar.Message{To: *r.Email, Subject: subject, Text: text, Method: method,
			ICS: calendar.ICS(ev, s.cfg.Mailer.From(), *r.Email, s.now())})
		if err != nil {
			s.log.Warn("calendar email failed", "user", r.UserID, "error", err)
			return res("failed", "The invite email couldn't be sent.")
		}
		return res("sent", "Invite sent to "+*r.Email+".")
	}
}

func googleMessage(err error) string {
	if errors.Is(err, calendar.ErrGoogleRevoked) {
		return "Google Calendar access was revoked. Reconnect it in Settings."
	}
	return "Google Calendar didn't accept the event. Try again later."
}

// googleAccess returns a live access token, refreshing and re-sealing it when needed.
func (s *Server) googleAccess(ctx context.Context, userID string) (string, error) {
	t, err := s.db.Read().GoogleTokenFor(ctx, userID)
	if err != nil {
		return "", err
	}
	now := s.nowMs()
	if t.SealedAccess != nil && t.AccessExpires != nil && *t.AccessExpires-60_000 > now {
		if a, err := s.sealer.Open(*t.SealedAccess); err == nil {
			return a, nil
		}
	}
	refresh, err := s.sealer.Open(t.SealedRefresh)
	if err != nil {
		return "", err
	}
	tok, err := s.cfg.Google.Refresh(ctx, refresh)
	if err != nil {
		return "", err
	}
	exp := now + tok.ExpiresIn*1000
	err = s.db.Write(ctx, func(q *store.Queries) error {
		return q.UpdateGoogleAccess(ctx, userID, s.sealer.Seal(tok.AccessToken), exp)
	})
	return tok.AccessToken, err
}

func (s *Server) calendarEvent(e *store.Event, seq int, cancelled bool) (calendar.Event, error) {
	start, end, err := calendar.Window(e.StartsAt, e.EndsAt)
	if err != nil {
		return calendar.Event{}, err
	}
	ev := calendar.Event{UID: "event-" + e.ID + "@cseed-dashboard", Sequence: seq, Summary: e.Name, Start: start, End: end, Cancelled: cancelled,
		URL: s.cfg.AppURL + "/events?id=" + e.ID}
	if e.Location != nil {
		ev.Location = *e.Location
	}
	if e.URL != nil {
		ev.Description = "Event page: " + *e.URL
	}
	return ev, nil
}

// ─── delivery queue ───
//
// Deliveries run one at a time, in order, on a single background worker. Each job names an
// event by ID and re-reads it when it runs, so the newest saved content always wins and
// two quick edits can never land out of order. Deletes carry a snapshot, since the row is gone.

type calJob struct {
	eventID   string       // "" with onlyUser: every upcoming event (backfill)
	snapshot  *store.Event // set for cancellations
	cancelled bool
	priors    map[string]store.Delivery // cancellations: what each officer received before the delete
	onlyUser  string
}

func (s *Server) calendarsConfigured() bool { return s.cfg.Mailer != nil || s.cfg.Google != nil }

func (s *Server) enqueue(j calJob) {
	if !s.calendarsConfigured() {
		return
	}
	s.calMu.Lock()
	s.calQueue = append(s.calQueue, j)
	start := !s.calRunning
	s.calRunning = true
	s.calMu.Unlock()
	if start {
		s.bg.Add(1)
		go s.calWorker()
	}
}

func (s *Server) calWorker() {
	defer s.bg.Done()
	for {
		s.calMu.Lock()
		if len(s.calQueue) == 0 {
			s.calRunning = false
			s.calMu.Unlock()
			return
		}
		j := s.calQueue[0]
		s.calQueue = s.calQueue[1:]
		s.calMu.Unlock()
		s.runCalJob(j)
	}
}

func (s *Server) runCalJob(j calJob) {
	ctx := context.Background()
	if j.eventID == "" {
		events, err := s.db.Read().Events(ctx)
		if err != nil {
			s.log.Error("calendar backfill: list events", "error", err)
			return
		}
		for i := range events {
			if start, _, err := calendar.Window(events[i].StartsAt, events[i].EndsAt); err == nil && start.After(s.now()) {
				s.deliverEvent(&events[i], false, j.onlyUser)
			}
		}
		return
	}
	if j.cancelled {
		s.cancelEvent(j.snapshot, j.priors)
		return
	}
	e, err := s.db.Read().EventByID(ctx, j.eventID)
	if err != nil {
		return // deleted since; its cancellation job handles it
	}
	s.deliverEvent(e, false, j.onlyUser)
}

// SyncEventToCalendars sends a created or changed event to every active officer, in the
// background, after the caller's write has committed.
func (s *Server) SyncEventToCalendars(e store.Event) { s.enqueue(calJob{eventID: e.ID}) }

// CancelEventOnCalendars withdraws a deleted event from every calendar it reached. priors
// must be read (DeliveriesForEvent) before the delete, since those rows cascade with it.
func (s *Server) CancelEventOnCalendars(e store.Event, priors map[string]store.Delivery) {
	if len(priors) == 0 {
		return
	}
	snap := e
	s.enqueue(calJob{eventID: e.ID, snapshot: &snap, cancelled: true, priors: priors})
}

// cancelEvent sends a CANCEL (.ics) or cancels the Google event for each officer who had
// it. The event row is gone, so nothing is recorded afterwards.
func (s *Server) cancelEvent(e *store.Event, priors map[string]store.Delivery) {
	for userID, prior := range priors {
		if prior.Status != "sent" {
			continue
		}
		p := prior
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		ev, err := s.calendarEvent(e, prior.Sequence+1, true)
		if err == nil {
			out := s.deliverOne(ctx, recipient{UserID: userID, Mode: prior.Mode, Email: prior.Recipient}, ev, &p)
			if out.Status == "failed" {
				s.log.Warn("calendar cancellation failed", "event", e.ID, "user", userID, "error", out.Message)
			}
		}
		cancel()
	}
}

// syncEventID is SyncEventToCalendars for callers that only have the ID (imports).
func (s *Server) syncEventID(id string) { s.enqueue(calJob{eventID: id}) }

// backfillCalendar sends upcoming events to one officer after their destination changes
// (mode, Google connection, active calendar email, finishing onboarding). Events already
// delivered unchanged to the same place are skipped.
func (s *Server) backfillCalendar(userID string) { s.enqueue(calJob{onlyUser: userID}) }

// fingerprint identifies what a recipient last received, so unchanged events aren't resent.
func fingerprint(ev calendar.Event, r recipient) string {
	email := ""
	if r.Email != nil {
		email = *r.Email
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{r.Mode, email, ev.Summary, ev.Start.UTC().Format(time.RFC3339), ev.End.UTC().Format(time.RFC3339),
		ev.Location, ev.Description, ev.URL, fmt.Sprint(ev.Cancelled)}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (s *Server) deliverEvent(e *store.Event, cancelled bool, onlyUser string) {
	recips, err := s.db.Read().CalendarRecipients(context.Background())
	if err != nil {
		s.log.Error("calendar recipients", "error", err)
		return
	}
	for _, rc := range recips {
		if onlyUser != "" && rc.UserID != onlyUser {
			continue
		}
		s.deliverTo(e, cancelled, recipient{UserID: rc.UserID, Name: rc.Name, Mode: rc.Mode, Email: rc.ActiveEmail})
	}
}

// deliverTo sends one event to one officer with its own timeout, so a slow relay or a
// long queue never starves later recipients.
func (s *Server) deliverTo(e *store.Event, cancelled bool, r recipient) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	prior, err := s.db.Read().DeliveryFor(ctx, e.ID, r.UserID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return
	}
	seq := 0
	if prior != nil {
		seq = prior.Sequence + 1
	}
	ev, err := s.calendarEvent(e, seq, cancelled)
	if err != nil {
		s.log.Warn("calendar: unparseable event time", "event", e.ID)
		return
	}
	hash := fingerprint(ev, r)
	if prior != nil && prior.Status == "sent" && prior.ContentHash != nil && *prior.ContentHash == hash {
		return // already on their calendar, unchanged
	}
	out := s.deliverOne(ctx, r, ev, prior)
	if out.Status != "sent" && out.Status != "failed" {
		return
	}
	d := store.Delivery{EventID: e.ID, UserID: r.UserID, Mode: r.Mode, Recipient: r.Email, Sequence: seq, Status: out.Status, UpdatedAt: s.nowMs()}
	if prior != nil {
		d.GoogleEventID = prior.GoogleEventID // kept across mode switches, so going back patches it
	}
	if out.GoogleEventID != "" {
		id := out.GoogleEventID
		d.GoogleEventID = &id
	}
	if out.Status == "sent" {
		d.ContentHash = &hash
	} else {
		d.Error = &out.Message
		if prior != nil {
			d.Sequence = prior.Sequence // a failed send doesn't consume a SEQUENCE
		}
	}
	if err := s.db.Write(ctx, func(q *store.Queries) error { return q.SaveDelivery(ctx, d) }); err != nil {
		s.log.Error("save calendar delivery", "error", err)
	}
}
