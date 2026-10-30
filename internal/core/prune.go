package core

import (
	"time"

	"github.com/Exploded-views/yo-human/internal/store"
)

// pruneClosed removes, from incidents held under the lock, each closed record older than
// incident_ttl that no queued or in-flight entry still refers to. Nothing else ever removes one, and
// a resolve of a key never seen leaves one. A record an entry refers to is kept however old: a
// message waiting for its incident check needs it to be dropped rather than paged (admit), and an
// entry with no record is sent as it stands (outdated). If the entries cannot be listed, nothing is
// pruned: a record kept a while longer is harmless, one removed too soon could page.
func (p *Pipeline) pruneClosed(incs map[string]*store.Incident, now time.Time) {
	var old []string
	for id, inc := range incs {
		if !inc.Open && now.Sub(inc.Updated) > p.d.Config.IncidentTTL {
			old = append(old, id)
		}
	}
	if len(old) == 0 {
		return
	}
	inUse, err := p.d.Store.IncidentsInUse()
	for _, id := range old {
		if err == nil && !inUse[id] {
			delete(incs, id)
		}
	}
}
