// Package configtest provides the frozen public layout fixture for contract tests.
package configtest

import "github.com/mattwilkinsonn/tessera/internal/config"

const (
	arcSlot         = "arc"
	ghosttyWaveSlot = "ghostty-wave"
	ghosttyMBPSlot  = "ghostty-mbp"
	vscodeSlot      = "vscode"
	akiflowSlot     = "akiflow"
	obsidianSlot    = "obsidian"
	linearSlot      = "linear"
	spotifySlot     = "spotify"
	discordSlot     = "discord"
	qalculateSlot   = "qalculate"
)

func Profile() *config.Profile {
	return &config.Profile{
		Displays: config.Displays{
			G9:     config.DisplaySpec{Width: 5120},
			AW:     config.DisplaySpec{Width: 3440},
			Laptop: config.DisplaySpec{Width: 1728},
		},
		Windows: map[string]config.WindowSpec{
			arcSlot:         {App: "Arc"},
			ghosttyWaveSlot: {App: "Ghostty", Title: "pc"},
			ghosttyMBPSlot:  {App: "Ghostty", Title: "mbp"},
			vscodeSlot:      {App: "Code"},
			akiflowSlot:     {App: "Akiflow"},
			obsidianSlot:    {App: "Obsidian"},
			linearSlot:      {App: "Linear"},
			spotifySlot:     {App: "Spotify"},
			discordSlot:     {App: "Discord"},
			qalculateSlot:   {App: "Qalculate"},
		},
		Desk: []config.DeskLayout{
			{
				Display: config.DisplayG9,
				Label:   "main",
				Kind:    config.LayoutColumns,
				Tracks:  [][]string{{arcSlot, obsidianSlot}, {ghosttyWaveSlot}, {ghosttyMBPSlot, vscodeSlot}},
			},
			{
				Display: config.DisplayAW,
				Label:   "plan",
				Kind:    config.LayoutColumns,
				Tracks:  [][]string{{linearSlot, arcSlot}, {arcSlot, akiflowSlot}},
			},
			{
				Display: config.DisplayLaptop,
				Label:   "laptop",
				Tracks:  [][]string{{arcSlot, spotifySlot, discordSlot, qalculateSlot}},
				Kind:    config.LayoutStack,
			},
		},
		Weights: &config.WeightDefaults{Columns: map[int][]float64{3: {3, 4, 3}}},
		DeskSlots: []config.DeskSlot{
			{Name: ghosttyWaveSlot},
			{Name: arcSlot, OnDisplay: config.DisplayG9},
			{Name: ghosttyMBPSlot},
			{Name: arcSlot, OnDisplay: config.DisplayAW},
			{Name: arcSlot, OnDisplay: config.DisplayAW},
			{Name: linearSlot},
			{Name: obsidianSlot},
			{Name: akiflowSlot},
			{Name: vscodeSlot},
		},
		LaptopPinned:    []string{arcSlot, ghosttyWaveSlot, arcSlot, ghosttyMBPSlot, linearSlot, obsidianSlot, arcSlot, akiflowSlot, arcSlot},
		LaptopStackApps: []string{"Messages", "System Settings", "Slack", "Discord", "1Password", "Activity Monitor"},
	}
}
