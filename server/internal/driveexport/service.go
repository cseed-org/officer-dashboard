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
	"officerdashboard/internal/store"
)

var ErrBusy = errors.New("a Drive export is already running")

type Service struct {
	DB    *store.DB
	Drive *files.Drive
	Log   *slog.Logger
	mu    sync.Mutex
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
		if err := s.Run(ctx); err != nil && ctx.Err() == nil {
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
	Event  store.Event
	Record eventRecord
	Cover  *store.Cover
}

// Run takes one consistent read snapshot, closes the transaction before network
// work, and records each successful upload separately so failed passes can resume.
func (s *Service) Run(ctx context.Context) error {
	if !s.mu.TryLock() {
		return ErrBusy
	}
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var events []eventSnapshot
	var tracks []trackRecord
	var states map[string]store.DriveExportState
	err := s.DB.ReadTx(ctx, func(q *store.Queries) error {
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
				total.TotalCents += p.TotalCents
				r.Expenses[p.Status] = total
			}
			cover, err := q.EventCover(ctx, e.ID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			events = append(events, eventSnapshot{e, r, cover})
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
	for _, e := range events {
		var ms *int64
		if t, err := luma.ParseInstant(e.Event.StartsAt); err == nil {
			v := t.UnixMilli()
			ms = &v
		}
		if _, err := files.EventFolder(ctx, s.Drive, ms, e.Event.Name, e.Event.ID); err != nil {
			return err
		}
		folder := []string{"Events", files.EventFolderName(ms, e.Event.Name, e.Event.ID)}
		for _, name := range []string{"event.json", "summary.md"} {
			mime := "application/json"
			if name == "summary.md" {
				mime = "text/markdown"
			}
			render := func(at int64) ([]byte, error) { r := e.Record; r.ExportedAt = at; return renderEvent(r, name) }
			if err := s.upload(ctx, states, "Events/"+e.Event.ID+"/"+name, folder, name, mime, render); err != nil {
				return err
			}
		}
		if c := e.Cover; c != nil {
			ext, ok := coverExtensions[c.MimeType]
			if !ok {
				return fmt.Errorf("unsupported event cover MIME type")
			}
			name := "cover." + ext
			if err := s.upload(ctx, states, "Events/"+e.Event.ID+"/"+name, folder, name, c.MimeType, func(int64) ([]byte, error) { return c.Bytes, nil }); err != nil {
				return err
			}
		}
	}
	for _, t := range tracks {
		name := files.SafeName(t.Name)
		if name == "" {
			name = files.SafeName(t.ID)
		}
		if err := s.upload(ctx, states, "Programs/"+t.ID+"/program.json", []string{"Programs", name}, "program.json", "application/json", func(at int64) ([]byte, error) { t.ExportedAt = at; return json.MarshalIndent(t, "", "  ") }); err != nil {
			return err
		}
	}
	return nil
}

var coverExtensions = map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp", "image/heic": "heic"}

// Render using the last successful export time first. A changed document gets a
// fresh time and hash; unchanged bytes keep their time and cause no upload.
func (s *Service) upload(ctx context.Context, states map[string]store.DriveExportState, key string, folder []string, name, mime string, render func(int64) ([]byte, error)) error {
	previous := states[key]
	body, err := render(previous.ExportedAt)
	if err != nil {
		return err
	}
	hash := func(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }
	if previous.SHA256 == hash(body) {
		return nil
	}
	now := time.Now().UTC().UnixMilli()
	body, err = render(now)
	if err != nil {
		return err
	}
	entry, err := s.Drive.Upsert(ctx, files.UpsertInput{Folder: folder, Name: name, MimeType: mime, Body: body, MaxBytes: 16 << 20, AllowedMIME: []string{mime}})
	if err != nil {
		return err
	}
	return s.DB.Write(ctx, func(q *store.Queries) error {
		return q.SaveDriveExportState(ctx, store.DriveExportState{Path: key, DriveFileID: entry.ID, SHA256: hash(body), ExportedAt: now})
	})
}
