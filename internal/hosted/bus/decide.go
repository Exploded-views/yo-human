package bus

import (
	"context"
	"time"
)

// AnswerInput is a question's or blocker's answer as a caller submits it.
type AnswerInput struct {
	ItemID        string
	Revision      int
	Text          string
	HumanTS       time.Time
	Source        string
	SourceMessage string
}

// ApprovalInput is an approval's decision as a caller submits it.
type ApprovalInput struct {
	ItemID   string
	Revision int
	Decision Decision
	Text     string
	HumanTS  time.Time
	Source   string
}

// RelayInput is the owner's words as a node or Duet passes them on.
type RelayInput struct {
	ItemID   string
	Revision int
	Text     string
	HumanTS  time.Time
	Source   string
}

func notBuilt() *Error {
	return &Error{Code: CodeInternal, What: "decide", Valid: "-", Fix: "-"}
}

// RecordAnswer is a stub.
func (b *Bus) RecordAnswer(ctx context.Context, p Principal, in AnswerInput) (Record, error) {
	return Record{}, notBuilt()
}

// RecordApproval is a stub.
func (b *Bus) RecordApproval(ctx context.Context, p Principal, in ApprovalInput) (Record, error) {
	return Record{}, notBuilt()
}

// RelayWords is a stub.
func (b *Bus) RelayWords(ctx context.Context, p Principal, in RelayInput) (Relay, error) {
	return Relay{}, notBuilt()
}
