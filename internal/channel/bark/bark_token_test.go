package bark

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// tokenShaped is 64 hexadecimal characters, the shape of an APNs device token. It is built, not
// written out, so no long hex literal sits in the tree.
var tokenShaped = strings.Repeat("a1", 32)

const tokenHint = "looks like an APNs device token"

func sendWithKey(t *testing.T, h http.HandlerFunc, key string) error {
	t.Helper()
	f := newServer(t, h)
	c, err := buildAuth(t, f, map[string]string{"device_key": key})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, err = c.Send(context.Background(), msg(), active)
	return err
}

func TestBadRequestWithTokenShapedKeyHints(t *testing.T) {
	cases := map[string]struct {
		h   http.HandlerFunc
		key string
	}{
		"HTTP 400":           {status(http.StatusBadRequest), tokenShaped},
		"envelope code 400":  {reply(`{"code":400,"message":"failed to push"}`), tokenShaped},
		"upper-case hex key": {status(http.StatusBadRequest), strings.ToUpper(tokenShaped)},
	}
	for name, tc := range cases {
		err := sendWithKey(t, tc.h, tc.key)
		if err == nil || channel.KindOf(err) != channel.PermanentError {
			t.Fatalf("%s: err=%v kind=%v, want permanent", name, err, channel.KindOf(err))
		}
		s := err.Error()
		if !strings.Contains(s, tokenHint) || !strings.Contains(s, "server URL") {
			t.Errorf("%s: no device-token hint in %q", name, s)
		}
		if strings.Contains(strings.ToLower(s), strings.ToLower(tokenShaped)) {
			t.Errorf("%s: error echoes the key: %q", name, s)
		}
	}
}

func TestNoTokenHintOtherwise(t *testing.T) {
	cases := map[string]struct {
		h   http.HandlerFunc
		key string
	}{
		"400, ordinary key":      {status(http.StatusBadRequest), fakeKey},
		"400, 63 hex":            {status(http.StatusBadRequest), tokenShaped[1:]},
		"400, 64 non-hex":        {status(http.StatusBadRequest), strings.Repeat("g1", 32)},
		"404, token-shaped key":  {status(http.StatusNotFound), tokenShaped},
		"code 404, token-shaped": {reply(`{"code":404,"message":"no"}`), tokenShaped},
		"500, token-shaped key":  {status(http.StatusInternalServerError), tokenShaped},
	}
	for name, tc := range cases {
		err := sendWithKey(t, tc.h, tc.key)
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
		if strings.Contains(err.Error(), tokenHint) {
			t.Errorf("%s: unexpected device-token hint in %q", name, err.Error())
		}
	}
}
