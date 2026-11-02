package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
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
	sep := "?"
	for _, ch := range path {
		if ch == '?' {
			sep = "&"
		}
	}
	return path + sep + key + "=" + value
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
		if prior != nil && prior.Mode == "google" && prior.GoogleEventID != nil {
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
		when := ev.Start.In(pacific()).Format("Mon Jan 2, 3:04 PM MST")
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

// SyncEventToCalendars sends a created or changed event to every active officer, in the
// background. Event pages call it after their write commits; deletes pass cancelled.
func (s *Server) SyncEventToCalendars(e store.Event, cancelled bool) { s.fanOut(&e, cancelled, "") }

// backfillCalendar sends upcoming events to one officer after they change delivery.
func (s *Server) backfillCalendar(userID string) {
	if s.cfg.Mailer == nil && s.cfg.Google == nil {
		return
	}
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		events, err := s.db.Read().Events(ctx)
		if err != nil {
			return
		}
		for i := range events {
			if start, _, err := calendar.Window(events[i].StartsAt, events[i].EndsAt); err == nil && start.After(s.now()) {
				s.deliverEvent(ctx, &events[i], false, userID)
			}
		}
	}()
}

func (s *Server) fanOut(e *store.Event, cancelled bool, onlyUser string) {
	if s.cfg.Mailer == nil && s.cfg.Google == nil {
		return
	}
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		s.deliverEvent(ctx, e, cancelled, onlyUser)
	}()
}

func (s *Server) deliverEvent(ctx context.Context, e *store.Event, cancelled bool, onlyUser string) {
	s.calMu.Lock()
	defer s.calMu.Unlock()
	recips, err := s.db.Read().CalendarRecipients(ctx)
	if err != nil {
		s.log.Error("calendar recipients", "error", err)
		return
	}
	for _, rc := range recips {
		if onlyUser != "" && rc.UserID != onlyUser {
			continue
		}
		r := recipient{UserID: rc.UserID, Name: rc.Name, Mode: rc.Mode, Email: rc.ActiveEmail}
		prior, err := s.db.Read().DeliveryFor(ctx, e.ID, rc.UserID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			continue
		}
		if cancelled && (prior == nil || prior.Status != "sent") {
			continue
		}
		seq := 0
		if prior != nil {
			seq = prior.Sequence + 1
		}
		if r.Mode == "ics" && prior != nil && prior.Mode == "ics" && prior.Recipient != nil && r.Email != nil && *prior.Recipient != *r.Email && cancelled {
			r.Email = prior.Recipient // cancel where the invite went
		}
		ev, err := s.calendarEvent(e, seq, cancelled)
		if err != nil {
			s.log.Warn("calendar: unparseable event time", "event", e.ID)
			return
		}
		out := s.deliverOne(ctx, r, ev, prior)
		status, msg := out.Status, out.Message
		if status != "sent" && status != "failed" {
			continue
		}
		d := store.Delivery{EventID: e.ID, UserID: r.UserID, Mode: r.Mode, Recipient: r.Email, Sequence: seq, Status: status, UpdatedAt: s.nowMs()}
		if prior != nil && prior.Mode == "google" {
			d.GoogleEventID = prior.GoogleEventID
		}
		if out.GoogleEventID != "" {
			id := out.GoogleEventID
			d.GoogleEventID = &id
		}
		if status == "failed" {
			d.Error = &msg
		}
		if err := s.db.Write(ctx, func(q *store.Queries) error { return q.SaveDelivery(ctx, d) }); err != nil {
			s.log.Error("save calendar delivery", "error", err)
		}
	}
}
