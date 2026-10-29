package bus

import (
	"errors"
	"fmt"
	"testing"
)

func TestStoreErrorsMapToBusErrors(t *testing.T) {
	notFound := notFoundErr()
	for _, c := range []struct {
		name string
		in   error
		code Code
	}{
		{"not found", fmt.Errorf("db: update item: %w", ErrNotFound), CodeNotFound},
		{"stale revision", ErrStaleRevision, CodeStaleRevision},
		{"kind mismatch", fmt.Errorf("x: %w", ErrKindMismatch), CodeInternal},
		{"anything else", errors.New("disk /secret/path on fire"), CodeInternal},
	} {
		got, ok := AsError(mapStoreErr(c.in))
		if !ok || got.Code != c.code {
			t.Errorf("%s: got %+v, want code %s", c.name, got, c.code)
			continue
		}
		if c.code == CodeInternal {
			checkInternal(t, c.name, got)
		}
	}
	if got, _ := AsError(mapStoreErr(ErrNotFound)); got == nil || *got != *notFound {
		t.Errorf("store not-found = %+v, want the same error as a missing id: %+v", got, notFound)
	}
	own := invalid("title", "x")
	if got, _ := AsError(mapStoreErr(fmt.Errorf("w: %w", own))); got != own {
		t.Error("a bus error was rewrapped")
	}
	if mapStoreErr(nil) != nil {
		t.Error("nil mapped to an error")
	}
}

// The cause of an internal error stays on the error for the operator's log; Error() and the
// fields a caller sees never carry it.
func TestInternalErrorKeepsItsCause(t *testing.T) {
	cause := errors.New("disk /secret/path on fire")
	got, _ := AsError(mapStoreErr(fmt.Errorf("wrapped: %w", cause)))
	if got == nil || !errors.Is(got.Cause(), cause) {
		t.Fatalf("Cause() = %v, want the store error", got)
	}
	if containsAny(got.Error(), "secret", "disk") {
		t.Errorf("Error() prints the cause: %q", got.Error())
	}
	if e := notFoundErr(); e.Cause() != nil {
		t.Errorf("a not_found error has a cause: %v", e.Cause())
	}
}

func checkInternal(t *testing.T, name string, e *Error) {
	t.Helper()
	if e.What == "" || e.Fix == "" || containsAny(e.What+e.Valid+e.Fix, "secret", "disk") {
		t.Errorf("%s: internal error leaks or is empty: %+v", name, e)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		for i := 0; i+len(x) <= len(s); i++ {
			if s[i:i+len(x)] == x {
				return true
			}
		}
	}
	return false
}

func TestAsBusErrorFallsBackToInternal(t *testing.T) {
	if e := asBusError(errors.New("not a bus error")); e == nil || e.Code != CodeInternal {
		t.Errorf("asBusError(plain) = %+v, want an internal error, never nil", e)
	}
	own := invalid("title", "x")
	if e := asBusError(own); e != own {
		t.Errorf("asBusError(bus error) = %+v", e)
	}
}
