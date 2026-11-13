package money

import "testing"

func TestParseMoneyToCents(t *testing.T) {
	ok := map[string]int64{
		"12.34": 1234, "$12.34": 1234, "1,234.5": 123450, "  $0.07 ": 7, "40": 4000, "-3.10": -310, "($3.10)": -310,
	}
	for in, want := range ok {
		got, valid := ParseToCents(in)
		if !valid || got != want {
			t.Errorf("ParseToCents(%q) = %d,%v want %d", in, got, valid, want)
		}
	}
	for _, in := range []string{"", "abc", "1.234", "12.3.4", "$", "1,23"} {
		if _, valid := ParseToCents(in); valid {
			t.Errorf("ParseToCents(%q) should be rejected", in)
		}
	}
}

func TestFormatCents(t *testing.T) {
	cases := map[int64]string{123450: "$1,234.50", 7: "$0.07", 0: "$0.00", -310: "-$3.10", 12501: "$125.01", 100000000: "$1,000,000.00"}
	for in, want := range cases {
		if got := Format(in); got != want {
			t.Errorf("Format(%d) = %q want %q", in, got, want)
		}
	}
}

func TestPurchaseTotal(t *testing.T) {
	got, err := PurchaseTotal(3, 1999, 550)
	if err != nil || got != 6547 {
		t.Fatalf("got %d, %v want 6547", got, err)
	}
	for _, c := range [][3]int64{{0, 100, 0}, {1, -1, 0}, {1, 100, -1}} {
		if _, err := PurchaseTotal(c[0], c[1], c[2]); err == nil {
			t.Errorf("PurchaseTotal(%v) should fail", c)
		}
	}
	if _, err := PurchaseTotal(10000, 1<<62, 0); err == nil {
		t.Error("overflow should fail")
	}
}
