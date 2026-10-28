package bus

import (
	"regexp"
	"testing"
)

func TestNewIDUniqueAndShaped(t *testing.T) {
	shape := regexp.MustCompile(`^itm_[a-z2-7]{26}$`)
	seen := make(map[string]bool, 10000)
	for range 10000 {
		id := NewID("itm")
		if !shape.MatchString(id) {
			t.Fatalf("NewID = %q, want %s", id, shape)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}
