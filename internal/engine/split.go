package engine

import (
	"cmp"
	"slices"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

type SnapMode string

const (
	SnapThreeColumns SnapMode = "3col"
	SnapHalf         SnapMode = "50-50"
	SnapColumns      SnapMode = "columns"
)

func SnapPlan(profile *config.Profile, world World, focusedSpace wm.SpaceID, mode SnapMode) []Op {
	windows := make([]wm.Window, 0)
	for _, window := range world.Windows {
		if window.SpaceID == focusedSpace && !window.Floating && !window.Minimized {
			windows = append(windows, window)
		}
	}
	slices.SortFunc(windows, func(a, b wm.Window) int { return cmp.Compare(a.Frame.X, b.Frame.X) })
	if len(windows) == 0 {
		return []Op{}
	}
	if mode != SnapThreeColumns && mode != SnapHalf {
		return []Op{BalanceSpaceOp{Space: focusedSpace}}
	}
	name, _ := displayOfSpace(profile, world, focusedSpace)
	count := 2
	if mode == SnapThreeColumns {
		count = 3
	}
	weights := WeightsFor(profile, config.LayoutColumns, count, name, nil)
	tracks := make([][]int, 0, count)
	if mode == SnapThreeColumns {
		for _, window := range windows[:min(3, len(windows))] {
			tracks = append(tracks, []int{window.ID})
		}
		if len(windows) > 3 {
			tracks[len(tracks)-1] = append(tracks[len(tracks)-1], windowIDs(windows[3:])...)
		}
	} else {
		half := (len(windows) + 1) / 2
		tracks = append(tracks, windowIDs(windows[:half]))
		if half < len(windows) {
			tracks = append(tracks, windowIDs(windows[half:]))
		}
	}
	selected := make([]float64, 0, len(tracks))
	for i := range tracks {
		if i < len(weights) {
			selected = append(selected, weights[i])
		}
	}
	return []Op{RealizeLayoutOp{Space: focusedSpace, Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: tracks, Weights: selected}}}
}

func windowIDs(windows []wm.Window) []int {
	ids := make([]int, len(windows))
	for i, window := range windows {
		ids[i] = window.ID
	}
	return ids
}
