package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

var channelCommands = map[string]handler{"test": cmdChannelTest}

const channelTestUsage = `yo-human channel test <instance> [--interruption passive|active|time-sensitive|critical]`

// interruptionOrder is every interruption, quietest first: the order a problem lists them in.
var interruptionOrder = []channel.Interruption{channel.Passive, channel.Active, channel.TimeSensitive, channel.Critical}

// cmdChannelTest sends one test message through the named instance only, straight to the channel:
// nothing is queued, retried or recorded as an incident. It is passive unless --interruption names
// another, which the channel type must be able to send. Like every command it first sends what is due.
func cmdChannelTest(e *env, args []string) int {
	name, level, given, err := parseChannelTest(args)
	if err != nil {
		return e.fail(err)
	}
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
	if err := checkSendable(capsOf(cfg)[name], name, level); err != nil {
		return e.fail(err)
	}
	res, err := sendTest(ctx, chans[name], name, level)
	if given {
		res.Interruption = string(level)
	}
	above := level != channel.Passive && res.Status == core.StatusDelivered
	if above {
		res.DedupKey = res.ID // the test carries no key of its own, so the provider keys it by the message id
	}
	code := e.report(res, err)
	if above && !e.asJSON {
		_, _ = fmt.Fprintln(e.stdout, "close: channel test records no incident for resolve to find; resolve the incident in the provider, by the key above")
	}
	return code
}

// parseChannelTest reads the instance and --interruption, in either order. given tells whether the
// flag was present; a repeated flag takes its last value, as every flag does.
func parseChannelTest(args []string) (name string, level channel.Interruption, given bool, err error) {
	fs := newFlags("channel test")
	raw := fs.String("interruption", "", "")
	var names []string
	for rest := args; ; {
		if perr := fs.Parse(rest); perr != nil {
			return "", "", false, problem.Usagef("%s was given a flag it does not know, or --interruption with no value (the text is not echoed)", channelTestUsage,
				"check the spelling against the valid line", "channel test")
		}
		if fs.NArg() == 0 {
			break
		}
		names = append(names, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(names) != 1 {
		return "", "", false, problem.Usagef("channel test takes one channel name", channelTestUsage, "name a channel under channels: in the config")
	}
	fs.Visit(func(*flag.Flag) { given = true })
	level = channel.Passive
	if given {
		if level, err = channel.ParseInterruption(*raw); err != nil {
			return "", "", false, problem.Usagef("--interruption is not one of the four interruptions (the value is not echoed)", "one of "+interruptionList(interruptionOrder),
				"pass --interruption passive, active, time-sensitive or critical, or leave it out for passive")
		}
	}
	return names[0], level, given, nil
}

// checkSendable refuses an interruption the channel type cannot send, before anything is sent. It
// reads the type's capabilities, the same closed set the config check reads for a level's sends.
func checkSendable(caps channel.Capabilities, name string, level channel.Interruption) error {
	if caps.Interruptions[level] {
		return nil
	}
	var can []channel.Interruption
	for _, i := range interruptionOrder {
		if caps.Interruptions[i] {
			can = append(can, i)
		}
	}
	return problem.Usagef("channel %s cannot send %s", "an interruption it supports: "+interruptionList(can),
		"pass one of those with --interruption, or test a channel that supports "+string(level), name, level)
}

func interruptionList(is []channel.Interruption) string {
	out := make([]string, len(is))
	for i, v := range is {
		out[i] = string(v)
	}
	return strings.Join(out, ", ")
}

// sendTest makes the call and describes its outcome. A transient failure is "transient": the test
// may be tried again. A channel that rejects its configuration is a Config problem beside the result.
func sendTest(ctx context.Context, ch channel.Channel, name string, level channel.Interruption) (core.Result, error) {
	id := newID()
	msg := channel.Message{ID: id, Level: "test", Title: "yo-human test message", Project: "yo-human", Agent: "channel test", Created: now(),
		Ask: "nothing: this only tests channel " + name, Body: "If you can read this, channel " + name + " works."}
	_, err := ch.Send(ctx, msg, channel.SendSpec{Instance: name, Interruption: level})
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
