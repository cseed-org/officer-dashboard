// Package luma parses Luma guest exports and survey (reflection) exports.
package luma

import (
	"errors"
	"regexp"
	"strings"
	"unicode"

	"officerdashboard/internal/validate"
)

// Guest is one parsed guest row. Nil fields were absent or blank in the CSV.
type Guest struct {
	Email          string
	Name           *string
	LumaGuestID    *string
	ApprovalStatus *string
	RegisteredAt   *string // ISO UTC
	CheckedInAt    *string // ISO UTC
}

// Reflection is one parsed survey response.
type Reflection struct {
	Email       string
	SubmittedAt string // ISO UTC
}

// RowError locates a bad row by its physical line in the file.
type RowError struct {
	Row     int
	Message string
}

// Result is a parse outcome: valid rows, per-row errors and the data row count.
type Result[T any] struct {
	Rows      []T
	Errors    []RowError
	TotalRows int
}

// csvRows implements RFC 4180: quotes, escaped quotes, CRLF and embedded newlines.
// positions receives the physical line each record starts on.
func csvRows(text string, positions *[]int) ([][]string, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	rs := []rune(text)
	physical, record := 1, 1
	var rows [][]string
	var row []string
	var field strings.Builder
	quoted, ended := false, false
	nonBlank := func(r []string) bool {
		for _, v := range r {
			if strings.TrimSpace(v) != "" {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if c == '\r' || (c == '\n' && (i == 0 || rs[i-1] != '\r')) {
			physical++
		}
		switch {
		case quoted:
			if c == '"' {
				if i+1 < len(rs) && rs[i+1] == '"' {
					field.WriteRune('"')
					i++
				} else {
					quoted, ended = false, true
				}
			} else {
				field.WriteRune(c)
			}
		case c == '"':
			if field.Len() > 0 || ended {
				return nil, errors.New("Malformed CSV quote")
			}
			quoted = true
		case c == ',':
			row = append(row, field.String())
			field.Reset()
			ended = false
		case c == '\n' || c == '\r':
			if c == '\r' && i+1 < len(rs) && rs[i+1] == '\n' {
				i++
			}
			row = append(row, field.String())
			if nonBlank(row) {
				rows = append(rows, row)
				if positions != nil {
					*positions = append(*positions, record)
				}
			}
			record = physical
			row = nil
			field.Reset()
			ended = false
		default:
			if ended && !unicode.IsSpace(c) {
				return nil, errors.New("Unexpected text after CSV quote")
			}
			field.WriteRune(c)
		}
	}
	if quoted {
		return nil, errors.New("Unterminated CSV quote")
	}
	row = append(row, field.String())
	if nonBlank(row) {
		rows = append(rows, row)
		if positions != nil {
			*positions = append(*positions, record)
		}
	}
	return rows, nil
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]`)

func headerKey(s string) string { return nonAlnum.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "") }

var aliases = map[string][]string{
	"email":          {"email", "emailaddress", "guestemail", "youremail"},
	"name":           {"name", "fullname", "guestname"},
	"lumaGuestId":    {"apiid", "guestid", "lumaguestid"},
	"approvalStatus": {"approvalstatus", "status", "registrationstatus"},
	"registeredAt":   {"registeredat", "registrationtime", "registeredtime", "createdat"},
	"checkedInAt":    {"checkedinat", "checkedintime", "checkintime", "checkinat"},
	"submittedAt":    {"timestamp", "submittedat", "submissiontime", "submittedtime"},
}

type getter func(key string) *string

func parse[T any](text string, reflections bool, convert func(get getter) (T, error)) (Result[T], error) {
	var positions []int
	all, err := csvRows(text, &positions)
	if err != nil {
		return Result[T]{}, err
	}
	if len(all) == 0 {
		return Result[T]{}, errors.New("CSV is empty")
	}
	header, data := all[0], all[1:]
	keys := make([]string, len(header))
	for i, h := range header {
		keys[i] = headerKey(h)
	}
	index := func(k string) int {
		for i, h := range keys {
			for _, a := range aliases[k] {
				if a == h {
					return i
				}
			}
		}
		return -1
	}
	if index("email") < 0 {
		return Result[T]{}, errors.New("Missing email header")
	}
	if reflections && index("submittedAt") < 0 {
		return Result[T]{}, errors.New("Missing timestamp header")
	}
	res := Result[T]{TotalRows: len(data)}
	for i, row := range data {
		get := func(k string) *string {
			idx := index(k)
			if idx < 0 || idx >= len(row) {
				return nil
			}
			v := strings.TrimSpace(row[idx])
			if v == "" {
				return nil
			}
			return &v
		}
		v, err := convert(get)
		if err != nil {
			line := i + 2
			if i+1 < len(positions) {
				line = positions[i+1]
			}
			res.Errors = append(res.Errors, RowError{Row: line, Message: err.Error()})
			continue
		}
		res.Rows = append(res.Rows, v)
	}
	return res, nil
}

func email(v *string) (string, error) {
	s := ""
	if v != nil {
		s = strings.ToLower(strings.TrimSpace(*v))
	}
	if !validate.IsEmail(s) {
		return "", errors.New("Invalid email address")
	}
	return s, nil
}

func stamp(v *string) (*string, error) {
	if v == nil {
		return nil, nil
	}
	t, err := CSVTimestamp(*v)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ParseGuests reads a Luma guest export.
func ParseGuests(text string) (Result[Guest], error) {
	return parse(text, false, func(g getter) (Guest, error) {
		e, err := email(g("email"))
		if err != nil {
			return Guest{}, err
		}
		reg, err := stamp(g("registeredAt"))
		if err != nil {
			return Guest{}, err
		}
		chk, err := stamp(g("checkedInAt"))
		if err != nil {
			return Guest{}, err
		}
		return Guest{Email: e, Name: g("name"), LumaGuestID: g("lumaGuestId"), ApprovalStatus: g("approvalStatus"), RegisteredAt: reg, CheckedInAt: chk}, nil
	})
}

// ParseReflections reads a survey export.
func ParseReflections(text string) (Result[Reflection], error) {
	return parse(text, true, func(g getter) (Reflection, error) {
		at, err := stamp(g("submittedAt"))
		if err != nil {
			return Reflection{}, err
		}
		if at == nil {
			return Reflection{}, errors.New("Missing timestamp")
		}
		e, err := email(g("email"))
		if err != nil {
			return Reflection{}, err
		}
		return Reflection{Email: e, SubmittedAt: *at}, nil
	})
}

var declined = map[string]bool{
	"declined": true, "rejected": true, "waitlist": true, "waitlisted": true, "pending": true,
	"pending_approval": true, "invited": true, "cancelled": true, "canceled": true,
}

var sepRun = regexp.MustCompile(`[ -]+`)

// IsRegistered normalises Luma approval states independently from timestamp imports.
func IsRegistered(status *string, checkedIn bool) bool {
	if checkedIn {
		return true
	}
	if status == nil {
		return true
	}
	n := sepRun.ReplaceAllString(strings.ToLower(strings.TrimSpace(*status)), "_")
	return !declined[n]
}
