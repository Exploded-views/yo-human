package db

import (
	"errors"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// ErrStarterCap is returned when a third live desk starter is asked for.
var ErrStarterCap = errors.New("db: the desk starter cap is reached")

var errStub = errors.New("stub")

func (d *DB) NodeByHash(h string) (bus.Node, bool, error)       { return bus.Node{}, false, errStub }
func (d *DB) AccessByHash(h string) (bus.Access, bool, error)   { return bus.Access{}, false, errStub }
func (d *DB) DeskByHash(h string) (bus.Desk, bool, error)       { return bus.Desk{}, false, errStub }
func (d *DB) StarterByHash(h string) (bus.Starter, bool, error) { return bus.Starter{}, false, errStub }
func (d *DB) TouchNode(name string, at time.Time) error         { return errStub }

func (d *DB) InsertNode(name, swarm, shown, hash string, at time.Time) error { return errStub }
func (d *DB) InsertStarter(name, shown, hash string, at time.Time, maxLive int) error {
	return errStub
}
func (d *DB) InsertDesk(hash string, at time.Time) error     { return errStub }
func (d *DB) ListTokens() ([]bus.TokenRow, error)            { return nil, errStub }
func (d *DB) Revoke(name string, at time.Time) (bool, error) { return false, errStub }
func (d *DB) RevokeDesk(at time.Time) (int, int, error)      { return 0, 0, errStub }
func (d *DB) RevokeDuet(at time.Time) (int, error)           { return 0, errStub }
