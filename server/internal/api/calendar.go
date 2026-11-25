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
			if err := c.S.queueBackfill(ctx, q, u.ID); err != nil {
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
		c.S.kickCalendar()
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
	// Deliveries are keyed by the Google account, so a grant must say which one it is.
	// Google always sends the ID token's sub with the openid scope; without it two
	// accounts could not be told apart, so the grant is refused rather than stored.
	acct := calendar.AccountFromIDToken(tok.IDToken)
	if acct.Sub == "" {
		c.S.log.Warn("google oauth: token response has no ID token sub; not connecting")
		return back("failed")
	}
	now := c.S.nowMs()
	exp := now + tok.ExpiresIn*1000
	sealedAccess := c.S.sealer.Seal(tok.AccessToken)
	t := store.GoogleToken{UserID: c.User.ID, AccountSub: &acct.Sub, SealedRefresh: c.S.sealer.Seal(tok.RefreshToken), SealedAccess: &sealedAccess, AccessExpires: &exp, ConnectedAt: now}
	if acct.Email != "" {
		t.AccountEmail = &acct.Email
	}
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		if err := w.SaveGoogleToken(ctx, t); err != nil {
			return err
		}
		// Deliveries made under a grant from before V0.14.1 (keyed by email, or by nothing)
		// become this account's, so reconnecting it doesn't add copies of its events.
		if err := w.AdoptLegacyGoogleDeliveries(ctx, t); err != nil {
			return err
		}
		// The (possibly different) account needs the upcoming events.
		if err := c.S.queueBackfill(ctx, w, c.User.ID); err != nil {
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
	c.S.kickCalendar()
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
			// Falling back to .ics email: send the upcoming events there.
			if err := c.S.queueBackfill(ctx, q, u.ID); err != nil {
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
	if switched {
		c.S.kickCalendar()
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
	// Revoked marks a Google failure only reconnecting fixes. Retrying cannot help, and
	// reconnecting queues a backfill, so event syncs don't keep retrying it.
	Revoked bool
}

// deliverOne sends ev to one person. prior is the last delivery of the same event to that
// destination, which carries the Google event id to update.
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
			return deliveryResult{Status: "failed", Message: googleMessage(err), Revoked: errors.Is(err, calendar.ErrGoogleRevoked)}
		}
		known := ""
		if prior != nil && prior.GoogleEventID != nil {
			known = *prior.GoogleEventID
		}
		// A new event gets the dashboard's own stable id for this calendar, so repeating an
		// insert whose result was lost updates the first copy instead of adding another.
		dest, _ := s.destination(ctx, r)
		id, err := s.cfg.Google.UpsertEvent(ctx, access, known, calendar.EventID(ev.UID, dest), ev)
		if err != nil {
			return deliveryResult{Status: "failed", Message: googleMessage(err), Revoked: errors.Is(err, calendar.ErrGoogleRevoked)}
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
			s.log.Warn("calendar email failed", "user", r.UserID)
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
// change (syncEventInTx, and eventDelete's cancel job); so is every recipient backfill,
// written in the transaction that changes where that officer's invites go (queueBackfill).
// One background worker drains the rows in order. A job is deleted only once all of its
// work succeeded; a failed one stays, with its attempt count and error, and is retried
// after a backoff (on the next change once it is due) and in full at startup and hourly
// (ResumeCalendar). Nothing is ever dropped after a number of failures. A sync job re-reads
// its event when it runs, so the newest saved content always wins and two quick edits never
// land out of order; destinations that already have that content are skipped, so a retry
// only repeats what failed. A delete stores its cancellations (the event snapshot and every
// destination that has or may have it), because the delivery rows cascade with the event.
//
// Delivery is at least once. Before each send the destination row records the attempt
// (its SEQUENCE and, for a new Google event, the event id the dashboard chose), so a crash
// between the send and the row that records its outcome loses nothing: a delete before the
// retry cancels that destination too, with a newer SEQUENCE, and the retry repeats the
// send with the same UID and, for the same content, the same SEQUENCE, which calendar
// clients apply as the same invite, and the same Google event id, which Google answers
// with "already exists" and the dashboard then updates instead of creating a second event.
//
// A missing transport is a failure, not a skip, for a destination that has deliveries
// (SMTP or Google left out of a restart's config, both of them included): its work stays
// queued until the transport is back. Destinations that never had a working transport,
// and Google accounts the officer has disconnected, are skipped. So jobs are written and
// the worker runs whatever is configured: on a dashboard that never had SMTP or Google
// every job finds nothing to send and is removed, and the outbox stays empty.

const (
	calendarRetryMin = time.Minute
	calendarRetryMax = time.Hour
	// calendarAlertAttempts is when a job that keeps failing is logged as an error. It
	// still stays in the outbox and is retried.
	calendarAlertAttempts = 24
)

// calendarBackoff is how long a job that has failed attempts times waits before a change
// retries it: 1, 2, 4 … minutes, at most an hour.
func calendarBackoff(attempts int) time.Duration {
	d := calendarRetryMin
	for i := 1; i < attempts && d < calendarRetryMax; i++ {
		d *= 2
	}
	return min(d, calendarRetryMax)
}

// enqueueCalendar adds a job in the caller's write transaction. Call kickCalendar after
// the transaction commits. It writes the job even when no transport is configured on
// this run: a destination that already has deliveries still needs the change once its
// transport is back, and the worker removes jobs that have nowhere to go.
func (s *Server) enqueueCalendar(ctx context.Context, w *store.Queries, j store.CalendarJob) error {
	return w.EnqueueCalendar(ctx, j, s.nowMs())
}

// syncEventInTx queues delivery of a created or changed event to every recipient.
func (s *Server) syncEventInTx(ctx context.Context, w *store.Queries, eventID string) error {
	if err := s.projectOrgEvent(ctx, w, eventID); err != nil {
		return err
	}
	return s.enqueueCalendar(ctx, w, store.CalendarJob{Kind: "sync", EventID: eventID})
}

// queueBackfill queues sending the upcoming events to one officer, in the write
// transaction that changes where their invites go (mode, Google connection, active
// calendar email, a new profile, finishing onboarding), so a restart right after that
// commit cannot lose it. Call kickCalendar after the transaction commits. Events already
// delivered unchanged to the same place are skipped.
func (s *Server) queueBackfill(ctx context.Context, w *store.Queries, userID string) error {
	return s.enqueueCalendar(ctx, w, store.CalendarJob{Kind: "backfill", UserID: userID})
}

// kickCalendar starts the worker, or tells a running one to look again, for jobs that are
// due: new ones, and failed ones whose backoff has passed.
func (s *Server) kickCalendar() { s.startCalendar(false); s.kickOrgCalendar() }

// ResumeCalendar drains every job in the outbox, failed ones included whatever their
// backoff: call it at startup for what a previous run left; the server also calls it hourly.
func (s *Server) ResumeCalendar() { s.startCalendar(true); s.resumeOrgCalendar() }

func (s *Server) startCalendar(all bool) {
	s.calMu.Lock()
	s.calKicked = true
	s.calAll = s.calAll || all
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
		s.calKicked = false
		all := s.calAll
		s.calAll = false
		s.calMu.Unlock()
		// One pass runs each due job once, oldest first; a job that fails waits for its
		// backoff (or the next full pass), so passes never spin on a failing job.
		var last int64
		for {
			j, err := s.db.Read().NextCalendarJob(context.Background(), last, s.nowMs(), all)
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
		left, err = s.cancelDeleted(ctx, j)
	default:
		err = fmt.Errorf("unknown calendar job kind %q", j.Kind)
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
	// Keep the job, and for a cancellation exactly the destinations still to withdraw.
	attempts := j.Attempts + 1
	msg := "cancellation not delivered"
	if err != nil {
		msg = err.Error()
	}
	if attempts >= calendarAlertAttempts {
		s.log.Error("calendar job still failing; it stays queued and is retried", "job", j.ID, "kind", j.Kind, "event", j.EventID, "attempts", attempts, "error", msg)
	} else {
		s.log.Warn("calendar job failed; will retry", "job", j.ID, "kind", j.Kind, "event", j.EventID, "attempts", attempts, "error", msg)
	}
	if j.Kind != "cancel" {
		left = nil
	}
	next := s.now().Add(calendarBackoff(attempts)).UnixMilli()
	write(func(q *store.Queries) error { return q.UpdateCalendarJob(ctx, j.ID, left, attempts, &msg, next) })
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

// backfillUser brings one officer's calendar up to date with the upcoming events. A
// cancelled event is never a new invitation; deliverTo only withdraws it where this
// officer still has it.
func (s *Server) backfillUser(ctx context.Context, userID string) error {
	r, err := s.recipientFor(ctx, userID)
	if err != nil || r == nil {
		return err
	}
	events, err := s.db.Read().Events(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for i := range events {
		if start, _, err := calendar.Window(events[i].StartsAt, events[i].EndsAt); err == nil && start.After(s.now()) {
			errs = append(errs, s.deliverTo(ctx, events[i].ID, *r))
		}
	}
	return errors.Join(errs...)
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
		return s.withdraw(ctx, e.ID, "")
	}
	if e.StartsAt == "" {
		unlock := s.eventLock(eventID)
		defer unlock()
		cur, err := s.db.Read().EventByID(ctx, eventID)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if cur.StartsAt != "" {
			return nil
		}
		return s.withdrawTentativeLocked(ctx, cur.ID, "")
	}
	recips, err := s.db.Read().CalendarRecipients(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, rc := range recips {
		errs = append(errs, s.deliverTo(ctx, e.ID, recipient{UserID: rc.UserID, Name: rc.Name, Mode: rc.Mode, Email: rc.ActiveEmail}))
	}
	return errors.Join(errs...)
}

// destState says whether a recipient's destination can be delivered to now.
type destState int

const (
	destNone        destState = iota // nothing to deliver to: no address chosen, Google not connected
	destReady                        // the destination and its transport are both there
	destUnavailable                  // the destination is set but its transport (SMTP, Google) isn't configured
)

// destination names where r's invites go now: "ics:<address>" or the Google account's
// key (store.GoogleToken.Destination), and whether it can be delivered to.
func (s *Server) destination(ctx context.Context, r recipient) (string, destState) {
	if r.Mode == "google" {
		t, err := s.db.Read().GoogleTokenFor(ctx, r.UserID)
		if err != nil {
			return "", destNone
		}
		if s.cfg.Google == nil {
			return t.Destination(), destUnavailable
		}
		return t.Destination(), destReady
	}
	if r.Email == nil {
		return "", destNone
	}
	dest := "ics:" + strings.ToLower(*r.Email)
	if s.cfg.Mailer == nil {
		return dest, destUnavailable
	}
	return dest, destReady
}

// transportMissing is the error for work kept in the outbox because the transport its
// destination needs is not configured on this run (SMTP or Google missing after a
// restart); the job retries with backoff and goes out once it is configured again.
func transportMissing(mode string) error {
	if mode == "google" {
		return errors.New("google calendar sync is not configured on this run; kept for retry")
	}
	return errors.New(".ics email (SMTP) is not configured on this run; kept for retry")
}

// eventLock serialises, per event, a delivery's read-record-send-record step with the
// event's deletion: a delete either happens before a send starts (the send then finds no
// event and stops) or sees the delivery it made and cancels it with a newer SEQUENCE.
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
// a slow relay or a long queue never starves later recipients. It returns an error when
// the destination still needs the event (the send or its record failed), so the job that
// asked for it stays queued.
func (s *Server) deliverTo(ctx context.Context, eventID string, r recipient) error {
	unlock := s.eventLock(eventID)
	defer unlock()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	e, err := s.db.Read().EventByID(ctx, eventID)
	if errors.Is(err, store.ErrNotFound) {
		return nil // deleted while queued; its cancel job withdraws what was sent
	}
	if err != nil {
		return err
	}
	if e.Status == "cancelled" {
		return s.withdrawLocked(ctx, e, r.UserID)
	}
	if e.StartsAt == "" {
		return s.withdrawTentativeLocked(ctx, e.ID, r.UserID)
	}
	p, err := s.planDelivery(ctx, e, r)
	if err != nil || p == nil {
		return err
	}
	if testDeliveryHook != nil {
		testDeliveryHook(e.ID, r.UserID)
	}
	return s.finishDelivery(ctx, p, s.deliverOne(ctx, p.r, p.ev, p.prior))
}

// testDeliveryHook, when a test sets it, runs inside deliverTo just before the send.
var testDeliveryHook func(eventID, userID string)

// plannedDelivery is a send whose attempt is already recorded on its destination row.
type plannedDelivery struct {
	r     recipient
	ev    calendar.Event
	hash  string
	prior *store.Delivery // the row before this attempt; nil for a first send
	row   store.Delivery  // the row with this attempt recorded
}

// planDelivery decides whether r's current destination needs e, and when it does records
// the attempt before anything is sent: the SEQUENCE about to go out and, for a new Google
// event, the event id it will get. It returns nil when nothing is to be sent. The caller
// holds the event lock.
func (s *Server) planDelivery(ctx context.Context, e *store.Event, r recipient) (*plannedDelivery, error) {
	dest, st := s.destination(ctx, r)
	switch st {
	case destNone:
		return nil, nil
	case destUnavailable:
		// A destination that already has deliveries had a working transport: this run is
		// missing it, so keep the work. One that never had any was never set up to receive.
		used, err := s.db.Read().DestinationUsed(ctx, r.UserID, dest)
		if err != nil {
			return nil, err
		}
		if !used {
			return nil, nil
		}
		return nil, transportMissing(r.Mode)
	}
	prior, err := s.db.Read().DeliveryFor(ctx, e.ID, r.UserID, dest)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	ev, err := s.calendarEvent(e, 0, false)
	if err != nil {
		s.log.Warn("calendar: unparseable event time", "event", e.ID)
		return nil, nil
	}
	hash := fingerprint(ev, r)
	if prior != nil && prior.Live() && !prior.Unresolved() && prior.ContentHash != nil && *prior.ContentHash == hash {
		return nil, nil // already on their calendar, unchanged
	}
	// Replaying an unresolved attempt of this same content repeats it exactly, with the
	// SEQUENCE it was recorded with. Anything else is a new message: newer than everything
	// sent there, the unresolved attempt included, since it may have arrived.
	seq := 0
	if prior != nil {
		seq = prior.NextSequence()
		if prior.Replays(hash) {
			seq = *prior.AttemptSeq
		}
	}
	ev.Sequence = seq
	row := store.Delivery{EventID: e.ID, UserID: r.UserID, Destination: dest, Mode: r.Mode, Recipient: r.Email, Status: "pending"}
	if prior != nil {
		row = *prior
	}
	row.AttemptSeq, row.AttemptHash = &seq, &hash
	if r.Mode == "google" && row.GoogleEventID == nil {
		id := calendar.EventID(ev.UID, dest)
		row.GoogleEventID = &id
	}
	row.UpdatedAt = s.nowMs()
	if err := s.db.Write(ctx, func(q *store.Queries) error { return q.SaveDelivery(ctx, row) }); err != nil {
		return nil, fmt.Errorf("record calendar attempt: %w", err)
	}
	return &plannedDelivery{r: r, ev: ev, hash: hash, prior: prior, row: row}, nil
}

// finishDelivery records the outcome of a planned send and clears its attempt.
func (s *Server) finishDelivery(ctx context.Context, p *plannedDelivery, out deliveryResult) error {
	d := p.row
	seq := *d.AttemptSeq
	d.AttemptSeq, d.AttemptHash, d.UpdatedAt = nil, nil, s.nowMs()
	var sendErr error
	switch out.Status {
	case "sent":
		d.Mode, d.Recipient, d.Sequence, d.Status, d.Cancelled, d.Error, d.ContentHash = p.r.Mode, p.r.Email, seq, "sent", false, nil, &p.hash
		if out.GoogleEventID != "" {
			id := out.GoogleEventID
			d.GoogleEventID = &id
		}
	case "failed":
		// A failed update leaves the last success (destination, Google id, SEQUENCE, content)
		// in place, so a later delete still cancels it; only the error is new. A failed
		// send doesn't consume a SEQUENCE.
		d.Error = &out.Message
		if d.Status == "pending" {
			d.Status = "failed"
		}
		if !out.Revoked {
			sendErr = errors.New(out.Message)
		}
	default:
		// Nothing went out (not configured, not connected): put the row back as it was.
		if p.prior == nil {
			return s.db.Write(ctx, func(q *store.Queries) error { return q.DeleteDelivery(ctx, d.EventID, d.UserID, d.Destination) })
		}
		d = *p.prior
	}
	if err := s.db.Write(ctx, func(q *store.Queries) error { return q.SaveDelivery(ctx, d) }); err != nil {
		s.log.Error("save calendar delivery", "error", err)
		return errors.Join(sendErr, fmt.Errorf("record calendar delivery: %w", err))
	}
	return sendErr
}

// sendCancel withdraws an event from one destination that has (or may have) it, with a
// SEQUENCE newer than anything sent there. A Google destination is reachable only while
// that same account is still connected. When the transport the destination needs isn't
// configured on this run, the cancellation fails, so its job keeps it for later.
func (s *Server) sendCancel(ctx context.Context, e *store.Event, d store.Delivery) deliveryResult {
	ev, err := s.calendarEvent(e, d.NextSequence(), true)
	if err != nil {
		return deliveryResult{Status: "skipped", Message: "unparseable event time"}
	}
	r := recipient{UserID: d.UserID, Mode: d.Mode, Email: d.Recipient}
	cur, st := s.destination(ctx, r)
	if d.Mode == "google" {
		if d.GoogleEventID == nil {
			return deliveryResult{Status: "skipped", Message: "no Google event to cancel"}
		}
		if st == destNone || cur != d.Destination {
			return deliveryResult{Status: "skipped", Message: "that Google account is no longer connected"}
		}
	}
	if st == destUnavailable {
		return deliveryResult{Status: "failed", Message: transportMissing(d.Mode).Error()}
	}
	return s.deliverOne(ctx, r, ev, &d)
}

// withdraw cancels an event marked cancelled on every destination that still shows it,
// for one officer or (userID "") everyone.
func (s *Server) withdraw(ctx context.Context, eventID, userID string) error {
	unlock := s.eventLock(eventID)
	defer unlock()
	e, err := s.db.Read().EventByID(ctx, eventID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.withdrawLocked(ctx, e, userID)
}

// withdrawLocked does withdraw's work under the event lock. It returns an error while a
// destination still shows the event (its cancellation or the record of it failed).
func (s *Server) withdrawLocked(ctx context.Context, e *store.Event, userID string) error {
	if e.StartsAt == "" {
		return s.withdrawTentativeLocked(ctx, e.ID, userID)
	}
	rows, err := s.db.Read().DeliveriesForEvent(ctx, e.ID, userID)
	if err != nil {
		return fmt.Errorf("calendar withdraw: deliveries: %w", err)
	}
	var errs []error
	for _, d := range rows {
		if !d.NeedsCancel() {
			continue
		}
		dctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		out := s.sendCancel(dctx, e, d)
		cancel()
		switch out.Status {
		case "sent":
			seq := d.NextSequence()
			ev, _ := s.calendarEvent(e, seq, true)
			hash := fingerprint(ev, recipient{Mode: d.Mode, Email: d.Recipient})
			d.Status, d.Sequence, d.AttemptSeq, d.AttemptHash, d.Cancelled, d.Error, d.ContentHash = "sent", seq, nil, nil, true, nil, &hash
		case "failed":
			d.Error = &out.Message
			s.log.Warn("calendar cancellation failed", "event", e.ID, "user", d.UserID, "error", out.Message)
			if !out.Revoked {
				errs = append(errs, errors.New(out.Message))
			}
		default:
			continue
		}
		d.UpdatedAt = s.nowMs()
		if err := s.db.Write(ctx, func(q *store.Queries) error { return q.SaveDelivery(ctx, d) }); err != nil {
			s.log.Error("save calendar delivery", "error", err)
			errs = append(errs, fmt.Errorf("record calendar cancellation: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (s *Server) withdrawTentativeLocked(ctx context.Context, id, userID string) error {
	fixed, err := s.db.Read().LastFixedCalendarEvent(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if fixed.StartsAt == "" {
		return nil
	}
	return s.withdrawLocked(ctx, fixed, userID)
}

// cancelDeleted sends a CANCEL (.ics) or cancels the Google event on each destination a
// deleted event reached. It returns the destinations still to retry, and records progress
// after each success so a restart doesn't repeat finished ones; a failure to record that
// progress is an error too, so the job is retried rather than considered done.
func (s *Server) cancelDeleted(ctx context.Context, j *store.CalendarJob) ([]store.Delivery, error) {
	if j.Snapshot == nil {
		return nil, nil
	}
	e := j.Snapshot.Event()
	left := append([]store.Delivery(nil), j.Priors...)
	var errs []error
	for i := 0; i < len(left); {
		dctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		out := s.sendCancel(dctx, e, left[i])
		cancel()
		if out.Status == "failed" {
			s.log.Warn("calendar cancellation failed", "event", e.ID, "user", left[i].UserID, "error", out.Message)
			errs = append(errs, errors.New(out.Message))
			i++
			continue
		}
		left = append(left[:i], left[i+1:]...)
		rest := append([]store.Delivery{}, left...)
		if err := s.db.Write(ctx, func(q *store.Queries) error {
			return q.UpdateCalendarJob(ctx, j.ID, rest, j.Attempts, j.LastError, j.NextAttemptAt)
		}); err != nil {
			s.log.Error("calendar outbox: record progress", "job", j.ID, "error", err)
			errs = append(errs, fmt.Errorf("record cancellation progress: %w", err))
		}
	}
	return left, errors.Join(errs...)
}
