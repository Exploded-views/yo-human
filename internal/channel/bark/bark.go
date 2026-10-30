// Package bark is the Bark channel: it pushes through Bark's JSON API
// (https://github.com/Finb/bark-server/blob/master/docs/API_V2.md). The device key works as a
// password, so it arrives as a secret at construction and never appears in an error.
//
// Bark cannot update or close a notification, so the channel has no resolve, dedup or schedule.
// A Bark server reports many failures in the JSON envelope (code, message), so a 2xx answer is
// only a success when the envelope says code 200.
package bark

import (
	"context"
	_ "embed" // the settings schema
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sync"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/transport"
)

//go:embed schema.json
var schema []byte

const (
	maxBody       = 4000
	maxTitle      = 200
	defaultServer = "https://api.day.app"
)

var caps = channel.Capabilities{
	Interruptions: map[channel.Interruption]bool{channel.Passive: true, channel.Active: true,
		channel.TimeSensitive: true, channel.Critical: true},
	Link:         true,
	Subtitle:     true,
	ReachesHuman: true,
	MaxTitle:     maxTitle,
	MaxBody:      maxBody,
}

// NewType returns the bark channel type whose user agent is yo-human/<version>. The caller passes
// version.Version: depguard keeps a channel from importing internal/version itself.
func NewType(version string) channel.Type {
	return channel.Type{
		Name:           "bark",
		Capabilities:   caps,
		SecretFields:   []string{"device_key", "username", "password"},
		SettingsSchema: schema,
		New: func(in channel.Instance) (channel.Channel, error) {
			return newWithClient(in, version, nil)
		},
	}
}

// keyRE is compiled on first use, not at init: init runs on every command, version included.
var keyRE = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`) })

type barkChannel struct {
	base, key, ua string
	user, pass    string // basic auth for a self-hosted server; both set or both empty
	client        *http.Client
}

// newWithClient builds a channel; a nil client means a default one. The client has no timeout:
// the context carries the deadline. It never follows redirects, so the key and any basic-auth credentials are not forwarded.
func newWithClient(in channel.Instance, version string, client *http.Client) (channel.Channel, error) {
	base, err := parseServer(in.Settings["server"])
	if err != nil {
		return nil, channel.Misconfigured(fmt.Errorf("bark instance %q: setting server %w", in.Name, err))
	}
	key := in.Secrets["device_key"]
	if !keyRE().MatchString(key) {
		return nil, channel.Misconfigured(fmt.Errorf(
			"bark instance %q: secret field device_key is missing or invalid (1 to 64 letters, digits, - or _)", in.Name))
	}
	user, pass := in.Secrets["username"], in.Secrets["password"]
	if (user == "") != (pass == "") {
		return nil, channel.Misconfigured(fmt.Errorf(
			"bark instance %q: secret fields username and password go together; set both or neither", in.Name))
	}
	return &barkChannel{base: base, key: key, user: user, pass: pass, ua: "yo-human/" + version,
		client: transport.NoRedirectClient(client)}, nil
}

// parseServer checks the server setting without echoing it; the text continues "setting server ...".
// An absent setting means the public Bark server.
func parseServer(v any) (string, error) {
	if v == nil {
		return defaultServer, nil
	}
	s, _ := v.(string)
	server, ok := transport.ServerURL(s)
	if !ok {
		return "", errors.New("must be an https URL with a host, without credentials, query or fragment")
	}
	return server, nil
}

func (*barkChannel) Type() string                       { return "bark" }
func (*barkChannel) Capabilities() channel.Capabilities { return caps }

// options are the per-send options.
type options struct {
	sound     string
	call      bool
	volume    int
	hasVolume bool
}

func parseOptions(o map[string]any) (options, error) {
	var out options
	for k, v := range o {
		switch k {
		case "sound":
			s, ok := v.(string)
			if !ok {
				return out, errors.New("bark option sound must be a string")
			}
			out.sound = s
		case "call":
			b, ok := v.(bool)
			if !ok {
				return out, errors.New("bark option call must be true or false")
			}
			out.call = b
		case "volume":
			n, ok := wholeNumber(v)
			if !ok || n < 0 || n > 10 {
				return out, errors.New("bark option volume must be a whole number from 0 to 10")
			}
			out.volume, out.hasVolume = n, true
		default:
			return out, fmt.Errorf("unknown bark option %q; valid: sound, call, volume", k)
		}
	}
	return out, nil
}

// wholeNumber accepts the integer forms a config decoder produces.
func wholeNumber(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), n == int64(int(n))
	case float64:
		return int(n), n == float64(int(n))
	}
	return 0, false
}

// Validate checks the per-send options: sound (a string), call (a bool) and volume (0 to 10).
func (*barkChannel) Validate(o map[string]any) error {
	if _, err := parseOptions(o); err != nil {
		return channel.Misconfigured(err)
	}
	return nil
}

// Resolve is not supported: Bark cannot close a notification.
func (*barkChannel) Resolve(context.Context, string, channel.Message) error {
	return channel.Permanent(errors.New("bark cannot resolve an incident"))
}

// Cancel is not supported: Bark has no scheduled delivery.
func (*barkChannel) Cancel(context.Context, channel.Receipt) error {
	return channel.Permanent(errors.New("bark cannot cancel a message"))
}
