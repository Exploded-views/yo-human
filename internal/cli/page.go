package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

const pageUsage = `yo-human page [--low] "<summary>" [details] [dedup-key]   or   yo-human page [--low] --resolve <dedup-key>   or   yo-human page --check`

// pageCall is a validated page: either a message to send, or a key to resolve.
type pageCall struct {
	low     bool
	resolve bool
	check   bool
	key     string
	msg     channel.Message
}

// parsePage reads and validates the arguments of the old paging script. Every rule that needs no
// config is checked here, with the same checks notify makes.
func (e *env) parsePage(args []string) (pageCall, error) {
	fs := newFlags("page")
	low := fs.Bool("low", false, "")
	resolve := fs.String("resolve", "", "")
	check := fs.Bool("check", false, "")
	if err := fs.Parse(args); errors.Is(err, flag.ErrHelp) {
		return pageCall{}, errHelp
	} else if err != nil {
		return pageCall{}, problem.Usagef("%s was given a flag it does not know, or a flag with no value (the text is not echoed)", pageUsage,
			`use only --low, --resolve <dedup-key> and --check, before the summary; use -- before a summary that starts with "-"`, "page")
	}
	call := pageCall{low: *low, resolve: setFlags(fs)["resolve"], check: *check}
	pos := fs.Args()
	switch {
	case call.check:
		return call, checkOnly(call, pos)
	case call.resolve:
		call.key = *resolve
		return call, resolveKeyOnly(call, pos)
	}
	if len(pos) < 1 || len(pos) > 3 {
		return call, problem.Usagef("page takes a summary, then optional details and a dedup key", pageUsage, `run yo-human page "<summary>" ["<details>" [<dedup-key>]]; use -- before a summary that starts with "-"`)
	}
	if err := refuseLoneWord(args, pos); err != nil {
		return call, err
	}
	return call, e.pageMessage(&call.msg, pos)
}

// checkOnly refuses anything given with --check: it takes no other argument and no other flag.
func checkOnly(call pageCall, pos []string) error {
	if len(pos) > 0 || call.low || call.resolve {
		return problem.Usagef("page --check takes no other argument and no other flag", pageUsage, "run `yo-human page --check` on its own; it prints the shim's mapping and sends nothing")
	}
	return nil
}

// resolveKeyOnly checks a --resolve call: a key, and nothing else.
func resolveKeyOnly(call pageCall, pos []string) error {
	if len(pos) > 0 {
		return problem.Usagef("page --resolve takes a key and nothing else", pageUsage, "run `yo-human page --resolve <dedup-key>`")
	}
	if call.key == "" {
		return problem.Usagef("page --resolve needs a dedup key", pageUsage, "pass the key the incident was opened with")
	}
	return checkKeyAs("the dedup key after --resolve", call.key)
}

// pageFlagWords are page's own flags, the help and version flags, and the global --json and
// --config, spelled as a caller might type them among page's arguments. The exact global forms
// (`--json`, `--config <file>`) are taken out before page sees its arguments, so one that arrives
// here is a near miss (`-json`, `--JSON`, `--json=true`, `-config f`) and must not page.
var pageFlagWords = map[string]bool{
	"--low": true, "-low": true, "--check": true, "-check": true, "--resolve": true, "-resolve": true,
	"--help": true, "-help": true, "-h": true, "--version": true, "-version": true, "-v": true,
	"--json": true, "-json": true, "--config": true, "-config": true,
}

// isPageFlagWord reports whether a positional argument is really a flag: Go's flag parsing stops at
// the first positional, so a flag typed after the summary arrives here as text. Case, surrounding
// space and an =value do not hide it.
func isPageFlagWord(a string) bool {
	w := strings.ToLower(strings.TrimSpace(a))
	if name, _, ok := strings.Cut(w, "="); ok {
		w = name
	}
	return pageFlagWords[w]
}

// refuseLoneWord refuses, unless -- came first, a page whose arguments hold a flag (one typed after
// the summary, which would page with the flag as text: `page "test" --check`, meant as a dry run),
// or whose only argument is "version" or "help" (any case), a request for information, not a
// summary. After --, every argument is text on purpose.
func refuseLoneWord(args, pos []string) error {
	if len(args) > len(pos) && args[len(args)-len(pos)-1] == "--" {
		return nil
	}
	for _, a := range pos {
		if isPageFlagWord(a) {
			return problem.Usagef("page was given %q among its arguments, where it is not read as a flag; nothing was sent", pageUsage,
				"put the flags before the summary (yo-human page --low \"<summary>\"); to send that text on purpose, put -- before the summary", strings.TrimSpace(a))
		}
	}
	if w := strings.ToLower(strings.TrimSpace(pos[0])); len(pos) == 1 && (w == "version" || w == "help") {
		return problem.Usagef("page was given only %q, which looks like a request for the version or help, not a summary; nothing was sent", pageUsage,
			"to send that word as the summary on purpose, put -- before it: yo-human page -- <summary>; yo-human help prints the usage", w)
	}
	return nil
}

// pageMessage fills m from the positional arguments: summary, details, key.
func (e *env) pageMessage(m *channel.Message, pos []string) error {
	var err error
	if m.Title, err = titleAs("the summary (first argument)", "pass the summary as the first argument",
		"shorten the summary and put the detail in the details (second argument)", pos[0]); err != nil {
		return err
	}
	if len(pos) > 1 {
		if m.Body, err = readText(strings.NewReader(pos[1]), "the details (second argument)"); err != nil {
			return err
		}
	}
	m.DedupKey = now().UTC().Format("orch-20060102T150405Z") + "-" + randomHex(2)
	if len(pos) > 2 && pos[2] != "" { // as the old script's ${3:-default}: empty is absent
		m.DedupKey = pos[2]
	}
	if m.Project, err = sourceArg("--project", "", "YO_HUMAN_PROJECT"); err != nil {
		return err
	}
	return checkKeyAs("the dedup key (third argument)", m.DedupKey)
}

// noShim is the config problem for a page with no shim block to map it.
func noShim() error {
	return problem.Configf("yo-human page needs a shim: block in the config, and there is none",
		"shim: with default and low (each a level and an ask), and an agent",
		"add `shim: {default: {level: <level>, ask: <ask>}, low: {level: <level>, ask: <ask>}, agent: <name>}` to the config")
}

// shimModeOf is the shim's level and ask for a call, and the config path of its ask; a mode with
// no level is a config problem.
func shimModeOf(shim *config.Shim, low bool) (config.ShimMode, string, error) {
	mode, path := shim.Default, "shim.default"
	if low {
		mode, path = shim.Low, "shim.low"
	}
	if mode.Level == "" {
		return mode, "", problem.Configf("%s.level is not set", "a level name from the config", "set "+path+".level to one of the names under levels", path)
	}
	return mode, path + ".ask", nil
}

// pageAgent is YO_HUMAN_AGENT when set, else the shim's agent.
func pageAgent(cfg *config.Config) string {
	if v := flatLine(os.Getenv("YO_HUMAN_AGENT")); v != "" && utf8.RuneCountInString(v) <= maxSourceChars {
		return v
	}
	if v := flatLine(cfg.Shim.Agent); v != "" {
		return v
	}
	return "unknown"
}

// cmdPage is the drop-in for the old paging script: it maps onto notify and resolve through the
// config's shim block.
func cmdPage(e *env, args []string) int {
	call, err := e.parsePage(args)
	if err != nil {
		return e.fail(err)
	}
	cfg, st, err := e.loadWithStore()
	if err != nil {
		return e.fail(err)
	}
	if cfg.Shim == nil {
		return e.fail(noShim())
	}
	if call.check {
		return e.pageCheck(cfg)
	}
	if call.resolve {
		return e.pageResolve(cfg, st, call)
	}
	mode, askPath, err := shimModeOf(cfg.Shim, call.low)
	if err != nil {
		return e.fail(err)
	}
	call.msg.Level, call.msg.Ask, call.msg.Agent = mode.Level, flatLine(mode.Ask), pageAgent(cfg)
	return e.sendNotify(cfg, st, notifyCall{level: mode.Level, msg: call.msg, askPath: askPath, showKey: true})
}

// pageResolve is the shim's --resolve. An open incident under the project is resolved as `resolve`
// does. Otherwise the old script would still have told PagerDuty, so the shim passes the resolve
// through (see core.Pipeline.ResolveThrough): it never ends as a noop.
func (e *env) pageResolve(cfg *config.Config, st *core.State, call pageCall) int {
	proj, err := sourceArg("--project", "", "YO_HUMAN_PROJECT")
	if err != nil {
		return e.fail(err)
	}
	if core.LooksNamespaced(call.key) {
		return e.fail(problem.Usagef("the dedup key after --resolve looks like an internal provider key (it is not echoed); nothing was sent",
			"the key the page was opened with", "pass the key the page was opened with, not one a provider shows"))
	}
	again := "yo-human page --resolve " + problem.ShellQuote(call.key)
	// Another project's incidents are looked for only by a caller with no project of its own (the
	// gate of `status --all`); a project-scoped agent never sees or closes another project's.
	scoped := flatLine(os.Getenv("YO_HUMAN_PROJECT")) != ""
	src, err := st.ResolveSource(cfg, proj, call.key, now(), !scoped)
	if err != nil {
		return e.reportResolve(core.Result{}, err, again)
	}
	switch src.Kind {
	case core.SourceOpen:
		return e.closeIncident(cfg, st, core.IncidentKey(proj, call.key), "", false, again)
	case core.SourceOther:
		return e.closeIncident(cfg, st, core.IncidentKey(src.Projects[0], call.key), "", false, again)
	case core.SourceSeveral:
		return e.fail(problem.Usagef("page --resolve found the key open under %d other projects (%s) and none under this one; nothing was changed",
			"one project's incident to close", "set YO_HUMAN_PROJECT to the project to close and run the resolve again",
			len(src.Projects), strings.Join(src.Projects, ", ")))
	}
	return e.passResolve(cfg, st, call, proj, src, again)
}

// passResolve resolves a key yo-human holds no open incident for. Where a record still lists the
// instances it was delivered to, exactly those are told; otherwise every instance of the shim
// mode's level that can resolve is, with the bare key.
func (e *env) passResolve(cfg *config.Config, st *core.State, call pageCall, proj string, src core.ResolveSource, again string) int {
	level, instances, ferr := resolvable(cfg, call.low)
	reached := src.Reached
	if src.Closed { // the human heard of it: only the channels that can resolve are told again
		caps := capsOf(cfg)
		reached = slices.DeleteFunc(slices.Clone(reached), func(r core.Reach) bool {
			c, configured := caps[r.Instance]
			return configured && !c.Resolve // one gone from the config is kept: its close parks, visibly
		})
	}
	if len(reached) == 0 { // no record to answer with: the level's resolvers are told, and only then built
		for _, name := range instances {
			reached = append(reached, core.Reach{Instance: name, Delivered: true})
		}
	}
	if len(reached) == 0 { // nothing to tell and no resolver at the level: say which level, and send nothing
		return e.report(core.Result{Status: core.StatusFailed, ID: newID()}, ferr)
	}
	chans, problems := e.reachedChannels(cfg, st, reached)
	sendProject := ""                                  // no project: the bare key, which is what the old script sent
	if flatLine(os.Getenv("YO_HUMAN_PROJECT")) != "" { // the same test as the gate above
		sendProject = proj
	}
	res, err := e.pipeline(cfg, st, chans).ResolveThrough(context.Background(), proj, sendProject, call.key, level, instances)
	return e.reportResolve(res, errors.Join(append(problems, err)...), again)
}

// resolvable is the shim mode's level and the instances of it that can resolve an alert, once each,
// in config order. A level with none is an Unavailable problem naming it: the alert cannot be
// closed from here, and a silent success would leave it open.
func resolvable(cfg *config.Config, low bool) (string, []string, error) {
	level, path := cfg.Shim.Default.Level, "shim.default" // the config check has made the level one of the config's
	if low {
		level, path = cfg.Shim.Low.Level, "shim.low"
	}
	caps := capsOf(cfg)
	var names []string
	for _, s := range levelNamed(cfg, level).Send {
		if caps[s.Channel].Resolve && !slices.Contains(names, s.Channel) {
			names = append(names, s.Channel)
		}
	}
	if len(names) == 0 {
		return "", nil, &problem.Problem{Kind: problem.Unavailable,
			What:  "level " + level + " has no channel that can resolve an alert, so page --resolve has nothing to close; nothing was sent",
			Valid: "a level with a channel that can resolve (type pagerduty)",
			Fix:   "tell the human: the alert is still open and they resolve it there; to fix the shim, point " + path + ".level at a level that sends to a pagerduty channel"}
	}
	return level, names, nil
}

// checkSend and checkMode are the JSON shapes of `page --check`: instance names and interruptions
// only, never a server, topic, key or path.
type checkSend struct {
	Channel      string `json:"channel"`
	Interruption string `json:"interruption"`
}

type checkMode struct {
	Level string      `json:"level"`
	Ask   string      `json:"ask"`
	Sends []checkSend `json:"sends"`
}

// sendsText is the sends of one level as one line: each "<channel> <interruption>", joined by ", ",
// or "none".
func sendsText(s []checkSend) string {
	if len(s) == 0 {
		return "none"
	}
	parts := make([]string, len(s))
	for i, x := range s {
		parts[i] = x.Channel + " " + x.Interruption
	}
	return strings.Join(parts, ", ")
}

// sendsOf is the sends of one level in config order: instance names and interruptions, never a
// destination. It is never nil, so it encodes as [] in JSON.
func sendsOf(l config.Level) []checkSend {
	out := make([]checkSend, 0, len(l.Send))
	for _, s := range l.Send {
		out = append(out, checkSend{Channel: s.Channel, Interruption: string(s.Interruption)})
	}
	return out
}

// checkModeOf is what one mode of the shim sends: its level, ask and the level's sends in config
// order. It reads the config only.
func checkModeOf(cfg *config.Config, low bool) (checkMode, error) {
	mode, path, err := shimModeOf(cfg.Shim, low)
	if err != nil {
		return checkMode{}, err
	}
	out := checkMode{Level: mode.Level, Ask: flatLine(mode.Ask), Sends: []checkSend{}}
	for _, l := range cfg.Levels {
		if l.Name != mode.Level {
			continue
		}
		out.Sends = sendsOf(l)
		return out, nil
	}
	return out, problem.Configf("%s names no level in the config", "a level name from the config",
		"set "+strings.TrimSuffix(path, ".ask")+".level to one of the names under levels", strings.TrimSuffix(path, ".ask"))
}

// pageCheck prints what `page` and `page --low` would send, and sends nothing: it opens no channel,
// flushes nothing and changes no queue entry or incident, so it is safe to run anywhere.
func (e *env) pageCheck(cfg *config.Config) int {
	if err := shimAskError(cfg); err != nil {
		return e.fail(err)
	}
	def, err := checkModeOf(cfg, false)
	if err != nil {
		return e.fail(err)
	}
	low, err := checkModeOf(cfg, true)
	if err != nil {
		return e.fail(err)
	}
	agent := flatLine(cfg.Shim.Agent)
	if agent == "" {
		agent = "unknown"
	}
	line := func(label string, m checkMode) []string {
		return []string{
			fmt.Sprintf("%s level %s, ask %q", label, m.Level, m.Ask),
			"            sends: " + sendsText(m.Sends),
		}
	}
	lines := append(line("page:      ", def), line("page --low:", low)...)
	lines = append(lines, "agent: "+agent+" (YO_HUMAN_AGENT wins when it is set)")
	e.ok(lines, map[string]any{"default": def, "low": low, "agent": agent})
	return ExitOK
}

// randomHex is n random bytes in hex: the suffix that makes the shim's default key its own, so two
// pages in one second are two incidents and nobody can predict the key to resolve it.
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // crypto/rand.Read never fails on supported platforms
	return hex.EncodeToString(b)
}

// shimAskError is the empty-ask problems of the shim as one error, or nil. Only the two no-send
// checks call it: a real page refuses at askMissing, and no other command is held up by it.
func shimAskError(cfg *config.Config) error {
	var errs []error
	for _, p := range config.ShimAskProblems(cfg) {
		errs = append(errs, p)
	}
	return errors.Join(errs...)
}
