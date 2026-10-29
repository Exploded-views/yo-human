package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// The input limits of the contract.
const (
	maxTitleChars = 200
	maxBodyBytes  = 16 << 10
	maxActions    = 3
	maxKeyChars   = 255
	maxAskChars   = 1000
	maxLabelChars = 64
	bodyReadCap   = 1 << 20 // how much of an oversized body is read to size it
)

// notifyUsage is the valid text of every notify usage error.
const notifyUsage = `yo-human notify --level <name> --title <text> [--ask <text>] [--body <text> | --body-file <path> | --body -] [--link <url>] [--action "<label>=<url>" (up to 3)] [--project <name>] [--agent <name>] [--dedup-key <key>]`

// repeated collects a flag given more than once.
type repeated []string

func (r *repeated) String() string     { return strings.Join(*r, ",") }
func (r *repeated) Set(v string) error { *r = append(*r, v); return nil }

// notifyCall is a validated notify: the level to send at and the message.
type notifyCall struct {
	level   string
	msg     channel.Message
	askPath string // set by the page shim: where in the config its ask comes from
	showKey bool   // set by the page shim: print the dedup key, which may be its default
}

// newFlags is a flag set that prints nothing: a rejected flag is reported as a usage problem that
// never repeats what was typed.
func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseFlags parses args and refuses a positional argument.
func parseFlags(fs *flag.FlagSet, command, valid string, args []string) error {
	if err := fs.Parse(args); errors.Is(err, flag.ErrHelp) {
		return errHelp
	} else if err != nil {
		return problem.Usagef("%s was given a flag it does not know, or a flag with no value (the text is not echoed)", valid,
			"check the spelling of each flag against the valid line", command)
	}
	if fs.NArg() > 0 {
		return problem.Usagef("%s takes no arguments, only flags", valid, "put each value after its flag", command)
	}
	return nil
}

// flatLine makes s one line of plain text: control and format characters become spaces.
func flatLine(s string) string { return strings.TrimSpace(oneLine(s)) }

// utf8Check refuses text that is not valid UTF-8, naming the flag but not the text.
func utf8Check(flagName, s string) error {
	if utf8.ValidString(s) {
		return nil
	}
	return problem.Usagef("%s is not valid UTF-8 text", "UTF-8 text", "re-encode the value as UTF-8", flagName)
}

// checkKey validates a dedup key without echoing it.
func checkKey(key string) error { return checkKeyAs("--dedup-key", key) }

// checkKeyAs is checkKey for a caller that names the key its own way.
func checkKeyAs(label, key string) error {
	if err := utf8Check(label, key); err != nil {
		return err
	}
	if n := utf8.RuneCountInString(key); n > maxKeyChars {
		return problem.Usagef("%s is %d characters (it is not echoed)", fmt.Sprintf("at most %d characters", maxKeyChars),
			"use a shorter, stable key, such as a service name and a symptom", label, n)
	}
	if strings.ContainsFunc(key, unicode.IsControl) {
		return problem.Usagef("%s holds a control character (it is not echoed)", "printable characters only", "remove the control characters from the key", label)
	}
	return nil
}

// checkLink validates a link without echoing it.
func checkLink(flagName, link string) error {
	if utf8.ValidString(link) && channel.SafeLink(link) {
		return nil
	}
	return problem.Usagef("%s is not a safe link (%d characters; it is not echoed)", "an http or https URL with a host and no user:password@",
		"pass the plain http(s) URL", flagName, utf8.RuneCountInString(link))
}

// parseActions turns each "label=url" into an Action. The label ends at the first equals sign.
func parseActions(raw []string) ([]channel.Action, error) {
	if len(raw) > maxActions {
		return nil, problem.Usagef("%d --action flags were given", fmt.Sprintf("at most %d --action flags, each \"<label>=<url>\"", maxActions),
			"keep the most useful actions", len(raw))
	}
	var out []channel.Action
	for i, a := range raw {
		label, url, ok := strings.Cut(a, "=")
		label = flatLine(label)
		if n := utf8.RuneCountInString(label); n > maxLabelChars {
			return nil, problem.Usagef("--action %d has a label of %d characters", fmt.Sprintf("a label of at most %d characters", maxLabelChars),
				"shorten the label; the link carries the detail", i+1, n)
		}
		if !ok || label == "" || !utf8.ValidString(a) {
			return nil, problem.Usagef("--action %d is not \"<label>=<url>\" (it is not echoed)", `"<label>=<url>", with a label and a link`,
				`write each action as --action "Open the dashboard=https://example.com/d"`, i+1)
		}
		if err := checkLink(fmt.Sprintf("the URL of --action %d", i+1), url); err != nil {
			return nil, err
		}
		out = append(out, channel.Action{Label: label, URL: url})
	}
	return out, nil
}

// readText reads a body from r, which names where it came from, and checks its size and encoding.
func readText(r io.Reader, source string) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, bodyReadCap+1))
	if err != nil {
		return "", problem.Usagef("%s cannot be read", "readable text", "check the file, or pipe the text in on stdin", source)
	}
	if len(b) > maxBodyBytes {
		size := strconv.Itoa(len(b))
		if len(b) > bodyReadCap {
			size = "over " + strconv.Itoa(bodyReadCap)
		}
		return "", problem.Usagef("%s is too long: %s bytes > %d", fmt.Sprintf("at most %d bytes", maxBodyBytes),
			"shorten it, or leave the detail out and link to it", source, size, maxBodyBytes)
	}
	if !utf8.Valid(b) {
		return "", problem.Usagef("%s is not valid UTF-8 text", "UTF-8 text", "re-encode it as UTF-8", source)
	}
	return string(b), nil
}

// bodyOf resolves --body (a literal, or - for stdin) and --body-file.
func (e *env) bodyOf(body, file string, bodySet, fileSet bool) (string, error) {
	switch {
	case bodySet && fileSet:
		return "", problem.Usagef("--body and --body-file were both given", "one of them", "pass the body one way")
	case fileSet:
		f, err := e.openBody(file)
		if err != nil {
			return "", err
		}
		defer func() { _ = f.Close() }()
		return readText(f, "--body-file")
	case body == "-":
		return readText(e.stdin, "the body on stdin")
	}
	return readText(strings.NewReader(body), "--body")
}

// sourceOf is the flag's value, else the environment variable's, else "unknown".
func sourceOf(flagValue, envName string) string {
	if v := flatLine(flagValue); v != "" {
		return v
	}
	if v := flatLine(os.Getenv(envName)); v != "" {
		return v
	}
	return "unknown"
}

// maxSourceChars caps a project or agent name: a tag, a group and an incident namespace.
const maxSourceChars = 64

// sourceArg is sourceOf, refusing a value longer than maxSourceChars.
func sourceArg(flagName, flagValue, envName string) (string, error) {
	v := sourceOf(flagValue, envName)
	if n := utf8.RuneCountInString(v); n > maxSourceChars {
		return "", problem.Usagef("%s (or %s) is %d characters", fmt.Sprintf("at most %d characters", maxSourceChars),
			"use a short name for the project or agent", flagName, envName, n)
	}
	return v, nil
}

// callerProject is the project a call acts on: --project, else YO_HUMAN_PROJECT, else "unknown",
// at most maxSourceChars. The namespace is a boundary: when the harness has set YO_HUMAN_PROJECT
// (an agent allowed only to run yo-human cannot change it), a --project naming another project is
// refused.
func callerProject(flagValue string) (string, error) {
	if env, flag := flatLine(os.Getenv("YO_HUMAN_PROJECT")), flatLine(flagValue); env != "" && flag != "" && flag != env {
		return "", outOfProject("--project names another project than YO_HUMAN_PROJECT")
	}
	return sourceArg("--project", flagValue, "YO_HUMAN_PROJECT")
}

// outOfProject is the refusal of a call that reaches beyond the project YO_HUMAN_PROJECT names.
func outOfProject(what string) error {
	return problem.Usagef("%s", "this project's incidents only, while YO_HUMAN_PROJECT names it",
		"run it from that project; an agent reaches only its own project's incidents", what)
}

// setFlags records which flags were given, so an empty value can be told from an absent flag.
func setFlags(fs *flag.FlagSet) map[string]bool {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}
