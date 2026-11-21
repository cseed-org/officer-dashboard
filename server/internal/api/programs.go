package api

import (
	"encoding/json"
	"strings"
	"time"

	"officerdashboard/internal/auth"
	"officerdashboard/internal/store"
)

func (c *Ctx) programTracks() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	out, err := c.S.db.Read().ProgramTracks(c.Context())
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"tracks": out})
	return nil
}
func (c *Ctx) programTrackCreate() error {
	if _, err := c.requireLeadership(); err != nil {
		return err
	}
	v := store.ProgramTrack{ID: auth.UUID()}
	if err := c.mergeOrgJSON(&v); err != nil {
		return err
	}
	v.ID = auth.UUID()
	if len(v.Key) < 1 || len(v.Key) > 64 || len(v.Name) < 1 || len(v.Name) > 200 || len(v.Hue) < 1 || len(v.Hue) > 30 {
		return badRequest("Track key, name and hue are required")
	}
	err := c.S.db.Write(c.Context(), func(q *store.Queries) error {
		if _, err := c.requireLeadershipIn(q); err != nil {
			return err
		}
		if err := q.SaveProgramTrack(c.Context(), v); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "program_track.created", "program_track", v.ID, nil, v)
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"track": v})
	return nil
}
func (c *Ctx) programsList() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	out, err := c.S.db.Read().Programs(c.Context())
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"programs": out})
	return nil
}
func validateProgram(p store.Program) error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 200 || len(p.Description) > 10000 || len(p.Location) > 500 || p.Weekday < 0 || p.Weekday > 6 || p.DurationMin < 1 || p.DurationMin > 1440 {
		return badRequest("Invalid program fields")
	}
	if !orgDate(p.StartsOn) || !orgDate(p.EndsOn) || p.EndsOn < p.StartsOn {
		return badRequest("Program dates must be an increasing date range")
	}
	if _, err := time.Parse("15:04", p.StartTime); err != nil {
		return badRequest("startTime must be HH:MM")
	}
	a, _ := time.Parse("2006-01-02", p.StartsOn)
	b, _ := time.Parse("2006-01-02", p.EndsOn)
	if b.Sub(a) > 5*366*24*time.Hour {
		return badRequest("Program range must be at most five years")
	}
	return nil
}
func (c *Ctx) programWrite() error {
	// Buildspace project editing permits any officer; programs use that same permission.
	if _, err := c.officer(); err != nil {
		return err
	}
	id := c.R.PathValue("id")
	create := id == ""
	if create {
		id = auth.UUID()
	}
	raw, err := readBody(c, 1<<20)
	if err != nil {
		return err
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return badRequest("Expected JSON object")
	}
	for k := range obj {
		switch k {
		case "trackId", "name", "weekday", "startTime", "durationMin", "location", "startsOn", "endsOn", "description":
		default:
			return badRequest("Unknown field: " + k)
		}
	}
	var out *store.Program
	err = c.S.db.Write(c.Context(), func(q *store.Queries) error {
		p := &store.Program{ID: id, CreatedAt: c.S.nowMs(), Exceptions: []store.ProgramException{}}
		if !create {
			var err error
			p, err = q.Program(c.Context(), id)
			if err != nil {
				return err
			}
		}
		before := *p
		if json.Unmarshal(raw, p) != nil {
			return badRequest("Invalid field type")
		}
		p.UpdatedAt = c.S.nowMs()
		if err := validateProgram(*p); err != nil {
			return err
		}
		tracks, err := q.ProgramTracks(c.Context())
		if err != nil {
			return err
		}
		found := false
		for _, t := range tracks {
			found = found || t.ID == p.TrackID
		}
		if !found {
			return badRequest("Unknown program track")
		}
		if err = q.SaveProgram(c.Context(), *p); err != nil {
			return err
		}
		if err = c.S.projectProgram(c.Context(), q, *p); err != nil {
			return err
		}
		if err = c.audit(c.Context(), q, "program.saved", "program", id, before, p); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return err
	}
	c.S.kickOrgCalendar()
	writeJSON(c.W, 200, map[string]any{"program": out})
	return nil
}
func (c *Ctx) programDelete() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	id := c.R.PathValue("id")
	err := c.S.db.Write(c.Context(), func(q *store.Queries) error {
		p, err := q.Program(c.Context(), id)
		if err != nil {
			return err
		}
		if err = q.DeleteOrgSource(c.Context(), "program", id, c.S.nowMs()); err != nil {
			return err
		}
		if err = q.DeleteProgram(c.Context(), id); err != nil {
			return err
		}
		return c.audit(c.Context(), q, "program.deleted", "program", id, p, nil)
	})
	if err != nil {
		return err
	}
	c.S.kickOrgCalendar()
	writeJSON(c.W, 200, map[string]any{"ok": true})
	return nil
}
func (c *Ctx) programExceptionWrite() error {
	if _, err := c.officer(); err != nil {
		return err
	}
	id := c.R.PathValue("id")
	v := store.ProgramException{ProgramID: id}
	deleting := c.R.Method == "DELETE"
	if deleting {
		v.OnDate = c.R.URL.Query().Get("onDate")
	} else {
		if err := c.mergeOrgJSON(&v); err != nil {
			return err
		}
		v.ProgramID = id
	}
	if !orgDate(v.OnDate) {
		return badRequest("onDate is required (YYYY-MM-DD)")
	}
	if !deleting {
		if v.Kind != "skip" && v.Kind != "move" {
			return badRequest("Exception kind must be skip or move")
		}
		if v.Kind == "move" {
			if v.NewStartsAt == nil || v.NewEndsAt == nil || !orgInstant(*v.NewStartsAt) || !orgInstant(*v.NewEndsAt) {
				return badRequest("Moved session needs start and end instants")
			}
			a, _ := time.Parse(time.RFC3339Nano, *v.NewStartsAt)
			b, _ := time.Parse(time.RFC3339Nano, *v.NewEndsAt)
			if !b.After(a) {
				return badRequest("End must follow start")
			}
		}
	}
	var out *store.Program
	err := c.S.db.Write(c.Context(), func(q *store.Queries) error {
		p, err := q.Program(c.Context(), id)
		if err != nil {
			return err
		}
		d, _ := time.Parse("2006-01-02", v.OnDate)
		if v.OnDate < p.StartsOn || v.OnDate > p.EndsOn || int(d.Weekday()) != p.Weekday {
			return badRequest("onDate must be a scheduled program session")
		}
		if deleting {
			err = q.DeleteProgramException(c.Context(), id, v.OnDate)
		} else {
			err = q.SaveProgramException(c.Context(), v)
		}
		if err != nil {
			return err
		}
		p, err = q.Program(c.Context(), id)
		if err != nil {
			return err
		}
		if err = c.S.projectProgram(c.Context(), q, *p); err != nil {
			return err
		}
		if err = c.audit(c.Context(), q, "program.exception", "program", id, nil, v); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return err
	}
	c.S.kickOrgCalendar()
	writeJSON(c.W, 200, map[string]any{"program": out})
	return nil
}
