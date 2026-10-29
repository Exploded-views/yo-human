package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// The starter's log path is written into YAML. A state directory whose name holds a double quote,
// a backslash and a colon followed by a space must come back from the written file exactly.
func TestConfigInitRoundTripsALogPathWithQuotesAndBackslashes(t *testing.T) {
	s := newSandbox(t)
	state := filepath.Join(s.home, `st"a\te: x # y`)
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
