package engine

import (
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

func SpawnsNeeded(profile *config.Profile, names []string, windows []wm.Window) []string {
	consumed := make([]bool, len(windows))
	satisfied := make([]bool, len(names))
	// Specific slots claim first so generic slots cannot consume their matches.
	for _, titled := range []bool{true, false} {
		for i, name := range names {
			spec, exists := profile.Windows[name]
			if !exists || (spec.TitleRE != nil) != titled {
				continue
			}
			for j, window := range windows {
				if !consumed[j] && MatchesSpec(spec, window.App, window.Title) {
					consumed[j], satisfied[i] = true, true
					break
				}
			}
		}
	}
	needed := make([]string, 0)
	for i, name := range names {
		if spec, exists := profile.Windows[name]; exists && !satisfied[i] && len(spec.Spawn) > 0 {
			needed = append(needed, name)
		}
	}
	return needed
}
