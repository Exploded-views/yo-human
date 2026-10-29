package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/secrets"
)

var configCommands = map[string]handler{"init": cmdInit, "check": cmdCheck, "schema": cmdSchema, "trust": cmdTrust}

// placeholder stands in for every secret while `config check` builds the channels: check validates
// settings, not credentials, and no real value should reach a channel's constructor.
const placeholder = "FAKE-PLACEHOLDER"

// cmdInit writes the starter config to --config, YO_HUMAN_CONFIG or the user config path, and
// refuses to replace a file that is already there.
func cmdInit(e *env, args []string) int {
	if len(args) > 0 {
		return e.fail(usageErr("config init"))
	}
	p := e.paths()
	path := p.Explicit
	if path == "" {
		path = filepath.Join(p.UserDir, "config.yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return e.fail(problem.Storef("cannot create the directory for %q", "a directory you can write", "check the permissions of its parents", path))
		}
	} else if err := initExplicit(path); err != nil {
		return e.fail(err)
	}
	logPath, _ := json.Marshal(filepath.Join(stateDir(), "messages.log")) // a JSON string is a YAML string
	err := core.CreateFile(path, []byte(fmt.Sprintf(starterConfig, logPath)))
	if errors.Is(err, fs.ErrExist) {
		return e.fail(problem.Usagef("the config file %q already exists", "a path with no file at it",
			"edit it, or remove "+problem.ShellQuote(path)+" first; `yo-human config init` never overwrites", path))
	}
	if err != nil {
		return e.fail(err)
	}
	e.ok([]string{
		"wrote the starter config to " + path,
		"next: run `yo-human config check`; it lists the secrets the human sets with `yo-human secret set <name>` in their own terminal",
	}, map[string]any{"path": path})
	return ExitOK
}

// initExplicit gates config init at a path the caller chose: that writes where the caller says, so
// it is the human's, like config trust. Its directory must already exist; nothing is created for it.
func initExplicit(path string) error {
	if fi, err := os.Stat(filepath.Dir(path)); err != nil || !fi.IsDir() {
		return problem.Usagef("the directory of %q does not exist", "a path in an existing directory",
			"create the directory first, or run `yo-human config init` with no --config for the user config", path)
	}
	return approve(noninteractive(), "config init", []string{"write the starter config to " + strconv.Quote(path)},
		"Write it?", map[string]any{"event": "config-init", "path": path})
}

func cmdSchema(e *env, args []string) int {
	if len(args) > 0 {
		return e.fail(usageErr("config schema"))
	}
	schema := config.Schema()
	e.ok([]string{string(schema)}, map[string]any{"schema": json.RawMessage(schema)})
	return ExitOK
}

// cmdCheck validates everything the config says without sending anything. Structure, references
// and capabilities are errors (78). A secret that is not stored yet is a warning, so the starter
// config passes before its secrets exist. Each channel type's own checks run too, with a
// placeholder for every secret.
func cmdCheck(e *env, args []string) int {
	if len(args) > 0 {
		return e.fail(usageErr("config check"))
	}
	cfg, err := e.load()
	if err != nil {
		return e.fail(err)
	}
	stored, err := secrets.List(e.secretsFile())
	if err != nil {
		return e.fail(err)
	}
	warnings, values := secretReferences(cfg, channelTypes(), stored)
	e.warn(warnings)
	if err := checkChannels(cfg, values); err != nil {
		return e.fail(err)
	}
	e.ok(append([]string{fmt.Sprintf("config ok: %d channels, %d levels", len(cfg.Channels), len(cfg.Levels))}, mergedLines(cfg)...),
		map[string]any{"merged": cfg.Merged, "origins": cfg.Origin, "channels": len(cfg.Channels), "levels": len(cfg.Levels),
			"warnings": append([]jsonProblem{}, toWarningList(e.warns)...)})
	return ExitOK
}

// mergedLines is the merged config, one line per setting: its path, the file it came from, and its
// value as JSON. A config holds secret names only, so a secret field shows {"secret": name}.
func mergedLines(cfg *config.Config) []string {
	out := make([]string, 0, len(cfg.Origin))
	for _, k := range sortedNames(cfg.Origin) {
		v := cfg.Merged[k]
		if name, ok := strings.CutPrefix(k, "channels."); ok {
			chans, _ := cfg.Merged["channels"].(map[string]any)
			v = chans[name]
		}
		b, _ := json.Marshal(v) // decoded from YAML that Load validated: maps, lists and scalars
		out = append(out, k+" <- "+oneLine(cfg.Origin[k])+" = "+oneLine(string(b)))
	}
	return out
}

// secretReferences finds every {secret: name} the channels use. It returns a warning for each one
// not in stored, and a placeholder value for each name.
func secretReferences(cfg *config.Config, reg channel.Registry, stored []secrets.Entry) ([]*problem.Problem, map[string]string) {
	have := map[string]bool{}
	for _, s := range stored {
		have[s.Name] = true
	}
	values := map[string]string{}
	var warnings []*problem.Problem
	for _, name := range sortedNames(cfg.Channels) {
		ci := cfg.Channels[name]
		for _, field := range reg[ci.Type].SecretFields {
			ref, _ := ci.Settings[field].(map[string]any) // config.Load vouched for the shape
			secret, _ := ref["secret"].(string)
			if secret == "" {
				continue
			}
			values[secret] = placeholder
			if !have[secret] {
				warnings = append(warnings, problem.Configf("warning: secret %q, used by channels.%s.%s, is not set", "a name listed by `yo-human secret list`",
					`tell the human to run, in their own terminal: op read "op://<vault>/<item>/<field>" | yo-human secret set `+secret, secret, name, field))
			}
		}
	}
	return warnings, values
}

// checkChannels builds every channel with placeholder secrets, which applies each type's own
// settings checks (PagerDuty's events_url rules are stricter than its schema), then asks each
// channel to validate the options its levels send it.
func checkChannels(cfg *config.Config, values map[string]string) error {
	built, err := buildChannels(cfg, channelTypes(), values, sortedNames(cfg.Channels))
	errs := []error{err}
	for _, l := range cfg.Levels {
		for _, s := range l.Send {
			if ch, ok := built[s.Channel]; ok {
				if verr := ch.Validate(s.Options); verr != nil {
					errs = append(errs, problem.Configf("level %q sends to channel %q with options it refuses: %s", "options the channel type accepts",
						"edit the options of that send in the config", l.Name, s.Channel, verr.Error()))
				}
			}
		}
	}
	return errors.Join(errs...)
}
