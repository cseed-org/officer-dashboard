package notify

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestConsoleOmitsPersonalAndPurchaseContent(t *testing.T) {
	var out bytes.Buffer
	Console{Log: slog.New(slog.NewJSONHandler(&out, nil))}.Notify(context.Background(), Event{
		Type: "request.submitted", RequestID: "request-test",
		Requester: "Private requester", Director: "Private approver",
		Title: "Private purchase", Comment: "Private comment", Reason: "Private reason",
		TotalCents: Cents(123456),
	}, "https://app.example/finance?token=private-token")
	for _, private := range []string{"Private", "private-token", "123456", "https://"} {
		if strings.Contains(out.String(), private) {
			t.Fatalf("notification log contains sensitive event content")
		}
	}
	if !strings.Contains(out.String(), "request.submitted") || !strings.Contains(out.String(), "request-test") {
		t.Fatal("notification log lost its operational identifiers")
	}
}
