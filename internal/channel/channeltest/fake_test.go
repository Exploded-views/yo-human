package channeltest_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/channeltest"
)

func TestFakeRecordsAndScripts(t *testing.T) {
	f := channeltest.NewFake(channel.Capabilities{ReachesHuman: true, MaxActions: 2})
	if f.Type() != "fake" || !f.Capabilities().ReachesHuman || f.Capabilities().MaxActions != 2 {
		t.Errorf("Type/Capabilities not as constructed: %q %+v", f.Type(), f.Capabilities())
	}
	if err := f.Validate(map[string]any{"a": 1}); err != nil {
		t.Errorf("Validate = %v", err)
	}
}

func TestFakeSendRecordsAndScripts(t *testing.T) {
	f := channeltest.NewFake(channel.Capabilities{})
	m := channel.Message{ID: "01A", Title: "t"}
	s := channel.SendSpec{Instance: "i", Interruption: channel.Active}
	rc, err := f.Send(context.Background(), m, s)
	if err != nil {
		t.Fatalf("default send failed: %v", err)
	}
	if rc.Instance != "i" {
		t.Errorf("receipt instance = %q, want the spec's", rc.Instance)
	}
	if len(f.Sent) != 1 || f.Sent[0].ID != "01A" || f.Specs[0].Interruption != channel.Active {
		t.Errorf("not recorded: %+v %+v", f.Sent, f.Specs)
	}

	boom := errors.New("down")
	f.SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{}, channel.Transient(boom)
	}
	_, err = f.Send(context.Background(), m, s)
	if !errors.Is(err, boom) || channel.KindOf(err) != channel.TransientError {
		t.Errorf("scripted error = %v", err)
	}
	if len(f.Sent) != 2 {
		t.Errorf("a failed send is still recorded; Sent = %d", len(f.Sent))
	}
}

func TestFakeRecordsResolveAndCancel(t *testing.T) {
	f := channeltest.NewFake(channel.Capabilities{})
	m := channel.Message{ID: "01A"}
	if err := f.Resolve(context.Background(), "key", m); err != nil || len(f.Resolved) != 1 || f.Resolved[0] != "key" {
		t.Errorf("Resolve: %v %v", err, f.Resolved)
	}
	r := channel.Receipt{Instance: "i", ProviderID: "p"}
	if err := f.Cancel(context.Background(), r); err != nil || len(f.Cancelled) != 1 || f.Cancelled[0] != r {
		t.Errorf("Cancel: %v %v", err, f.Cancelled)
	}
}

func TestFakeIsConcurrentSafe(t *testing.T) {
	f := channeltest.NewFake(channel.Capabilities{})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = f.Send(context.Background(), channel.Message{}, channel.SendSpec{})
			_ = f.Resolve(context.Background(), "k", channel.Message{})
			_ = f.Cancel(context.Background(), channel.Receipt{})
		}()
	}
	wg.Wait()
	if len(f.Sent) != 50 || len(f.Resolved) != 50 || len(f.Cancelled) != 50 {
		t.Errorf("recorded %d/%d/%d, want 50 each", len(f.Sent), len(f.Resolved), len(f.Cancelled))
	}
}
