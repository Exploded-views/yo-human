package config

import "github.com/Exploded-views/yo-human/internal/problem"

// checkLogPaths refuses a log channel whose settings come from a project file. A trusted repo
// could otherwise point a log at a shell startup file and have agent text appended to it, and
// no path rule is safe against relative paths, symlinked directories and a moving working
// directory. The layer comes from how the file was found (OriginLayer), never from its name.
func checkLogPaths(c *Config) []*problem.Problem {
	var out []*problem.Problem
	for _, name := range sortedKeys(c.Channels) {
		ci := c.Channels[name]
		if kind := c.OriginLayer["channels."+name]; ci.Type != "log" || (kind != LayerProject && kind != LayerExplicitOutside) {
			continue
		}
		out = append(out, problem.Configf("channels.%s in %q is a log channel, and a project file may not define one (nor may a --config file outside the user config directory)",
			"log channels defined in the system or user config, or a --config file in the user config directory",
			"define log channels in your user config ($XDG_CONFIG_HOME/yo-human/config.yaml)", name, c.Origin["channels."+name]))
	}
	return out
}
