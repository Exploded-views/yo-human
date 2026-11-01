package channel

import "errors"

// ErrorKind tells the pipeline what to do with a failed send.
type ErrorKind int

const (
	// TransientError is retried.
	TransientError ErrorKind = iota
	// PermanentError goes to the dead letter.
	PermanentError
	// ConfigError exits 78.
	ConfigError
)

// kinded is an error carrying its kind; it unwraps to the cause.
type kinded struct {
	kind        ErrorKind
	err         error
	unconfirmed bool // a transient error whose request went out with no answer read
}

func (e *kinded) Error() string { return e.err.Error() }
func (e *kinded) Unwrap() error { return e.err }

func mark(kind ErrorKind, err error) error {
	if err == nil {
		return nil
	}
	return &kinded{kind: kind, err: err}
}

// Transient marks err as worth retrying.
func Transient(err error) error { return mark(TransientError, err) }

// Permanent marks err as a rejection that retrying will not fix.
func Permanent(err error) error { return mark(PermanentError, err) }

// Misconfigured marks err as a configuration fault.
func Misconfigured(err error) error { return mark(ConfigError, err) }

// KindOf returns the kind of the outermost marked error in err's chain; an unmarked error counts
// as TransientError.
func KindOf(err error) ErrorKind {
	var k *kinded
	if errors.As(err, &k) {
		return k.kind
	}
	return TransientError
}

// Unconfirmed marks err as transient, and as a send whose request was fully written but whose
// answer never arrived: the provider may have acted on it. Core retries it only where a repeat
// cannot be a duplicate, or a lost send would be worse than one.
func Unconfirmed(err error) error {
	if err == nil {
		return nil
	}
	return &kinded{kind: TransientError, err: err, unconfirmed: true}
}

// IsUnconfirmed reports whether the outermost marked error in err's chain is an Unconfirmed one.
func IsUnconfirmed(err error) bool {
	var k *kinded
	return errors.As(err, &k) && k.unconfirmed
}
