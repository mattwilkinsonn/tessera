package engine

import (
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/samber/mo"
)

// NewClaimSet creates a per-plan set of distinct window claims.
func NewClaimSet(profile *config.Profile, memory SlotMemory, reserved map[int]SlotKey) *ClaimSet {
	return &ClaimSet{
		profile: profile, claimed: make(map[int]struct{}), memory: memory,
		reserved: reserved,
	}
}

func (claims *ClaimSet) Claim(windows []wm.Window, name string, preferDisplay mo.Option[int]) (int, bool) {
	return claims.claim(windows, "", name, preferDisplay)
}

// ClaimSlot tries memory first, then avoids windows held for other slots.
func (claims *ClaimSet) ClaimSlot(windows []wm.Window, key SlotKey, name string, preferDisplay mo.Option[int]) (int, bool) {
	if ref, exists := claims.memory.Slots[key]; exists {
		if spec, known := claims.profile.Windows[name]; known {
			for _, window := range windows {
				if window.ID != ref.ID || window.App != ref.App || window.Minimized || window.Floating || !matchesSpec(spec, window.App, window.Title) {
					continue
				}
				if _, claimed := claims.claimed[window.ID]; !claimed && claims.reserved[window.ID] == key {
					claims.claimed[window.ID] = struct{}{}
					return window.ID, true
				}
			}
		}
	}
	return claims.claim(windows, key, name, preferDisplay)
}

func (claims *ClaimSet) claim(windows []wm.Window, key SlotKey, name string, preferDisplay mo.Option[int]) (int, bool) {
	spec, exists := claims.profile.Windows[name]
	if !exists {
		return 0, false
	}
	candidates := make([]wm.Window, 0)
	for _, window := range windows {
		if !window.Minimized && !window.Floating && matchesSpec(spec, window.App, window.Title) {
			candidates = append(candidates, window)
		}
	}
	if display, preferred := preferDisplay.Get(); preferred {
		for _, window := range candidates {
			if window.DisplayIdx == display {
				if claims.canClaim(window.ID, key) {
					claims.claimed[window.ID] = struct{}{}
					return window.ID, true
				}
			}
		}
	}
	for _, window := range candidates {
		if claims.canClaim(window.ID, key) {
			claims.claimed[window.ID] = struct{}{}
			return window.ID, true
		}
	}
	if len(spec.Spawn) == 0 {
		return 0, false
	}
	fallback := make([]wm.Window, 0)
	for _, window := range windows {
		if window.Minimized || window.Floating || !matchesSpec(config.WindowSpec{App: spec.App, AppRE: spec.AppRE}, window.App, window.Title) {
			continue
		}
		if !claims.canClaim(window.ID, key) {
			continue
		}
		if _, classified := claims.profile.Windows[SlugForWindow(claims.profile, window.App, window.Title)]; classified {
			continue
		}
		fallback = append(fallback, window)
	}
	if display, preferred := preferDisplay.Get(); preferred {
		for _, window := range fallback {
			if window.DisplayIdx == display {
				if claims.canClaim(window.ID, key) {
					claims.claimed[window.ID] = struct{}{}
					return window.ID, true
				}
			}
		}
	}
	for _, window := range fallback {
		if claims.canClaim(window.ID, key) {
			claims.claimed[window.ID] = struct{}{}
			return window.ID, true
		}
	}
	return 0, false
}

func (claims *ClaimSet) canClaim(id int, key SlotKey) bool {
	if _, claimed := claims.claimed[id]; claimed {
		return false
	}
	reserved, exists := claims.reserved[id]
	return !exists || (key != "" && reserved == key)
}

func (claims *ClaimSet) ClaimMany(windows []wm.Window, names []string, preferDisplay mo.Option[int]) []int {
	claimed := make([]int, 0, len(names))
	for _, name := range names {
		if id, ok := claims.Claim(windows, name, preferDisplay); ok {
			claimed = append(claimed, id)
		}
	}
	return claimed
}

func (claims *ClaimSet) Reset() {
	clear(claims.claimed)
}
