package main

import (
	"testing"

	"github.com/hib2018/HodoCode/internal/analyze"
)

func TestSemanticUnresolvedReason(t *testing.T) {
	s := &screen{}
	for _, tc := range []struct {
		site analyze.CallSite
		want string
	}{
		{analyze.CallSite{Edge: analyze.Edge{Unknown: true}}, "呼び出し先を静的に特定できない"},
		{analyze.CallSite{Edge: analyze.Edge{Unknown: true}, Reason: "動的な呼び出し先は未確定"}, "動的な呼び出し先は未確定"},
		{analyze.CallSite{Edge: analyze.Edge{Unknown: true}, Reason: "複数候補", Candidates: []string{"a.f", "b.f"}}, "複数候補（候補: a.f, b.f）"},
	} {
		if got := s.reason(tc.site); got != tc.want {
			t.Fatalf("reason = %q, want %q", got, tc.want)
		}
	}
}
