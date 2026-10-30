package core_test

import (
	"context"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/store"
)

// incCfg has two levels, low then page, each sending to names, with a 24h incident ttl.
func incCfg(reminder time.Duration, names ...string) *config.Config {
	lv := func(n string) config.Level {
		return config.Level{Name: n, Retry: retry5, Send: sends(names...), Reminder: reminder}
	}
	c := levelsCfg(5*time.Second, lv("low"), lv("page"))
	c.IncidentTTL = 24 * time.Hour
	return c
}

// say notifies level with a dedup key and a title.
func (e *env) say(level, key, title string) core.Result {
	e.t.Helper()
	r, err := e.p.Notify(context.Background(), channel.Message{Level: level, Title: title, DedupKey: key})
	if err != nil {
		e.t.Fatalf("Notify: %v", err)
	}
	return r
}

// incident is a copy of the stored incident, or an empty one when there is none (see hasIncident).
func (e *env) incident(key string) *store.Incident {
	e.t.Helper()
	out := &store.Incident{}
	err := e.st.WithIncidents(func(m map[string]*store.Incident) error {
		if inc := m[key]; inc != nil {
			c := *inc
			out = &c
		}
		return nil
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *env) hasIncident(key string) bool { return e.incident(key).Key != "" }

func (e *env) resolve(key, note string) core.Result {
	e.t.Helper()
	r, err := e.p.Resolve(context.Background(), key, note)
	if err != nil {
		e.t.Fatalf("Resolve: %v", err)
	}
	return r
}

func sentCount(e *env, name string) int { return len(e.fakes[name].Sent) }
