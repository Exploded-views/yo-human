package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/secrets"
)

var secretCommands = map[string]handler{"set": cmdSecretSet, "list": cmdSecretList}

// maxSecretBytes caps a secret read from stdin; a token is a few hundred bytes.
const maxSecretBytes = 1 << 16

// cmdSecretSet stores the value on stdin under the name. The value is never an argument (it would
// sit in shell history and the process list), and neither it nor a rejected name is printed.
func cmdSecretSet(e *env, args []string) int {
	if len(args) != 1 {
		return e.fail(problem.Usagef("secret set takes one name and reads the value from stdin; a value is never accepted as an argument",
			`yo-human secret set <name>, with the value piped in: op read "op://<vault>/<item>/<field>" | yo-human secret set <name>`,
			"remove the extra arguments and pipe the value in on stdin"))
	}
	name := args[0]
	if !secrets.ValidName(name) {
		return e.fail(problem.Usagef("that is not a valid secret name (it is not echoed: a pasted value must never be printed)",
			"lower-case letters, digits and hyphens, starting with a letter or digit, 63 characters at most, and not a run of 16 or more hex digits",
			"choose a descriptive name such as pd-critical, and pipe the value in on stdin"))
	}
	if f, ok := e.stdin.(*os.File); ok {
		if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			return e.fail(problem.Usagef("secret set reads the value from a pipe: stdin is a terminal or device, not a pipe", "a value piped in on stdin",
				`pipe it in: op read "op://<vault>/<item>/<field>" | yo-human secret set `+name))
		}
	}
	value, err := io.ReadAll(io.LimitReader(e.stdin, maxSecretBytes+1))
	if err != nil || len(value) > maxSecretBytes {
		return e.fail(problem.Usagef("cannot read the secret from stdin, or it is longer than %d bytes", "a value on stdin",
			`pipe it in: op read "op://<vault>/<item>/<field>" | yo-human secret set `+name, maxSecretBytes))
	}
	question := fmt.Sprintf("Store the %d bytes piped in as secret %q?", len(value), name)
	if err := approve(noninteractive(), "secret set "+name, nil, question, map[string]any{"event": "secret-set", "name": name}); err != nil {
		return e.fail(err)
	}
	if err := secrets.Set(e.secretsFile(), name, value); err != nil {
		return e.fail(err)
	}
	e.ok([]string{fmt.Sprintf("stored secret %s", name)}, map[string]any{"name": name})
	return ExitOK
}

// cmdSecretList prints each secret's name and size, never a value.
func cmdSecretList(e *env, args []string) int {
	if len(args) > 0 {
		return e.fail(usageErr("secret list"))
	}
	entries, err := secrets.List(e.secretsFile())
	if err != nil {
		return e.fail(err)
	}
	lines := make([]string, len(entries))
	items := make([]map[string]any, len(entries))
	for i, s := range entries {
		lines[i] = fmt.Sprintf("%s %d bytes", s.Name, s.Bytes)
		items[i] = map[string]any{"name": s.Name, "bytes": s.Bytes}
	}
	e.ok(lines, map[string]any{"secrets": items})
	return ExitOK
}
