package engine

import (
	"slices"
	"strings"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

func MatchesSpec(spec config.WindowSpec, app, title string) bool {
	return matchesSpec(spec, app, title)
}

func SlugForWindow(profile *config.Profile, app, title string) string {
	keys := make([]string, 0, len(profile.Windows))
	for key := range profile.Windows {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if spec, ok := profile.Windows[key]; ok && matchesSpec(spec, app, title) {
			return key
		}
	}
	var slug strings.Builder
	dash := false
	for _, r := range strings.ToLower(app) {
		isASCIIAlpha := r >= 'a' && r <= 'z'
		isASCIIDigit := r >= '0' && r <= '9'
		if isASCIIAlpha || isASCIIDigit {
			slug.WriteRune(r)
			dash = false
			continue
		}
		if slug.Len() > 0 && !dash {
			slug.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(slug.String(), "-")
}

func ResolveDisplay(profile *config.Profile, name config.DisplayName, displays []wm.Display) (int, bool) {
	spec, ok := displaySpec(profile, name)
	if !ok {
		return 0, false
	}
	for _, display := range displays {
		if display.Frame.W == float64(spec.Width) {
			return display.Idx, true
		}
	}
	return 0, false
}

func displaySpec(profile *config.Profile, name config.DisplayName) (config.DisplaySpec, bool) {
	switch name {
	case config.DisplayG9:
		return profile.Displays.G9, true
	case config.DisplayAW:
		return profile.Displays.AW, true
	case config.DisplayLaptop:
		return profile.Displays.Laptop, true
	default:
		return config.DisplaySpec{}, false
	}
}

func ResolveDesk(profile *config.Profile, displays []wm.Display) []config.DeskLayout {
	if len(profile.Topologies) == 0 {
		return profile.Desk
	}
	present := make(map[config.DisplayName]struct{}, len(displays))
	for _, display := range displays {
		name, ok := displayNameForWidth(profile, display.Frame.W)
		if !ok {
			return profile.Desk
		}
		if _, duplicate := present[name]; duplicate {
			return profile.Desk
		}
		present[name] = struct{}{}
	}
	for _, topology := range profile.Topologies {
		declared := make(map[config.DisplayName]struct{}, len(topology.Displays))
		for _, name := range topology.Displays {
			declared[name] = struct{}{}
		}
		if len(declared) != len(present) {
			continue
		}
		match := true
		for name := range declared {
			if _, exists := present[name]; !exists {
				match = false
				break
			}
		}
		if match {
			return topology.Desk
		}
	}
	return profile.Desk
}

func WeightsFor(profile *config.Profile, kind config.LayoutKind, count int, display config.DisplayName, override []float64) []float64 {
	if override != nil {
		return slices.Clone(override)
	}
	if spec, ok := displaySpec(profile, display); ok && spec.Weights != nil {
		if weights := weightsByKind(spec.Weights, kind)[count]; weights != nil {
			return slices.Clone(weights)
		}
	}
	if profile.Weights != nil {
		if weights := weightsByKind(profile.Weights, kind)[count]; weights != nil {
			return slices.Clone(weights)
		}
	}
	weights := make([]float64, count)
	for i := range weights {
		weights[i] = 1
	}
	return weights
}

func weightsByKind(defaults *config.WeightDefaults, kind config.LayoutKind) map[int][]float64 {
	if kind == config.LayoutRows {
		return defaults.Rows
	}
	return defaults.Columns
}

func displayOfSpace(profile *config.Profile, world World, id wm.SpaceID) (config.DisplayName, bool) {
	for _, space := range world.Spaces {
		if space.ID != id {
			continue
		}
		for _, display := range world.Displays {
			if display.Idx == space.DisplayIdx {
				return displayNameForWidth(profile, display.Frame.W)
			}
		}
	}
	return "", false
}
