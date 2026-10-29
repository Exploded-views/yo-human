package ntfy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/transport"
)

const (
	truncMarker = "…[truncated]"
	// ntfy refuses a delay under 10 seconds. The 15 seconds leave room for clock skew between us
	// and the server, so a nearer delivery time is sent at once.
	minDelay = 15 * time.Second
	// ntfy refuses a delay over 3 days (its default message-delay-limit).
	maxDelay = 72 * time.Hour
)

type action struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	URL    string `json:"url"`
}

// payload is ntfy's JSON publish body. ntfy's JSON API has no client-chosen message id; the
// sequence_id is the id a later DELETE names.
type payload struct {
	Topic      string   `json:"topic"`
	Title      string   `json:"title,omitempty"`
	Message    string   `json:"message"`
	Priority   int      `json:"priority"`
	Tags       []string `json:"tags,omitempty"`
	Click      string   `json:"click,omitempty"`
	Actions    []action `json:"actions,omitempty"`
	Markdown   bool     `json:"markdown,omitempty"`
	Delay      string   `json:"delay,omitempty"`
	SequenceID string   `json:"sequence_id,omitempty"`
}

var priorities = map[channel.Interruption]int{channel.Passive: 2, channel.Active: 3, channel.TimeSensitive: 4}

func buildPayload(topic string, m channel.Message, s channel.SendSpec, o options, now time.Time) (payload, error) {
	prio, ok := priorities[s.Interruption]
	if !ok {
		return payload{}, fmt.Errorf("ntfy cannot send interruption %q (it has no such alert)", s.Interruption)
	}
	if err := checkURLs(m); err != nil {
		return payload{}, err
	}
	if s.DeliverAt.After(now.Add(maxDelay)) {
		return payload{}, errors.New("ntfy cannot schedule a message more than 3 days ahead")
	}
	p := payload{Topic: topic, Title: m.Title, Message: channel.Truncate(compose(m), maxBody, truncMarker), Priority: prio,
		Click: m.Link, Markdown: o.markdown, SequenceID: m.ID}
	for _, t := range append(append(append([]string{}, o.tags...), m.Project), m.Agent) {
		if t != "" {
			p.Tags = append(p.Tags, t)
		}
	}
	for _, a := range m.Actions {
		p.Actions = append(p.Actions, action{Action: "view", Label: a.Label, URL: a.URL})
	}
	if s.DeliverAt.After(now.Add(minDelay)) {
		p.Delay = strconv.FormatInt(s.DeliverAt.Unix(), 10)
	}
	return p, nil
}

// checkURLs refuses any link or action URL that is not http(s): ntfy hands the URI to the phone,
// which would open an intent: or custom scheme. The error never quotes the URL.
func checkURLs(m channel.Message) error {
	if m.Link != "" && !channel.SafeLink(m.Link) {
		return errors.New("ntfy link must be an http or https URL")
	}
	for _, a := range m.Actions {
		if !channel.SafeLink(a.URL) {
			return errors.New("ntfy action URLs must be http or https URLs")
		}
	}
	return nil
}

// compose puts the ask on the first line, then the body.
func compose(m channel.Message) string {
	if m.Ask == "" {
		return m.Body
	}
	if m.Body == "" {
		return m.Ask
	}
	return m.Ask + "\n" + m.Body
}

// Send publishes one message. A scheduled send returns a receipt that Cancel can delete.
func (c *ntfyChannel) Send(ctx context.Context, m channel.Message, s channel.SendSpec) (channel.Receipt, error) {
	o, err := parseOptions(s.Options)
	if err != nil {
		return channel.Receipt{}, channel.Misconfigured(err)
	}
	p, err := buildPayload(c.topic, m, s, o, time.Now())
	if err != nil {
		return channel.Receipt{}, channel.Permanent(err)
	}
	body, err := json.Marshal(p)
	if err != nil {
		return channel.Receipt{}, channel.Permanent(errors.New("ntfy message could not be encoded"))
	}
	req, err := c.request(ctx, http.MethodPost, c.base+"/", bytes.NewReader(body))
	if err != nil {
		return channel.Receipt{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := c.do(ctx, req); err != nil {
		return channel.Receipt{}, err
	}
	return channel.Receipt{Instance: s.Instance, ProviderID: m.ID, Scheduled: p.Delay != ""}, nil
}

// Cancel deletes a scheduled message: DELETE /<topic>/<id>.
func (c *ntfyChannel) Cancel(ctx context.Context, r channel.Receipt) error {
	if r.ProviderID == "" {
		return channel.Permanent(errors.New("ntfy cancel needs the receipt's provider id"))
	}
	req, err := c.request(ctx, http.MethodDelete, c.base+"/"+url.PathEscape(c.topic)+"/"+url.PathEscape(r.ProviderID), nil)
	if err != nil {
		return err
	}
	err = c.do(ctx, req)
	if channel.KindOf(err) == channel.PermanentError && errors.Is(err, errNotFound) {
		// A 404 cannot tell a send already gone from a server (before v2.16) or proxy that cannot
		// cancel at all, so it stays a failure the human sees: a page may still fire.
		return channel.Permanent(fmt.Errorf("%w: %s", err, cancelUnsure))
	}
	return err
}

// cancelUnsure explains a cancel answered 404.
const cancelUnsure = "the scheduled send may already be gone, or this ntfy server cannot cancel scheduled messages (needs v2.16 or later); the human may still be paged"

// errNotFound marks a 404 answer.
var errNotFound = errors.New("HTTP 404")

// Resolve is not supported: ntfy has no incidents to close.
func (*ntfyChannel) Resolve(context.Context, string, channel.Message) error {
	return channel.Permanent(errors.New("ntfy cannot resolve an incident"))
}

func (c *ntfyChannel) request(ctx context.Context, method, target string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, channel.Permanent(errors.New("ntfy request could not be built")) // the raw error embeds the URL
	}
	req.Header.Set("User-Agent", c.ua)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

// do sends req and maps the outcome to a channel error that carries no URL and no response body.
func (c *ntfyChannel) do(ctx context.Context, req *http.Request) error {
	resp, err := c.client.Do(req)
	if err != nil {
		return transport.TransportError(ctx, "ntfy", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	err = fmt.Errorf("ntfy answered HTTP %d", resp.StatusCode)
	if resp.StatusCode == http.StatusNotFound {
		err = fmt.Errorf("ntfy answered %w", errNotFound)
	}
	switch {
	case resp.StatusCode == http.StatusRequestTimeout, resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return channel.Transient(err)
	}
	return channel.Permanent(err)
}
