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

func TestHandleWindowCreatedHoldsFullLaptopSpace(t *testing.T) {
	seed := fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}, {Idx: 2, Frame: displayFrame(1728, 1117)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 2, Layout: wm.LayoutStack}}}
	for id := 1; id <= 24; id++ {
		seed.Windows = append(seed.Windows, deskSlotWindow(id, "Zen", 2, float64(id*10)))
	}
	arrival := deskSlotWindow(90, "Zen", 1, 0)
	arrival.Floating = true
	seed.Windows = append(seed.Windows, arrival)
	driver := fake.New(seed)
	if err := HandleWindowCreated(t.Context(), testProfile(t), driver, commandEnv(t), 90); err != nil {
		t.Fatal(err)
	}
	window, exists, err := driver.QueryWindow(t.Context(), 90)
	if err != nil || !exists {
		t.Fatalf("arrival: %v %v", exists, err)
	}
	if !window.Floating || window.SpaceID != "1" {
		t.Fatalf("rejected arrival moved or tiled: %+v", window)
	}
	allowed, err := wm.CanStack(t.Context(), driver, 1, 90)
	if err != nil || allowed {
		t.Fatalf("unequal-frame stack allowed = %v, error %v", allowed, err)
	}
}
