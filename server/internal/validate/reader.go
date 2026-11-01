package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Issues collects validation errors as zod's flatten(): {formErrors, fieldErrors}.
type Issues struct {
	Form  []string
	Field map[string][]string
	order []string
}

// Add records a field error.
func (i *Issues) Add(field, msg string) {
	if i.Field == nil {
		i.Field = map[string][]string{}
	}
	if _, ok := i.Field[field]; !ok {
		i.order = append(i.order, field)
	}
	i.Field[field] = append(i.Field[field], msg)
}

// AddForm records an error that belongs to no field.
func (i *Issues) AddForm(msg string) { i.Form = append(i.Form, msg) }

// Any reports whether anything was recorded.
func (i *Issues) Any() bool { return len(i.Form) > 0 || len(i.Field) > 0 }

// Flatten is the JSON `details` object.
func (i *Issues) Flatten() map[string]any {
	form := i.Form
	if form == nil {
		form = []string{}
	}
	fields := map[string][]string{}
	for k, v := range i.Field {
		fields[k] = v
	}
	return map[string]any{"formErrors": form, "fieldErrors": fields}
}

// Field is a parsed optional value: Set means the key was present, Null that it was JSON null.
type Field[T any] struct {
	V    T
	Set  bool
	Null bool
}

// Has reports a present, non-null value.
func (f Field[T]) Has() bool { return f.Set && !f.Null }

// Reader reads typed fields out of a JSON object (or a query string) while collecting issues.
type Reader struct {
	raw    map[string]json.RawMessage
	query  url.Values
	isForm bool
	Issues *Issues
	prefix string
}

// FromJSON parses an object body. A non-object body is a form error.
func FromJSON(body []byte) *Reader {
	r := &Reader{Issues: &Issues{}}
	var obj map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&obj); err != nil || obj == nil {
		r.Issues.AddForm("Invalid input: expected object")
		r.raw = map[string]json.RawMessage{}
		return r
	}
	r.raw = obj
	return r
}

// FromValues reads a query string or multipart text fields; every value is a string.
func FromValues(v url.Values) *Reader {
	return &Reader{query: v, isForm: true, Issues: &Issues{}}
}

func (r *Reader) key(k string) string { return r.prefix + k }

func (r *Reader) lookup(k string) (json.RawMessage, bool, bool) {
	if r.isForm {
		return nil, false, false
	}
	raw, ok := r.raw[k]
	if !ok {
		return nil, false, false
	}
	return raw, true, bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func (r *Reader) formValue(k string) (string, bool) {
	vals, ok := r.query[k]
	if !ok || len(vals) == 0 {
		return "", false
	}
	return vals[0], true
}

// S describes a string field.
type S struct {
	Req     bool
	Null    bool // null is accepted
	Trim    bool
	Min     int
	Max     int
	Enum    []string
	Format  string // "email", "url", "date", "datetime", "discord"
	Default string
	HasDef  bool
}

// Str reads a string field. An empty Query value counts as present (zod does the same).
func (r *Reader) Str(key string, s S) Field[string] {
	var f Field[string]
	var val string
	if r.isForm {
		v, ok := r.formValue(key)
		if !ok {
			return r.missing(key, s.Req, s.HasDef, s.Default, &f)
		}
		f.Set, val = true, v
	} else {
		raw, ok, isNull := r.lookup(key)
		if !ok {
			return r.missing(key, s.Req, s.HasDef, s.Default, &f)
		}
		f.Set = true
		if isNull {
			if s.Null {
				f.Null = true
				return f
			}
			r.Issues.Add(r.key(key), "Invalid input: expected string, received null")
			return f
		}
		if err := json.Unmarshal(raw, &val); err != nil {
			r.Issues.Add(r.key(key), "Invalid input: expected string, received "+kindOf(raw))
			return f
		}
	}
	if s.Trim {
		val = strings.TrimSpace(val)
	}
	f.V = val
	n := utf8.RuneCountInString(val)
	if s.Min > 0 && n < s.Min {
		r.Issues.Add(r.key(key), fmt.Sprintf("Too small: expected string to have >=%d characters", s.Min))
	}
	if s.Max > 0 && n > s.Max {
		r.Issues.Add(r.key(key), fmt.Sprintf("Too big: expected string to have <=%d characters", s.Max))
	}
	if len(s.Enum) > 0 {
		found := false
		for _, e := range s.Enum {
			if e == val {
				found = true
			}
		}
		if !found {
			r.Issues.Add(r.key(key), "Invalid option: expected one of "+strings.Join(quoteAll(s.Enum), "|"))
		}
	}
	if s.Format != "" && !formatOK(s.Format, val) {
		r.Issues.Add(r.key(key), "Invalid "+s.Format)
	}
	return f
}

func (r *Reader) missing(key string, req, hasDef bool, def string, f *Field[string]) Field[string] {
	if hasDef {
		f.V = def
		return *f
	}
	if req {
		r.Issues.Add(r.key(key), "Invalid input: expected string, received undefined")
	}
	return *f
}

// I describes an integer field.
type I struct {
	Req    bool
	Null   bool
	Min    *int64
	Max    *int64
	Def    int64
	HasDef bool
}

// Min and Max helpers for I literals.
func Lo(n int64) *int64 { return &n }

// Int reads an integer field (JSON number with no fraction, or a decimal string in forms).
func (r *Reader) Int(key string, s I) Field[int64] {
	var f Field[int64]
	var n float64
	if r.isForm {
		v, ok := r.formValue(key)
		if !ok {
			return r.missingInt(key, s, &f)
		}
		f.Set = true
		p, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil || strings.TrimSpace(v) == "" {
			r.Issues.Add(r.key(key), "Invalid input: expected number, received NaN")
			return f
		}
		n = p
	} else {
		raw, ok, isNull := r.lookup(key)
		if !ok {
			return r.missingInt(key, s, &f)
		}
		f.Set = true
		if isNull {
			if s.Null {
				f.Null = true
				return f
			}
			r.Issues.Add(r.key(key), "Invalid input: expected number, received null")
			return f
		}
		if err := json.Unmarshal(raw, &n); err != nil {
			r.Issues.Add(r.key(key), "Invalid input: expected number, received "+kindOf(raw))
			return f
		}
	}
	if n != math.Trunc(n) || math.Abs(n) > 1<<53-1 {
		r.Issues.Add(r.key(key), "Invalid input: expected int, received number")
		return f
	}
	f.V = int64(n)
	if s.Min != nil && f.V < *s.Min {
		r.Issues.Add(r.key(key), fmt.Sprintf("Too small: expected number to be >=%d", *s.Min))
	}
	if s.Max != nil && f.V > *s.Max {
		r.Issues.Add(r.key(key), fmt.Sprintf("Too big: expected number to be <=%d", *s.Max))
	}
	return f
}

func (r *Reader) missingInt(key string, s I, f *Field[int64]) Field[int64] {
	if s.HasDef {
		f.V = s.Def
		return *f
	}
	if s.Req {
		r.Issues.Add(r.key(key), "Invalid input: expected number, received undefined")
	}
	return *f
}

// Bool reads a boolean field with an optional default.
func (r *Reader) Bool(key string, def bool) bool {
	raw, ok, isNull := r.lookup(key)
	if !ok {
		return def
	}
	var b bool
	if isNull || json.Unmarshal(raw, &b) != nil {
		r.Issues.Add(r.key(key), "Invalid input: expected boolean, received "+kindOf(raw))
		return def
	}
	return b
}

// BoolField reads an optional boolean without a default.
func (r *Reader) BoolField(key string) Field[bool] {
	var f Field[bool]
	raw, ok, isNull := r.lookup(key)
	if !ok {
		return f
	}
	f.Set = true
	if isNull || json.Unmarshal(raw, &f.V) != nil {
		r.Issues.Add(r.key(key), "Invalid input: expected boolean, received "+kindOf(raw))
	}
	return f
}

// Strings reads an array of strings from enum values.
func (r *Reader) Strings(key string, req bool, min, max int, enum []string, itemMax int) []string {
	raw, ok, isNull := r.lookup(key)
	if !ok {
		if req {
			r.Issues.Add(r.key(key), "Invalid input: expected array, received undefined")
		}
		return nil
	}
	var out []string
	if isNull || json.Unmarshal(raw, &out) != nil {
		r.Issues.Add(r.key(key), "Invalid input: expected array of strings")
		return nil
	}
	if len(out) < min {
		r.Issues.Add(r.key(key), fmt.Sprintf("Too small: expected array to have >=%d items", min))
	}
	if max > 0 && len(out) > max {
		r.Issues.Add(r.key(key), fmt.Sprintf("Too big: expected array to have <=%d items", max))
	}
	for _, s := range out {
		if len(enum) > 0 && !contains(enum, s) {
			r.Issues.Add(r.key(key), "Invalid option: expected one of "+strings.Join(quoteAll(enum), "|"))
			break
		}
		if itemMax > 0 && utf8.RuneCountInString(s) > itemMax {
			r.Issues.Add(r.key(key), "Too big: item too long")
			break
		}
		if len(s) == 0 {
			r.Issues.Add(r.key(key), "Too small: item is empty")
			break
		}
	}
	return out
}

// Objects reads an array of objects; fn receives a sub-reader per element.
func (r *Reader) Objects(key string, req bool, max int, fn func(i int, sub *Reader)) bool {
	raw, ok, isNull := r.lookup(key)
	if !ok {
		if req {
			r.Issues.Add(r.key(key), "Invalid input: expected array, received undefined")
		}
		return false
	}
	var items []json.RawMessage
	if isNull || json.Unmarshal(raw, &items) != nil {
		r.Issues.Add(r.key(key), "Invalid input: expected array, received "+kindOf(raw))
		return false
	}
	if max > 0 && len(items) > max {
		r.Issues.Add(r.key(key), fmt.Sprintf("Too big: expected array to have <=%d items", max))
		return true
	}
	for i, it := range items {
		sub := FromJSON(it)
		sub.Issues = r.Issues
		sub.prefix = fmt.Sprintf("%s%s.%d.", r.prefix, key, i)
		fn(i, sub)
	}
	return true
}

// Has reports whether a key is present in the object.
func (r *Reader) Has(key string) bool { _, ok := r.raw[key]; return ok }

// Raw returns the raw JSON of a key (for audit snapshots).
func (r *Reader) Raw(key string) json.RawMessage { return r.raw[key] }

func kindOf(raw json.RawMessage) string {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return "undefined"
	}
	switch t[0] {
	case '"':
		return "string"
	case '{':
		return "object"
	case '[':
		return "array"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	}
	return "number"
}

func formatOK(format, v string) bool {
	switch format {
	case "email":
		return IsEmail(v)
	case "url":
		return IsURL(v)
	case "date":
		return IsDate(v)
	case "datetime":
		return IsDateTime(v)
	case "discord":
		return IsDiscordID(v)
	}
	return true
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func quoteAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strconv.Quote(s)
	}
	return out
}
