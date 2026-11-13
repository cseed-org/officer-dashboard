// Package money handles integer-cent amounts. Dollars appear only here.
package money

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var moneyRE = regexp.MustCompile(`^(-)?\$?(\d{1,3}(?:,\d{3})+|\d+)(?:\.(\d{1,2}))?$`)
var parenRE = regexp.MustCompile(`^\((.*)\)$`)

// maxSafe mirrors JS Number.MAX_SAFE_INTEGER so amounts stay portable to the TS contract.
const maxSafe = 1<<53 - 1

// ParseToCents turns "$1,234.50" into 123450. It accepts an optional $, thousands
// separators, up to two decimals, a leading minus or accounting parentheses.
func ParseToCents(input string) (int64, bool) {
	s := strings.TrimSpace(input)
	negative := false
	if m := parenRE.FindStringSubmatch(s); m != nil {
		negative = true
		s = strings.TrimSpace(m[1])
	}
	m := moneyRE.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	if m[1] != "" {
		negative = true
	}
	dollars, err := strconv.ParseInt(strings.ReplaceAll(m[2], ",", ""), 10, 64)
	if err != nil {
		return 0, false
	}
	frac := m[3]
	for len(frac) < 2 {
		frac += "0"
	}
	c, _ := strconv.ParseInt(frac, 10, 64)
	if dollars > (maxSafe-c)/100 {
		return 0, false
	}
	total := dollars*100 + c
	if negative {
		return -total, true
	}
	return total, true
}

// Format renders 123450 as "$1,234.50" and -310 as "-$3.10".
func Format(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	dollars := strconv.FormatInt(cents/100, 10)
	var b strings.Builder
	for i, r := range dollars {
		if i > 0 && (len(dollars)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return fmt.Sprintf("%s$%s.%02d", sign, b.String(), cents%100)
}

// PurchaseTotal matches the purchase_requests_total CHECK constraint:
// quantity × unit cost + tax/shipping, as a safe integer.
func PurchaseTotal(quantity, unitCostCents, taxShipCents int64) (int64, error) {
	if quantity < 1 {
		return 0, errors.New("quantity must be an integer ≥ 1")
	}
	if unitCostCents < 0 || taxShipCents < 0 {
		return 0, errors.New("cents must be integers ≥ 0")
	}
	if unitCostCents != 0 && quantity > math.MaxInt64/unitCostCents {
		return 0, errors.New("total exceeds safe integer cents")
	}
	total := quantity*unitCostCents + taxShipCents
	if total < 0 || total > maxSafe {
		return 0, errors.New("total exceeds safe integer cents")
	}
	return total, nil
}
