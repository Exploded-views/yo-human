package cli

import (
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// Exit codes returned by Run. Usage errors use 64 (EX_USAGE from sysexits.h).
const (
	ExitOK       = 0
	ExitUsage    = 64
	ExitFailed   = 69
	ExitInternal = 70
	ExitStore    = 74
	ExitQueued   = 75
	ExitConfig   = 78
)

// exitFor maps a result's status to the contract's exit code. A status the contract does not name
// is a bug and exits 70.
func exitFor(r core.Result) int {
	switch r.Status {
	case core.StatusDelivered, core.StatusPartial, core.StatusHeld, core.StatusSuppressed,
		core.StatusResolved, core.StatusNoop, core.StatusOK:
		return ExitOK
	case core.StatusQueued:
		return ExitQueued
	case core.StatusFailed:
		return ExitFailed
	}
	return ExitInternal
}

// exitForProblem maps a problem's kind to its exit code. Unavailable is EX_UNAVAILABLE (69), the
// same code as a failed send: a service the call needs cannot be used and retrying cannot help.
// Queued is 75, the same code as a queued send: what is left is queued, and the result's next step
// retries it.
func exitForProblem(p *problem.Problem) int {
	switch p.Kind {
	case problem.Usage:
		return ExitUsage
	case problem.Config:
		return ExitConfig
	case problem.Store:
		return ExitStore
	case problem.Unavailable:
		return ExitFailed
	case problem.Queued:
		return ExitQueued
	}
	return ExitInternal
}
