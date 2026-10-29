package core

import "github.com/Exploded-views/yo-human/internal/problem"

// closeOutcome holds a resolve to what the human can still be paged by. A channel that closes
// alerts (PagerDuty) and was not told to keeps escalating: a close queued for another try is a
// Queued problem (exit 75), and one refused for good is Unavailable (exit 69). A passive note to
// any other channel is retried quietly and is no problem. It returns the next step when a cancel or
// a close is queued: the flush that retries the one due first.
func (b *batch) closeOutcome(key string, cancels, closes []ChannelResult) *Next {
	var soonest *ChannelResult
	queued := func(r ChannelResult) {
		if soonest == nil || r.retryAt.Before(soonest.retryAt) {
			soonest = &r
		}
	}
	for _, c := range cancels {
		if c.Status == ChannelQueued {
			queued(c)
		}
	}
	for _, c := range closes {
		if !b.p.d.Caps[c.Channel].Resolve {
			continue
		}
		switch c.Status {
		case ChannelQueued:
			queued(c)
			b.fail(&problem.Problem{Kind: problem.Queued, What: "the close of incident " + key + " on " + c.Channel + " is queued: " + c.Error,
				Valid: "a close the provider accepts",
				Fix:   "run the next step (`yo-human flush`), which retries it; until it gets through, " + c.Channel + " keeps the alert open and may page the human again"})
		case ChannelDead:
			b.fail(&problem.Problem{Kind: problem.Unavailable, What: "the close of incident " + key + " on " + c.Channel + " was refused: " + c.Error,
				Valid: "a close the provider accepts",
				Fix:   "tell the human: the alert is still open on " + c.Channel + ", so they resolve it there; the close is in the dead letter (see `yo-human status`)"})
		}
	}
	if soonest == nil {
		return nil
	}
	return b.p.nextFor(*soonest, b.now)
}
