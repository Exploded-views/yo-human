// Package bark sends hosted mode's pushes to the owner's self-hosted Bark server. It is an
// egress package: standard library and internal/hosted/notify only.
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
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/notify"
)

// timeout bounds one push, whatever the context allows.
const timeout = 5 * time.Second

// ownClient is the client a nil Client.HTTP means, so app never builds one.
var ownClient = &http.Client{Timeout: timeout}

// Client is the Bark channel. HTTP is nil for the package's own client, with a 5 s timeout.
type Client struct {
	BaseURL, User, Password, DeviceKey string
	HTTP                               *http.Client
}

var _ notify.Channel = (*Client)(nil)

// Name is the channel's name.
func (c *Client) Name() string { return "bark" }

// Levels are the levels Bark can carry.
func (c *Client) Levels() []notify.Level {
	return []notify.Level{notify.LevelPassive, notify.LevelActive, notify.LevelTimeSensitive, notify.LevelCritical}
}

// push is the request body. ID is the APNs collapse id: the same id replaces the notification on
// the phone instead of adding a second.
type push struct {
	DeviceKey string `json:"device_key"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Group     string `json:"group"`
	Level     string `json:"level"`
	ID        string `json:"id,omitempty"`
}

// barkLevel maps the bus's level names to Bark's: only time-sensitive is spelled differently.
func barkLevel(l notify.Level) string {
	if l == notify.LevelTimeSensitive {
		return "timeSensitive"
	}
	return string(l)
}

// Send pushes m to the owner's phone. An error names the status and never the password or the
// device key, nor anything the server answered.
func (c *Client) Send(ctx context.Context, m notify.Message) error {
	body, err := json.Marshal(push{DeviceKey: c.DeviceKey, Title: m.Title, Body: m.Body, Group: m.Group, Level: barkLevel(m.Level), ID: m.ID})
	if err != nil {
		return fmt.Errorf("bark: encode push: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(c.BaseURL, "/")+"/push", bytes.NewReader(body))
	if err != nil {
		return notify.Broken(errors.New("bark: the base URL is not usable"))
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.User, c.Password)
	hc := c.HTTP
	if hc == nil {
		hc = ownClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		// The transport's error names the URL; the cause that matters survives as a context error
		// or as the bare fact that the request failed.
		if ctx.Err() != nil {
			return fmt.Errorf("bark: request cancelled: %w", ctx.Err())
		}
		return errors.New("bark: request failed (network or timeout)")
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	return classify(resp)
}

// classify turns a response into nil (2xx) or an error in one of the notify classes. The message
// names the status only: the response body may echo anything.
func classify(resp *http.Response) error {
	code := resp.StatusCode
	if code >= 200 && code < 300 {
		return nil
	}
	err := notify.WithStatus(fmt.Errorf("bark: push refused with status %d", code), code)
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return notify.Broken(err)
	case code == http.StatusTooManyRequests || code == http.StatusServiceUnavailable:
		return notify.WithRetryAfter(err, retryAfter(resp.Header.Get("Retry-After")))
	case code >= 400 && code < 500 && code != http.StatusRequestTimeout:
		return notify.Permanent(err)
	}
	return err
}

// retryAfter reads a Retry-After in whole seconds; anything else is absent.
func retryAfter(v string) time.Duration {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(min(n, int64(notify.MaxRetryAfter/time.Second))) * time.Second
}
