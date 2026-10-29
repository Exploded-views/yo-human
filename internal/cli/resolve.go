package cli

import (
	"context"
	"errors"
	"strings"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

const resolveUsage = `yo-human resolve --dedup-key <key> [--note <text>] [--project <name>]`

// cmdResolve closes the incident for a dedup key. It builds the channels the incident reached, and
// those of the backlog it flushes.
func cmdResolve(e *env, args []string) int {
	fs := newFlags("resolve")
	key := fs.String("dedup-key", "", "")
	note := fs.String("note", "", "")
	project := fs.String("project", "", "")
	if err := parseFlags(fs, "resolve", resolveUsage, args); err != nil {
		return e.fail(err)
	}
	if *key == "" {
		return e.fail(problem.Usagef("--dedup-key is missing", "the key the incident was opened with", "pass --dedup-key <key>, the same key given to notify"))
	}
	if err := checkKey(*key); err != nil {
		return e.fail(err)
	}
	text, err := readText(strings.NewReader(*note), "--note")
	if err != nil {
		return e.fail(err)
	}
	proj, err := callerProject(*project)
	if err != nil {
		return e.fail(err)
	}
	cfg, st, err := e.loadWithStore()
	if err != nil {
		return e.fail(err)
	}
	found := false
	if text != "" {
		if found, err = e.redact(&text); err != nil {
			return e.fail(err)
		}
	}
	return e.closeIncident(cfg, st, core.IncidentKey(proj, *key), text, found, resolveAgain(*key, *project, text))
}

// resolveAgain is the command that runs the same resolve again.
func resolveAgain(key, project, note string) string {
	again := "yo-human resolve --dedup-key " + problem.ShellQuote(key)
	if project != "" {
		again += " --project " + problem.ShellQuote(project)
	}
	if note != "" {
		again += " --note " + problem.ShellQuote(note)
	}
	return again
}

// closeIncident resolves a project's incident (core.IncidentKey) under a loaded config; resolve and
// the page shim share it. again is the command that runs the same resolve.
func (e *env) closeIncident(cfg *config.Config, st *core.State, key, note string, redactedNote bool, again string) int {
	reached, err := st.Reached(cfg, key, now())
	var res core.Result
	if err == nil {
		res, err = e.resolveWith(cfg, st, key, note, reached)
		e.warnRedacted(redactedNote, res)
	}
	if res.Status == "" && core.IsBusy(err) {
		res = e.retryResult(res.ID, again)
	}
	return e.report(res, err)
}

// retryResult is the result of a resolve that could not take the incident lock: nothing changed,
// and the next step runs the same resolve again, which is safe to repeat.
func (e *env) retryResult(id, again string) core.Result {
	if e.configFile != "" {
		again += " --config " + problem.ShellQuote(e.configPath())
	}
	if id == "" {
		id = newID()
	}
	return core.Result{Status: core.StatusQueued, ID: id,
		Next: &core.Next{Do: again, AfterSeconds: 5, AttemptsLeft: 5, Then: "tell the human in your session"}}
}

// resolveWith closes the incident whatever can be built: the resolve cancels the incident's queued
// sends, and that must never wait on a channel (core queues the cancellation of a scheduled send
// it cannot reach). A reached channel that cannot be built, or is no longer configured, is a
// problem printed after the result. Only a channel that delivered has a resolve note waiting.
func (e *env) resolveWith(cfg *config.Config, st *core.State, key, note string, reached []core.Reach) (core.Result, error) {
	var gone, present []string
	delivered := map[string]bool{}
	for _, r := range reached {
		delivered[r.Instance] = r.Delivered
		if _, ok := cfg.Channels[r.Instance]; ok {
			present = append(present, r.Instance)
		} else {
			gone = append(gone, r.Instance)
		}
	}
	chans, failed, err := e.channelsFor(cfg, st, nil, present)
	problems := []error{err}
	for _, name := range gone {
		problems = append(problems, removedProblem(name, delivered[name]))
	}
	for _, name := range sortedNames(failed) {
		problems = append(problems, noteProblems(name, failed[name], delivered[name])...)
	}
	res, rerr := e.pipeline(cfg, st, chans).Resolve(context.Background(), key, note)
	return res, errors.Join(append(problems, rerr)...)
}

// removedProblem is the problem for a reached channel the config no longer has. Its queued sends
// are cancelled; a resolve note is waiting only if the channel delivered something.
func removedProblem(name string, delivered bool) error {
	if delivered {
		return problem.Configf("channel %s is no longer configured; its queued sends were cancelled, and the resolve note for it waits in the queue",
			"a channel named under channels: in the config in force",
			"restore the channel unchanged in the config (or pass --config <file>) and run `yo-human flush` to send the note", name)
	}
	return problem.Configf("channel %s is no longer configured; its queued sends were cancelled, and no resolve note is queued for it",
		"a channel named under channels: in the config in force",
		"restore it in the config only if you want it back; a scheduled send it holds is queued for cancellation until then", name)
}

// noteProblems explains a channel that could not be built. For one that delivered, its resolve note
// waits in the queue until it can be built.
func noteProblems(name string, err error, delivered bool) []error {
	if !delivered {
		return []error{err}
	}
	var out []error
	for _, p := range flatten(err) {
		out = append(out, problem.Configf("%s; the resolve note for channel %s waits in the queue until it can be built", p.Valid, p.Fix, p.What, name))
	}
	return out
}
