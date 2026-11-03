package engine

import "testing"

func TestHorizonVerdict(t *testing.T) {
	t.Parallel()
	closed := Gate{AnswerYes, AnswerNo, AnswerYes, AnswerYes}
	strictOK := HorizonResult{Horizon: 10}
	relaxedOnly := HorizonResult{Horizon: 15, Strict: ProfileScores{Failures: 2}}
	failing := HorizonResult{Horizon: 20, Strict: ProfileScores{Failures: 3}, Relaxed: ProfileScores{Failures: 1}}
	tests := []struct {
		name string
		h    HorizonResult
		g    Gate
		want Verdict
		ok   bool
	}{
		{"indicatif", HorizonResult{Horizon: 5, Indicative: true}, openGate, "", false},
		{"candidat", strictOK, openGate, VerdictCandidate, true},
		{"à surveiller", relaxedOnly, openGate, VerdictWatch, true},
		{"rejeté", failing, openGate, VerdictRejected, true},
		{"exclu", strictOK, closed, VerdictExcluded, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := HorizonVerdict(tc.h, tc.g)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("HorizonVerdict = %q, %v ; attendu %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
