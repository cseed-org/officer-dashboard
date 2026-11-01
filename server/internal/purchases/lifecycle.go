// Package purchases holds the purchase-request lifecycle and the agent settlement tolerance.
package purchases

import (
	"math"
	"strconv"
	"strings"
)

const (
	ClubPurchase  = "club_purchase"
	Reimbursement = "reimbursement"
)

// Lifecycle:
//
//	draft → submitted → approved | denied
//	club_purchase:  approved → fulfilling → fulfilled → closed   (fulfilling → approved on a failed job)
//	reimbursement:  approved → reimbursed → closed
//
// draft, submitted and approved can be cancelled. denied, closed, cancelled are final.
// Who may perform each move (requester, director, agent) is enforced by the API layer.
var common = map[string][]string{
	"draft":     {"submitted", "cancelled"},
	"submitted": {"approved", "denied", "cancelled"},
}

var byType = map[string]map[string][]string{
	ClubPurchase: {
		"approved":   {"fulfilling", "cancelled"},
		"fulfilling": {"fulfilled", "approved"},
		"fulfilled":  {"closed"},
	},
	Reimbursement: {
		"approved":   {"reimbursed", "cancelled"},
		"reimbursed": {"closed"},
	},
}

// NextStatuses lists the statuses a request of this type can move to from `from`.
func NextStatuses(typ, from string) []string {
	if n, ok := common[from]; ok {
		return append([]string{}, n...)
	}
	return append([]string{}, byType[typ][from]...)
}

// CanTransition reports whether the lifecycle allows from → to.
func CanTransition(typ, from, to string) bool {
	for _, s := range NextStatuses(typ, from) {
		if s == to {
			return true
		}
	}
	return false
}

// IsEditable: only drafts can be edited; after submission changes go through cancel + resubmit.
func IsEditable(status string) bool { return status == "draft" }

// Tolerance is how far an automated (agent) settlement may exceed the approved estimate
// before it is held for director review. The director's own transition path is trusted.
type Tolerance struct {
	Percent float64 // e.g. 10 for 10%
	Cents   int64   // flat allowance
}

var DefaultTolerance = Tolerance{Percent: 10, Cents: 2_500}

// MaxAgentSettlementCents is the largest actual total an agent may post: whichever allowance is larger.
func MaxAgentSettlementCents(estimate int64, t Tolerance) int64 {
	pct := int64(math.Floor(float64(estimate) * (100 + t.Percent) / 100))
	flat := estimate + t.Cents
	if pct > flat {
		return pct
	}
	return flat
}

// ToleranceFromEnv reads AGENT_SETTLEMENT_TOLERANCE_PERCENT / _CENTS, ignoring invalid values.
func ToleranceFromEnv(percent, cents string) Tolerance {
	read := func(v string, fallback float64) float64 {
		v = strings.TrimSpace(v)
		if v == "" {
			return fallback
		}
		n, err := strconv.ParseFloat(v, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return fallback
		}
		return n
	}
	return Tolerance{
		Percent: read(percent, DefaultTolerance.Percent),
		Cents:   int64(math.Round(read(cents, float64(DefaultTolerance.Cents)))),
	}
}
