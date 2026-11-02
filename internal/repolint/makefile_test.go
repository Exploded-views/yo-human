package repolint

import (
	"os"
	"strings"
	"testing"
)

// TestMakeCoverChecksFloors: make cover is the one command a developer runs for coverage, and
// CI's floor check runs in its place, so cover must depend on floors.
func TestMakeCoverChecksFloors(t *testing.T) {
	md, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(md), "\n") {
		if !strings.HasPrefix(line, "cover:") {
			continue
		}
		prereqs := strings.Fields(strings.TrimPrefix(line, "cover:"))
		if !contains(prereqs, "floors") {
			t.Errorf("cover prerequisites = %v; want floors", prereqs)
		}
		return
	}
	t.Fatal("Makefile has no cover: rule")
}
