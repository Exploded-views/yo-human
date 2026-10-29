// Package logch is the local log channel: it appends one JSON line per send to a file. It never
// reaches a human, so it never counts as delivery.
package logch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

const schema = `{"type":"object","required":["path"],"properties":{"path":{"type":"string","minLength":1}},"additionalProperties":false}`

var caps = channel.Capabilities{
	ReachesHuman: false,
	Interruptions: map[channel.Interruption]bool{
		channel.Passive: true, channel.Active: true, channel.TimeSensitive: true, channel.Critical: true,
	},
	Link:       true,
	MaxActions: 3,
	MaxTitle:   200,
	MaxBody:    16384,
}

// Type is the log channel type.
var Type = channel.Type{
	Name:           "log",
	Capabilities:   caps,
	SettingsSchema: []byte(schema),
	New:            newChannel,
}

type logChannel struct{ path string }

func newChannel(in channel.Instance) (channel.Channel, error) {
	path, _ := in.Settings["path"].(string)
	if path == "" {
		return nil, channel.Misconfigured(fmt.Errorf("log channel %q needs a path setting (a file to append to)", in.Name))
	}
	if !filepath.IsAbs(path) {
		return nil, channel.Misconfigured(fmt.Errorf("log channel %q needs an absolute path: a relative one lands wherever the caller's working directory is", in.Name))
	}
	return &logChannel{path: path}, nil
}

type line struct {
	Time         time.Time            `json:"time"`
	Instance     string               `json:"instance"`
	Interruption channel.Interruption `json:"interruption"`
	Message      channel.Message      `json:"message"`
}

func (*logChannel) Type() string                       { return "log" }
func (*logChannel) Capabilities() channel.Capabilities { return caps }

// Validate accepts no per-send options.
func (*logChannel) Validate(options map[string]any) error {
	if len(options) > 0 {
		return channel.Misconfigured(errors.New("the log channel takes no per-send options"))
	}
	return nil
}

// Send appends one JSON line. A file error is transient: the disk may recover.
func (c *logChannel) Send(ctx context.Context, m channel.Message, s channel.SendSpec) (channel.Receipt, error) {
	if err := ctx.Err(); err != nil {
		return channel.Receipt{}, channel.Transient(err)
	}
	b, err := json.Marshal(line{Time: time.Now().UTC(), Instance: s.Instance, Interruption: s.Interruption, Message: m})
	if err != nil {
		return channel.Receipt{}, channel.Permanent(err)
	}
	// #nosec G304 -- the path is the owner's own config setting.
	f, err := os.OpenFile(c.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY|openFlags, 0o600)
	if err != nil {
		return channel.Receipt{}, openError(err)
	}
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		_ = f.Close()
		return channel.Receipt{}, channel.Permanent(errNotRegular)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return channel.Receipt{}, channel.Transient(err)
	}
	if err := f.Close(); err != nil {
		return channel.Receipt{}, channel.Transient(err)
	}
	return channel.Receipt{Instance: s.Instance}, nil
}

var errNotRegular = errors.New("the log path is not a regular file")

// openError classifies a failed open: a symlink, a directory or a FIFO nobody reads will not get
// better by retrying; a full disk might.
func openError(err error) error {
	if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EISDIR) || errors.Is(err, syscall.ENXIO) {
		return channel.Permanent(errNotRegular)
	}
	return channel.Transient(err)
}

var errUnsupported = errors.New("the log channel cannot do that")

// Resolve is unsupported: Capabilities.Resolve is false.
func (*logChannel) Resolve(context.Context, string, channel.Message) error {
	return channel.Permanent(errUnsupported)
}

// Cancel is unsupported: Capabilities.Schedule is false.
func (*logChannel) Cancel(context.Context, channel.Receipt) error {
	return channel.Permanent(errUnsupported)
}
