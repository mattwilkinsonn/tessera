package configtest

import "github.com/mattwilkinsonn/tessera/internal/config"

const (
	alphaSlot = "alpha"
	betaSlot  = "beta"
)

func Full() *config.Profile {
	return &config.Profile{
		Displays: config.Displays{
			G9:     config.DisplaySpec{Width: 1200, Weights: &config.WeightDefaults{Rows: map[int][]float64{2: {1, 1}}}},
			AW:     config.DisplaySpec{Width: 900, Weights: &config.WeightDefaults{Columns: map[int][]float64{2: {2, 3}}}},
			Laptop: config.DisplaySpec{Width: 700, Weights: &config.WeightDefaults{Columns: map[int][]float64{2: {1, 2}}}},
		},
		Windows: map[string]config.WindowSpec{
			alphaSlot: {App: "Alpha", Title: "first", TitleInvert: true, Spawn: []string{"open", alphaSlot}},
			betaSlot:  {App: "Beta", Title: "second", Spawn: []string{"open", betaSlot}},
		},
		Desk: []config.DeskLayout{
			{Display: config.DisplayG9, Label: "primary", Kind: config.LayoutColumns, Tracks: [][]string{{alphaSlot}, {betaSlot}}, Weights: []float64{2, 3}},
			{Display: config.DisplayLaptop, Label: "portable", Kind: config.LayoutStack, Tracks: [][]string{{alphaSlot, betaSlot}}},
		},
		Topologies: []config.Topology{{
			Name:     "dual",
			Displays: []config.DisplayName{config.DisplayG9, config.DisplayLaptop},
			Desk: []config.DeskLayout{
				{Display: config.DisplayG9, Label: "primary", Kind: config.LayoutRows, Tracks: [][]string{{alphaSlot}, {betaSlot}}},
				{Display: config.DisplayLaptop, Label: "portable", Kind: config.LayoutStack, Tracks: [][]string{{alphaSlot}}},
			},
		}},
		Weights: &config.WeightDefaults{
			Columns: map[int][]float64{2: {1, 1}},
			Rows:    map[int][]float64{2: {3, 2}},
		},
		DeskSlots:       []config.DeskSlot{{Name: alphaSlot, OnDisplay: config.DisplayG9}, {Name: betaSlot}},
		LaptopPinned:    []string{alphaSlot, betaSlot},
		LaptopStackApps: []string{"Background App"},
	}
}
