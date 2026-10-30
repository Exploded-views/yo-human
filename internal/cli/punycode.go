package cli

import (
	"strings"
	"unicode/utf8"
)

// The parameters of RFC 3492's Bootstring for Punycode.
const (
	pcBase        = 36
	pcTMin        = 1
	pcTMax        = 26
	pcSkew        = 38
	pcDamp        = 700
	pcInitialBias = 72
	pcInitialN    = 128
)

// asciiHost is host as the DNS sees it: each label that is not plain ASCII becomes its "xn--"
// Punycode form, so a look-alike letter cannot pass for the name it imitates. Labels are lower-cased
// first; the full IDNA mapping is not applied, since the point is to show the bytes, not to resolve.
func asciiHost(host string) (string, bool) {
	labels := strings.Split(host, ".")
	changed := false
	for i, l := range labels {
		if !isASCII(l) {
			labels[i], changed = "xn--"+punycode(strings.ToLower(l)), true
		}
	}
	return strings.Join(labels, "."), changed
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// punycode encodes one label (RFC 3492, section 6.3).
func punycode(label string) string {
	runes := []rune(label)
	var out []byte
	for _, r := range runes {
		if r < utf8.RuneSelf {
			out = append(out, byte(r))
		}
	}
	basic := len(out)
	if basic > 0 {
		out = append(out, '-')
	}
	n, delta, bias, handled := rune(pcInitialN), 0, pcInitialBias, basic
	for handled < len(runes) {
		m := smallestFrom(runes, n)
		delta += int(m-n) * (handled + 1)
		n = m
		for _, r := range runes {
			if r < n {
				delta++
			}
			if r == n {
				out = appendDelta(out, delta, bias)
				bias = adapt(delta, handled+1, handled == basic)
				delta, handled = 0, handled+1
			}
		}
		delta, n = delta+1, n+1
	}
	return string(out)
}

// smallestFrom is the smallest rune in runes that is at least n.
func smallestFrom(runes []rune, n rune) rune {
	m := rune(utf8.MaxRune)
	for _, r := range runes {
		if r >= n && r < m {
			m = r
		}
	}
	return m
}

// appendDelta writes delta as a generalized variable-length integer.
func appendDelta(out []byte, delta, bias int) []byte {
	q := delta
	for k := pcBase; ; k += pcBase {
		t := min(max(k-bias, pcTMin), pcTMax)
		if q < t {
			return append(out, digit(q))
		}
		out = append(out, digit(t+(q-t)%(pcBase-t)))
		q = (q - t) / (pcBase - t)
	}
}

// digits are Punycode's 36 digit values, in order.
const digits = "abcdefghijklmnopqrstuvwxyz0123456789"

func digit(d int) byte { return digits[d] }

func adapt(delta, points int, first bool) int {
	if first {
		delta /= pcDamp
	} else {
		delta /= 2
	}
	delta += delta / points
	k := 0
	for delta > ((pcBase-pcTMin)*pcTMax)/2 {
		delta /= pcBase - pcTMin
		k += pcBase
	}
	return k + (pcBase-pcTMin+1)*delta/(delta+pcSkew)
}
