package db

import (
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func (x *txn) InsertOutbox(bus.OutboxRow) (int64, error)             { return 0, nil }
func (x *txn) UpdateOutbox(bus.OutboxRow) error                      { return nil }
func (x *txn) PendingOutbox(time.Time, int) ([]bus.OutboxRow, error) { return nil, nil }
func (x *txn) FailedSince(time.Time) ([]bus.OutboxRow, error)        { return nil, nil }
func (x *txn) AllOutbox() ([]bus.OutboxRow, error)                   { return nil, nil }
func (x *txn) LastPush(string) (time.Time, bool, error)              { return time.Time{}, false, nil }
func (x *txn) InsertPush(bus.Push) error                             { return nil }
func (x *txn) Pushes(string) ([]bus.Push, error)                     { return nil, nil }
func (x *txn) LastPageSent(string) (time.Time, bool, error)          { return time.Time{}, false, nil }
func (x *txn) NudgeState() (bus.NudgeState, error)                   { return bus.NudgeState{}, nil }
func (x *txn) PutNudgeState(bus.NudgeState) error                    { return nil }
func (x *txn) Deferred() ([]bus.Deferred, error)                     { return nil, nil }
func (x *txn) PutDeferred(bus.Deferred) error                        { return nil }
func (x *txn) DeleteDeferred(string) error                           { return nil }
func (x *txn) BrokenFlag() (bus.BrokenFlag, error)                   { return bus.BrokenFlag{}, nil }
func (x *txn) PutBrokenFlag(bus.BrokenFlag) error                    { return nil }
func (x *txn) DeliveryState(time.Time) (*bus.DeliveryState, error)   { return nil, nil }
