package cli

import "fmt"

// levelsUsage is the valid text of every levels usage error.
const levelsUsage = `yo-human levels [--sends]`

// cmdLevels prints the configured levels, quietest first, with use_when: how an agent learns the
// human's ladder. Like every command but secret, config and version, it first sends what is due;
// use_when comes from a file, so both forms carry it as one plain line.
func cmdLevels(e *env, args []string) int {
	fs := newFlags("levels")
	_ = fs.Bool("sends", false, "")
	if err := parseFlags(fs, "levels", levelsUsage, args); err != nil {
		return e.fail(err)
	}
	cfg, st, err := e.loadWithStore()
	if err != nil {
		return e.fail(err)
	}
	e.flushFirst(cfg, st)
	type level struct {
		Name       string `json:"name"`
		UseWhen    string `json:"use_when"`
		Ask        string `json:"ask"`
		QuietHours string `json:"quiet_hours"`
	}
	levels := make([]level, len(cfg.Levels))
	lines := make([]string, len(cfg.Levels))
	for i, l := range cfg.Levels {
		levels[i] = level{Name: l.Name, UseWhen: oneLine(l.UseWhen), Ask: "optional", QuietHours: "send"}
		if l.AskRequired {
			levels[i].Ask = "required"
		}
		if l.HoldInQuietHours {
			levels[i].QuietHours = "hold"
		}
		lines[i] = fmt.Sprintf("%s (ask %s, quiet hours %s): %s", l.Name, levels[i].Ask, levels[i].QuietHours, levels[i].UseWhen)
	}
	e.ok(lines, map[string]any{"levels": levels})
	return ExitOK
}
