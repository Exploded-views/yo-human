package bus

import (
	"errors"
	"fmt"
)

// Code is a machine-readable error class.
type Code string

// The error codes.
const (
	CodeInvalid        Code = "invalid"
	CodeNotFound       Code = "not_found"
	CodeForbidden      Code = "forbidden"
	CodeExcludedAction Code = "excluded_action"
	CodeStanding       Code = "standing"
	CodeNotApprovable  Code = "not_approvable"
	CodeStaleRevision  Code = "stale_revision"
	CodeAlreadyDecided Code = "already_decided"
	CodeTooEarly       Code = "too_early"
	CodeNotAllowed     Code = "not_allowed"
	CodeInternal       Code = "internal"
)

// Error is a bus error: what was wrong, what is valid, and how to fix it. What names a field
// or a rule, never the caller's value. Winner is set for already_decided.
type Error struct {
	Code             Code
	What, Valid, Fix string
	Winner           *Record
	cause            error // behind an internal error; never printed or sent, see Cause
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s (valid: %s)", e.Code, e.What, e.Valid)
}

// AsError finds a bus error in err's chain.
func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// Cause is the error behind an internal error, for the operator's log (Task 9 logs it
// server-side). Error() never prints it, and no field a caller sees carries it. It is nil for
// every other error. It is not Unwrap, so errors.Is on a bus error never reaches the cause.
func (e *Error) Cause() error { return e.cause }

func invalid(what, valid string) *Error {
	return &Error{Code: CodeInvalid, What: what, Valid: valid, Fix: "correct " + what + " and post again"}
}
