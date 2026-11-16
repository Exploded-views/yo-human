package bus

import "context"

// RelayApproval is a stub until the fix commit.
func (b *Bus) RelayApproval(ctx context.Context, p Principal, in ApprovalInput) (Relay, error) {
	return Relay{}, &Error{Code: CodeInternal, What: "the bus", Valid: "a retry", Fix: "stub"}
}
