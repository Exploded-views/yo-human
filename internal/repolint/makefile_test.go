package repolint

import (
	"os"
	"strings"
	"testing"
)

// TestMakeCoverChecksFloors: make cover is the coverage command developers run, so it must fail
// exactly where CI's floor check fails: cover depends on floors.
func TestMakeCoverChecksFloors(t *testing.T) {
	mk, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(mk), "\n") {
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
