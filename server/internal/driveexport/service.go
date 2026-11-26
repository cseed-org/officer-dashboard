// Package driveexport writes readable, allow-listed dashboard records to Drive.
package driveexport

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"officerdashboard/internal/files"
	"officerdashboard/internal/luma"
	"officerdashboard/internal/notify"
	"officerdashboard/internal/store"
)

var ErrBusy = errors.New("a Drive export is already running")

type Service struct {
	DB *store.DB
	// Drive resolves the active connection once per pass, including connections
	// made in Settings after the dashboard started.
	Drive   func(context.Context) (*files.Drive, error)
	Alerts  *notify.Alerter
	Log     *slog.Logger
	Timeout time.Duration // zero uses the ten-minute pass limit
	mu      sync.Mutex
	// lastCoverSweep is when every live event folder was last listed for stale
	// covers; guarded by mu. Zero (a fresh process) sweeps on the first pass.
	lastCoverSweep time.Time
}

// Hourly delays the first pass to let startup settle. Every failure, including
// quota and throttling after the Drive client's retries, waits for the next hour.
func (s *Service) Hourly(ctx context.Context) {
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if err := s.Run(ctx); err != nil && ctx.Err() == nil && !disconnected(err) {
			log := s.Log
			if log == nil {
				log = slog.Default()
			}
			log.Warn("Drive readable export failed; retrying next hour", "error", err)
		}
		timer.Reset(time.Hour)
	}
}

type eventSnapshot struct {
	Event      store.Event
	Record     eventRecord
	CoverKnown bool
	CoverID    string
}

// Run snapshots text and metadata before network work. Changed covers use their
// own version-checked read transaction. Each successful upload is saved separately.
func (s *Service) Run(ctx context.Context) (err error) {
	if !s.mu.TryLock() {
		return ErrBusy
	}
	defer s.mu.Unlock()
	parent := ctx
	limit := s.Timeout
	if limit <= 0 {
		limit = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	defer func() {
		if err == nil || parent.Err() != nil || disconnected(err) {
			return
		}
		key, msg := "drive-export", "Google Drive readable event/program export failed; the next hourly pass will retry. Check the dashboard logs."
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			msg = "Google Drive readable export timed out; completed files are saved and the next hourly pass will retry."
		case errors.Is(err, files.ErrQuota):
			key, msg = "drive-quota-full", "Google Drive refused a readable dashboard export: the shared drive is out of storage. Free space; the next hourly export will retry."
		case errors.Is(err, files.ErrRateLimited):
			key, msg = "drive-rate-limited", "Google Drive is rate limiting the dashboard; a readable export failed after retries. The next hourly export will retry."
		}
		s.Alerts.Alert(parent, key, msg)
	}()
	drive, err := s.Drive(ctx)
	if err != nil {
		return err
	}
	var events []eventSnapshot
	var tracks []trackRecord
	var states map[string]store.DriveExportState
	err = s.DB.ReadTx(ctx, func(q *store.Queries) error {
		var err error
		states, err = q.DriveExportStates(ctx)
		if err != nil {
			return err
		}
		all, err := q.Events(ctx)
		if err != nil {
			return err
		}
		counts, err := q.EventCounts(ctx, "")
		if err != nil {
			return err
		}
		designs, err := q.DesignRequests(ctx)
		if err != nil {
			return err
		}
		users, err := q.Users(ctx)
		if err != nil {
			return err
		}
		teams, err := q.Subteams(ctx)
		if err != nil {
			return err
		}
		names := map[string]string{}
		for _, u := range users {
			names[u.ID] = u.Name
		}
		subteams := map[string]string{}
		for _, t := range teams {
			subteams[t.ID] = t.Name
		}
		for _, e := range all {
			r := eventRecord{Schema: 1, ID: e.ID, Name: e.Name, Status: e.Status, StartsAt: e.StartsAt, EndsAt: e.EndsAt, TentativeFrom: e.TentativeFrom, TentativeTo: e.TentativeTo, Location: e.Location, URL: e.URL, LumaURL: e.LumaURL, NotionURL: e.NotionURL, Description: e.Description, Capacity: e.Capacity, DesignNeed: e.DesignNeed, DesignRequests: []designRecord{}, Expenses: map[string]expenseRecord{}}
			if e.HostUserID != nil {
				r.Host.Name = names[*e.HostUserID]
			}
			if e.HostSubteamID != nil {
				r.Host.Subteam = subteams[*e.HostSubteamID]
			}
			if c := counts[e.ID]; c != nil {
				r.Guests = guestRecord{c.Registered, c.CheckedIn, c.Reflect}
			}
			for _, d := range designs {
				if d.EventID != nil && *d.EventID == e.ID {
					r.DesignRequests = append(r.DesignRequests, designRecord{d.ID, d.Title, d.Type, d.Stage, d.DueDate, "Design/" + d.ID})
				}
			}
			requests, err := q.RequestsByEvent(ctx, e.ID)
			if err != nil {
				return err
			}
			for _, p := range requests {
				total := r.Expenses[p.Status]
				total.Count++
				total.EstimatedTotalCents += p.TotalCents
				if p.ActualTotalCents != nil {
					actual := int64(0)
					if total.ActualTotalCents != nil {
						actual = *total.ActualTotalCents
					}
					actual += *p.ActualTotalCents
					total.ActualTotalCents = &actual
					total.ActualCount++
				}
				r.Expenses[p.Status] = total
			}
			events = append(events, eventSnapshot{Event: e, Record: r})
		}
		allTracks, err := q.ProgramTracks(ctx)
		if err != nil {
			return err
		}
		programs, err := q.Programs(ctx)
		if err != nil {
			return err
		}
		for _, t := range allTracks {
			r := trackRecord{Schema: 1, ID: t.ID, Key: t.Key, Name: t.Name, Hue: t.Hue, Programs: []programRecord{}}
			for _, p := range programs {
				if p.TrackID == t.ID {
					pr := programRecord{ID: p.ID, Name: p.Name, Weekday: p.Weekday, StartTime: p.StartTime, DurationMin: p.DurationMin, Location: p.Location, StartsOn: p.StartsOn, EndsOn: p.EndsOn, Description: p.Description, Exceptions: []exceptionRecord{}}
					for _, x := range p.Exceptions {
						pr.Exceptions = append(pr.Exceptions, exceptionRecord{x.OnDate, x.Kind, x.NewStartsAt, x.NewEndsAt, x.NewLocation})
					}
					r.Programs = append(r.Programs, pr)
				}
			}
			tracks = append(tracks, r)
		}
		return nil
	})
	if err != nil {
		return err
	}
	var failures []error
	for i := range events {
		if err := s.exportEvent(ctx, drive, states, &events[i]); err != nil {
			failures = append(failures, err)
			if stopPass(err) {
				return errors.Join(failures...)
			}
		}
	}

	for _, t := range tracks {
		// Keys are unique and stable; display names may collide or change. Keep
		// the exact key as one Drive name component rather than sanitizing two
		// distinct keys into the same name.
		name := t.Key
		if err := s.upload(ctx, drive, states, "Programs/"+t.ID+"/program.json", []string{"Programs", name}, "program.json", "application/json", func(at int64) ([]byte, error) { t.ExportedAt = at; return json.MarshalIndent(t, "", "  ") }); err != nil {
			failures = append(failures, err)
			if stopPass(err) {
				return errors.Join(failures...)
			}
		}
	}
	if err := s.cleanup(ctx, drive, states, events); err != nil {
		failures = append(failures, err)
		if stopPass(err) {
			return errors.Join(failures...)
		}
	}
	return errors.Join(failures...)
}

// A cover source marker is metadata, separate from the exact-byte file hash.
// EventCover is loaded only for a new version, and released after this event.
func coverVersion(at int64) string { return digest([]byte(fmt.Sprintf("cover-v1:%d", at))) }

func disconnected(err error) bool {
	return errors.Is(err, files.ErrNotConnected) || errors.Is(err, files.ErrReconnect)
}

func eventStart(e store.Event) *int64 {
	if t, err := luma.ParseInstant(e.StartsAt); err == nil {
		v := t.UnixMilli()
		return &v
	}
	return nil
}

type pendingFile struct {
	State      store.DriveExportState
	Name, MIME string
	Body       []byte
}

func stateKey(d *files.Drive, key string) string { return "Drive/" + d.RootID() + "/" + key }
func digest(body []byte) string                  { return fmt.Sprintf("%x", sha256.Sum256(body)) }

var coverExtensions = map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp", "image/heic": "heic"}

// Render using the last successful export time first. A changed document gets a
// fresh time and hash; unchanged bytes keep their time and cause no upload.
func (s *Service) upload(ctx context.Context, drive *files.Drive, states map[string]store.DriveExportState, key string, folder []string, name, mime string, render func(int64) ([]byte, error)) error {
	// The root is part of the destination identity. Switching folders in Settings
	// must export every file there even when its source content did not change.
	p, err := prepare(states, stateKey(drive, key), name, mime, render)
	if err != nil || p == nil {
		return err
	}
	return s.uploadPrepared(ctx, drive, states, folder, p)
}

func prepare(states map[string]store.DriveExportState, key, name, mime string, render func(int64) ([]byte, error)) (*pendingFile, error) {
	previous := states[key]
	body, err := render(previous.ExportedAt)
	if err != nil {
		return nil, err
	}
	if previous.SHA256 == digest(body) {
		return nil, nil
	}
	now := time.Now().UTC().UnixMilli()
	body, err = render(now)
	if err != nil {
		return nil, err
	}
	return &pendingFile{State: store.DriveExportState{Path: key, SHA256: digest(body), ExportedAt: now}, Name: name, MIME: mime, Body: body}, nil
}

func (s *Service) uploadPrepared(ctx context.Context, drive *files.Drive, states map[string]store.DriveExportState, folder []string, p *pendingFile) error {
	entry, err := drive.Upsert(ctx, files.UpsertInput{Folder: folder, Name: p.Name, MimeType: p.MIME, Body: p.Body, MaxBytes: 16 << 20, AllowedMIME: []string{p.MIME}})
	if err != nil {
		return err
	}
	p.State.DriveFileID = entry.ID
	err = s.DB.Write(ctx, func(q *store.Queries) error { return q.SaveDriveExportState(ctx, p.State) })
	if err == nil {
		states[p.State.Path] = p.State
	}
	return err
}

func (s *Service) exportEvent(ctx context.Context, drive *files.Drive, states map[string]store.DriveExportState, e *eventSnapshot) error {
	e.CoverKnown = e.Event.CoverUpdatedAt == nil
	marker := states[stateKey(drive, "Events/"+e.Event.ID+"/cover.source")]
	if e.Event.CoverUpdatedAt != nil && marker.SHA256 == coverVersion(*e.Event.CoverUpdatedAt) && marker.DriveFileID != "" {
		e.CoverKnown = true
		e.CoverID = marker.DriveFileID
	}
	var pending []*pendingFile
	var coverSource *store.DriveExportState
	var coverPath string
	folder := []string{"Events", files.EventFolderName(eventStart(e.Event), e.Event.Name, e.Event.ID)}
	for _, name := range []string{"event.json", "summary.md"} {
		mime := "application/json"
		if name == "summary.md" {
			mime = "text/markdown"
		}
		render := func(at int64) ([]byte, error) { r := e.Record; r.ExportedAt = at; return renderEvent(r, name) }
		p, err := prepare(states, stateKey(drive, "Events/"+e.Event.ID+"/"+name), name, mime, render)
		if err != nil {
			return err
		}
		if p != nil {
			pending = append(pending, p)
		}
	}
	if e.Event.CoverUpdatedAt != nil && !e.CoverKnown {
		var c *store.Cover
		err := s.DB.ReadTx(ctx, func(q *store.Queries) (err error) { c, err = q.EventCover(ctx, e.Event.ID); return })
		if err != nil {
			return err
		}
		if c.UpdatedAt != *e.Event.CoverUpdatedAt {
			return errors.New("event cover changed during export")
		}
		ext, ok := coverExtensions[c.MimeType]
		if !ok {
			e.CoverKnown = true // Unsupported replacement: no old cover may remain live.
			return fmt.Errorf("unsupported event cover MIME type")
		}
		name := "cover." + ext
		coverPath = stateKey(drive, "Events/"+e.Event.ID+"/"+name)
		p, err := prepare(states, coverPath, name, c.MimeType, func(int64) ([]byte, error) { return c.Bytes, nil })
		if err != nil {
			return err
		}
		if p != nil {
			pending = append(pending, p)
		}
		coverSource = &store.DriveExportState{Path: stateKey(drive, "Events/"+e.Event.ID+"/cover.source"), SHA256: coverVersion(c.UpdatedAt), ExportedAt: time.Now().UnixMilli()}
	}
	if len(pending) > 0 {
		if _, err := files.EventFolder(ctx, drive, eventStart(e.Event), e.Event.Name, e.Event.ID); err != nil {
			return err
		}
	}
	for _, p := range pending {
		if err := s.uploadPrepared(ctx, drive, states, folder, p); err != nil {
			return err
		}
	}
	if coverSource != nil {
		coverSource.DriveFileID = states[coverPath].DriveFileID
		if err := s.DB.Write(ctx, func(q *store.Queries) error { return q.SaveDriveExportState(ctx, *coverSource) }); err != nil {
			return err
		}
		states[coverSource.Path] = *coverSource
		e.CoverKnown = true
		e.CoverID = coverSource.DriveFileID
	}

	return nil
}

func stopPass(err error) bool {
	return disconnected(err) || errors.Is(err, files.ErrQuota) || errors.Is(err, files.ErrRateLimited) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
