package repolint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// goreleaserBuild is the part of a goreleaser build that decides what is built.
type goreleaserBuild struct {
	Env    []string `yaml:"env"`
	Flags  []string `yaml:"flags"`
	Tags   []string `yaml:"tags"`
	Goos   []string `yaml:"goos"`
	Goarch []string `yaml:"goarch"`
	Ignore []any    `yaml:"ignore"`
}

// GoreleaserTargets checks root/.goreleaser.yaml against root/RELEASE_TARGETS: every build's goos
// and goarch make exactly the release targets (and it has no ignore list), its env sets
// CGO_ENABLED=0 and no GOFLAGS, and it builds with no tags. The gates that read RELEASE_TARGETS
// (lint, release_net_check, nodeps, nohooks) then see what the release builds.
func GoreleaserTargets(root string) ([]string, error) {
	targets, err := releaseTargets(filepath.Join(root, "RELEASE_TARGETS"))
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yaml")) //nolint:gosec // the repo the caller names
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Builds []goreleaserBuild `yaml:"builds"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf(".goreleaser.yaml: %w", err)
	}
	if len(cfg.Builds) == 0 {
		return []string{".goreleaser.yaml has no builds"}, nil
	}
	var found []string
	for i, b := range cfg.Builds {
		found = append(found, b.check(fmt.Sprintf("builds[%d]", i), targets)...)
	}
	return found, nil
}

func (b goreleaserBuild) check(name string, targets map[string]bool) []string {
	found := targetFindings(name, b.Goos, b.Goarch, targets)
	if len(b.Ignore) > 0 {
		found = append(found, name+" has an ignore list; RELEASE_TARGETS must be goos x goarch exactly")
	}
	return append(append(found, envFindings(name, b.Env)...), tagFindingsOf(name, b.Flags, b.Tags)...)
}

// envFindings: exactly one CGO_ENABLED entry, CGO_ENABLED=0, and no GOFLAGS.
func envFindings(name string, env []string) []string {
	var found, cgo []string
	for _, e := range env {
		switch {
		case strings.HasPrefix(e, "CGO_ENABLED="):
			cgo = append(cgo, e)
		case strings.HasPrefix(e, "GOFLAGS="):
			found = append(found, name+" env sets "+e+"; the release builds with no GOFLAGS")
		}
	}
	if len(cgo) != 1 || cgo[0] != "CGO_ENABLED=0" {
		has := "none"
		if len(cgo) > 0 {
			has = strings.Join(cgo, ", ")
		}
		found = append(found, name+" env must set CGO_ENABLED=0 and nothing else for CGO_ENABLED (has "+has+")")
	}
	return found
}

// tagFindingsOf: no -tags flag and no tags key.
func tagFindingsOf(name string, flags, tags []string) []string {
	var found []string
	for _, f := range flags {
		if f == "-tags" || strings.HasPrefix(f, "-tags=") || strings.HasPrefix(f, "--tags") {
			found = append(found, name+" flags hold "+f+"; the release builds with no tags")
		}
	}
	if len(tags) > 0 {
		found = append(found, name+" sets tags; the release builds with no tags")
	}
	return found
}

// targetFindings compares goos x goarch with the release targets, both ways, sorted.
func targetFindings(name string, goos, goarch []string, targets map[string]bool) []string {
	built := map[string]bool{}
	for _, goOS := range goos {
		for _, arch := range goarch {
			built[goOS+"/"+arch] = true
		}
	}
	var extra, missing []string
	for t := range built {
		if !targets[t] {
			extra = append(extra, name+" builds "+t+", which is not in RELEASE_TARGETS")
		}
	}
	for t := range targets {
		if !built[t] {
			missing = append(missing, name+" does not build "+t+", which RELEASE_TARGETS lists")
		}
	}
	sort.Strings(extra)
	sort.Strings(missing)
	return append(extra, missing...)
}

// releaseTargets reads a RELEASE_TARGETS file as scripts/release-targets.sh does: one GOOS/GOARCH
// per line, # comments and blank lines skipped, at least one.
func releaseTargets(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the repo the caller names
	if err != nil {
		return nil, err
	}
	targets := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		line = strings.Join(strings.Fields(line), "")
		if line != "" {
			targets[line] = true
		}
	}
	if len(targets) == 0 {
		return nil, errors.New(path + " lists no target")
	}
	return targets, nil
}
