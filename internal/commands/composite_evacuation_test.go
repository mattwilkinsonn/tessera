package commands

import (
	"context"
	"fmt"
	"testing"

	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
)

type capacityMoveDriver struct{ *fake.Driver }

func (d *capacityMoveDriver) MoveWindowToSpace(ctx context.Context, id int, space wm.SpaceID) error {
	if _, err := wm.CanAddToSpace(ctx, d.Driver, space, id); err != nil {
		return err
	}
	return d.Driver.MoveWindowToSpace(ctx, id, space)
}

func TestApplyEvacuationOverflow(t *testing.T) {
	profile := testProfile(t)
	for i := range profile.Desk {
		profile.Desk[i].Tracks = nil
	}
	seed := fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}, {Idx: 2, Frame: displayFrame(3440, 1440)}, {Idx: 3, Frame: displayFrame(1728, 1117)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 2}, {DisplayIdx: 3, Layout: wm.LayoutStack}}}
	for id := 1; id <= 23; id++ {
		seed.Windows = append(seed.Windows, fake.SeedWindow{ID: id, App: fmt.Sprintf("park-%d", id), SpaceIndex: 3})
	}
	for id := 90; id < 93; id++ {
		seed.Windows = append(seed.Windows, fake.SeedWindow{ID: id, App: "overflow", SpaceIndex: 1})
	}
	driver := &capacityMoveDriver{Driver: fake.New(seed)}
	if err := Apply(t.Context(), profile, driver, commandEnv(t)); err != nil {
		t.Fatal(err)
	}
	windows, err := driver.QueryWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	moved, floated := 0, 0
	for _, window := range windows {
		if window.ID < 90 {
			continue
		}
		if window.SpaceID == "3" && !window.Floating {
			moved++
		}
		if window.SpaceID == "1" && window.Floating {
			floated++
		}
	}
	if moved != 1 || floated != 2 {
		t.Fatalf("moved %d, floated %d; want 1, 2", moved, floated)
	}
}
