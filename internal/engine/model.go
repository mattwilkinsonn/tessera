package engine

import (
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

type World struct {
	Windows  []wm.Window
	Spaces   []wm.Space
	Displays []wm.Display
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

func displayNameForWidth(profile *config.Profile, width float64) (config.DisplayName, bool) {
	for _, entry := range []struct {
		name  config.DisplayName
		width int
	}{
		{name: config.DisplayG9, width: profile.Displays.G9.Width},
		{name: config.DisplayAW, width: profile.Displays.AW.Width},
		{name: config.DisplayLaptop, width: profile.Displays.Laptop.Width},
	} {
		if float64(entry.width) == width {
			return entry.name, true
		}
	}
	return "", false
}
