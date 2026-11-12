package main

import "testing"

func TestFinalizationHandoffPreservesAcceptedSteps(t *testing.T) {
	for _, next := range []int{10, 27} {
		p := cutoverProgress{Version: 1, Next: next, Pending: true, Fingerprint: "reviewed", Events: []cutoverEvent{{Step: "drill/check/im-listening"}}}
		if !recoverableFinalization(p, "reviewed") {
			t.Fatal("rejected paused finalization")
		}
		for _, change := range []func(*cutoverProgress){
			func(p *cutoverProgress) { p.Next-- },
			func(p *cutoverProgress) { p.Next++ },
			func(p *cutoverProgress) { p.Pending = false },
			func(p *cutoverProgress) { p.RolledBack = true },
			func(p *cutoverProgress) { p.Version++ },
			func(p *cutoverProgress) { p.Fingerprint = "different" },
		} {
			bad := p
			change(&bad)
			if recoverableFinalization(bad, "reviewed") {
				t.Fatal("accepted unrelated or finished phase")
			}
		}
	}
	for _, next := range []int{-1, 0, 2, 8, 12, 13, 14, 15, 16, 17, 29, 30, 31, 32, 33} {
		if recoverableFinalization(cutoverProgress{Version: 1, Next: next, Pending: true, Fingerprint: "reviewed"}, "reviewed") {
			t.Fatalf("adopted step %d", next)
		}
	}
}
