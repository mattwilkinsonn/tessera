package engine

import (
	"slices"
	"strconv"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

type FlexRow struct {
	ID    int
	Label string
}

type FlexOrder struct {
	Order     []string
	ToPersist []string
}

func ReconcileFlexOrder(persisted, current []string) FlexOrder {
	toPersist := slices.Clone(persisted)
	for _, slug := range current {
		if !slices.Contains(toPersist, slug) {
			toPersist = append(toPersist, slug)
		}
	}
	present := make(map[string]struct{}, len(current))
	for _, slug := range current {
		present[slug] = struct{}{}
	}
	order := make([]string, 0, len(toPersist))
	for _, slug := range toPersist {
		if slug != "" {
			if _, ok := present[slug]; ok {
				order = append(order, slug)
			}
		}
	}
	return FlexOrder{Order: order, ToPersist: toPersist}
}

func LaptopFlexWindows(profile *config.Profile, windows []wm.Window, claimed map[int]struct{}) []FlexRow {
	occurrences := make(map[string]int)
	for _, name := range profile.LaptopPinned {
		occurrences[name]++
	}
	survivors := make([]wm.Window, 0)
	for _, window := range windows {
		if window.Minimized || window.Floating || slices.Contains(profile.LaptopStackApps, window.App) {
			continue
		}
		if _, ok := claimed[window.ID]; !ok {
			survivors = append(survivors, window)
		}
	}
	slices.SortFunc(survivors, func(a, b wm.Window) int { return a.ID - b.ID })
	rows := make([]FlexRow, 0, len(survivors))
	for _, window := range survivors {
		base := SlugForWindow(profile, window.App, window.Title)
		occurrences[base]++
		label := base
		if occurrences[base] > 1 {
			label += "-" + strconv.Itoa(occurrences[base])
		}
		rows = append(rows, FlexRow{ID: window.ID, Label: label})
	}
	return rows
}
