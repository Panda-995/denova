package resourceexchange

import "testing"

func TestUpdateDecision(t *testing.T) {
	for _, tc := range []struct {
		name, source, local, incoming, choice, state string
		apply, acknowledge                           bool
	}{
		{"unchanged", "base", "base", "base", "", "unchanged", false, true},
		{"remote only", "next", "base", "next", "", "update", true, true},
		{"local only", "base", "mine", "base", "", "keep", false, true},
		{"converged", "next", "next", "next", "", "unchanged", false, true},
		{"conflict", "next", "mine", "next", "", "conflict", false, false},
		{"keep conflict", "next", "mine", "next", "keep", "keep", false, true},
		{"replace conflict", "next", "mine", "next", "remote", "update", true, true},
		{"deleted locally", "base", "missing", "base", "", "keep", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := updateReview{choices: map[string]map[string]string{"r": {"": tc.choice}}}
			apply, ack := r.decide("r", "", "Resource", "base", tc.source, "base", tc.local, tc.incoming)
			if apply != tc.apply || ack != tc.acknowledge || r.items[0].State != tc.state {
				t.Fatalf("decision: apply=%v acknowledge=%v items=%+v", apply, ack, r.items)
			}
		})
	}
}
