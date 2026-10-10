package engine

import (
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

func ResolveSlot(profile *config.Profile, world World, number int) (int, bool) {
	if number < 1 || number > len(profile.DeskSlots) {
		return 0, false
	}
	slot := profile.DeskSlots[number-1]
	spec, exists := profile.Windows[slot.Name]
	if !exists {
		return 0, false
	}
	candidates := make([]wm.Window, 0)
	for _, window := range world.Windows {
		if !window.Minimized && matchesSpec(spec, window.App, window.Title) {
			candidates = append(candidates, window)
		}
	}
	if slot.OnDisplay != "" {
		if display, ok := ResolveDisplay(profile, slot.OnDisplay, world.Displays); ok {
			for _, window := range candidates {
				if window.DisplayIdx == display {
					return window.ID, true
				}
			}
		}
	}
	if len(candidates) == 0 {
		return 0, false
	}
	return candidates[0].ID, true
}
