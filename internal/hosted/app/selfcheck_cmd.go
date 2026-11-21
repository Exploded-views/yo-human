package app

import (
	"context"
	"errors"
	"io"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

var errSelfcheckNotBuilt = errors.New("selfcheck is not built yet")

// runSelfcheck is a stub until its tests are red.
func runSelfcheck(_ []string, _, _ io.Writer) int { return ExitInternal }

func selfcheckIn(_ string) error { return errSelfcheckNotBuilt }

func newSelfcheckBus(s bus.Store) *bus.Bus { return bus.New(s, now, bus.Limits{}) }

func exerciseBus(_ context.Context, _ *bus.Bus) error { return errSelfcheckNotBuilt }
