package luma

import (
	"strings"
	"testing"
)

func TestCSVUnquotedAllocationBudget(t *testing.T) {
	text := strings.Repeat("Ann,ann@example.org,approved,東京\n", 1000)
	allocs := testing.AllocsPerRun(3, func() {
		rows, err := csvRows(text, nil)
		if err != nil || len(rows) != 1000 {
			t.Fatal(err, len(rows))
		}
	})
	// Row slices grow three times; plain fields should remain slices of text.
	if allocs > 3100 {
		t.Fatalf("plain fields allocated: %.0f", allocs)
	}
}

func TestCSVUnicodeAfterQuoteAndEscapes(t *testing.T) {
	rows, err := csvRows("\"héllo\"\u2003,\"a\"\"b\",東京\r\n", nil)
	if err != nil || len(rows) != 1 || rows[0][0] != "héllo\u2003" || rows[0][1] != `a"b` || rows[0][2] != "東京" {
		t.Fatalf("%q %v", rows, err)
	}
}

func BenchmarkCSVGuests(b *testing.B) {
	text := "name,email,approval_status\n" + strings.Repeat("Ann,ann@example.org,approved\n", 26000)
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := ParseGuests(text)
		if err != nil || len(r.Rows) != 26000 {
			b.Fatal(err)
		}
	}
}
