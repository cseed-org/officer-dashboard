package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscordEmbedForAHeldSettlement(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(204)
	}))
	defer srv.Close()
	NewDiscord(srv.URL).Notify(context.Background(), Event{
		Type: "request.held", RequestID: "r1", Number: "2027-0007", Title: "Arduino kits",
		AttemptedCents: Cents(12501), ApprovedCents: Cents(10000), Reason: "held for director review",
	}, "https://app.example/requests/r1")
	if got["allowed_mentions"].(map[string]any)["parse"] == nil {
		t.Error("mentions must be disabled with an empty parse list")
	}
	embed := got["embeds"].([]any)[0].(map[string]any)
	if embed["description"] != "request.held" || !strings.HasPrefix(embed["title"].(string), "2027-0007") || embed["url"] != "https://app.example/requests/r1" {
		t.Errorf("embed %v", embed)
	}
	fields := map[string]string{}
	for _, f := range embed["fields"].([]any) {
		m := f.(map[string]any)
		fields[m["name"].(string)] = m["value"].(string)
	}
	if fields["Agent reported"] != "$125.01" || fields["Approved"] != "$100.00" || fields["reason"] != "held for director review" {
		t.Errorf("fields %v", fields)
	}
	if _, ok := fields["Amount"]; ok {
		t.Error("absent amounts are skipped")
	}
}

func TestDeliveryFailuresNeverPanicOrPropagate(t *testing.T) {
	NewDiscord("http://127.0.0.1:1/unreachable").Notify(context.Background(), Event{Type: "request.status", Number: "n", Title: "t"}, "l")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	NewDiscord(srv.URL).Notify(context.Background(), Event{Type: "request.status", Number: "n", Title: "t"}, "l")
	Console{}.Notify(context.Background(), Event{Type: "request.submitted", Number: "n", Title: "t"}, "l")
}

func TestFieldsSkipBlankAndTruncate(t *testing.T) {
	f := Fields(Event{Requester: "  ", Comment: strings.Repeat("x", 2000), TotalCents: Cents(4050)})
	if len(f) != 2 || f[0].Name != "Amount" || f[0].Value != "$40.50" || len(f[1].Value) != 1024 {
		t.Errorf("fields %+v", f)
	}
}
