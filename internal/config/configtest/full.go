package configtest

import "github.com/mattwilkinsonn/tessera/internal/config"

func Full() *config.Profile {
	return &config.Profile{
		Displays: config.Displays{
			G9:     config.DisplaySpec{Width: 1200, Weights: &config.WeightDefaults{Rows: map[int][]float64{2: {1, 1}}}},
			AW:     config.DisplaySpec{Width: 900, Weights: &config.WeightDefaults{Columns: map[int][]float64{2: {2, 3}}}},
			Laptop: config.DisplaySpec{Width: 700, Weights: &config.WeightDefaults{Columns: map[int][]float64{2: {1, 2}}}},
		},
		Windows: map[string]config.WindowSpec{
			"alpha": {App: "Alpha", Title: "first", TitleInvert: true, Spawn: []string{"open", "alpha"}},
			"beta":  {App: "Beta", Title: "second", Spawn: []string{"open", "beta"}},
		},
		Desk: []config.DeskLayout{
			{Display: config.DisplayG9, Label: "primary", Kind: config.LayoutColumns, Tracks: [][]string{{"alpha"}, {"beta"}}, Weights: []float64{2, 3}},
			{Display: config.DisplayLaptop, Label: "portable", Kind: config.LayoutStack, Tracks: [][]string{{"alpha", "beta"}}},
		},
		Topologies: []config.Topology{{
			Name:     "dual",
			Displays: []config.DisplayName{config.DisplayG9, config.DisplayLaptop},
			Desk: []config.DeskLayout{
				{Display: config.DisplayG9, Label: "primary", Kind: config.LayoutRows, Tracks: [][]string{{"alpha"}, {"beta"}}},
				{Display: config.DisplayLaptop, Label: "portable", Kind: config.LayoutStack, Tracks: [][]string{{"alpha"}}},
			},
		}},
		Weights: &config.WeightDefaults{
			Columns: map[int][]float64{2: {1, 1}},
			Rows:    map[int][]float64{2: {3, 2}},
		},
		DeskSlots:       []config.DeskSlot{{Name: "alpha", OnDisplay: config.DisplayG9}, {Name: "beta"}},
		LaptopPinned:    []string{"alpha", "beta"},
		LaptopStackApps: []string{"Background App"},
	}
}
