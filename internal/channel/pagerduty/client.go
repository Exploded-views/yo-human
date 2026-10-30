package pagerduty

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/transport"
)

type link struct {
	Href string `json:"href"`
	Text string `json:"text,omitempty"`
}

type details struct {
	Ask       string `json:"ask"`
	Body      string `json:"body"`
	MessageID string `json:"message_id"`
	Level     string `json:"level"`
}

type eventPayload struct {
	Summary       string  `json:"summary"`
	Source        string  `json:"source"`
	Severity      string  `json:"severity"`
	Component     string  `json:"component,omitempty"`
	CustomDetails details `json:"custom_details"`
}

// event is an Events API v2 body. Payload and Links are only set on a trigger.
type event struct {
	RoutingKey  string        `json:"routing_key"`
	EventAction string        `json:"event_action"`
	DedupKey    string        `json:"dedup_key"`
	Payload     *eventPayload `json:"payload,omitempty"`
	Links       []link        `json:"links,omitempty"`
}

var severities = map[channel.Interruption]string{channel.Passive: "info", channel.Active: "warning",
	channel.TimeSensitive: "error", channel.Critical: "critical"}

func buildTrigger(key string, m channel.Message, i channel.Interruption) (event, error) {
	sev, ok := severities[i]
	if !ok {
		return event{}, errors.New("pagerduty cannot send that interruption (it has no such severity)")
	}
	links, err := buildLinks(m)
	if err != nil {
		return event{}, err
	}
	source := m.Project
	if source == "" {
		source = "yo-human"
	}
	dedup := m.DedupKey
	if dedup == "" {
		dedup = m.ID
	}
	return event{RoutingKey: key, EventAction: "trigger", DedupKey: dedup, Links: links, Payload: &eventPayload{
		Summary: channel.Truncate(m.Title, maxSummary, truncMarker), Source: source, Severity: sev, Component: m.Agent,
		CustomDetails: details{Ask: channel.Truncate(m.Ask, maxBody, truncMarker), Body: channel.Truncate(m.Body, maxBody, truncMarker),
			MessageID: m.ID, Level: m.Level}}}, nil
}

// buildLinks is the link then one per action. A URL that is not a plain http(s) URL with a host is
// refused, and the error never quotes it.
func buildLinks(m channel.Message) ([]link, error) {
	var out []link
	if m.Link != "" {
		if !channel.SafeLink(m.Link) {
			return nil, errors.New("pagerduty link must be an http or https URL with a host and no credentials")
		}
		out = append(out, link{Href: m.Link})
	}
	for _, a := range m.Actions {
		if !channel.SafeLink(a.URL) {
			return nil, errors.New("pagerduty action URLs must be http or https URLs with a host and no credentials")
		}
		out = append(out, link{Href: a.URL, Text: a.Label})
	}
	return out, nil
}

// Send triggers (or updates) the alert keyed by the message's dedup key, else its id.
func (c *pdChannel) Send(ctx context.Context, m channel.Message, s channel.SendSpec) (channel.Receipt, error) {
	if err := c.Validate(s.Options); err != nil {
		return channel.Receipt{}, err
	}
	ev, err := buildTrigger(c.key, m, s.Interruption)
	if err != nil {
		return channel.Receipt{}, channel.Permanent(err)
	}
	if err := c.post(ctx, ev); err != nil {
		return channel.Receipt{}, err
	}
	return channel.Receipt{Instance: s.Instance, ProviderID: ev.DedupKey}, nil
}

// Resolve closes the alert with that dedup key. The note is not sent: a resolve event has no field
// for it.
func (c *pdChannel) Resolve(ctx context.Context, dedupKey string, _ channel.Message) error {
	if dedupKey == "" {
		return channel.Permanent(errors.New("pagerduty resolve needs a dedup key"))
	}
	return c.post(ctx, event{RoutingKey: c.key, EventAction: "resolve", DedupKey: dedupKey})
}

// post sends ev and maps the outcome to a channel error that carries no URL and no response text.
func (c *pdChannel) post(ctx context.Context, ev event) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return channel.Permanent(errors.New("pagerduty event could not be encoded"))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.events, bytes.NewReader(body))
	if err != nil {
		return channel.Permanent(errors.New("pagerduty request could not be built")) // the raw error embeds the URL
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return transport.TransportError(ctx, "pagerduty", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return classify(resp.StatusCode)
}

// classify maps a status to an error kind. The text holds the number only: the reply's message is
// the server's, and may echo what it was sent. Only 202 is a success; another 2xx is a proxy or
// portal answering, and retrying it risks a duplicate update of one keyed alert, not a lost page.
func classify(code int) error {
	if code == http.StatusAccepted {
		return nil
	}
	err := fmt.Errorf("pagerduty answered HTTP %d", code)
	if code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500 || (code >= 200 && code < 300) {
		return channel.Transient(err)
	}
	return channel.Permanent(err)
}
