package cli

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// parseNotify reads and validates the flags of a notify. Every rule that needs no config is
// checked here, before the config is read, so a malformed call costs almost nothing.
func (e *env) parseNotify(args []string) (notifyCall, error) {
	fs := newFlags("notify")
	var actions repeated
	level := fs.String("level", "", "")
	title := fs.String("title", "", "")
	ask := fs.String("ask", "", "")
	body := fs.String("body", "", "")
	bodyFile := fs.String("body-file", "", "")
	link := fs.String("link", "", "")
	project := fs.String("project", "", "")
	agent := fs.String("agent", "", "")
	key := fs.String("dedup-key", "", "")
	fs.Var(&actions, "action", "")
	if err := parseFlags(fs, "notify", notifyUsage, args); err != nil {
		return notifyCall{}, err
	}
	set := setFlags(fs)
	call := notifyCall{level: *level}
	if call.level == "" || !utf8.ValidString(call.level) {
		return call, problem.Usagef("--level is missing", "a level name from the config", `run "yo-human levels --json" to read them, then pass --level <name>`)
	}
	m := &call.msg
	m.Level, m.Ask, m.DedupKey, m.Link = call.level, flatLine(*ask), *key, *link
	steps := []func() error{
		func() (err error) { m.Project, err = callerProject(*project); return },
		func() (err error) { m.Agent, err = sourceArg("--agent", *agent, "YO_HUMAN_AGENT"); return },
		func() (err error) { m.Title, err = titleOf(*title); return },
		func() error { return askOf(*ask) },
		func() error { return checkKey(*key) },
		func() (err error) { return linkOf(*link, set["link"]) },
		func() (err error) { m.Actions, err = parseActions(actions); return },
		func() (err error) { m.Body, err = e.bodyOf(*body, *bodyFile, set["body"], set["body-file"]); return },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return call, err
		}
	}
	return call, nil
}

// titleOf flattens a title to one line and checks its length.
func titleOf(raw string) (string, error) {
	return titleAs("--title", `add --title "<what happened>"`, "shorten the title and put the detail in --body", raw)
}

// titleAs is titleOf for a caller that names the title its own way and has its own fixes.
func titleAs(label, missingFix, longFix, raw string) (string, error) {
	if err := utf8Check(label, raw); err != nil {
		return "", err
	}
	title := flatLine(raw)
	if title == "" {
		return "", problem.Usagef("%s is missing or blank", "a one-line title of 1 to 200 characters", missingFix, label)
	}
	if n := utf8.RuneCountInString(title); n > maxTitleChars {
		return "", problem.Usagef("%s is %d characters", "at most 200 characters", longFix, label, n)
	}
	return title, nil
}

// linkOf validates --link when it was given.
func linkOf(link string, given bool) error {
	if !given {
		return nil
	}
	return checkLink("--link", link)
}

// askMissing is the problem for a level that needs an ask and got none. A call from the page shim
// has no --ask to add: its ask comes from the config at askPath.
func askMissing(level, askPath string) error {
	if askPath != "" {
		return problem.Configf("level %q needs an ask, and %s is empty", "a sentence saying what the human must do",
			"set "+askPath+" in the shim: block of the config", level, askPath)
	}
	return problem.Usagef("level %q needs --ask, and none was given", `a sentence saying what the human must do, or a level with "ask: optional"`,
		`add --ask "<what the human must do>"`, level)
}

// cmdNotify sends a message at the chosen level. It builds only the channels that level sends to
// (and those of the backlog it flushes), each on its own, so one channel's unset secret cannot
// block the others.
func cmdNotify(e *env, args []string) int {
	call, err := e.parseNotify(args)
	if err != nil {
		return e.fail(err)
	}
	cfg, st, err := e.loadWithStore()
	if err != nil {
		return e.fail(err)
	}
	return e.sendNotify(cfg, st, call)
}

// sendNotify delivers a validated call under a loaded config; notify and the page shim share it.
func (e *env) sendNotify(cfg *config.Config, st *core.State, call notifyCall) int {
	plan, err := core.Plan(cfg, call.level, now(), capsOf(cfg))
	if err != nil {
		return e.fail(err)
	}
	if lv := levelNamed(cfg, call.level); lv.AskRequired && call.msg.Ask == "" {
		return e.fail(askMissing(call.level, call.askPath))
	}
	found, err := e.redact(messageFields(&call.msg)...)
	if err != nil {
		return e.fail(err)
	}
	used := map[string]bool{}
	for _, p := range plan {
		used[p.Instance] = true
	}
	chans, failed, err := e.channelsFor(cfg, st, nil, sortedNames(used))
	if err != nil {
		return e.fail(err)
	}
	res, err := e.pipeline(cfg, st, chans).Notify(context.Background(), call.msg)
	e.warnRedacted(found, res)
	if call.showKey {
		res.DedupKey = call.msg.DedupKey
	}
	return e.report(skippedProblems(res, err, failed))
}

// skippedProblems adds the build failure of each channel the result skipped. With nothing left
// pending it is an error, so the call exits 78 and the human hears of it; while another send is
// queued it is a warning, so the agent first follows the next step (exit 75). A failure for a
// channel this message did not go to (a backlog entry's, or any on a suppressed repeat) is a
// warning too.
func skippedProblems(res core.Result, err error, failed buildFailures) (core.Result, error) {
	skipped := map[string]bool{}
	for _, c := range res.Channels {
		skipped[c.Channel] = skipped[c.Channel] || c.Status == core.ChannelSkipped
	}
	var errs []error
	for _, name := range sortedNames(failed) {
		if skipped[name] && res.Status != core.StatusQueued {
			errs = append(errs, failed[name])
		} else {
			res.Warnings = append(res.Warnings, failed[name])
		}
	}
	return res, errors.Join(append(errs, err)...)
}

// askOf checks the length of --ask.
func askOf(raw string) error {
	if err := utf8Check("--ask", raw); err != nil {
		return err
	}
	if n := utf8.RuneCountInString(raw); n > maxAskChars {
		return problem.Usagef("--ask is %d characters", fmt.Sprintf("at most %d characters", maxAskChars),
			"say in a sentence or two what the human must do, and put the detail in --body", n)
	}
	return nil
}
