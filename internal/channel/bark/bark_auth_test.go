package bark

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
)

const (
	fakeUser = "USER-FAKE-1"
	fakePass = "PASS-FAKE-1"
)

func buildAuth(t *testing.T, f *fakeServer, secrets map[string]string) (channel.Channel, error) {
	t.Helper()
	return newWithClient(channel.Instance{Name: "phone", Type: "bark",
		Settings: map[string]any{"server": f.URL}, Secrets: secrets}, "9.9.9", f.Client())
}

func TestBasicAuthIsSentWhenBothSet(t *testing.T) {
	var mu sync.Mutex
	var user, pass string
	var ok bool
	f := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		user, pass, ok = r.BasicAuth()
		mu.Unlock()
		_, _ = w.Write([]byte(okReply))
	})
	c, err := buildAuth(t, f, map[string]string{"device_key": fakeKey, "username": fakeUser, "password": fakePass})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := c.Send(context.Background(), msg(), active); err != nil {
		t.Fatalf("send: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !ok || user != fakeUser || pass != fakePass {
		t.Errorf("basic auth = %q %q ok=%v", user, pass, ok)
	}
}

func TestNoAuthHeaderWithoutCredentials(t *testing.T) {
	var mu sync.Mutex
	var got string
	f := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Get("Authorization")
		mu.Unlock()
		_, _ = w.Write([]byte(okReply))
	})
	if _, err := build(t, f).Send(context.Background(), msg(), active); err != nil {
		t.Fatalf("send: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got != "" {
		t.Errorf("Authorization = %q, want none", got)
	}
}

func TestOneCredentialIsMisconfigured(t *testing.T) {
	f := newServer(t, nil)
	cases := map[string]map[string]string{
		"username only": {"device_key": fakeKey, "username": fakeUser},
		"password only": {"device_key": fakeKey, "password": fakePass},
	}
	for name, s := range cases {
		_, err := buildAuth(t, f, s)
		if err == nil || channel.KindOf(err) != channel.ConfigError {
			t.Errorf("%s: err=%v kind=%v, want misconfigured", name, err, channel.KindOf(err))
			continue
		}
		text := err.Error()
		for _, want := range []string{"phone", "username", "password"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: message %q lacks %q", name, text, want)
			}
		}
		for _, secret := range []string{fakeUser, fakePass, fakeKey} {
			if strings.Contains(text, secret) {
				t.Errorf("%s: message echoes a secret value: %q", name, text)
			}
		}
	}
}

func TestTeapotIsPermanentWithAuthHint(t *testing.T) {
	for _, code := range []int{http.StatusTeapot, http.StatusUnauthorized} {
		f := newServer(t, status(code))
		c, err := buildAuth(t, f, map[string]string{"device_key": fakeKey, "username": fakeUser, "password": fakePass})
		if err != nil {
			t.Fatalf("new: %v", err)
		}
		_, err = c.Send(context.Background(), msg(), active)
		if err == nil || channel.KindOf(err) != channel.PermanentError {
			t.Fatalf("%d: err=%v kind=%v, want permanent", code, err, channel.KindOf(err))
		}
		s := err.Error()
		if !strings.Contains(s, "basic auth") || !strings.Contains(s, "username") || !strings.Contains(s, "password") {
			t.Errorf("%d: no basic-auth hint in %q", code, s)
		}
		for _, secret := range []string{fakeUser, fakePass, fakeKey} {
			if strings.Contains(s, secret) {
				t.Errorf("%d: error echoes a secret value: %q", code, s)
			}
		}
	}
}
