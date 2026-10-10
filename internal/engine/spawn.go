package engine

import (
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/samber/mo"
)

func SpawnsNeeded(profile *config.Profile, names []string, windows []wm.Window) []string {
	claims := NewClaimSet(profile)
	needed := make([]string, 0)
	for _, name := range names {
		spec, exists := profile.Windows[name]
		if exists && len(spec.Spawn) > 0 {
			if _, claimed := claims.Claim(windows, name, mo.None[int]()); !claimed {
				needed = append(needed, name)
			}
		}
	}
	return needed
}
