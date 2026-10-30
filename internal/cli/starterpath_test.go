package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The starter's log path is written into YAML. A state directory whose name holds a double quote,
// a single quote, a backslash, a colon followed by a space, a newline and a very long run of words
// must come back from the written file exactly, on one `path:` line.
func TestConfigInitRoundTripsALogPathWithQuotesAndBackslashes(t *testing.T) {
	s := newSandbox(t)
	long := strings.TrimSpace(strings.Repeat("word ", 30))
	state := filepath.Join(s.home, `st"a\te: x # y'z`+"\nw "+long)
	expect(t, len(long) > 120, "the long name is %d bytes", len(long))
	t.Setenv("XDG_STATE_HOME", state)
	s.do("", "config", "init").succeeds(t, "init")
	body, err := os.ReadFile(s.userConfig())
	expect(t, err == nil, "read the starter: %v", err)
	var doc struct {
		Channels map[string]struct{ Path string }
	}
	expect(t, yamlUnmarshal(body, &doc) == nil, "the starter is not YAML")
	want := filepath.Join(state, "yo-human", "messages.log")
	expect(t, doc.Channels["journal"].Path == want, "journal path = %q, want %q", doc.Channels["journal"].Path, want)
	lines := 0
	for _, l := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "path:") {
			lines++
			expect(t, strings.HasSuffix(l, `"`), "the path line is not a double-quoted scalar: %q", l)
		}
	}
	expect(t, lines == 1, "found %d path: lines in the starter; want 1, with the value on it", lines)
	for _, l := range strings.Split(string(body), "\n") {
		expect(t, !strings.HasPrefix(l, "word ") && !strings.HasPrefix(l, "w "), "a continuation line leaked out of the scalar: %q", l)
	}
}

// A state directory that is not valid UTF-8 cannot be written into YAML. config init refuses, and
// writes nothing, rather than leaving a `path:` with no value for `config check` to trip over.
func TestConfigInitRefusesAStatePathYAMLCannotEncode(t *testing.T) {
	s := newSandbox(t)
	t.Setenv("XDG_STATE_HOME", filepath.Join(s.home, "st\xff"))
	r := s.do("", "config", "init")
	expect(t, r.code != ExitOK && r.out == "", "init exited %d with out=%q err=%q; want a failure and no stdout", r.code, r.out, r.err)
	_, err := os.Stat(s.userConfig())
	expect(t, os.IsNotExist(err), "a config file was written: stat err = %v", err)
}

// config init checks the starter log path before it asks the human or creates anything: an
// unencodable state path on the default path creates no user config directory.
func TestConfigInitCreatesNoDirectoryForAnUnencodableStatePath(t *testing.T) {
	s := newSandbox(t)
	t.Setenv("XDG_STATE_HOME", filepath.Join(s.home, "st\xff"))
	r := s.do("", "config", "init")
	expect(t, r.code != ExitOK, "init exited %d; want a failure", r.code)
	_, err := os.Stat(s.configDir())
	expect(t, os.IsNotExist(err), "the user config directory was created: stat err = %v", err)
}

// On a --config path the human is never asked to approve a write that cannot happen.
func TestConfigInitNeverAsksToWriteAnUnencodableStatePath(t *testing.T) {
	s := newSandbox(t)
	t.Setenv("XDG_STATE_HOME", filepath.Join(s.home, "st\xff"))
	shown := s.tty("yes\n")
	target := filepath.Join(s.home, "mine.yaml")
	r := s.do("", "config", "init", "--config", target)
	expect(t, r.code != ExitOK, "init exited %d; want a failure", r.code)
	expect(t, shown.Len() == 0, "the human was asked: %q", shown.String())
	_, err := os.Stat(target)
	expect(t, os.IsNotExist(err), "the config was written: stat err = %v", err)
}
