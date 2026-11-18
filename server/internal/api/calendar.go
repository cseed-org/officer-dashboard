package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
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
	if code == "" || state == "" || stored == "" || subtle.ConstantTimeCompare([]byte(state), []byte(stored)) != 1 || verifier == "" {
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
	switched := false
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
			switched = true
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
	// Falling back to .ics email: send the upcoming events there.
	if switched {
		c.S.backfillCalendar(u.ID)
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

// ─── delivery outbox ───
//
// Every event change is a row in calendar_outbox, written in the same transaction as the
// change (syncEventInTx, and eventDelete's cancel job); recipient backfills get a row too.
// One background worker drains the rows in order and deletes each when done. A sync job
// re-reads its event when it runs, so the newest saved content always wins and two quick
// edits never land out of order; a delete stores its cancellations (the event snapshot and
// every destination that still shows it), because the delivery rows cascade with the
// event. Rows survive a restart, and ResumeCalendar drains what is left.
//
// Delivery is at least once. A crash between a send and the row that records it repeats
// that send after restart with the same UID and SEQUENCE, which calendar clients apply as
// the same invite. A cancellation that fails stays in the outbox and is retried when the
// worker next runs (each change, startup, and hourly), up to calendarMaxAttempts times.

const calendarMaxAttempts = 24

func (s *Server) calendarsConfigured() bool { return s.cfg.Mailer != nil || s.cfg.Google != nil }

// enqueueCalendar adds a job in the caller's write transaction. Call kickCalendar after
// the transaction commits.
func (s *Server) enqueueCalendar(ctx context.Context, w *store.Queries, j store.CalendarJob) error {
	if !s.calendarsConfigured() {
		return nil
	}
	return w.EnqueueCalendar(ctx, j, s.nowMs())
}

// syncEventInTx queues delivery of a created or changed event to every recipient.
func (s *Server) syncEventInTx(ctx context.Context, w *store.Queries, eventID string) error {
	return s.enqueueCalendar(ctx, w, store.CalendarJob{Kind: "sync", EventID: eventID})
}

// kickCalendar starts the worker, or tells a running one to look for new rows.
func (s *Server) kickCalendar() {
	if !s.calendarsConfigured() {
		return
	}
	s.calMu.Lock()
	s.calKicked = true
	start := !s.calRunning
	s.calRunning = true
	s.calMu.Unlock()
	if start {
		s.bg.Add(1)
		go s.calWorker()
	}
}

// ResumeCalendar drains jobs a previous run left in the outbox (call it at startup) and
// retries cancellations that failed; the server also calls it hourly.
func (s *Server) ResumeCalendar() { s.kickCalendar() }

func (s *Server) calWorker() {
	defer s.bg.Done()
	var last int64
	for {
		s.calMu.Lock()
		s.calKicked = false
		s.calMu.Unlock()
		for {
			j, err := s.db.Read().NextCalendarJob(context.Background(), last)
			if err != nil {
				if !errors.Is(err, store.ErrNotFound) {
					s.log.Error("calendar outbox: read", "error", err)
				}
				break
			}
			last = j.ID
			s.runCalJob(j)
		}
		s.calMu.Lock()
		if !s.calKicked {
			s.calRunning = false
			s.calMu.Unlock()
			return
		}
		s.calMu.Unlock()
	}
}

func (s *Server) runCalJob(j *store.CalendarJob) {
	ctx := context.Background()
	var err error
	var left []store.Delivery
	switch j.Kind {
	case "backfill":
		err = s.backfillUser(ctx, j.UserID)
	case "sync":
		err = s.syncEvent(ctx, j.EventID)
	case "cancel":
		left = s.cancelDeleted(ctx, j)
	}
	write := func(fn func(q *store.Queries) error) {
		if werr := s.db.Write(ctx, fn); werr != nil {
			s.log.Error("calendar outbox: write", "job", j.ID, "error", werr)
		}
	}
	if err == nil && len(left) == 0 {
		write(func(q *store.Queries) error { return q.DeleteCalendarJob(ctx, j.ID) })
		return
	}
	attempts := j.Attempts + 1
	if attempts >= calendarMaxAttempts {
		s.log.Error("calendar job abandoned after retries", "job", j.ID, "kind", j.Kind, "event", j.EventID, "attempts", attempts)
		write(func(q *store.Queries) error { return q.DeleteCalendarJob(ctx, j.ID) })
		return
	}
	msg := "cancellation not delivered"
	if err != nil {
		msg = err.Error()
	}
	if j.Kind != "cancel" {
		left = nil
	}
	write(func(q *store.Queries) error { return q.UpdateCalendarJob(ctx, j.ID, left, attempts, &msg) })
}

// backfillCalendar sends upcoming events to one officer after their destination changes
// (mode, Google connection, active calendar email, finishing onboarding). Events already
// delivered unchanged to the same place are skipped.
func (s *Server) backfillCalendar(userID string) {
	if !s.calendarsConfigured() {
		return
	}
	ctx := context.Background()
	if err := s.db.Write(ctx, func(w *store.Queries) error {
		return s.enqueueCalendar(ctx, w, store.CalendarJob{Kind: "backfill", UserID: userID})
	}); err != nil {
		s.log.Error("calendar backfill: queue", "user", userID, "error", err)
		return
	}
	s.kickCalendar()
}

func (s *Server) recipientFor(ctx context.Context, userID string) (*recipient, error) {
	recips, err := s.db.Read().CalendarRecipients(ctx)
	if err != nil {
		return nil, err
	}
	for _, rc := range recips {
		if rc.UserID == userID {
			return &recipient{UserID: rc.UserID, Name: rc.Name, Mode: rc.Mode, Email: rc.ActiveEmail}, nil
		}
	}
	return nil, nil
}

func (s *Server) backfillUser(ctx context.Context, userID string) error {
	r, err := s.recipientFor(ctx, userID)
	if err != nil || r == nil {
		return err
	}
	events, err := s.db.Read().Events(ctx)
	if err != nil {
		return err
	}
	for i := range events {
		// A cancelled event is never a new invitation.
		if events[i].Status == "cancelled" {
			continue
		}
		if start, _, err := calendar.Window(events[i].StartsAt, events[i].EndsAt); err == nil && start.After(s.now()) {
			s.deliverTo(ctx, events[i].ID, *r)
		}
	}
	return nil
}

func (s *Server) syncEvent(ctx context.Context, eventID string) error {
	e, err := s.db.Read().EventByID(ctx, eventID)
	if errors.Is(err, store.ErrNotFound) {
		return nil // deleted since; its cancel job handles it
	}
	if err != nil {
		return err
	}
	if e.Status == "cancelled" {
		s.withdraw(ctx, e.ID, "")
		return nil
	}
	recips, err := s.db.Read().CalendarRecipients(ctx)
	if err != nil {
		return err
	}
	for _, rc := range recips {
		s.deliverTo(ctx, e.ID, recipient{UserID: rc.UserID, Name: rc.Name, Mode: rc.Mode, Email: rc.ActiveEmail})
	}
	return nil
}

// destination names where r's invites go now: "ics:<address>" or "google:<account>".
// ok is false when nothing can be delivered (not configured, no address, not connected).
func (s *Server) destination(ctx context.Context, r recipient) (string, bool) {
	if r.Mode == "google" {
		if s.cfg.Google == nil {
			return "", false
		}
		t, err := s.db.Read().GoogleTokenFor(ctx, r.UserID)
		if err != nil {
			return "", false
		}
		account := ""
		if t.AccountEmail != nil {
			account = strings.ToLower(*t.AccountEmail)
		}
		return "google:" + account, true
	}
	if s.cfg.Mailer == nil || r.Email == nil {
		return "", false
	}
	return "ics:" + strings.ToLower(*r.Email), true
}

// eventLock serialises, per event, a delivery's read-send-record step with the event's
// deletion: a delete either happens before a send starts (the send then finds no event and
// stops) or sees the delivery it made and cancels it with a newer SEQUENCE.
func (s *Server) eventLock(eventID string) func() {
	h := fnv.New32a()
	_, _ = h.Write([]byte(eventID))
	m := &s.evLocks[h.Sum32()%uint32(len(s.evLocks))]
	m.Lock()
	return m.Unlock
}

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

// deliverTo sends one event to one officer's current destination, with its own timeout so
// a slow relay or a long queue never starves later recipients.
func (s *Server) deliverTo(ctx context.Context, eventID string, r recipient) {
	unlock := s.eventLock(eventID)
	defer unlock()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	e, err := s.db.Read().EventByID(ctx, eventID)
	if err != nil {
		return // deleted while queued; its cancel job withdraws what was sent
	}
	if e.Status == "cancelled" {
		s.withdrawLocked(ctx, e, r.UserID)
		return
	}
	dest, ok := s.destination(ctx, r)
	if !ok {
		return
	}
	prior, err := s.db.Read().DeliveryFor(ctx, e.ID, r.UserID, dest)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return
	}
	seq := 0
	if prior != nil {
		seq = prior.Sequence + 1
	}
	ev, err := s.calendarEvent(e, seq, false)
	if err != nil {
		s.log.Warn("calendar: unparseable event time", "event", e.ID)
		return
	}
	hash := fingerprint(ev, r)
	if prior != nil && prior.Live() && prior.ContentHash != nil && *prior.ContentHash == hash {
		return // already on their calendar, unchanged
	}
	if testDeliveryHook != nil {
		testDeliveryHook(e.ID, r.UserID)
	}
	out := s.deliverOne(ctx, r, ev, prior)
	if out.Status != "sent" && out.Status != "failed" {
		return
	}
	now := s.nowMs()
	var d store.Delivery
	switch {
	case out.Status == "sent":
		d = store.Delivery{EventID: e.ID, UserID: r.UserID, Destination: dest, Mode: r.Mode, Recipient: r.Email, Sequence: seq, Status: "sent", ContentHash: &hash, UpdatedAt: now}
		if prior != nil {
			d.GoogleEventID = prior.GoogleEventID
		}
		if out.GoogleEventID != "" {
			id := out.GoogleEventID
			d.GoogleEventID = &id
		}
	case prior != nil && prior.Status == "sent":
		// A failed update leaves the last success (destination, Google id, SEQUENCE, content)
		// in place, so a later delete still cancels it; only the error is new.
		d = *prior
		d.Error, d.UpdatedAt = &out.Message, now
	default:
		d = store.Delivery{EventID: e.ID, UserID: r.UserID, Destination: dest, Mode: r.Mode, Recipient: r.Email, Status: "failed", Error: &out.Message, UpdatedAt: now}
		if prior != nil {
			d.Sequence = prior.Sequence // a failed send doesn't consume a SEQUENCE
		}
	}
	if err := s.db.Write(ctx, func(q *store.Queries) error { return q.SaveDelivery(ctx, d) }); err != nil {
		s.log.Error("save calendar delivery", "error", err)
	}
}

// testDeliveryHook, when a test sets it, runs inside deliverTo just before the send.
var testDeliveryHook func(eventID, userID string)

// sendCancel withdraws an event from one destination that has it, with the next SEQUENCE.
// A Google destination is reachable only while that same account is still connected.
func (s *Server) sendCancel(ctx context.Context, e *store.Event, d store.Delivery) deliveryResult {
	ev, err := s.calendarEvent(e, d.Sequence+1, true)
	if err != nil {
		return deliveryResult{Status: "skipped", Message: "unparseable event time"}
	}
	r := recipient{UserID: d.UserID, Mode: d.Mode, Email: d.Recipient}
	if d.Mode == "google" {
		if d.GoogleEventID == nil {
			return deliveryResult{Status: "skipped", Message: "no Google event to cancel"}
		}
		if cur, ok := s.destination(ctx, r); !ok || cur != d.Destination {
			return deliveryResult{Status: "skipped", Message: "that Google account is no longer connected"}
		}
	}
	return s.deliverOne(ctx, r, ev, &d)
}

// withdraw cancels an event marked cancelled on every destination that still shows it,
// for one officer or (userID "") everyone.
func (s *Server) withdraw(ctx context.Context, eventID, userID string) {
	unlock := s.eventLock(eventID)
	defer unlock()
	e, err := s.db.Read().EventByID(ctx, eventID)
	if err != nil {
		return
	}
	s.withdrawLocked(ctx, e, userID)
}

func (s *Server) withdrawLocked(ctx context.Context, e *store.Event, userID string) {
	rows, err := s.db.Read().DeliveriesForEvent(ctx, e.ID, userID)
	if err != nil {
		s.log.Error("calendar withdraw: deliveries", "event", e.ID, "error", err)
		return
	}
	for _, d := range rows {
		if !d.Live() {
			continue
		}
		dctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		out := s.sendCancel(dctx, e, d)
		cancel()
		switch out.Status {
		case "sent":
			ev, _ := s.calendarEvent(e, d.Sequence+1, true)
			hash := fingerprint(ev, recipient{Mode: d.Mode, Email: d.Recipient})
			d.Sequence, d.Cancelled, d.Error, d.ContentHash = d.Sequence+1, true, nil, &hash
		case "failed":
			d.Error = &out.Message
			s.log.Warn("calendar cancellation failed", "event", e.ID, "user", d.UserID, "error", out.Message)
		default:
			continue
		}
		d.UpdatedAt = s.nowMs()
		if err := s.db.Write(ctx, func(q *store.Queries) error { return q.SaveDelivery(ctx, d) }); err != nil {
			s.log.Error("save calendar delivery", "error", err)
		}
	}
}

// cancelDeleted sends a CANCEL (.ics) or cancels the Google event on each destination a
// deleted event reached. It returns the destinations still to retry, and records progress
// after each success so a restart doesn't repeat finished ones.
func (s *Server) cancelDeleted(ctx context.Context, j *store.CalendarJob) []store.Delivery {
	if j.Snapshot == nil {
		return nil
	}
	e := j.Snapshot.Event()
	left := append([]store.Delivery(nil), j.Priors...)
	for i := 0; i < len(left); {
		dctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		out := s.sendCancel(dctx, e, left[i])
		cancel()
		if out.Status == "failed" {
			s.log.Warn("calendar cancellation failed", "event", e.ID, "user", left[i].UserID, "error", out.Message)
			i++
			continue
		}
		left = append(left[:i], left[i+1:]...)
		rest := append([]store.Delivery{}, left...)
		if err := s.db.Write(ctx, func(q *store.Queries) error { return q.UpdateCalendarJob(ctx, j.ID, rest, j.Attempts, j.LastError) }); err != nil {
			s.log.Error("calendar outbox: record progress", "job", j.ID, "error", err)
		}
	}
	return left
}
