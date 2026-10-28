package bus

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorShape(t *testing.T) {
	e := &Error{Code: CodeInvalid, What: "title", Valid: "1 to 200 characters", Fix: "shorten the title"}
	if !strings.Contains(e.Error(), "title") || !strings.Contains(e.Error(), "invalid") {
		t.Errorf("Error() = %q, want the code and what", e.Error())
	}
	got, ok := AsError(fmt.Errorf("post: %w", e))
	if !ok || got != e {
		t.Errorf("AsError through %%w = %v, %v", got, ok)
	}
	if _, ok := AsError(errors.New("plain")); ok {
		t.Error("AsError found a bus error in a plain error")
	}
	if _, ok := AsError(nil); ok {
		t.Error("AsError found a bus error in nil")
	}
}
