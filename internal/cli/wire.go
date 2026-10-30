package cli

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/registry"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/version"
)

// registryHook lets a test replace the channel constructors with fakes. It is unexported and nil;
// only _test.go files set it, so in the binary it does nothing.
var registryHook func(channel.Registry)

// clockHook lets a test move the clock the commands read. Like registryHook it is nil in the
// binary, and only _test.go files set it.
var clockHook func() time.Time

// channelTypes is every channel type the binary knows. Only internal/channel/registry names them.
func channelTypes() channel.Registry {
	reg := registry.New(version.Version)
	if registryHook != nil {
		registryHook(reg)
	}
	return reg
}

// stateDir is $XDG_STATE_HOME/yo-human, or $HOME/.local/state/yo-human.
func stateDir() string {
	return filepath.Join(config.XDGDir("XDG_STATE_HOME", filepath.Join(".local", "state")), "yo-human")
}

// homeProblem refuses to run without somewhere absolute for the config and the state: a relative
// HOME (or none) would put them, and the secrets, wherever the working directory happens to be.
func homeProblem() error {
	needHome := !filepath.IsAbs(os.Getenv("XDG_CONFIG_HOME")) || !filepath.IsAbs(os.Getenv("XDG_STATE_HOME"))
	if !needHome || filepath.IsAbs(os.Getenv("HOME")) {
		return nil
	}
	return problem.Configf("HOME is not an absolute path (it is unset, empty or relative), and XDG_CONFIG_HOME and XDG_STATE_HOME do not both name absolute directories in its place",
		"an absolute HOME, or absolute XDG_CONFIG_HOME and XDG_STATE_HOME",
		"stop and tell the human: yo-human must run with HOME set to their home directory; nothing was created")
}

// paths is where the config layers live, with --config overriding YO_HUMAN_CONFIG.
func (e *env) paths() config.Paths {
	cwd, _ := os.Getwd() // without a working directory there is no project file to find
	p := config.DefaultPaths(cwd)
	if systemPath != "" {
		p.System = systemPath
	}
	if e.configFile != "" {
		p.Explicit = e.configFile
	}
	return p
}

// secretsFile is the secrets file, beside the user config.
func (e *env) secretsFile() string { return filepath.Join(e.paths().UserDir, "secrets.yaml") }

// load reads the merged config, printing the loader's notices (an untrusted project file, say) to
// stderr whether or not it succeeds.
func (e *env) load() (*config.Config, error) {
	c, _, err := e.loadWithStore()
	return c, err
}

// buildChannels builds only the named channel instances, so one unset secret on a channel nobody
// asked for cannot block a send through the others. A name that is not configured, an unset
// secret on a named channel and a channel its type refuses are Config problems; every refusal is
// reported, and the channels that did build come back beside the error.
func buildChannels(cfg *config.Config, reg channel.Registry, secretValues map[string]string, names []string) (map[string]channel.Channel, error) {
	sub := *cfg
	sub.Channels = make(map[string]config.ChannelInstance, len(names))
	for _, n := range names {
		ci, ok := cfg.Channels[n]
		if !ok {
			return nil, problem.Configf("there is no channel named %q", "a name under channels:", "use a name under channels: in the config, or add a channels entry for it", problem.Clip(n))
		}
		sub.Channels[n] = ci
	}
	insts, err := sub.Instances(secretValues, reg)
	if err != nil {
		return nil, err
	}
	built := make(map[string]channel.Channel, len(insts))
	var refused []error
	for _, n := range sortedNames(insts) {
		ch, err := reg[insts[n].Type].New(insts[n])
		if err != nil {
			refused = append(refused, problem.Configf("channel %q is misconfigured: %s", "settings and secrets its type accepts; run `yo-human config schema`",
				"edit channels."+n+" in the config", n, err.Error()))
			continue
		}
		built[n] = ch
	}
	return built, errors.Join(refused...)
}

func sortedNames[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// systemPath replaces the system config path when set. Only tests set it.
var systemPath string
