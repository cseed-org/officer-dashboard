package plan

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Report is the dry-run output, rendered as text or JSON.
type Report struct {
	Hostname string   `json:"hostname"`
	DryRun   bool     `json:"dry_run"`
	Actions  []Action `json:"actions"`
}

// WriteText prints one line per action, its details indented below it, then a summary.
func (r Report) WriteText(w io.Writer) error {
	var b strings.Builder
	mode := "preview"
	if r.DryRun {
		mode = "dry run: nothing was changed"
	}
	fmt.Fprintf(&b, "Cloudflare setup for %s (%s)\n\n", r.Hostname, mode)
	res := 0
	for _, a := range r.Actions {
		res = max(res, len(a.Resource))
	}
	for _, a := range r.Actions {
		fmt.Fprintf(&b, "  %-10s  %-*s  %s", a.Kind, res, a.Resource, a.Name)
		if a.ExistingID != "" {
			fmt.Fprintf(&b, "  [%s]", a.ExistingID)
		}
		b.WriteByte('\n')
		for _, d := range a.Detail {
			fmt.Fprintf(&b, "  %-10s  %-*s    - %s\n", "", res, "", d)
		}
	}
	if r.DryRun {
		b.WriteByte('\n')
		WriteAttention(&b, AttentionFrom(r.Actions, true))
	}
	s := Summary(r.Actions)
	parts := make([]string, 0, len(Kinds))
	for _, k := range Kinds {
		parts = append(parts, fmt.Sprintf("%d %s", s[k], k))
	}
	fmt.Fprintf(&b, "\n%s\n", strings.Join(parts, ", "))
	_, err := io.WriteString(w, b.String())
	return err
}

// Attention is one item a run leaves for the admin: a drift, unknowable or manual plan
// line with its details, which say what was found, what was expected and what to do; or a
// blocker, a manual step or a secret missing on the host.
type Attention struct {
	Kind   string
	Title  string
	Detail []string
}

// AttentionFrom lists the drift, unknowable and manual lines of a plan, in plan order.
// withManual false leaves out the manual lines, for an apply whose plan lists them as
// manual steps already.
func AttentionFrom(actions []Action, withManual bool) []Attention {
	var out []Attention
	for _, a := range actions {
		if a.Kind != Drift && a.Kind != Unknowable && (a.Kind != Manual || !withManual) {
			continue
		}
		out = append(out, AttentionOf(a))
	}
	return out
}

// AttentionOf is one plan line as an attention item.
func AttentionOf(a Action) Attention {
	title := a.Resource + " " + a.Name
	if a.ExistingID != "" {
		title += " [" + a.ExistingID + "]"
	}
	return Attention{Kind: string(a.Kind), Title: title, Detail: a.Detail}
}

// WriteAttention prints the one "Needs your attention" section that closes a dry run or
// an apply, just before its final status line.
func WriteAttention(w io.Writer, items []Attention) {
	var b strings.Builder
	fmt.Fprintf(&b, "Needs your attention (%d):\n", len(items))
	if len(items) == 0 {
		b.WriteString("  nothing\n")
	}
	for _, it := range items {
		fmt.Fprintf(&b, "  - %s: %s\n", it.Kind, it.Title)
		for _, d := range it.Detail {
			fmt.Fprintf(&b, "      %s\n", d)
		}
	}
	_, _ = io.WriteString(w, b.String())
}

// WriteJSON prints the report with a summary, indented, followed by a newline.
func (r Report) WriteJSON(w io.Writer) error {
	if r.Actions == nil {
		r.Actions = []Action{}
	}
	out := struct {
		Report
		Summary map[Kind]int `json:"summary"`
	}{r, Summary(r.Actions)}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}
