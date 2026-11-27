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
	s := Summary(r.Actions)
	parts := make([]string, 0, len(Kinds))
	for _, k := range Kinds {
		parts = append(parts, fmt.Sprintf("%d %s", s[k], k))
	}
	fmt.Fprintf(&b, "\n%s\n", strings.Join(parts, ", "))
	_, err := io.WriteString(w, b.String())
	return err
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
