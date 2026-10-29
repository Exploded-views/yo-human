package bark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/transport"
)

const truncMarker = "…[truncated]"

// payload is Bark's JSON push body. Bark takes call and volume as strings.
type payload struct {
	DeviceKey string `json:"device_key"`
	Title     string `json:"title,omitempty"`
	Subtitle  string `json:"subtitle,omitempty"`
	Body      string `json:"body"`
	Level     string `json:"level"`
	Group     string `json:"group,omitempty"`
	URL       string `json:"url,omitempty"`
	Sound     string `json:"sound,omitempty"`
	Call      string `json:"call,omitempty"`
	Volume    string `json:"volume,omitempty"`
}

var levels = map[channel.Interruption]string{channel.Passive: "passive", channel.Active: "active",
	channel.TimeSensitive: "timeSensitive", channel.Critical: "critical"}

func buildPayload(key string, m channel.Message, i channel.Interruption, o options) (payload, error) {
	level, ok := levels[i]
	if !ok {
		return payload{}, errors.New("bark cannot send that interruption (it has no such alert)")
	}
	if err := checkURLs(m); err != nil {
		return payload{}, err
	}
	p := payload{DeviceKey: key, Title: m.Title, Subtitle: m.Ask, Body: composeBody(m), Level: level,
		Group: m.Project, URL: m.Link, Sound: o.sound}
	if o.call {
		p.Call = "1"
	}
	if o.hasVolume {
		p.Volume = strconv.Itoa(o.volume)
	}
	return p, nil
}

// checkURLs refuses any link or action URL that is not a plain http(s) URL with a host: the phone
// would open an intent: or custom scheme, and userinfo is a phishing aid. The error never quotes it.
func checkURLs(m channel.Message) error {
	if m.Link != "" && !channel.SafeLink(m.Link) {
		return errors.New("bark link must be an http or https URL with a host and no credentials")
	}
	for _, a := range m.Actions {
		if !channel.SafeLink(a.URL) {
			return errors.New("bark action URLs must be http or https URLs with a host and no credentials")
		}
	}
	return nil
}

// composeBody is the body, then each action as "label: url" (Bark has no buttons). The action
// lines are the human's only way to act, so when the whole is too long only the body is cut. If
// the action lines alone fill the limit, the whole string is cut instead.
func composeBody(m channel.Message) string {
	lines := make([]string, len(m.Actions))
	for i, a := range m.Actions {
		lines[i] = a.Label + ": " + a.URL
	}
	suffix := strings.Join(lines, "\n")
	switch {
	case suffix == "":
		return channel.Truncate(m.Body, maxBody, truncMarker)
	case m.Body == "":
		return channel.Truncate(suffix, maxBody, truncMarker)
	}
	if room := maxBody - len(suffix) - 1; room >= len(truncMarker) {
		return channel.Truncate(m.Body, room, truncMarker) + "\n" + suffix
	}
	return channel.Truncate(m.Body+"\n"+suffix, maxBody, truncMarker)
}

// Send pushes one message. Bark returns no message id, so the receipt names only the instance.
func (c *barkChannel) Send(ctx context.Context, m channel.Message, s channel.SendSpec) (channel.Receipt, error) {
	o, err := parseOptions(s.Options)
	if err != nil {
		return channel.Receipt{}, channel.Misconfigured(err)
	}
	p, err := buildPayload(c.key, m, s.Interruption, o)
	if err != nil {
		return channel.Receipt{}, channel.Permanent(err)
	}
	body, err := json.Marshal(p)
	if err != nil {
		return channel.Receipt{}, channel.Permanent(errors.New("bark message could not be encoded"))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/push", bytes.NewReader(body))
	if err != nil {
		return channel.Receipt{}, channel.Permanent(errors.New("bark request could not be built")) // the raw error embeds the URL
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Content-Type", "application/json")
	if err := c.do(ctx, req); err != nil {
		return channel.Receipt{}, err
	}
	return channel.Receipt{Instance: s.Instance}, nil
}

// do sends req and maps the outcome to a channel error that carries no URL and no response text.
func (c *barkChannel) do(ctx context.Context, req *http.Request) error {
	resp, err := c.client.Do(req)
	if err != nil {
		return transport.TransportError(ctx, "bark", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return transport.TransportError(ctx, "bark", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return classify("HTTP", resp.StatusCode)
	}
	var env struct {
		Code *int `json:"code"`
	}
	if json.Unmarshal(raw, &env) != nil || env.Code == nil {
		// A 2xx without the envelope is not a Bark success: a proxy or portal answered. Retrying
		// risks a duplicate page; accepting it risks a lost one, which is worse. The reference
		// server always returns the envelope, so a real success is never retried.
		return channel.Transient(errors.New("bark answered without a recognisable reply"))
	}
	if *env.Code == http.StatusOK {
		return nil
	}
	return classify("code", *env.Code)
}

// classify maps a status or envelope code to an error kind. The text holds the number only: the
// envelope's message is the server's, and may echo what it was sent.
func classify(what string, code int) error {
	err := fmt.Errorf("bark answered %s %d", what, code)
	if code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500 {
		return channel.Transient(err)
	}
	return channel.Permanent(err)
}
