package engine

import (
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

func SpawnsNeeded(profile *config.Profile, names []string, windows []wm.Window) []string {
	owners := make([]int, len(windows))
	for i := range owners {
		owners[i] = -1
	}
	satisfied := make([]bool, len(names))
	var match func(int, []bool) bool
	match = func(slot int, seen []bool) bool {
		spec := profile.Windows[names[slot]]
		for j, window := range windows {
			if seen[j] || !MatchesSpec(spec, window.App, window.Title) {
				continue
			}
			seen[j] = true
			if owners[j] < 0 || match(owners[j], seen) {
				owners[j] = slot
				return true
			}
		}
		return false
	}
	// Earlier titled matches remain satisfied when generic slots seek augmenting paths.
	for _, titled := range []bool{true, false} {
		for i, name := range names {
			spec, exists := profile.Windows[name]
			if exists && (spec.TitleRE != nil) == titled {
				satisfied[i] = match(i, make([]bool, len(windows)))
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
