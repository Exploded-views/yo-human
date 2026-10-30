package cli

import (
	"context"
	"maps"
	"path/filepath"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
)

// now is the clock every command reads. Only a test sets clockHook.
func now() time.Time {
	if clockHook != nil {
		return clockHook()
	}
	return time.Now()
}

// newID is a message id: a ULID, so ids sort by time.
func newID() string { return ulid.Make().String() }

// loadWithStore is load, with the state it opened to read the trust records. Once the config is
// read, the state's waits for the incident lock end with the config's deadline, counted from here,
// so a stopped process holding the lock cannot stall the command (constitution VII).
func (e *env) loadWithStore() (*config.Config, *core.State, error) {
	st, err := core.OpenState(stateDir())
	if err != nil {
		return nil, nil, err
	}
	c, notices, err := config.Load(e.paths(), channelTypes(), st)
	e.addNotices(notices)
	if err != nil {
		return c, st, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.Deadline)
	e.cleanup = append(e.cleanup, cancel)
	return c, st.Within(ctx), nil
}

// capsOf is what each configured instance can do, by instance name. It needs no secret.
func capsOf(cfg *config.Config) map[string]channel.Capabilities {
	reg := channelTypes()
	out := make(map[string]channel.Capabilities, len(cfg.Channels))
	for name, ci := range cfg.Channels {
		out[name] = reg[ci.Type].Capabilities
	}
	return out
}

// configPath is the explicit config file of this run, made absolute so a flush from another
// directory can name the same file.
func (e *env) configPath() string {
	p := e.paths().Explicit
	if abs, err := filepath.Abs(p); p != "" && err == nil {
		return abs
	}
	return p
}

func (e *env) pipeline(cfg *config.Config, st *core.State, chans map[string]channel.Channel) *core.Pipeline {
	return st.Pipeline(core.Deps{Config: cfg, Channels: chans, Caps: capsOf(cfg), Now: now, NewID: newID, ConfigPath: e.configPath()})
}

// buildFailures are the channels that could not be built, by instance name.
type buildFailures map[string]error

// channelsFor builds the channels a command works with. The must instances are the command's own:
// one that cannot be built fails the command. The instances of entries due in the queue, and the
// optional ones, are built too, best effort: a failure is reported in the failures, and its entries
// stay queued. A due entry for a channel this config does not name belongs to another config; it is
// left for core to park, and is no failure. No secret is read when there is nothing to build.
func (e *env) channelsFor(cfg *config.Config, st *core.State, must, optional []string) (chans map[string]channel.Channel, failed buildFailures, err error) {
	// A recovered entry is due, so its channel is built too. A recovery that fails is left to the
	// core call every command makes next (notify, resolve, or the flush before the others), which
	// runs it again and reports the failure.
	_ = st.Recover(cfg)
	due, err := st.DueInstances(now())
	if err != nil {
		return nil, nil, err
	}
	chans = map[string]channel.Channel{}
	if len(must)+len(due)+len(optional) == 0 {
		return chans, nil, nil
	}
	vals, err := e.secretValues()
	if err != nil {
		return nil, nil, err
	}
	if chans, err = buildChannels(cfg, channelTypes(), vals, must); err != nil {
		return nil, nil, err
	}
	failed = buildFailures{}
	for _, name := range sortedNames(setOf(due, optional)) {
		if _, built := chans[name]; built {
			continue
		}
		if _, configured := cfg.Channels[name]; !configured {
			continue
		}
		one, berr := buildChannels(cfg, channelTypes(), vals, []string{name})
		maps.Copy(chans, one)
		if berr != nil {
			failed[name] = berr
		}
	}
	return chans, failed, nil
}

func setOf(lists ...[]string) map[string]bool {
	set := map[string]bool{}
	for _, l := range lists {
		for _, n := range l {
			set[n] = true
		}
	}
	return set
}

// warnBacklog reports the channels that could not be built, as warnings.
func (e *env) warnBacklog(failed buildFailures) {
	for _, name := range sortedNames(failed) {
		e.warn(flatten(failed[name]))
	}
}

// flushFirst sends what is due before a read-only command (status, levels), under the deadline.
// Every failure is a warning: the command it precedes goes on. With nothing due it builds nothing
// and reads no secret.
func (e *env) flushFirst(cfg *config.Config, st *core.State) {
	chans, failed, err := e.channelsFor(cfg, st, nil, nil)
	if err != nil {
		e.warn(flatten(err))
	}
	e.warnBacklog(failed)
	ctx, cancel := deadlineCtx(cfg)
	defer cancel()
	e.flushWarn(ctx, e.pipeline(cfg, st, chans))
}

// flushWarn sends what is due under ctx. Its failures are warnings: the command it precedes goes on.
func (e *env) flushWarn(ctx context.Context, p *core.Pipeline) {
	if _, err := p.Flush(ctx); err != nil {
		e.warn(flatten(err))
	}
}

// report prints a command's result, its warnings and the problems that came with it. A result with
// no status means the command failed before it had one.
func (e *env) report(res core.Result, err error) int {
	for _, w := range res.Warnings {
		e.warn(flatten(w))
	}
	if err != nil && res.Status == "" {
		return e.fail(err)
	}
	return e.finish(&res, err)
}

// deadlineCtx is the context a single call runs under: the config's deadline.
func deadlineCtx(cfg *config.Config) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), cfg.Deadline)
}

// levelNamed is the level called name. The caller has already planned it, so it exists.
func levelNamed(cfg *config.Config, name string) (found config.Level) {
	for _, l := range cfg.Levels {
		if l.Name == name {
			found = l
		}
	}
	return found
}
