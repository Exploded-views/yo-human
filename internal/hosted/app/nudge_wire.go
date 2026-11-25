package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/egress/bark"
	"github.com/Exploded-views/yo-human/internal/hosted/notify"
	"github.com/Exploded-views/yo-human/internal/hosted/nudge"
)

var errBarkHalfSet = errors.New("YH_BARK_URL, YH_BARK_USER, YH_BARK_PASSWORD and YH_BARK_DEVICE_KEY must all be set, or none of them")

// barkChannel builds the nudge worker's channel from the configuration: nil when Bark is not
// configured at all, an error naming the variables when it is configured in part.
func barkChannel(cfg Config) (notify.Channel, error) {
	set := 0
	for _, v := range []string{cfg.BarkURL, cfg.BarkUser, cfg.BarkPassword.Reveal(), cfg.BarkDeviceKey.Reveal()} {
		if v != "" {
			set++
		}
	}
	switch set {
	case 0:
		return nil, nil //nolint:nilnil // no channel is a valid configuration
	case 4:
		return &bark.Client{BaseURL: cfg.BarkURL, User: cfg.BarkUser, Password: cfg.BarkPassword.Reveal(), DeviceKey: cfg.BarkDeviceKey.Reveal()}, nil
	}
	return nil, errBarkHalfSet
}

// kicker lets the bus, built first, wake the worker, built after it.
type kicker struct{ w atomic.Pointer[nudge.Worker] }

func (k *kicker) kick() {
	if w := k.w.Load(); w != nil {
		w.Kick()
	}
}

// startWorker runs the nudge worker until ctx ends and returns a function that waits for it. With
// no channel there is no worker, and the owner is told so. LoadConfig has already refused a
// level or severity the worker would not run with, and the worker refuses to tick on one.
func (a admin) startWorker(ctx context.Context, cfg Config, ch notify.Channel, b *bus.Bus, s bus.Store, k *kicker, logTo io.Writer) (wait func()) {
	if ch == nil {
		a.say("serve: YH_BARK_URL is not set: no push will be sent")
		return func() {}
	}
	w := nudge.NewWorker(b, s, ch, nudge.Config{Window: cfg.NudgeWindow, MinSeverity: bus.Severity(cfg.NudgeMinSeverity), Level: notify.Level(cfg.NudgeLevel),
		Log: slog.New(slog.NewTextHandler(logTo, nil))}, now)
	k.w.Store(w)
	var wg sync.WaitGroup
	wg.Go(func() { w.Run(ctx) })
	return wg.Wait
}
