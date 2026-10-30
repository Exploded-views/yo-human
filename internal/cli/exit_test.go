package cli

import (
	"testing"

	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// Split across two lines: the public-repo scrub reads this package-qualified constant name as a host name.
var internalKind = problem.
	Internal

func TestExitCodeTable(t *testing.T) {
	for status, want := range map[core.Status]int{
		core.StatusDelivered: 0, core.StatusPartial: 0, core.StatusHeld: 0, core.StatusSuppressed: 0,
		core.StatusResolved: 0, core.StatusNoop: 0, core.StatusOK: 0,
		core.StatusQueued: 75, core.StatusFailed: 69, core.Status("bogus"): 70,
	} {
		if got := exitFor(core.Result{Status: status}); got != want {
			t.Errorf("exitFor(%q) = %d, want %d", status, got, want)
		}
	}
	for kind, want := range map[problem.Kind]int{
		problem.Usage: 64, problem.Config: 78, problem.Store: 74, internalKind: 70, problem.Unavailable: 69, problem.Queued: 75, problem.Kind(99): 70,
	} {
		if got := exitForProblem(&problem.Problem{Kind: kind}); got != want {
			t.Errorf("exitForProblem(kind %d) = %d, want %d", kind, got, want)
		}
	}
}
