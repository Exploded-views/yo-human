package cli

import (
	"context"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

var channelCommands = map[string]handler{"test": cmdChannelTest}

// cmdChannelTest sends one passive test message through the named instance only, straight to the
// channel: nothing is queued, retried or recorded as an incident. Like every command it first
// sends what is due.
func cmdChannelTest(e *env, args []string) int {
	if len(args) != 1 {
		return e.fail(problem.Usagef("channel test takes one channel name", "yo-human channel test <instance>", "name a channel under channels: in the config"))
	}
	name := args[0]
	cfg, st, err := e.loadWithStore()
	if err != nil {
		return e.fail(err)
	}
	chans, failed, err := e.channelsFor(cfg, st, []string{name}, nil)
	if err != nil {
		return e.fail(err)
	}
	e.warnBacklog(failed)
	ctx, cancel := deadlineCtx(cfg) // one deadline for the flush and the test
	defer cancel()
	e.flushWarn(ctx, e.pipeline(cfg, st, chans))
	return e.report(sendTest(ctx, chans[name], name))
}

// sendTest makes the call and describes its outcome. A transient failure is "transient": the test
// may be tried again. A channel that rejects its configuration is a Config problem beside the result.
func sendTest(ctx context.Context, ch channel.Channel, name string) (core.Result, error) {
	id := newID()
	msg := channel.Message{ID: id, Level: "test", Title: "yo-human test message", Project: "yo-human", Agent: "channel test", Created: now(),
		Ask: "nothing: this only tests channel " + name, Body: "If you can read this, channel " + name + " works."}
	_, err := ch.Send(ctx, msg, channel.SendSpec{Instance: name, Interruption: channel.Passive})
	if err == nil {
		return core.Result{Status: core.StatusDelivered, ID: id, Channels: []core.ChannelResult{{Channel: name, Status: core.ChannelSent}}}, nil
	}
	out := core.ChannelResult{Channel: name, Status: core.ChannelDead, Error: err.Error()}
	if channel.KindOf(err) == channel.TransientError {
		out.Status, out.Error = "transient", err.Error()+" (transient; try again)"
	}
	res := core.Result{Status: core.StatusFailed, ID: id, Channels: []core.ChannelResult{out}}
	if channel.KindOf(err) != channel.ConfigError {
		return res, nil
	}
	return res, problem.Configf("channel %s rejected its configuration: %s", "a channel whose settings its provider accepts",
		"correct channels."+name+" in the config", name, err.Error())
}
