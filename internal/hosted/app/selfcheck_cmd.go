package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
	"github.com/Exploded-views/yo-human/internal/hosted/policy"
)

// runSelfcheck is `selfcheck`: in a temporary directory it opens a fresh database, has a node post
// an approval, has Duet record the owner's go on it and has the node read it back. It reads no
// configuration and, app importing no network package, makes no call.
func runSelfcheck(args []string, stdout, stderr io.Writer) int {
	a := admin{name: "selfcheck", stdout: stdout, stderr: stderr}
	if code, stop := a.parse(flag.NewFlagSet(a.name, flag.ContinueOnError), args); stop {
		return code
	}
	if err := selfcheck(); err != nil {
		a.say("selfcheck: %v", err)
		return ExitInternal
	}
	_, _ = fmt.Fprintln(stdout, "selfcheck: ok")
	return ExitOK
}

func selfcheck() error {
	dir, err := os.MkdirTemp("", "yh-selfcheck-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	return selfcheckIn(dir)
}

func selfcheckIn(dir string) error {
	d, err := db.Open(filepath.Join(dir, "bus.db"))
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return exerciseBus(context.Background(), newSelfcheckBus(d))
}

func newSelfcheckBus(s bus.Store) *bus.Bus {
	return bus.New(s, now, bus.Limits{LinkHosts: []string{"github.com"}, Policy: policy.Policy{V: 1, Rules: []policy.Rule{
		{Action: "merge", Repo: "o/r", Ref: "main", Mode: policy.Passkey},
	}}})
}

var (
	selfNode = bus.Principal{Kind: bus.PrincipalNode, Name: "selfcheck", Swarm: "crew"}
	selfDuet = bus.Principal{Kind: bus.PrincipalDuet, Name: "duet"}
)

// exerciseBus is the steps: the node posts a merge approval and a question, Duet records the
// owner's words on the approval (a relay: only the owner's passkey resolves it) and answers the
// question, and the node reads that answer from its swarm. A node never reads a relay.
func exerciseBus(ctx context.Context, b *bus.Bus) error {
	appr, err := b.Post(ctx, selfNode, bus.Draft{Kind: bus.KindApproval, Severity: bus.SeverityHigh, To: "human",
		Handle: "selfcheck-merge", Title: "Merge PR 7", Action: bus.ActionMerge, Target: "o/r#7", Repo: "o/r", Ref: "7",
		BaseRef: "main", PinnedCommit: strings.Repeat("0a", 20), Urgency: bus.UrgencyDigest})
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	q, err := b.Post(ctx, selfNode, bus.Draft{Kind: bus.KindQuestion, Severity: bus.SeverityNormal, To: "human",
		Handle: "selfcheck-ask", Title: "Ship it?", Action: bus.ActionNone, Urgency: bus.UrgencyDigest})
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	at := now()
	if _, err := b.RelayApproval(ctx, selfDuet, bus.ApprovalInput{ItemID: appr.Item.ID, Revision: appr.Item.Revision,
		Decision: bus.DecisionApprove, Text: "yes, merge it", HumanTS: at}); err != nil {
		return fmt.Errorf("record: %w", err)
	}
	if _, err := b.RecordAnswer(ctx, selfDuet, bus.AnswerInput{ItemID: q.Item.ID, Revision: q.Item.Revision, Text: "yes", HumanTS: at}); err != nil {
		return fmt.Errorf("record: %w", err)
	}
	ups, err := b.Answers(ctx, selfNode, 0, 0)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	return checkRead(ups, q.Item.ID)
}

// checkRead wants exactly one update: the answer to the question.
func checkRead(ups bus.Updates, questionID string) error {
	if len(ups.Events) != 1 || ups.Events[0].Type != bus.EventAnswerRecorded || ups.Events[0].ItemID != questionID {
		return errors.New("answer: the node did not read the answer Duet recorded")
	}
	return nil
}
