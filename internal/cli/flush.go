package cli

import (
	"context"
	"errors"
)

// cmdFlush sends whatever is queued and due, within the deadline. It builds the channels of the
// due entries only. An entry whose channel cannot be built stays queued, and the failure is
// reported beside the result of the rest.
func cmdFlush(e *env, args []string) int {
	if len(args) > 0 {
		return e.fail(usageErr("flush"))
	}
	cfg, st, err := e.loadWithStore()
	if err != nil {
		return e.fail(err)
	}
	chans, failed, err := e.channelsFor(cfg, st, nil, nil)
	if err != nil {
		return e.fail(err)
	}
	res, ferr := e.pipeline(cfg, st, chans).Flush(context.Background())
	var problems []error
	for _, name := range sortedNames(failed) {
		problems = append(problems, failed[name])
	}
	return e.report(res, errors.Join(append(problems, ferr)...))
}
