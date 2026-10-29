//go:build e2e

package e2e

import (
	"os"
	"testing"
)

// The real binary with no HOME and no XDG directories exits 78 and creates nothing in its working
// directory.
func TestNoHomeCreatesNothing(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	sb.extraEnv = []string{"HOME=", "XDG_CONFIG_HOME=", "XDG_STATE_HOME="}
	for _, args := range [][]string{{"levels"}, {"config", "init"}, {"notify", "--level", "note", "--title", "t"}} {
		sb.run(args...).wantExit(t, 78)
	}
	if ents, _ := os.ReadDir(sb.cwd); len(ents) != 0 {
		t.Errorf("the working directory now holds %v", ents)
	}
	for _, f := range sb.fakesAll() {
		f.wantCount(t, 0)
	}
}
