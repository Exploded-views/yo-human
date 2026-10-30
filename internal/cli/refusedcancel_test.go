package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/channeltest"
)

// refusesCancel is a fake whose cancellations fail with err.
type refusesCancel struct {
	*channeltest.Fake
	err error
}

func (f refusesCancel) Cancel(context.Context, channel.Receipt) error { return f.err }

// cancelsFailWith makes every phone the registry builds refuse to cancel, with err.
func cancelsFailWith(t *testing.T, fk fakeSet, err error) {
	t.Helper()
	registryHook = func(reg channel.Registry) {
		for name, typ := range reg {
			typ.New = func(in channel.Instance) (channel.Channel, error) {
				if in.Name == "phone" {
					return refusesCancel{fk["phone"], err}, nil
				}
				return fk[in.Name], nil
			}
			reg[name] = typ
		}
	}
}

const mayStillPage = "the human may still be paged"

func TestResolveAndStatusShowACancelTheProviderRefused(t *testing.T) {
	s, fk := scheduledPage(t)
	cancelsFailWith(t, fk, channel.Permanent(errors.New("ntfy answered HTTP 404: the scheduled send may already be gone, or this ntfy server cannot cancel scheduled messages (needs v2.16 or later); "+mayStillPage)))
	r := s.do("", "resolve", "--dedup-key", "disk")
	expect(t, r.code == ExitFailed && strings.HasPrefix(r.out, "RESULT resolved "), "resolve: %+v; want resolved, and exit 69 for the cancel", r)
	mustContain(t, "resolve stdout", r.out, "phone: dead (", mayStillPage)
	mustContain(t, "resolve stderr", r.err, "error: the scheduled send on phone could not be cancelled", mayStillPage, "yo-human status")
	mustNotContain(t, "resolve stderr", r.err, "unexpected error", "report this as a bug")
	st := s.do("", "status")
	mustContain(t, "status", st.out, "dead: 1", "kind=cancel", "needs v2.16 or later", mayStillPage)
	mustContain(t, "status --json", s.do("", "status", "--json").out, `"kind":"cancel"`, mayStillPage)
	expect(t, s.entries("queue") == 0, "queue %d; a refused cancel is not retried", s.entries("queue"))
}
