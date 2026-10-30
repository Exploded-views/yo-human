package core_test

import (
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/store"
)

func TestDecide(t *testing.T) {
	const ttl = 24 * time.Hour
	open := func(rank int, hash string, sentAgo time.Duration) *store.Incident {
		return &store.Incident{Key: "k", Open: true, Rank: rank, ContentHash: hash, LastSent: t0.Add(-sentAgo)}
	}
	closed := open(1, "h", time.Minute)
	closed.Open = false
	cases := []struct {
		name     string
		inc      *store.Incident
		rank     int
		hash     string
		reminder time.Duration
		want     core.Decision
	}{
		{"no incident", nil, 1, "h", 0, core.SendNew},
		{"a resolved incident", closed, 1, "h", 0, core.SendNew},
		{"same rank and hash", open(1, "h", time.Minute), 1, "h", 30 * time.Minute, core.Suppress},
		{"same rank and hash after the reminder", open(1, "h", 31*time.Minute), 1, "h", 30 * time.Minute, core.Remind},
		{"exactly at the reminder", open(1, "h", 30*time.Minute), 1, "h", 30 * time.Minute, core.Remind},
		{"no reminder is never due", open(1, "h", 20*time.Hour), 1, "h", 0, core.Suppress},
		{"higher rank", open(1, "h", time.Minute), 2, "h", 30 * time.Minute, core.SendRaised},
		{"higher rank beats the reminder", open(1, "h", time.Hour), 2, "h", 30 * time.Minute, core.SendRaised},
		{"same rank, changed hash", open(1, "h", time.Minute), 1, "x", 30 * time.Minute, core.UpdateNative},
		{"changed hash after the reminder", open(1, "h", time.Hour), 1, "x", 30 * time.Minute, core.Remind},
		{"lower rank", open(2, "h", time.Minute), 1, "h", 30 * time.Minute, core.Suppress},
		{"lower rank, changed hash", open(2, "h", time.Minute), 1, "x", 30 * time.Minute, core.Suppress},
		{"lower rank after the reminder", open(2, "h", time.Hour), 1, "h", 30 * time.Minute, core.Remind},
		{"expired", open(1, "h", ttl+time.Second), 1, "h", 0, core.SendNew},
		{"exactly the ttl is not expired", open(1, "h", ttl), 1, "h", 0, core.Suppress},
		{"expired beats a higher rank", open(1, "h", ttl+time.Second), 2, "h", 0, core.SendNew},
	}
	for _, c := range cases {
		if got := core.Decide(c.inc, c.rank, c.hash, c.reminder, ttl, t0); got != c.want {
			t.Errorf("%s: Decide = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestContentHash(t *testing.T) {
	base := channel.Message{Title: "t", Ask: "a", Body: "b", Link: "https://x.example", Actions: []channel.Action{{Label: "l", URL: "https://y.example"}}}
	h := core.ContentHash(base)
	if len(h) != 64 || h != core.ContentHash(base) {
		t.Fatalf("hash %q is not a stable sha256 hex", h)
	}
	same := base
	same.ID, same.Level, same.Created, same.Project, same.Agent = "other", "other", t0, "p", "a"
	if core.ContentHash(same) != h {
		t.Error("fields a human does not read changed the hash")
	}
	edits := map[string]func(*channel.Message){
		"title":        func(m *channel.Message) { m.Title = "t2" },
		"ask":          func(m *channel.Message) { m.Ask = "a2" },
		"body":         func(m *channel.Message) { m.Body = "b2" },
		"link":         func(m *channel.Message) { m.Link = "https://z.example" },
		"action label": func(m *channel.Message) { m.Actions = []channel.Action{{Label: "l2", URL: "https://y.example"}} },
		"action url":   func(m *channel.Message) { m.Actions = []channel.Action{{Label: "l", URL: "https://z.example"}} },
		"no actions":   func(m *channel.Message) { m.Actions = nil },
		"shifted text": func(m *channel.Message) { m.Title, m.Ask = "ta", "" },
	}
	for name, edit := range edits {
		m := base
		edit(&m)
		if core.ContentHash(m) == h {
			t.Errorf("a changed %s did not change the hash", name)
		}
	}
}
