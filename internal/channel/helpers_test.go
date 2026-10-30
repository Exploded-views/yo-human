package channel

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncate(t *testing.T) {
	const m = "…[cut]"
	if got := Truncate("short", 100, m); got != "short" {
		t.Errorf("short = %q", got)
	}
	exact := strings.Repeat("a", 100)
	if got := Truncate(exact, 100, m); got != exact {
		t.Error("a string of exactly max was altered")
	}
	if got := Truncate(strings.Repeat("a", 101), 100, m); len(got) != 100 || !strings.HasSuffix(got, m) {
		t.Errorf("ascii: len=%d", len(got))
	}
	truncateMultibyte(t, m)
	if got := Truncate(strings.Repeat("é", 50), 3, m); len(got) > 3 || !utf8.ValidString(got) {
		t.Errorf("max below the marker: %q", got)
	}
}

func truncateMultibyte(t *testing.T, m string) {
	t.Helper()
	got := Truncate("x"+strings.Repeat("é", 200), 100, m)
	if len(got) > 100 || len(got) < 90 || !utf8.ValidString(got) || !strings.HasSuffix(got, m) {
		t.Errorf("multibyte: len=%d valid=%v", len(got), utf8.ValidString(got))
	}
}

func TestSafeLink(t *testing.T) {
	good := []string{"https://example.com/a?b=c", "http://example.com", "https://example.com:8443/x"}
	bad := []string{"", "intent://x#Intent;scheme=evil;end", "javascript:alert(1)", "tel:+15550100", "ftp://x.example/f",
		"//x.example", "https:///nohost", "https://:8080/", "https://u" + ":p" + "@x.example/", "https://u" + "@x.example/", "http://%zz"} // split in the source so the public-repo scrub stays uniform
	for _, u := range good {
		if !SafeLink(u) {
			t.Errorf("SafeLink(%q) = false", u)
		}
	}
	for _, u := range bad {
		if SafeLink(u) {
			t.Errorf("SafeLink(%q) = true", u)
		}
	}
}
