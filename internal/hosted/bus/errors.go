package bus

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

// Error is a bus error.
type Error struct {
	Code             Code
	What, Valid, Fix string
	Winner           *Record
}

func (e *Error) Error() string { return "" }

// AsError finds a bus error in err's chain.
func AsError(err error) (*Error, bool) { return nil, false }
