package store

import (
	"strings"
	"testing"
)

func TestTrustRoundTrip(t *testing.T) {
	s := open(t)
	shaA, shaB := strings.Repeat("a", 64), strings.Repeat("b", 64)
	if ok, err := s.Trusted("/p/.yo-human.yaml", shaA); err != nil || ok {
		t.Fatalf("before Trust: ok=%v err=%v, want false", ok, err)
	}
	if err := s.Trust("/p/.yo-human.yaml", shaA); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Trusted("/p/.yo-human.yaml", shaA); err != nil || !ok {
		t.Fatalf("trusted sha: ok=%v err=%v, want true", ok, err)
	}
	if ok, _ := s.Trusted("/p/.yo-human.yaml", shaB); ok {
		t.Error("a different sha for the same path must not be trusted")
	}
	if ok, _ := s.Trusted("/other/.yo-human.yaml", shaA); ok {
		t.Error("the same sha under a different path must not be trusted")
	}
}

func TestTrustSurvivesReopenAndReplaces(t *testing.T) {
	s := open(t)
	shaA, shaB := strings.Repeat("a", 64), strings.Repeat("b", 64)
	_ = s.Trust("/p", shaA)
	_ = s.Trust("/q", shaA)
	if err := s.Trust("/p", shaB); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := s2.Trusted("/p", shaB); !ok {
		t.Error("new sha should be trusted after reopen")
	}
	if ok, _ := s2.Trusted("/p", shaA); ok {
		t.Error("re-trusting a path replaces the old sha")
	}
	if ok, _ := s2.Trusted("/q", shaA); !ok {
		t.Error("other paths are kept")
	}
}
