package cli

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
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
		proj, err := sourceArg("--project", "", "YO_HUMAN_PROJECT")
		if err != nil {
			return e.fail(err)
		}
		return e.closeIncident(cfg, st, core.IncidentKey(proj, call.key), "", false, "yo-human page --resolve "+problem.ShellQuote(call.key))
	}
	mode, askPath, err := shimModeOf(cfg.Shim, call.low)
	if err != nil {
		return e.fail(err)
	}
	call.msg.Level, call.msg.Ask, call.msg.Agent = mode.Level, flatLine(mode.Ask), pageAgent(cfg)
	return e.sendNotify(cfg, st, notifyCall{level: mode.Level, msg: call.msg, askPath: askPath, showKey: true})
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
		for _, s := range l.Send {
			out.Sends = append(out.Sends, checkSend{Channel: s.Channel, Interruption: string(s.Interruption)})
		}
		return out, nil
	}
	return out, problem.Configf("%s names no level in the config", "a level name from the config",
		"set "+strings.TrimSuffix(path, ".ask")+".level to one of the names under levels", strings.TrimSuffix(path, ".ask"))
}

// pageCheck prints what `page` and `page --low` would send, and sends nothing: it opens no channel,
// flushes nothing and changes no queue entry or incident, so it is safe to run anywhere.
func (e *env) pageCheck(cfg *config.Config) int {
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
		sends := make([]string, len(m.Sends))
		for i, s := range m.Sends {
			sends[i] = s.Channel + " " + s.Interruption
		}
		if len(sends) == 0 {
			sends = []string{"none"}
		}
		return []string{
			fmt.Sprintf("%s level %s, ask %q", label, m.Level, m.Ask),
			"            sends: " + strings.Join(sends, ", "),
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
