package engine

import (
	"strings"

	"github.com/mattwilkinsonn/tessera/internal/wm"
)

func TeardownLabels(world World, prefix string) []wm.SpaceID {
	byID := make(map[wm.SpaceID]wm.Space, len(world.Spaces))
	for _, space := range world.Spaces {
		byID[space.ID] = space
	}
	ids := make([]wm.SpaceID, 0)
	for _, display := range world.Displays {
		for _, id := range display.SpaceIDs {
			space, exists := byID[id]
			if exists && strings.HasPrefix(space.Label, prefix) {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func StraySpaces(world World) []wm.SpaceID {
	windows := make(map[int]wm.Window, len(world.Windows))
	for _, window := range world.Windows {
		windows[window.ID] = window
	}
	spaces := make(map[wm.SpaceID]wm.Space, len(world.Spaces))
	for _, space := range world.Spaces {
		spaces[space.ID] = space
	}
	strays := make([]wm.SpaceID, 0)
	for _, display := range world.Displays {
		candidates := make([]wm.SpaceID, 0)
		for _, id := range display.SpaceIDs {
			space, exists := spaces[id]
			if !exists || space.Label != "" {
				continue
			}
			stray := true
			for _, windowID := range space.WindowIDs {
				window, found := windows[windowID]
				if !found || !window.Sticky {
					stray = false
					break
				}
			}
			if stray {
				candidates = append(candidates, id)
			}
		}
		if len(display.SpaceIDs) == 0 || len(candidates) == 0 {
			continue
		}
		if len(candidates) == len(display.SpaceIDs) {
			candidates = candidates[:len(candidates)-1]
		}
		strays = append(strays, candidates...)
	}
	return strays
}
