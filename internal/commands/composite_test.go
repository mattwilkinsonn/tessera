package commands

import (
	"context"
	"regexp"
	"testing"
	"testing/synctest"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
)

// A launch restores more windows than requested, then creates another batch
// after the spawn count has already been satisfied.
type restoringBrowserDriver struct {
	*fake.Driver
	polls int
}

func (d *restoringBrowserDriver) SpawnWindow(ctx context.Context, argv []string) error {
	if err := d.Driver.SpawnWindow(ctx, argv); err != nil {
		return err
	}
	windows, err := d.Driver.QueryWindows(ctx)
	if err != nil {
		return err
	}
	return d.Driver.ToggleFloat(ctx, windows[len(windows)-1].ID)
}

func (d *restoringBrowserDriver) QueryWindows(ctx context.Context) ([]wm.Window, error) {
	if len(d.SpawnCalls) > 0 {
		d.polls++
		if d.polls == 1 || d.polls == 5 {
			for range 3 {
				if err := d.SpawnWindow(ctx, []string{"restored session window"}); err != nil {
					return nil, err
				}
			}
		}
	}
	return d.Driver.QueryWindows(ctx)
}

func TestApplyRestoredBrowserWindows(t *testing.T) {
	for _, laptop := range []bool{true, false} {
		name := "external only"
		if laptop {
			name = "laptop present"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				profile := testProfile(t)
				profile.Windows["arc"] = config.WindowSpec{
					App: "Zen", AppRE: regexp.MustCompile("Zen"),
					Spawn: []string{"open", "-na", "Zen", "--args", "--new-window"},
				}
				seed := fake.Seed{
					Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}, {Idx: 2, Frame: displayFrame(3440, 1440)}},
					Spaces:   []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 2}},
					SpawnApp: "Zen",
					Windows:  []fake.SeedWindow{{ID: 100, App: "Zen", Floating: true, SpaceIndex: 1}},
				}
				if laptop {
					seed.Displays = append(seed.Displays, fake.SeedDisplay{Idx: 3, Frame: displayFrame(1728, 1117)})
					seed.Spaces = append(seed.Spaces, fake.SeedSpace{DisplayIdx: 3, Layout: wm.LayoutStack})
				}
				driver := &restoringBrowserDriver{Driver: fake.New(seed)}
				env := commandEnv(t)
				if err := Apply(t.Context(), profile, driver, env); err != nil {
					t.Fatal(err)
				}
				windows, err := driver.Driver.QueryWindows(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				memory, err := effects.ReadSlots(env.Paths.WindowSlots)
				if err != nil {
					t.Fatal(err)
				}
				wantSlots := 3
				if laptop {
					wantSlots++
				}
				if len(memory.Slots) != wantSlots {
					t.Fatalf("filled slots = %d; want %d", len(memory.Slots), wantSlots)
				}
				claimed := make(map[int]bool)
				for _, ref := range memory.Slots {
					claimed[ref.ID] = true
				}
				if len(windows) != wantSlots+7 {
					t.Fatalf("windows = %d; want %d including both restored batches", len(windows), wantSlots+7)
				}
				assertRestoredBrowsers(t, windows, claimed, laptop)
			})
		})
	}
}

func assertRestoredBrowsers(t *testing.T, windows []wm.Window, claimed map[int]bool, laptop bool) {
	t.Helper()
	wantDisplay := 2
	if laptop {
		wantDisplay = 3
	}
	claimedByDisplay := make(map[int]int)
	for _, window := range windows {
		if window.ID == 100 {
			if !window.Floating {
				t.Fatal("pre-existing float was adopted")
			}
			continue
		}
		if window.Floating {
			t.Errorf("restored window %d remains floating", window.ID)
		}
		if claimed[window.ID] {
			claimedByDisplay[window.DisplayIdx]++
		}
		if !claimed[window.ID] && window.DisplayIdx != wantDisplay {
			t.Errorf("extra window %d on display %d; want arrival target %d", window.ID, window.DisplayIdx, wantDisplay)
		}
	}
	if claimedByDisplay[1] != 1 || claimedByDisplay[2] != 2 {
		t.Errorf("claimed browser displays = %v; want G9=1, AW=2", claimedByDisplay)
	}
	if laptop && claimedByDisplay[3] != 1 {
		t.Errorf("claimed laptop browsers = %d; want 1", claimedByDisplay[3])
	}
}
