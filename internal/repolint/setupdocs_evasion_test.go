package repolint

import (
	"strings"
	"testing"
)

// withBlock appends a section holding body in a block opened by open and closed by closeFence.
func withBlock(open, closeFence, body string) string {
	if open == "" { // an indented block needs no fences
		return goodPage + "\n## More\n\nRun this:\n\n    " + body + "\n"
	}
	return goodPage + "\n## More\n\n" + open + "\n" + body + "\n" + closeFence + "\n"
}

func TestSetupPageTildeEvasions(t *testing.T) {
	cases := []struct{ name, open, close, body string }{
		{"text fence", "```text", "```", "ls ~/x"},
		{"fish fence", "```fish", "```", "ls ~/x"},
		{"ksh fence", "```ksh", "```", "ls ~/x"},
		{"shell-session fence", "```shell-session", "```", "ls ~/x"},
		{"upper-case SH fence", "```SH", "```", "ls ~/x"},
		{"info string with attributes", "```bash title=\"x\"", "```", "ls ~/x"},
		{"four-backtick fence", "````sh", "````", "ls ~/x"},
		{"tilde fence", "~~~sh", "~~~", "ls ~/x"},
		{"indented block", "", "", "ls ~/x"},
		{"cd to home", "```sh", "```", "cd ~"},
		{"cd to home then more", "```sh", "```", "cd ~ && ls"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rules(CheckSetupPage("p.md", []byte(withBlock(c.open, c.close, c.body)))); got != "tilde-home" {
				t.Errorf("rules = %q, want tilde-home", got)
			}
		})
	}
}

func TestSetupPageTildeAllowedInDataLanguages(t *testing.T) {
	for _, lang := range []string{"yaml", "YAML", "json", "md", "toml", "yaml title=\"x\""} {
		page := withBlock("```"+lang, "```", "path: ~/x")
		if got := CheckSetupPage("p.md", []byte(page)); len(got) != 0 {
			t.Errorf("%s fence: %+v; want none", lang, got)
		}
	}
}

func TestSetupPageOpPipeEvasions(t *testing.T) {
	bad := []string{
		`X=$(op read "op://v/i/f"); echo hi | yo-human secret set x`,
		`op read "op://v/i/f" > out; echo hi | yo-human secret set x`,
		`echo "op://v/i/f" | yo-human secret set x`,
		`op read "op://v/i/f" | tee out | yo-human secret set x`,
		`op read "op://v/i/f" && echo | yo-human secret set x`,
		"S=`op read \"op://v/i/f\"` | yo-human secret set x",
	}
	for _, b := range bad {
		if got := rules(CheckSetupPage("p.md", []byte(withBlock("```sh", "```", b)))); got != "op-unpiped" {
			t.Errorf("%q: rules = %q, want op-unpiped", b, got)
		}
	}
	good := []string{
		"op read \"op://v/i/f\" |\n  yo-human secret set x",
		"op read \"op://v/i/f\" \\\n  | yo-human secret set x",
		`op read --no-newline "op://$OP_VAULT/$OP_ITEM/f" | yo-human secret set x`,
	}
	for _, g := range good {
		if got := CheckSetupPage("p.md", []byte(withBlock("```sh", "```", g))); len(got) != 0 {
			t.Errorf("%q: %+v; want none", g, got)
		}
	}
}

func TestSetupPageReassignmentForms(t *testing.T) {
	for _, line := range []string{`TOPIC_NAME+=x`, `read TOPIC_NAME`, `read -r TOPIC_NAME`, `set TOPIC_NAME x`} {
		if got := rules(CheckSetupPage("p.md", []byte(withBlock("```sh", "```", line)))); got != "input-reassigned" {
			t.Errorf("%q: rules = %q, want input-reassigned", line, got)
		}
	}
}

func TestSetupPageReassignmentInAnotherBlock(t *testing.T) {
	page := withBlock("```bash", "```", "echo hi") + "\n```sh\nSERVER_URL=\"https://x.example.com\"\n```\n"
	if got := rules(CheckSetupPage("p.md", []byte(page))); got != "input-reassigned" {
		t.Errorf("rules = %q, want input-reassigned", got)
	}
}

func TestSetupPageInputsBullets(t *testing.T) {
	noBullets := strings.Replace(strings.Replace(goodPage,
		"- `TOPIC_NAME=\"<your topic>\"` — the topic to use\n", "", 1),
		"- `SERVER_URL=\"<https://ntfy.example.com>\"` — your server\n", "", 1)
	if got := rules(CheckSetupPage("p.md", []byte(noBullets))); !strings.Contains(got, "input-format") {
		t.Errorf("no bullets: rules = %q, want input-format", got)
	}
	star := strings.Replace(goodPage, "- `SERVER_URL", "* `SERVER_URL", 1)
	if got := rules(CheckSetupPage("p.md", []byte(star))); got != "input-format" {
		t.Errorf("star bullet: rules = %q, want input-format", got)
	}
	indented := strings.Replace(goodPage, "- `SERVER_URL", "  - `SERVER_URL", 1)
	if got := rules(CheckSetupPage("p.md", []byte(indented))); !strings.Contains(got, "input-format") {
		t.Errorf("indented bullet: rules = %q, want input-format", got)
	}
}

func TestSetupPageFenceEdges(t *testing.T) {
	cases := []struct{ name, page, want string }{
		{"a shorter run does not close a longer fence", withBlock("````sh", "````", "```\nls ~/x"), "tilde-home"},
		{"the other fence character does not close", withBlock("```sh", "```", "~~~\nls ~/x"), "tilde-home"},
		{"inline triple backticks open no fence", goodPage + "\n```x``` is inline code.\n\nProse may say ~/ freely.\n", ""},
		{"tab-indented block", goodPage + "\n## More\n\nRun this:\n\n\tls ~/x\n", "tilde-home"},
		{"a trailing || continues onto the next line", withBlock("```sh", "```", "false ||\nop read \"op://v/i/f\" | yo-human secret set x"), ""},
		{"command substitution inside the op reference", withBlock("```sh", "```", "op read \"op://v/$(date)/f\" | yo-human secret set x"), "op-unpiped"},
		{"|| is not a pipe into secret set", withBlock("```sh", "```", "op read \"op://v/i/f\" || yo-human secret set x"), "op-unpiped"},
		{"a bullet inside an Inputs fence is not a bullet", strings.Replace(goodPage, "## Already set", "```\n- not a bullet\n```\n\n## Already set", 1), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rules(CheckSetupPage("p.md", []byte(c.page))); got != c.want {
				t.Errorf("rules = %q, want %q", got, c.want)
			}
		})
	}
}

func TestSetupPageOneBadBulletIsOneFinding(t *testing.T) {
	page := "## Inputs\n\n* `A_B=\"<x>\"` — y\n\n## Already set\n\nok\n"
	if got := rules(CheckSetupPage("p.md", []byte(page))); got != "input-format" {
		t.Errorf("rules = %q, want one input-format", got)
	}
}
