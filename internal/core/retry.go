package core

import (
	"time"

	"github.com/Exploded-views/yo-human/internal/config"
)

const maxBackoff = time.Hour

// NextAttempt says when to try again after attempts failed sends: Every x 2^attempts, capped at an
// hour. It reports false once attempts reach Times, when the send goes to the dead letter.
func NextAttempt(r config.Retry, attempts int, now time.Time) (time.Time, bool) {
	if attempts >= r.Times {
		return time.Time{}, false
	}
	d := r.Every
	for i := 0; i < attempts && d < maxBackoff; i++ {
		d *= 2 // stops at the cap, so it cannot overflow
	}
	return now.Add(min(d, maxBackoff)), true
}
