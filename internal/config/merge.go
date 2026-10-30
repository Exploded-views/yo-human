package config

// merge folds layers, farthest first, into one mapping. Single settings come from the closest
// file that sets them; channels merge by instance name, a closer file replacing an instance whole;
// levels is replaced whole, because mixing rungs from two files would scramble the ladder. The
// second result maps each setting path to the file that supplied it, and the third to that layer's kind.
func merge(layers []layer) (map[string]any, map[string]string, map[string]LayerKind) {
	out := map[string]any{}
	origin := map[string]string{}
	kinds := map[string]LayerKind{}
	channels := map[string]any{}
	for _, l := range layers {
		for k, v := range l.data {
			if k != "channels" {
				out[k] = v
				origin[k] = l.path
				kinds[k] = l.kind
				continue
			}
			m, _ := v.(map[string]any) // the schema guarantees an object
			for name, inst := range m {
				channels[name] = inst
				origin["channels."+name] = l.path
				kinds["channels."+name] = l.kind
			}
		}
	}
	if len(channels) > 0 {
		out["channels"] = channels
	}
	return out, origin, kinds
}
