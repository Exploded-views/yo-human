//go:build e2e

// Package e2e runs the real yo-human binary against fake ntfy, Bark and PagerDuty servers. The
// binary is built with -tags e2e, which compiles in two test-only hooks (the clock and an extra
// root certificate for the TLS fakes); a release build has neither.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// binPath is the e2e binary, built once by TestMain.
var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "yo-human-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e: cannot make a build directory:", err)
		os.Exit(2)
	}
	binPath = filepath.Join(dir, "yo-human")
	build := exec.Command("go", "build", "-trimpath", "-tags", "e2e", "-o", binPath, "./cmd/yo-human")
	build.Dir = filepath.Join("..", "..")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: go build failed: %v\n%s", err, out)
		_ = os.RemoveAll(dir)
		os.Exit(2)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// TestVersionRuns is a smoke test: the e2e binary starts and prints its version.
func TestVersionRuns(t *testing.T) {
	sb := newSandbox(t)
	r := sb.run("version")
	r.wantExit(t, 0)
}
