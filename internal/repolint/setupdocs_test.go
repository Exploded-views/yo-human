package repolint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goodPage is a page that passes every rule; each fixture below breaks one rule of it.
const goodPage = "# Thing\n\n" +
	"## Inputs\n\n" +
	"- `TOPIC_NAME=\"<your topic>\"` — the topic to use\n" +
	"- `SERVER_URL=\"<https://ntfy.example.com>\"` — your server\n\n" +
	"## Already set\n\n" +
	"Nothing yet.\n\n" +
	"## Steps\n\n" +
	"```sh\n" +
	"op read \"op://vault/item/field\" | yo-human secret set \"$TOPIC_NAME\"\n" +
	"echo \"${SERVER_URL}\"\n" +
	"```\n"

func rules(fs []Finding) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	return strings.Join(out, ",")
}

func TestSetupPageGood(t *testing.T) {
	if got := CheckSetupPage("good.md", []byte(goodPage)); len(got) != 0 {
		t.Errorf("good page: %+v; want none", got)
	}
}

func TestSetupPageRules(t *testing.T) {
	cases := []struct {
		name, from, to, want string
	}{
		{"inputs not first", "## Inputs\n\n", "## Overview\n\nText.\n\n## Inputs\n\n", "inputs-first"},
		{"no h2 at all", "## Inputs\n\n", "# Thing\n\nText only.\n", "inputs-first"},
		{"bullet shape", "`TOPIC_NAME=\"<your topic>\"` —", "`topic_name=\"<your topic>\"` —", "input-format"},
		{"bullet without placeholder", "`SERVER_URL=\"<https://ntfy.example.com>\"`", "`SERVER_URL=\"https://ntfy.example.com\"`", "input-format"},
		{"bullet without description", "` — the topic to use", "`", "input-format"},
		{"already set not second", "## Already set\n\nNothing yet.\n\n", "", "already-set-second"},
		{"unused input", "echo \"${SERVER_URL}\"\n", "echo ok\n", "input-unused"},
		{"unused input, similar name", "\"$TOPIC_NAME\"", "\"$TOPIC_NAMES\"", "input-unused"},
		{"assigned again", "echo \"${SERVER_URL}\"\n", "SERVER_URL=\"x\"\necho \"$SERVER_URL\"\n", "input-reassigned"},
		{"exported again", "echo \"${SERVER_URL}\"\n", "export TOPIC_NAME=x\necho \"$SERVER_URL\"\n", "input-reassigned"},
		{"tilde in a shell block", "echo \"${SERVER_URL}\"\n", "ls ~/.config\necho \"$SERVER_URL\"\n", "tilde-home"},
		{"tilde in an unlabelled block", "```sh\n", "```\n", "tilde-home"},
		{"op without a pipe", "op read \"op://vault/item/field\" | yo-human secret set \"$TOPIC_NAME\"\n", "op read \"op://vault/item/field\"\necho \"$TOPIC_NAME\"\n", "op-unpiped"},
		{"op piped elsewhere", "| yo-human secret set \"$TOPIC_NAME\"", "| tee out \"$TOPIC_NAME\"", "op-unpiped"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(goodPage, c.from) {
				t.Fatalf("fixture text %q is not in the good page", c.from)
			}
			page := strings.Replace(goodPage, c.from, c.to, 1)
			if c.name == "tilde in an unlabelled block" {
				page = strings.Replace(page, "echo \"${SERVER_URL}\"\n", "ls ~/x\necho \"$SERVER_URL\"\n", 1)
			}
			if c.name == "no h2 at all" {
				page = c.to
			}
			got := CheckSetupPage("bad.md", []byte(page))
			if rules(got) != c.want {
				t.Errorf("rules = %q, want %q (findings %+v)", rules(got), c.want, got)
			}
			for _, f := range got {
				if f.Path != "bad.md" || f.Line < 1 || f.Msg == "" {
					t.Errorf("finding %+v: want the page name, a line number and a message", f)
				}
			}
		})
	}
}

func TestSetupPageTildeOutsideShellIsFine(t *testing.T) {
	page := goodPage + "\nProse may say ~/ freely.\n\n```yaml\npath: ~/x\n```\n"
	if got := CheckSetupPage("p.md", []byte(page)); len(got) != 0 {
		t.Errorf("tilde in prose and yaml: %+v; want none", got)
	}
}

func TestSetupPageInputUsedOnlyInItsOwnSection(t *testing.T) {
	// $SERVER_URL appears in the Inputs prose but nowhere after it: still unused.
	page := strings.Replace(goodPage, "echo \"${SERVER_URL}\"\n", "echo ok\n", 1)
	page = strings.Replace(page, "## Inputs\n\n", "## Inputs\n\nSet $SERVER_URL first.\n\n", 1)
	if got := rules(CheckSetupPage("p.md", []byte(page))); got != "input-unused" {
		t.Errorf("rules = %q, want input-unused", got)
	}
}

func TestSetupPageOpContinuation(t *testing.T) {
	page := strings.Replace(goodPage, "op read \"op://vault/item/field\" | yo-human secret set \"$TOPIC_NAME\"\n",
		"op read \"op://vault/item/field\" \\\n  | yo-human secret set \"$TOPIC_NAME\"\n", 1)
	if got := CheckSetupPage("p.md", []byte(page)); len(got) != 0 {
		t.Errorf("continued pipe: %+v; want none", got)
	}
}

func TestSetupPageHeadingsInsideFencesIgnored(t *testing.T) {
	page := strings.Replace(goodPage, "## Inputs\n", "```md\n## Notes\n```\n\n## Inputs\n", 1)
	if got := CheckSetupPage("p.md", []byte(page)); len(got) != 0 {
		t.Errorf("heading inside a fence: %+v; want none", got)
	}
}

func TestRepoSetupPages(t *testing.T) {
	files, err := filepath.Glob("../../docs/setup/*.md")
	if err != nil {
		t.Fatal(err)
	}
	var checked []string
	for _, f := range files {
		if filepath.Base(f) == "README.md" {
			continue
		}
		md, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		checked = append(checked, filepath.Base(f))
		for _, fd := range CheckSetupPage(filepath.Base(f), md) {
			t.Errorf("%s:%d [%s] %s", fd.Path, fd.Line, fd.Rule, fd.Msg)
		}
	}
	for _, want := range []string{"ntfy.md", "bark.md", "pagerduty.md", "cron-flush.md"} {
		if !contains(checked, want) {
			t.Errorf("docs/setup/%s is missing", want)
		}
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
