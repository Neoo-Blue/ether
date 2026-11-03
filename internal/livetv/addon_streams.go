package livetv

import "strings"

// mergeStremioStreams refreshes add-on inputs while preserving their owner
// enablement and separately added inputs.
func mergeStremioStreams(fresh, previous []StreamSource) []StreamSource {
	old := make(map[string]StreamSource, len(previous))
	for _, input := range previous {
		old[input.ID] = input
	}
	out := make([]StreamSource, 0, len(fresh)+len(previous))
	for _, input := range fresh {
		if prior, ok := old[input.ID]; ok {
			input.Enabled = prior.Enabled
		}
		out = append(out, input)
	}
	for _, input := range previous {
		if !strings.HasPrefix(input.ID, "stremio-") {
			out = append(out, input)
		}
	}
	return out
}
