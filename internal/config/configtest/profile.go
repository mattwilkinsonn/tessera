// Package configtest provides the frozen public layout fixture for contract tests.
package configtest

import "github.com/mattwilkinsonn/tessera/internal/config"

func Profile() *config.Profile {
	return &config.Profile{
		Displays: config.Displays{
			G9:     config.DisplaySpec{Width: 5120},
			AW:     config.DisplaySpec{Width: 3440},
			Laptop: config.DisplaySpec{Width: 1728},
		},
		Windows: map[string]config.WindowSpec{
			"arc":          {App: "Arc"},
			"ghostty-wave": {App: "Ghostty", Title: "pc"},
			"ghostty-mbp":  {App: "Ghostty", Title: "mbp"},
			"vscode":       {App: "Code"},
			"akiflow":      {App: "Akiflow"},
			"obsidian":     {App: "Obsidian"},
			"linear":       {App: "Linear"},
			"spotify":      {App: "Spotify"},
			"discord":      {App: "Discord"},
			"qalculate":    {App: "Qalculate"},
		},
		Desk: []config.DeskLayout{
			{
				Display: config.DisplayG9,
				Label:   "main",
				Kind:    config.LayoutColumns,
				Tracks:  [][]string{{"arc", "obsidian"}, {"ghostty-wave"}, {"ghostty-mbp", "vscode"}},
			},
			{
				Display: config.DisplayAW,
				Label:   "plan",
				Kind:    config.LayoutColumns,
				Tracks:  [][]string{{"linear", "arc"}, {"arc", "akiflow"}},
			},
			{
				Display: config.DisplayLaptop,
				Label:   "laptop",
				Kind:    config.LayoutStack,
				Tracks:  [][]string{{"arc", "spotify", "discord", "qalculate"}},
			},
		},
		Weights: &config.WeightDefaults{Columns: map[int][]float64{3: {3, 4, 3}}},
		DeskSlots: []config.DeskSlot{
			{Name: "ghostty-wave"},
			{Name: "arc", OnDisplay: config.DisplayG9},
			{Name: "ghostty-mbp"},
			{Name: "arc", OnDisplay: config.DisplayAW},
			{Name: "arc", OnDisplay: config.DisplayAW},
			{Name: "linear"},
			{Name: "obsidian"},
			{Name: "akiflow"},
			{Name: "vscode"},
		},
		LaptopPinned:    []string{"arc", "ghostty-wave", "arc", "ghostty-mbp", "linear", "obsidian", "arc", "akiflow", "arc"},
		LaptopStackApps: []string{"Messages", "System Settings", "Slack", "Discord", "1Password", "Activity Monitor"},
	}
}
