package commands

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/engine"
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
					Windows: []fake.SeedWindow{{
						ID: 100, App: "Zen", Floating: true, SpaceIndex: 1,
						Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true,
					}},
				}
				if laptop {
					seed.Displays = append(seed.Displays, fake.SeedDisplay{Idx: 3, Frame: displayFrame(1728, 1117)})
					seed.Spaces = append(seed.Spaces, fake.SeedSpace{DisplayIdx: 3, Layout: wm.LayoutStack})
				}
				driver := &restoringBrowserDriver{Driver: fake.New(seed)}
				baseline, _, err := driver.QueryWindow(t.Context(), 100)
				if err != nil || !IsTileCandidate(baseline) {
					t.Fatalf("baseline must be tile-eligible: %+v err=%v", baseline, err)
				}
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
				if len(windows) != wantSlots+6 {
					t.Fatalf("windows = %d; want %d including both restored batches", len(windows), wantSlots+6)
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
			if !window.Floating || window.DisplayIdx != 1 || window.SpaceID != "1" {
				t.Fatal("pre-existing float was adopted or moved")
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

// The first adoption sees unavailable windows, or cannot focus one once.
// Later snapshots expose the same IDs as eligible without another launch.
type lateBrowserDriver struct {
	*restoringBrowserDriver
	lateEligibility bool
	focusFailed     bool
	eligibleAfter   int
	disturbOwner    bool
	disturbed       bool
	layoutCalls     int
	layoutDelay     time.Duration
}

func (d *lateBrowserDriver) QueryWindows(ctx context.Context) ([]wm.Window, error) {
	windows, err := d.restoringBrowserDriver.QueryWindows(ctx)
	if d.lateEligibility && d.polls <= d.eligibleAfter {
		for i := range windows {
			windows[i].CanMove = false
		}
	}
	return windows, err
}

func (d *lateBrowserDriver) FocusWindow(ctx context.Context, id int) (bool, error) {
	if !d.lateEligibility && !d.focusFailed {
		d.focusFailed = true
		return false, nil
	}
	return d.Driver.FocusWindow(ctx, id)
}

func (d *lateBrowserDriver) RealizeSpaceLayout(ctx context.Context, id wm.SpaceID, target wm.SpaceLayoutTarget) error {
	d.layoutCalls++
	if d.layoutDelay > 0 {
		select {
		case <-time.After(d.layoutDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := d.Driver.RealizeSpaceLayout(ctx, id, target); err != nil {
		return err
	}
	if d.disturbOwner && !d.disturbed && id == "1" && len(target.Tracks) > 0 {
		d.disturbed = true
		return d.Driver.ToggleFloat(ctx, target.Tracks[0][0])
	}
	return nil
}

func TestApplyReconcilesLateSpawnedWindows(t *testing.T) {
	for _, test := range []struct {
		name          string
		eligibleAfter int
		disturbOwner  bool
		layoutDelay   time.Duration
	}{
		{name: "adoption focus fails once"},
		{name: "eligible after adoption", eligibleAfter: 16},
		{name: "eligible after reconciliation snapshot", eligibleAfter: 18},
		{name: "eligible at overflow boundary", eligibleAfter: 19},
		{name: "owner floats during second plan", disturbOwner: true},
		{name: "slow staggered eligibility", eligibleAfter: 55, layoutDelay: 3 * time.Second},
		{name: "slow floated owner", disturbOwner: true, layoutDelay: 3 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				profile := testProfile(t)
				profile.Windows["arc"] = config.WindowSpec{
					App: "Zen", AppRE: regexp.MustCompile("Zen"), Spawn: []string{"open", "-na", "Zen"},
				}
				driver := &lateBrowserDriver{
					restoringBrowserDriver: &restoringBrowserDriver{Driver: fake.New(fake.Seed{
						Displays: []fake.SeedDisplay{
							{Idx: 1, Frame: displayFrame(5120, 2160)},
							{Idx: 2, Frame: displayFrame(3440, 1440)},
							{Idx: 3, Frame: displayFrame(1728, 1117)},
						},
						Spaces:   []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 2}, {DisplayIdx: 3, Layout: wm.LayoutStack}},
						SpawnApp: "Zen",
						Windows:  populatedNonBrowserSeed(test.layoutDelay > 0),
					})},
					lateEligibility: test.eligibleAfter > 0,
					eligibleAfter:   test.eligibleAfter,
					disturbOwner:    test.disturbOwner,
					layoutDelay:     test.layoutDelay,
				}
				env := commandEnv(t)
				if err := Apply(t.Context(), profile, driver, env); err != nil {
					t.Fatal(err)
				}
				// The profile includes absent non-spawnable apps. They must not
				// force repeated rebuilds once all four browser slots are filled.
				if driver.layoutCalls > 9 {
					t.Fatalf("realized %d layouts; want at most 9", driver.layoutCalls)
				}
				memory, err := effects.ReadSlots(env.Paths.WindowSlots)
				if err != nil {
					t.Fatal(err)
				}
				wantSlots := 4
				if test.layoutDelay > 0 {
					wantSlots = 13
				}
				if len(memory.Slots) != wantSlots {
					t.Fatalf("single Apply filled %d slots; want %d", len(memory.Slots), wantSlots)
				}
				assertReconciledOwners(t, profile, driver, env, memory)
			})
		})
	}
}

func populatedNonBrowserSeed(enabled bool) []fake.SeedWindow {
	windows := make([]fake.SeedWindow, 0)
	if !enabled {
		return windows
	}
	for i, item := range []struct {
		app, title string
		space      int
	}{
		{app: "Obsidian", space: 1}, {app: "Ghostty", title: "pc", space: 1},
		{app: "Ghostty", title: "mbp", space: 1}, {app: "Code", space: 1},
		{app: "Linear", space: 2}, {app: "Akiflow", space: 2},
		{app: "Spotify", space: 3}, {app: "Discord", space: 3}, {app: "Qalculate", space: 3},
	} {
		windows = append(windows, fake.SeedWindow{ID: 100 + i, App: item.app, Title: item.title, SpaceIndex: item.space,
			Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true})
	}
	return windows
}

func TestSpawnedDemandAcrossLayouts(t *testing.T) {
	profile := testProfile(t)
	profile.Desk = []config.DeskLayout{
		{Display: config.DisplayAW, Tracks: [][]string{{"arc"}}},
		{Display: config.DisplayAW, Tracks: [][]string{{"arc"}}},
	}
	displays := []wm.Display{{Idx: 2, Frame: *displayFrame(3440, 1440), SpaceIDs: []wm.SpaceID{"2"}}}
	filled := map[engine.SlotKey]int{"desk:aw/arc#1": 1}
	if spawnedDemandFilled(profile, []string{"arc"}, displays, filled) {
		t.Fatal("second layout's browser vacancy was ignored")
	}
	filled["desk:aw/arc#2"] = 2
	if !spawnedDemandFilled(profile, []string{"arc"}, displays, filled) {
		t.Fatal("complete browser demand was rejected")
	}
}

func TestPlacedDeskSlotsRejectsWrongSpace(t *testing.T) {
	profile := testProfile(t)
	key := engine.SlotKey("desk:g9/arc#1")
	world := engine.World{
		Displays: []wm.Display{{Idx: 1, Frame: *displayFrame(5120, 2160), SpaceIDs: []wm.SpaceID{"home", "other"}}},
		Windows:  []wm.Window{{ID: 1, App: "Arc", DisplayIdx: 1, SpaceID: "other"}},
	}
	if got := placedDeskSlots(profile, world, map[engine.SlotKey]int{key: 1}); len(got) != 0 {
		t.Fatalf("wrong-space owner recorded: %v", got)
	}
	world.Windows[0].SpaceID = "home"
	if got := placedDeskSlots(profile, world, map[engine.SlotKey]int{key: 1}); got[key] != 1 {
		t.Fatalf("home-space owner rejected: %v", got)
	}
}

func assertReconciledOwners(t *testing.T, profile *config.Profile, driver wm.Driver, env Env, memory engine.SlotMemory) {
	t.Helper()
	for key, ref := range memory.Slots {
		before, exists, err := driver.QueryWindow(t.Context(), ref.ID)
		if err != nil || !exists || before.Floating {
			t.Fatalf("slot owner %d is unavailable: exists=%t floating=%t err=%v", ref.ID, exists, before.Floating, err)
		}
		wantDisplay := 3
		if strings.HasPrefix(string(key), "desk:g9/") {
			wantDisplay = 1
		}
		if strings.HasPrefix(string(key), "desk:aw/") {
			wantDisplay = 2
		}
		if before.DisplayIdx != wantDisplay {
			t.Fatalf("slot %s owner %d on display %d; want %d", key, ref.ID, before.DisplayIdx, wantDisplay)
		}
		if err := HandleWindowCreated(t.Context(), profile, driver, env, ref.ID); err != nil {
			t.Fatal(err)
		}
		after, _, err := driver.QueryWindow(t.Context(), ref.ID)
		if err != nil || after.SpaceID != before.SpaceID || after.Floating {
			t.Fatalf("queued creation event moved slot owner %d: before=%+v after=%+v err=%v", ref.ID, before, after, err)
		}
	}
}

type rebuildReviewDriver struct {
	*fake.Driver
	lateTiled   bool
	mutated     bool
	failedOwner int
}

func (d *rebuildReviewDriver) FocusWindow(ctx context.Context, id int) (bool, error) {
	if d.failedOwner == id {
		return false, nil
	}
	return d.Driver.FocusWindow(ctx, id)
}

func (d *rebuildReviewDriver) RealizeSpaceLayout(ctx context.Context, id wm.SpaceID, target wm.SpaceLayoutTarget) error {
	if err := d.Driver.RealizeSpaceLayout(ctx, id, target); err != nil {
		return err
	}
	if d.mutated || len(target.Tracks) == 0 {
		return nil
	}
	d.mutated = true
	if d.lateTiled {
		return d.Driver.SpawnWindow(ctx, []string{"restored tiled browser"})
	}
	d.failedOwner = target.Tracks[0][0]
	return d.Driver.ToggleFloat(ctx, d.failedOwner)
}

func TestReconciliationRecordsValidatedSelections(t *testing.T) {
	for _, lateTiled := range []bool{true, false} {
		name := "failed repair is not recorded"
		if lateTiled {
			name = "late tiled selection is recorded"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				profile := testProfile(t)
				profile.Windows["arc"] = config.WindowSpec{App: "Zen", AppRE: regexp.MustCompile("Zen"), Spawn: []string{"open"}}
				profile.Desk = []config.DeskLayout{{Display: config.DisplayLaptop, Kind: config.LayoutStack, Tracks: [][]string{{"arc", "arc"}}}}
				driver := &rebuildReviewDriver{Driver: fake.New(fake.Seed{
					Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(1728, 1117)}},
					Spaces:   []fake.SeedSpace{{DisplayIdx: 1}}, SpawnApp: "Zen",
					Windows: []fake.SeedWindow{{ID: 1, App: "Zen", SpaceIndex: 1, Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true}},
				}), lateTiled: lateTiled}
				world, err := worldSnapshot(t.Context(), driver)
				if err != nil {
					t.Fatal(err)
				}
				world.Slots = engine.SlotMemory{Slots: make(map[engine.SlotKey]engine.WindowRef)}
				world, filled, err := reconcileSpawnedWindows(t.Context(), profile, driver, []string{"arc"}, nil, world, nil)
				if err != nil {
					t.Fatal(err)
				}
				want := 0
				if lateTiled {
					want = 2
				}
				if len(filled) != want || len(world.Slots.Slots) != want {
					t.Fatalf("filled=%v recorded=%v; want %d validated owners", filled, world.Slots.Slots, want)
				}
			})
		})
	}
}
