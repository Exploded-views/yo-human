// Package pagerduty is the PagerDuty channel: it triggers and resolves alerts through the Events
// API v2 (https://docs.pagerduty.com/developer/send-alert-event). The routing key identifies the
// service and works as a password, so it arrives as a secret at construction, travels only in the
// JSON body, and never appears in an error.
//
// PagerDuty fixes urgency per service, so a loud and a quiet page are two instances. Alerts are
// keyed by dedup_key, so a repeat updates the open alert and a resolve closes it. The resolve event
// carries only the routing key, the action and the dedup key; it has no place for a note.
package pagerduty

import (
	"context"
	_ "embed" // the settings schema
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/transport"
)

//go:embed schema.json
var schema []byte

const (
	maxSummary    = 1024
	maxBody       = 16384 // the spec's body limit; far inside the Events API's 512 KB event limit
	defaultEvents = "https://events.pagerduty.com/v2/enqueue"
	truncMarker   = "…[truncated]"
)

var caps = channel.Capabilities{
	Interruptions: map[channel.Interruption]bool{channel.Passive: true, channel.Active: true,
		channel.TimeSensitive: true, channel.Critical: true},
	Link:         true,
	NativeDedup:  true,
	Resolve:      true,
	ReachesHuman: true,
	MaxTitle:     maxSummary,
	MaxBody:      maxBody,
}

// NewType returns the pagerduty channel type whose user agent is yo-human/<version>. The caller
// passes version.Version: depguard keeps a channel from importing internal/version itself.
func NewType(version string) channel.Type {
	return channel.Type{
		Name:           "pagerduty",
		Capabilities:   caps,
		SecretFields:   []string{"routing_key"},
		SettingsSchema: schema,
		New: func(in channel.Instance) (channel.Channel, error) {
			return newWithClient(in, version, nil)
		},
	}
}

type pdChannel struct {
	events, key, ua string
	client          *http.Client
}

// newWithClient builds a channel; a nil client means a default one. The client has no timeout: the
// context carries the deadline. It never follows redirects, so the key is not forwarded.
func newWithClient(in channel.Instance, version string, client *http.Client) (channel.Channel, error) {
	events, err := parseEventsURL(in.Settings["events_url"])
	if err != nil {
		return nil, channel.Misconfigured(fmt.Errorf("pagerduty instance %q: setting events_url %w", in.Name, err))
	}
	key := in.Secrets["routing_key"]
	if key == "" || strings.ContainsFunc(key, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return nil, channel.Misconfigured(fmt.Errorf(
			"pagerduty instance %q: secret field routing_key is missing or invalid (not empty, no spaces or control characters)", in.Name))
	}
	return &pdChannel{events: events, key: key, ua: "yo-human/" + version, client: transport.NoRedirectClient(client)}, nil
}

// parseEventsURL checks the events_url setting without echoing it; the text continues "setting
// events_url ...". An absent setting means PagerDuty's public endpoint.
func parseEventsURL(v any) (string, error) {
	if v == nil {
		return defaultEvents, nil
	}
	s, _ := v.(string)
	if _, ok := transport.ServerURL(s); !ok { // the check ntfy's and Bark's server take; the path is kept as given
		return "", errors.New("must be an https URL with a host, without credentials, query or fragment")
	}
	return s, nil
}

func (*pdChannel) Type() string                       { return "pagerduty" }
func (*pdChannel) Capabilities() channel.Capabilities { return caps }

// Validate rejects every per-send option: PagerDuty urgency and routing belong to the service, so
// a different routing key is a different instance.
func (*pdChannel) Validate(o map[string]any) error {
	if len(o) > 0 {
		return channel.Misconfigured(errors.New("pagerduty takes no per-send options; use a second instance for a second service"))
	}
	return nil
}

// Cancel is not supported: PagerDuty has no scheduled delivery.
func (*pdChannel) Cancel(context.Context, channel.Receipt) error {
	return channel.Permanent(errors.New("pagerduty cannot cancel a message"))
}
