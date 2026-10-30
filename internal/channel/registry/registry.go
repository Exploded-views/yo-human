// Package registry is the one place that names the channel implementations (constitution IV):
// every other package reaches a channel through the internal/channel interface, and depguard
// keeps them off the implementations.
package registry

import (
	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/bark"
	"github.com/Exploded-views/yo-human/internal/channel/logch"
	"github.com/Exploded-views/yo-human/internal/channel/ntfy"
	"github.com/Exploded-views/yo-human/internal/channel/pagerduty"
)

// New is every channel type the binary knows, by type name, in a map of its own. version is the
// user agent's version: the caller passes version.Version, which a channel may not import.
func New(version string) channel.Registry {
	types := []channel.Type{
		logch.Type,
		ntfy.NewType(version),
		bark.NewType(version),
		pagerduty.NewType(version),
	}
	reg := make(channel.Registry, len(types))
	for _, t := range types {
		reg[t.Name] = t
	}
	return reg
}
