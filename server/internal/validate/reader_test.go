package validate

import (
	"encoding/json"
	"net/url"
	"testing"
)

func TestReaderCollectsZodShapedFlatten(t *testing.T) {
	r := FromJSON([]byte(`{"name":"  ","qty":1.5,"when":"2026-02-30","extra":1}`))
	r.Str("name", S{Req: true, Trim: true, Min: 1, Max: 200})
	r.Int("qty", I{Req: true, Min: Lo(1)})
	r.Str("when", S{Req: true, Format: "date"})
	r.Str("missing", S{Req: true})
	if !r.Issues.Any() {
		t.Fatal("expected issues")
	}
	out, _ := json.Marshal(r.Issues.Flatten())
	var m struct {
		FormErrors  []string            `json:"formErrors"`
		FieldErrors map[string][]string `json:"fieldErrors"`
	}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"name", "qty", "when", "missing"} {
		if len(m.FieldErrors[k]) == 0 {
			t.Errorf("no error for %s: %s", k, out)
		}
	}
	if m.FormErrors == nil {
		t.Error("formErrors must serialise as [] not null")
	}
}

func TestReaderNullAndDefaults(t *testing.T) {
	r := FromJSON([]byte(`{"endsAt":null,"taxShip":0}`))
	e := r.Str("endsAt", S{Null: true, Format: "datetime"})
	if !e.Set || !e.Null || e.Has() {
		t.Errorf("endsAt = %+v", e)
	}
	d := r.Str("kind", S{HasDef: true, Default: "receipt"})
	if d.V != "receipt" || d.Set {
		t.Errorf("default = %+v", d)
	}
	if r.Issues.Any() {
		t.Errorf("unexpected issues %+v", r.Issues)
	}
	bad := FromJSON([]byte(`{"endsAt":null}`))
	bad.Str("endsAt", S{})
	if !bad.Issues.Any() {
		t.Error("null for a non-nullable string must be an issue")
	}
}

func TestReaderQueryAndObjects(t *testing.T) {
	q := FromValues(url.Values{"limit": {"500"}, "when": {"later"}})
	q.Int("limit", I{Min: Lo(1), Max: Lo(200), HasDef: true, Def: 50})
	q.Str("when", S{Enum: []string{"upcoming", "past", "all"}})
	if len(q.Issues.Field["limit"]) == 0 || len(q.Issues.Field["when"]) == 0 {
		t.Errorf("issues %+v", q.Issues.Field)
	}
	r := FromJSON([]byte(`{"lines":[{"accountId":"a","amountCents":5},{"amountCents":-1}]}`))
	r.Objects("lines", true, 500, func(i int, sub *Reader) {
		sub.Str("accountId", S{Req: true})
		sub.Int("amountCents", I{Req: true, Min: Lo(0)})
	})
	if len(r.Issues.Field["lines.1.accountId"]) == 0 || len(r.Issues.Field["lines.1.amountCents"]) == 0 {
		t.Errorf("nested issues %+v", r.Issues.Field)
	}
}
