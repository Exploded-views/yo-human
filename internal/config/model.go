// Package config loads the layered configuration: the system, user, project and explicit files,
// merged with the closest winning, project files only once the human has trusted them. It holds
// secret names, never secret values.
package config

import (
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// Config is the merged configuration with defaults applied.
type Config struct {
	Version     int
	Human       Human
	Deadline    time.Duration
	IncidentTTL time.Duration
	Channels    map[string]ChannelInstance
	Levels      []Level // quietest first
	QuietHours  QuietHours
	Shim        *Shim
	Merged      map[string]any       // the merged files as written, before defaults; a secret field holds its {secret: name}
	Origin      map[string]string    // setting path to the file it came from
	OriginLayer map[string]LayerKind // setting path to the kind of layer that file was found as
}

// LayerKind says how a config file was found, never inferred from its name.
type LayerKind int

const (
	LayerSystem LayerKind = iota + 1
	LayerUser
	LayerProject
	LayerExplicit        // --config or YO_HUMAN_CONFIG: a file in the user config directory that is the user's alone
	LayerExplicitOutside // --config or YO_HUMAN_CONFIG anywhere else: trusted by the human, and bound by the project rules
)

// Human is who the messages are for.
type Human struct{ Name string }

// ChannelInstance is a named use of a channel type. Settings holds every key except type; a secret
// field holds a {secret: name} reference, never a value.
type ChannelInstance struct {
	Type     string
	Settings map[string]any
}

// Level is one rung of the ladder an agent chooses from.
type Level struct {
	Name, UseWhen    string
	AskRequired      bool
	HoldInQuietHours bool
	Retry            Retry
	Reminder         time.Duration
	Send             []Send
}

// Send is one channel a level reaches, and how loudly.
type Send struct {
	Channel      string
	Interruption channel.Interruption
	Options      map[string]any
}

// Retry is what a queued result tells the agent to do.
type Retry struct {
	Every time.Duration
	Times int
}

// QuietHours says when levels that hold in quiet hours wait. A zero value has no windows.
type QuietHours struct {
	Location *time.Location
	Windows  []Window
}

// Window is a daily span in quiet hours; a span with From after To crosses midnight.
type Window struct {
	Days     []time.Weekday
	From, To clock
}

// clock is minutes since midnight.
type clock int

// Shim maps the old paging script's two modes onto levels.
type Shim struct {
	Default, Low ShimMode
	Agent        string
}

// ShimMode is the level and ask one mode of the shim uses.
type ShimMode struct{ Level, Ask string }
