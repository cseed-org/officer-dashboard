// Package orgcal implements Google Calendar v3 for the organization's four calendars.
// OAuth and token sealing are shared with the existing per-officer calendar client.
package orgcal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"officerdashboard/internal/calendar"
)

const Scope = "openid email https://www.googleapis.com/auth/calendar"

type Client struct{ OAuth *calendar.Google }
type HTTPError struct{ Status int }

func (e *HTTPError) Error() string        { return fmt.Sprintf("org Google calendar: HTTP %d", e.Status) }
func IsStatus(err error, status int) bool { e, ok := err.(*HTTPError); return ok && e.Status == status }
func (c Client) ConsentURL(state, challenge string) string {
	u, _ := url.Parse(c.OAuth.ConsentURL(state, challenge))
	v := u.Query()
	v.Set("scope", Scope)
	u.RawQuery = v.Encode()
	return u.String()
}
func (c Client) Request(ctx context.Context, token, method, path string, body, out any) error {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.OAuth.APIBase+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	h := c.OAuth.HTTP
	if h == nil {
		h = http.DefaultClient
	}
	res, err := h.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == 401 {
		return calendar.ErrGoogleRevoked
	}
	if res.StatusCode/100 != 2 {
		return &HTTPError{res.StatusCode}
	}
	if out != nil {
		if err = json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out); err != nil {
			return fmt.Errorf("org calendar response: %w", err)
		}
	}
	return nil
}
func CalendarPath(id string) string { return "/calendars/" + url.PathEscape(id) }

type Time struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}
type Attendee struct {
	Email string `json:"email"`
}
type Reminder struct {
	Method  string `json:"method"`
	Minutes int    `json:"minutes"`
}
type Reminders struct {
	UseDefault bool       `json:"useDefault"`
	Overrides  []Reminder `json:"overrides"`
}
type Event struct {
	ID                string     `json:"id,omitempty"`
	ETag              string     `json:"etag,omitempty"`
	Summary           string     `json:"summary"`
	Description       string     `json:"description"`
	Location          string     `json:"location"`
	Status            string     `json:"status"`
	Start             Time       `json:"start"`
	End               Time       `json:"end"`
	Recurrence        []string   `json:"recurrence"`
	Attendees         []Attendee `json:"attendees"`
	Reminders         Reminders  `json:"reminders"`
	RecurringEventID  string     `json:"recurringEventId,omitempty"`
	OriginalStartTime *Time      `json:"originalStartTime,omitempty"`
}
type EventPage struct {
	Items         []Event `json:"items"`
	NextPageToken string  `json:"nextPageToken"`
	NextSyncToken string  `json:"nextSyncToken"`
}

func (c Client) Events(ctx context.Context, token, calID, syncToken string) ([]Event, string, error) {
	all := []Event{}
	page := ""
	for {
		v := url.Values{"showDeleted": {"true"}, "maxResults": {"2500"}, "singleEvents": {"false"}}
		if syncToken != "" {
			v.Set("syncToken", syncToken)
		}
		if page != "" {
			v.Set("pageToken", page)
		}
		var out EventPage
		if err := c.Request(ctx, token, "GET", CalendarPath(calID)+"/events?"+v.Encode(), nil, &out); err != nil {
			return nil, "", err
		}
		all = append(all, out.Items...)
		if out.NextPageToken == "" {
			return all, out.NextSyncToken, nil
		}
		page = out.NextPageToken
	}
}
func (c Client) Upsert(ctx context.Context, token, calID, id, newID string, e Event, invites bool) (Event, error) {
	v := url.Values{"sendUpdates": {"none"}}
	if invites {
		v.Set("sendUpdates", "all")
	}
	base := CalendarPath(calID) + "/events"
	var out Event
	if id != "" {
		e.ID = ""
		err := c.Request(ctx, token, "PATCH", base+"/"+url.PathEscape(id)+"?"+v.Encode(), e, &out)
		if err == nil {
			if out.ID == "" {
				out.ID = id
			}
			return out, nil
		}
		if !IsStatus(err, 404) && !IsStatus(err, 410) {
			return out, err
		}
	}
	e.ID = newID
	err := c.Request(ctx, token, "POST", base+"?"+v.Encode(), e, &out)
	if IsStatus(err, 409) {
		e.ID = ""
		err = c.Request(ctx, token, "PATCH", base+"/"+url.PathEscape(newID)+"?"+v.Encode(), e, &out)
	}
	if out.ID == "" {
		out.ID = newID
	}
	return out, err
}
func (c Client) Delete(ctx context.Context, token, calID, id string, invites bool) error {
	v := "none"
	if invites {
		v = "all"
	}
	err := c.Request(ctx, token, "DELETE", CalendarPath(calID)+"/events/"+url.PathEscape(id)+"?sendUpdates="+v, nil, nil)
	if IsStatus(err, 404) || IsStatus(err, 410) {
		return nil
	}
	return err
}

type ACL struct {
	ID    string `json:"id"`
	Role  string `json:"role"`
	Scope struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	} `json:"scope"`
}

func (c Client) ACLs(ctx context.Context, token, calID string) ([]ACL, error) {
	all := []ACL{}
	page := ""
	for {
		v := url.Values{}
		if page != "" {
			v.Set("pageToken", page)
		}
		var out struct {
			Items []ACL  `json:"items"`
			Next  string `json:"nextPageToken"`
		}
		if err := c.Request(ctx, token, "GET", CalendarPath(calID)+"/acl?"+v.Encode(), nil, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Items...)
		if out.Next == "" {
			return all, nil
		}
		page = out.Next
	}
}
