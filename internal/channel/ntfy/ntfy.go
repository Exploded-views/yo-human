// Package ntfy is the ntfy channel: it publishes through ntfy's JSON API
// (https://docs.ntfy.sh/publish/#publish-as-json). The topic works as a password, so it and the
// token arrive as secrets at construction and never appear in an error.
//
// The ntfy server must be v2.16.0 or later: the channel relies on sequence_id for updates and on
// DELETE /<topic>/<sequence_id> to cancel a scheduled message. Two instances that share a topic
// share the sequence_id space (the message id), so one can replace or cancel the other's message.
// With the markdown option set, ntfy renders markdown from the agent, links included.
package ntfy

import (
	_ "embed" // the settings schema
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/transport"
)

//go:embed schema.json
var schema []byte

const (
	maxBody  = 4000
	maxTitle = 200
)

var caps = channel.Capabilities{
	Interruptions: map[channel.Interruption]bool{channel.Passive: true, channel.Active: true, channel.TimeSensitive: true},
	Markdown:      true,
	Link:          true,
	MaxActions:    3,
	Schedule:      true,
	ReachesHuman:  true,
	MaxTitle:      maxTitle,
	MaxBody:       maxBody,
}

// NewType returns the ntfy channel type whose user agent is yo-human/<version>. The caller passes
// version.Version: depguard keeps a channel from importing internal/version itself.
func NewType(version string) channel.Type {
	return channel.Type{
		Name:           "ntfy",
		Capabilities:   caps,
		SecretFields:   []string{"topic", "token"},
		SettingsSchema: schema,
		New: func(in channel.Instance) (channel.Channel, error) {
			return newWithClient(in, version, nil)
		},
	}
}

// topicRE is compiled on first use, not at init: init runs on every command, version included.
var topicRE = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`) })

type ntfyChannel struct {
	base, topic, token, ua string
	client                 *http.Client
}

// newWithClient builds a channel; a nil client means a default one. The client has no timeout:
// the context carries the deadline. It never follows redirects, so a token is not forwarded.
func newWithClient(in channel.Instance, version string, client *http.Client) (channel.Channel, error) {
	base, err := parseServer(in.Settings["server"])
	if err != nil {
		return nil, channel.Misconfigured(fmt.Errorf("ntfy instance %q: setting server %w", in.Name, err))
	}
	topic := in.Secrets["topic"]
	if !topicRE().MatchString(topic) {
		return nil, channel.Misconfigured(fmt.Errorf(
			"ntfy instance %q: secret field topic is missing or invalid (1 to 64 letters, digits, - or _)", in.Name))
	}
	token := in.Secrets["token"]
	if strings.ContainsAny(token, " \t\r\n") || strings.ContainsFunc(token, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return nil, channel.Misconfigured(fmt.Errorf("ntfy instance %q: secret field token must be one line without spaces", in.Name))
	}
	return &ntfyChannel{base: base, topic: topic, token: token, ua: "yo-human/" + version,
		client: transport.NoRedirectClient(client)}, nil
}

// parseServer checks the server setting without echoing it; the text continues "setting server ...".
func parseServer(v any) (string, error) {
	s, _ := v.(string)
	if s == "" {
		return "", errors.New("is required (an https URL)")
	}
	server, ok := transport.ServerURL(s)
	if !ok {
		return "", errors.New("must be an https URL with a host, without credentials, query or fragment")
	}
	return server, nil
}

func (*ntfyChannel) Type() string                       { return "ntfy" }
func (*ntfyChannel) Capabilities() channel.Capabilities { return caps }

// options are the per-send options.
type options struct {
	tags     []string
	markdown bool
}

func parseOptions(o map[string]any) (options, error) {
	var out options
	for k, v := range o {
		switch k {
		case "markdown":
			b, ok := v.(bool)
			if !ok {
				return out, errors.New("ntfy option markdown must be true or false")
			}
			out.markdown = b
		case "tags":
			tags, ok := stringList(v)
			if !ok {
				return out, errors.New("ntfy option tags must be a list of strings")
			}
			out.tags = tags
		default:
			return out, fmt.Errorf("unknown ntfy option %q; valid: tags, markdown", k)
		}
	}
	return out, nil
}

func stringList(v any) ([]string, bool) {
	switch l := v.(type) {
	case []string:
		return l, true
	case []any:
		out := make([]string, 0, len(l))
		for _, e := range l {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

// Validate checks the per-send options: tags (a list of strings) and markdown (a bool).
func (*ntfyChannel) Validate(o map[string]any) error {
	if _, err := parseOptions(o); err != nil {
		return channel.Misconfigured(err)
	}
	return nil
}
