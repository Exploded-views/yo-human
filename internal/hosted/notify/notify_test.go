package notify_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/notify"
)

var errBase = errors.New("boom")

func TestClassesAreDistinct(t *testing.T) {
	plain := errBase
	perm := notify.Permanent(errBase)
	broke := notify.Broken(errBase)
	for _, c := range []struct {
		name         string
		err          error
		perm, broken bool
	}{{"plain", plain, false, false}, {"permanent", perm, true, false}, {"broken", broke, false, true},
		{"wrapped permanent", fmt.Errorf("send: %w", perm), true, false}, {"wrapped broken", fmt.Errorf("send: %w", broke), false, true}} {
		if notify.IsPermanent(c.err) != c.perm || notify.IsBroken(c.err) != c.broken {
			t.Errorf("%s: permanent=%v broken=%v, want %v %v", c.name, notify.IsPermanent(c.err), notify.IsBroken(c.err), c.perm, c.broken)
		}
	}
}

func TestMarkedErrorsKeepTheirCause(t *testing.T) {
	for _, err := range []error{notify.Permanent(errBase), notify.Broken(errBase), notify.WithStatus(errBase, 500)} {
		if !errors.Is(err, errBase) || err.Error() != "boom" {
			t.Errorf("%v does not keep its cause and text", err)
		}
	}
}

func TestStatusSurvivesClassing(t *testing.T) {
	err := notify.Permanent(notify.WithStatus(errBase, 413))
	if notify.Status(err) != 413 || !notify.IsPermanent(err) {
		t.Errorf("status %d permanent %v, want 413 true", notify.Status(err), notify.IsPermanent(err))
	}
	err = notify.WithStatus(notify.Broken(errBase), 401)
	if notify.Status(err) != 401 || !notify.IsBroken(err) {
		t.Errorf("status %d broken %v, want 401 true", notify.Status(err), notify.IsBroken(err))
	}
	if notify.Status(errBase) != 0 {
		t.Error("an unmarked error has a status")
	}
}

func TestRetryAfter(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want time.Duration
	}{
		{"absent", errBase, 0},
		{"two minutes", notify.WithRetryAfter(errBase, 2*time.Minute), 2 * time.Minute},
		{"capped at an hour", notify.WithRetryAfter(errBase, 5*time.Hour), time.Hour},
		{"exactly an hour", notify.WithRetryAfter(errBase, time.Hour), time.Hour},
		{"negative is absent", notify.WithRetryAfter(errBase, -time.Second), 0},
		{"kept through a class", notify.Permanent(notify.WithRetryAfter(errBase, time.Minute)), time.Minute},
		{"kept through a wrap", fmt.Errorf("x: %w", notify.WithRetryAfter(errBase, time.Minute)), time.Minute},
	}
	for _, c := range cases {
		if got := notify.RetryAfter(c.err); got != c.want {
			t.Errorf("%s: RetryAfter = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestClassingNilIsNil(t *testing.T) {
	for _, err := range []error{notify.Permanent(nil), notify.Broken(nil), notify.WithStatus(nil, 500), notify.WithRetryAfter(nil, time.Second)} {
		if err != nil {
			t.Errorf("classing nil gave %v", err)
		}
	}
}
