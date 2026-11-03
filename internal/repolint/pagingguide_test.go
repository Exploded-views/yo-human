package repolint

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var starterLevel = regexp.MustCompile(`(?m)^\s+- name: ([a-z][a-z0-9-]*)\s*$`)

// starterLevels reads the level names out of the starter config the CLI writes. repolint is a leaf
// package, so it reads the source file instead of importing internal/cli.
func starterLevels(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("../../internal/cli/starter.go")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range starterLevel.FindAllStringSubmatch(string(src), -1) {
		names = append(names, m[1])
	}
	if len(names) == 0 {
		t.Fatal("no levels found in internal/cli/starter.go")
	}
	return names
}

func TestStarterLevelsAreRead(t *testing.T) {
	if got := strings.Join(starterLevels(t), ","); got != "note,heads-up,page" {
		t.Errorf("starter levels = %q, want note,heads-up,page", got)
	}
}

var known = []string{"note", "heads-up", "page"}

func TestLevelNamesGood(t *testing.T) {
	page := "| Event | Example level | Why |\n|---|---|---|\n| down | `page` | wakes |\n| cd | `heads-up` | soon |\n\n" +
		"```sh\nyo-human notify --level note --title x\nyo-human notify --level=heads-up --title y\n```\n"
	if got := CheckLevelNames(page, known); len(got) != 0 {
		t.Errorf("good page: %+v; want none", got)
	}
}

func TestLevelNamesUnknownInTable(t *testing.T) {
	page := "| Event | Example level | Why |\n|---|---|---|\n| down | `page` | wakes |\n| odd | `urgent` | no |\n"
	got := CheckLevelNames(page, known)
	if len(got) != 1 || got[0].Line != 4 || !strings.Contains(got[0].Msg, "urgent") {
		t.Errorf("findings = %+v; want one on line 4 naming urgent", got)
	}
}

func TestLevelNamesUnknownFlag(t *testing.T) {
	page := "```sh\nyo-human notify --level=urgent --title x\nyo-human notify --level urgent --title y\n```\n"
	got := CheckLevelNames(page, known)
	if len(got) != 2 || got[0].Line != 2 || got[1].Line != 3 {
		t.Errorf("findings = %+v; want two, on lines 2 and 3", got)
	}
}

func repoPages(t *testing.T) (guide string, examples []string) {
	t.Helper()
	guide = "../../docs/paging-guide.md"
	if _, err := os.Stat(guide); err != nil {
		t.Errorf("docs/paging-guide.md is missing: %v", err)
	}
	examples, err := filepath.Glob("../../docs/examples/*.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(examples) < 3 {
		t.Errorf("docs/examples has %d pages, want at least 3", len(examples))
	}
	return guide, examples
}

func TestRepoPagingGuideUsesStarterLevels(t *testing.T) {
	levels := starterLevels(t)
	guide, examples := repoPages(t)
	for _, f := range append([]string{guide}, examples...) {
		md, err := os.ReadFile(f)
		if err != nil {
			continue // already reported as missing
		}
		for _, fd := range CheckLevelNames(string(md), levels) {
			t.Errorf("%s:%d %s", filepath.Base(f), fd.Line, fd.Msg)
		}
	}
}

func TestRepoExamplePagesFollowSetupRules(t *testing.T) {
	_, examples := repoPages(t)
	for _, f := range examples {
		md, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, fd := range CheckSetupPage(filepath.Base(f), md) {
			t.Errorf("%s:%d [%s] %s", fd.Path, fd.Line, fd.Rule, fd.Msg)
		}
	}
}
