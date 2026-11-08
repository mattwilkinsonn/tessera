package commands

import (
	"testing"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
)

func TestArrivalHoldsAtStackCap(t *testing.T) {
	seed := fake.Seed{Spaces: []fake.SeedSpace{{DisplayIdx: 1, Layout: wm.LayoutStack}}}
	frame := wm.Frame{W: 1000, H: 800}
	for id := 1; id <= 24; id++ {
		seed.Windows = append(seed.Windows, fake.SeedWindow{ID: id, App: "Zen", SpaceIndex: 1, Frame: &frame})
	}
	seed.Windows = append(seed.Windows, fake.SeedWindow{ID: 25, App: "Zen", SpaceIndex: 1, Floating: true})
	driver := fake.New(seed)
	arrival, _, err := driver.QueryWindow(t.Context(), 25)
	if err != nil {
		t.Fatal(err)
	}
	placed, err := placeArrivalWindowOutcome(t.Context(), driver, arrival, arrivalTarget{space: "1", display: config.DisplayLaptop}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if placed {
		t.Fatal("placed arrival into full stack")
	}
	arrival, _, err = driver.QueryWindow(t.Context(), 25)
	if err != nil {
		t.Fatal(err)
	}
	if !arrival.Floating {
		t.Fatal("arrival did not remain floating")
	}
}
