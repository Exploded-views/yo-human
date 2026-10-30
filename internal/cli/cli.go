// Package cli parses arguments and maps results to exit codes. It is the top layer: it wires the
// channel types, the config, the secrets and the store together.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/version"
)

const usage = `usage: yo-human <command> [flags]   (--json and --config <file> go after the command)
       yo-human help                prints this

commands:
  version                          print the version
  levels                           the configured levels, quietest first
  notify --level <name> --title <text> [--ask <text>] [flags]
                                   send a message at a level; the other flags are in the error a bad call prints
  resolve --dedup-key <key>        close an incident
  page [--low] <summary> [details] [dedup-key] | page [--low] --resolve <dedup-key>
                                   the drop-in for the old paging script, mapped by the config's shim: block
  flush                            send whatever is queued and due
  status                           the queue, the dead letter and the open incidents
  channel test <instance>          send one test message through one channel
  config init|check|schema         write, validate or print the schema of a config
  config trust [<file>]            the human trusts a project file, typing yes at their own terminal
  secret set <name>                the human stores a secret piped in on stdin, typing yes at their own terminal
  secret list                      list secret names and sizes
`

// env is one invocation: its streams and the global flags.
type env struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	asJSON         bool
	configFile     string
	secretVals     map[string]string  // the secrets file, once read
	notes          []string           // notices for the human, kept for the --json object
	warns          []*problem.Problem // warnings, likewise
	cleanup        []func()           // run when the command returns
	verb           string             // the command named first, for its own help
}

// verbUsages are the commands whose help leads with their own valid line, flags and all.
var verbUsages = map[string]string{
	"notify": notifyUsage, "resolve": resolveUsage, "page": pageUsage, "status": statusUsage,
}

type handler func(e *env, args []string) int

// commands is the top-level table. The usage text above lists the same names.
var commands = map[string]handler{
	"version": func(e *env, _ []string) int {
		if e.asJSON {
			e.ok(nil, map[string]any{"release": version.Version, "commit": version.Commit})
			return ExitOK
		}
		_, _ = fmt.Fprintf(e.stdout, "yo-human %s %s\n", version.Version, version.Commit)
		return ExitOK
	},
	"levels":  cmdLevels,
	"notify":  cmdNotify,
	"page":    cmdPage,
	"resolve": cmdResolve,
	"flush":   cmdFlush,
	"status":  cmdStatus,
	"channel": func(e *env, args []string) int { return e.dispatch("channel", channelCommands, args) },
	"config":  func(e *env, args []string) int { return e.dispatch("config", configCommands, args) },
	"secret":  func(e *env, args []string) int { return e.dispatch("secret", secretCommands, args) },
}

// Run executes yo-human with args (without the program name) and returns the exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	return run(args, os.Stdin, stdout, stderr)
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	e := &env{stdin: stdin, stdout: stdout, stderr: stderr}
	defer func() {
		for _, f := range e.cleanup {
			f()
		}
	}()
	installTestHooks()
	if len(args) > 0 {
		e.verb = args[0] // the global flags go after the command, so the first word names it
	}
	if wantsHelp(args) {
		e.asJSON = slices.Contains(args, "--json")
		return e.help()
	}
	rest, err := e.globals(args)
	if err != nil {
		e.asJSON = slices.Contains(args, "--json") // a refused call that asked for JSON still gets one object
		return e.fail(err)
	}
	if len(rest) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return ExitUsage
	}
	h, ok := commands[rest[0]]
	if !ok {
		return e.fail(problem.Usagef("unknown command", "commands: "+strings.Join(sortedNames(commands), ", "),
			"run one of them; `yo-human` with no arguments prints the usage"))
	}
	if err := homeProblem(); err != nil && rest[0] != "version" {
		return e.fail(err)
	}
	return h(e, rest[1:])
}

// helpWords ask for the usage.
var helpWords = map[string]bool{"help": true, "--help": true, "-h": true}

// wantsHelp reports a request for the usage: help, --help or -h as the command, or as the word
// right after the command (and after a group's subcommand: `config check --help`). A flag set that
// meets -h or --help among its flags asks for it too (errHelp).
func wantsHelp(args []string) bool {
	switch {
	case len(args) == 0:
		return false
	case helpWords[args[0]]:
		return true
	case len(args) > 1 && (args[1] == "--help" || args[1] == "-h"):
		return true
	}
	return groups[args[0]] && len(args) > 2 && (args[2] == "--help" || args[2] == "-h")
}

// errHelp is what a flag set returns when it meets -h or --help: the usage, exit 0.
var errHelp = errors.New("help")

// help prints the usage on stdout (or, with --json, one object carrying it) and exits 0. Help asked
// of a command with flags of its own leads with that command's valid line.
func (e *env) help() int {
	text := usage
	if v, ok := verbUsages[e.verb]; ok {
		text = "usage: " + v + "\n\n" + usage
	}
	if e.asJSON {
		writeOK(e.stdout, true, nil, map[string]any{"usage": text})
		return ExitOK
	}
	_, _ = fmt.Fprint(e.stdout, text)
	return ExitOK
}

// groups are the commands whose subcommand is part of the command: config trust, secret set.
var groups = map[string]bool{"config": true, "secret": true, "channel": true}

// commandFirst refuses a flag among the command's own words.
func commandFirst(args []string) error {
	words := 1
	if len(args) > 0 && groups[args[0]] {
		words = 2
	}
	for _, a := range args[:min(words, len(args))] {
		if strings.HasPrefix(a, "-") {
			return problem.Usagef("a flag was given before the command", "yo-human <command> [<subcommand>] [flags]: --json and --config <file> go after the command",
				"put the flag after the command, for example `yo-human levels --json`")
		}
	}
	return nil
}

// globals strips --json and --config <file> (or --config=<file>) from args, wherever they sit after
// the command. A flag before the command (or between config, secret or channel and its
// subcommand) is refused, so a harness's prefix rule such as "yo-human secret" matches every way
// of running that command.
func (e *env) globals(args []string) ([]string, error) {
	if err := commandFirst(args); err != nil {
		return nil, err
	}
	var rest []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--json":
			e.asJSON = true
		case a == "--config" && i+1 < len(args):
			i++
			e.configFile = args[i]
		case a == "--config":
			return nil, problem.Usagef("--config needs a file", "--config <file>, or YO_HUMAN_CONFIG in the environment", "name the config file after --config")
		case strings.HasPrefix(a, "--config="):
			e.configFile = strings.TrimPrefix(a, "--config=")
		default:
			rest = append(rest, a)
		}
	}
	return rest, nil
}

// dispatch runs the subcommand named by args[0]. It never echoes the word it did not recognise:
// beside "secret" a pasted value could be sitting there.
func (e *env) dispatch(group string, table map[string]handler, args []string) int {
	if len(args) > 0 {
		if h, ok := table[args[0]]; ok {
			return h(e, args[1:])
		}
	}
	return e.fail(problem.Usagef("%s needs one of its subcommands", "yo-human "+group+" "+strings.Join(sortedNames(table), "|"),
		"run `yo-human "+group+" <subcommand>`", group))
}

// fail prints err (every problem inside it) and returns the first one's exit code. Under --json
// the object goes to stdout and stderr stays empty.
func (e *env) fail(err error) int {
	if errors.Is(err, errHelp) {
		return e.help()
	}
	if e.asJSON {
		return writeFailure(e.stdout, flatten(err), true, e.notes, e.warns)
	}
	return writeFailure(e.stderr, flatten(err), false, nil, nil)
}

// finish prints a result, then the problems that came with it, and exits with the problem's code,
// or the result's when there is none. Under --json that is one object whose "errors" holds the
// problems. Nothing the result holds is a secret.
func (e *env) finish(r *core.Result, err error) int {
	if r == nil {
		return e.fail(err)
	}
	if e.asJSON {
		var ps []*problem.Problem
		if err != nil {
			ps = flatten(err)
		}
		writeJSONResult(e.stdout, *r, ps, e.notes, e.warns)
		if len(ps) > 0 {
			return exitForProblem(ps[0])
		}
		return exitFor(*r)
	}
	writeResult(e.stdout, *r, false)
	if err != nil {
		return e.fail(err)
	}
	return exitFor(*r)
}

// ok prints a read-only command's output. Under --json the run's notices and warnings join the
// object, since stderr stays empty.
func (e *env) ok(lines []string, fields map[string]any) {
	if e.asJSON {
		if len(e.notes) > 0 {
			fields["notices"] = e.notes
		}
		if _, has := fields["warnings"]; !has && len(e.warns) > 0 {
			fields["warnings"] = toWarningList(e.warns)
		}
	}
	writeOK(e.stdout, e.asJSON, lines, fields)
}

// addNotices records the loader's notices: a warning keeps its problem, the rest are plain notes.
func (e *env) addNotices(ns []config.Notice) {
	var notes []string
	var warns []*problem.Problem
	for _, n := range ns {
		if n.Problem != nil {
			warns = append(warns, n.Problem)
		} else {
			notes = append(notes, n.Text)
		}
	}
	e.note(notes)
	e.warn(warns)
}

// note and warn keep what the run has to say; as text it goes to stderr at once, as JSON it waits
// for the one object on stdout.
func (e *env) note(notes []string) {
	e.notes = append(e.notes, notes...)
	if !e.asJSON {
		writeNotices(e.stderr, notes)
	}
}

func (e *env) warn(warns []*problem.Problem) {
	e.warns = append(e.warns, warns...)
	if !e.asJSON {
		writeWarnings(e.stderr, warns)
	}
}

// usageErr is the problem for a command given arguments it does not take.
func usageErr(command string) error {
	return problem.Usagef("%s takes no arguments", "yo-human "+command, "run `yo-human "+command+"` on its own", command)
}
