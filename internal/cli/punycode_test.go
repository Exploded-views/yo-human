package cli

import "testing"

// Labels checked against Python's punycode codec, and look-alike hosts.
func TestPunycode(t *testing.T) {
	for in, want := range map[string]string{
		"bücher":    "bcher-kva",
		"pagerdսty": "pagerdty-tnj",
		"münchen":   "mnchen-3ya",
		"例え":        "r8jz45g",
		"ασδ":       "mxag8b",
	} {
		if got := punycode(in); got != want {
			t.Errorf("punycode(%q) = %q, want %q", in, got, want)
		}
	}
	if got, changed := asciiHost("events.PagerDսty.com:443"); got != "events.xn--pagerdty-tnj.com:443" || !changed {
		t.Errorf("asciiHost = %q, %v", got, changed)
	}
	if got, changed := asciiHost("ntfy.example.com"); got != "ntfy.example.com" || changed {
		t.Errorf("an ASCII host changed: %q", got)
	}
}
