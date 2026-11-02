package core_test

import (
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/core"
)

func TestLooksNamespaced(t *testing.T) {
	for key, want := range map[string]bool{
		"5:alpha/x":                     true,
		"1:a/b":                         true,
		"3:a/b/c":                       true, // the project "a/b" and key "c"
		"h:" + strings.Repeat("0f", 32): true,
		"5:ab/x":                        false, // the length does not match
		"5:alpha":                       false, // no key after the project
		"5:alpha/":                      false, // an empty key
		"4:alpha/x":                     false, // no slash where the length says
		"0:/x":                          false, // no project
		"abc:def":                       false,
		"h:xyz":                         false,
		"h:" + strings.Repeat("A", 64):  false, // upper case is never produced
		"h:" + strings.Repeat("a", 63):  false,
		"h:" + strings.Repeat("a", 65):  false,
		"":                              false,
		"plain-key":                     false,
		"99999999999999999999:a/b":      false, // a length too big to be one
		"+5:alpha/x":                    false,
		"05:alpha/x":                    false, // not how Itoa writes 5
	} {
		if got := core.LooksNamespaced(key); got != want {
			t.Errorf("LooksNamespaced(%q) = %v; want %v", key, got, want)
		}
	}
	for _, c := range [][2]string{{"alpha", "x"}, {"a/b", "c"}, {"a", "b/c"}, {"p", strings.Repeat("k", 300)}} {
		if k := core.ProviderKey(c[0], c[1]); !core.LooksNamespaced(k) {
			t.Errorf("ProviderKey(%q, ...) = %q is not recognised", c[0], k)
		}
	}
}
