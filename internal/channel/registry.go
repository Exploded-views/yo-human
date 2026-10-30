package channel

// Instance is one named, configured use of a channel type.
type Instance struct {
	Name, Type string
	Settings   map[string]any
	Secrets    map[string]string
}

// Type describes a channel type and builds its instances.
type Type struct {
	Name           string
	Capabilities   Capabilities
	SecretFields   []string
	SettingsSchema []byte // JSON Schema for an instance's settings
	New            func(Instance) (Channel, error)
}

// Registry maps a type name to its Type.
type Registry map[string]Type
