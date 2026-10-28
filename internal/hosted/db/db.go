// Package db is hosted mode's SQLite store; it implements bus.Store.
package db

import (
	"context"
	"errors"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// ErrNotFound is returned when an update names a row that does not exist.
var ErrNotFound = errors.New("db: not found")

// Counts is what Check reports.
type Counts struct {
	Integrity                          string
	Items, Decisions, Events, Messages int
}

// DB is the store.
type DB struct{}

// Open is a stub.
func Open(string) (*DB, error) { return nil, errors.New("not implemented") }

// Tx is a stub.
func (*DB) Tx(context.Context, func(bus.Tx) error) error { return errors.New("not implemented") }

// Check is a stub.
func (*DB) Check(context.Context) (Counts, error) { return Counts{}, errors.New("not implemented") }

// Close is a stub.
func (*DB) Close() error { return nil }

// IsConflict is a stub.
func IsConflict(error) bool { return false }
