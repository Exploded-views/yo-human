package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// openTTY opens the controlling terminal, where only the human can type. An agent's commands have
// none. Only tests replace it.
var openTTY = func() (io.ReadWriteCloser, error) { return os.OpenFile("/dev/tty", os.O_RDWR, 0) }

// noninteractive reports whether the human's own provisioning script asked to skip the terminal.
// It is an environment variable, never a flag: an agent allowed only to run yo-human can type any
// flag but cannot set the environment (an agent with a whole shell can read the secrets file
// anyway). No message ever names it, so an agent does not learn it from yo-human.
func noninteractive() bool { return os.Getenv("YO_HUMAN_NONINTERACTIVE") == "1" }

// humanOnly is the refusal of a human-only command run with no terminal. It never names the flag
// that skips the terminal: an agent reads this.
func humanOnly(command string) error {
	return problem.Usagef("%s changes what yo-human trusts, so only the human may run it, at their own terminal, and this process has none",
		"the human running yo-human "+command+" in their own terminal, where they type yes to confirm",
		"stop and ask the human to run `yo-human "+command+"` in their terminal; never run it for them", command)
}

// confirm shows lines and a question on the human's terminal and returns nil only if they type yes.
// Nothing shown here goes to stdout or stderr.
func confirm(command string, lines []string, question string) error {
	tty, err := openTTY()
	if err != nil {
		return humanOnly(command)
	}
	defer func() { _ = tty.Close() }()
	for _, l := range lines {
		_, _ = fmt.Fprintln(tty, l)
	}
	_, _ = fmt.Fprintf(tty, "%s Type yes to confirm: ", question)
	answer, _ := bufio.NewReader(io.LimitReader(tty, 256)).ReadString('\n')
	if strings.TrimSpace(answer) != "yes" {
		_, _ = fmt.Fprintln(tty, "not confirmed; nothing was changed")
		return problem.Usagef("the human did not confirm %s; nothing was changed", "yes, typed at the prompt",
			"run it again and type yes when asked", command)
	}
	return nil
}

// approve lets a human-only command go ahead: the human's own script said so through the
// environment (audited in log.jsonl first), or the human typed yes at their terminal.
func approve(scripted bool, command string, lines []string, question string, record map[string]any) error {
	if !scripted {
		return confirm(command, lines, question)
	}
	event, _ := record["event"].(string)
	delete(record, "event")
	return audit(event, record)
}

// audit records a human-only command run without the terminal, for the human to review in
// log.jsonl. It never holds a secret value.
func audit(event string, fields map[string]any) error {
	st, err := core.OpenState(stateDir())
	if err != nil {
		return err
	}
	entry := map[string]any{"time": now().UTC(), "event": event, "noninteractive": true}
	for k, v := range fields {
		entry[k] = v
	}
	return st.Audit(entry)
}
