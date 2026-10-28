package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var out, errb bytes.Buffer
	code := Run([]string{"version"}, &out, &errb)
	if code != ExitOK || out.String() != "yo-human dev none\n" || errb.Len() != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errb.String())
	}
}

func TestRunNoArgsIsUsage(t *testing.T) {
	var out, errb bytes.Buffer
	code := Run(nil, &out, &errb)
	if code != ExitUsage || !strings.HasPrefix(errb.String(), "usage: yo-human") || out.Len() != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errb.String())
	}
}

func TestRunUnknownVerbIsUsage(t *testing.T) {
	var out, errb bytes.Buffer
	code := Run([]string{"bogus"}, &out, &errb)
	if code != ExitUsage || !strings.Contains(errb.String(), `unknown command "bogus"`) || out.Len() != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errb.String())
	}
}

// The exit codes are a contract with callers (agents), so pin the literals:
// comparing only against the constants cannot catch a changed value.
func TestExitCodeValues(t *testing.T) {
	if ExitOK != 0 || ExitUsage != 64 {
		t.Fatalf("ExitOK=%d ExitUsage=%d, want 0 and 64", ExitOK, ExitUsage)
	}
}
