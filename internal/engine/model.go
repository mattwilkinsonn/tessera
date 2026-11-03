package engine

import (
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

type World struct {
	Windows  []wm.Window
	Spaces   []wm.Space
	Displays []wm.Display
	Slots    SlotMemory
}

func matchesSpec(spec config.WindowSpec, app, title string) bool {
	if spec.AppRE == nil || !spec.AppRE.MatchString(app) {
		return false
	}
	if spec.Title == "" {
		return true
	}
	matched := spec.TitleRE != nil && spec.TitleRE.MatchString(title)
	if spec.TitleInvert {
		return !matched
	}
	return matched
}

func displayNameFor(profile *config.Profile, display wm.Display) (config.DisplayName, bool) {
	entries := [...]struct {
		name config.DisplayName
		spec config.DisplaySpec
	}{
		{name: config.DisplayG9, spec: profile.Displays.G9},
		{name: config.DisplayAW, spec: profile.Displays.AW},
		{name: config.DisplayLaptop, spec: profile.Displays.Laptop},
	}
	for _, entry := range entries {
		if entry.spec.DeviceID != "" && entry.spec.DeviceID == display.DeviceID {
			return entry.name, true
		}
	}
	for _, entry := range entries {
		if entry.spec.DeviceID == "" && float64(entry.spec.Width) == display.Frame.W {
			return entry.name, true
		}
	}
	return "", false
}
