// Package bark sends hosted mode's pushes to the owner's self-hosted Bark server. It is an
// egress package: standard library and internal/hosted/notify only.
package bark

import (
	"context"
	"net/http"

	"github.com/Exploded-views/yo-human/internal/hosted/notify"
)

// Client is the Bark channel. HTTP is nil for the package's own client.
type Client struct {
	BaseURL, User, Password, DeviceKey string
	HTTP                               *http.Client
}

var _ notify.Channel = (*Client)(nil)

// Name is the channel's name.
func (c *Client) Name() string { return "" }

// Levels are the levels Bark can carry.
func (c *Client) Levels() []notify.Level { return nil }

// Send pushes m to the owner's phone.
func (c *Client) Send(ctx context.Context, m notify.Message) error { return nil }
