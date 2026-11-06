package commands

import (
	"context"
	"errors"
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/config/configtest"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func testProfile(t *testing.T) *config.Profile {
	t.Helper()
	profile := configtest.Profile()
	if err := config.Validate(profile); err != nil {
		t.Fatal(err)
	}
	return profile
}

func commandEnv(t *testing.T) Env {
	t.Helper()
	root := t.TempDir()
	return Env{
		Paths: effects.Paths{
			ApplyLock:    filepath.Join(root, "apply.lock"),
			LaptopLock:   filepath.Join(root, "laptop.lock"),
			SignalGuard:  filepath.Join(root, "guard"),
			FlexOrder:    filepath.Join(root, "flex-order"),
			WindowSlots:  filepath.Join(root, "window-slots.json"),
			DaemonSocket: filepath.Join(root, "daemon.sock"),
		},
		Nudge:  func(context.Context, string) error { return nil },
		Stderr: io.Discard,
	}
}

func displayFrame(width, height float64) *wm.Frame {
	return &wm.Frame{W: width, H: height}
}

func deskWorld() *fake.Driver {
	return fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{
			{Idx: 1, Frame: displayFrame(5120, 2160)},
			{Idx: 2, Frame: displayFrame(3440, 1440)},
			{Idx: 3, Frame: displayFrame(1728, 1117)},
		},
		Spaces: []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 2}, {DisplayIdx: 3}},
		Windows: []fake.SeedWindow{
			{ID: 1, App: "Arc", Title: "one", SpaceIndex: 1},
			{ID: 2, App: "Obsidian", SpaceIndex: 1},
			{ID: 3, App: "Ghostty", Title: "pc", SpaceIndex: 1},
			{ID: 4, App: "Ghostty", Title: "mbp", SpaceIndex: 1},
			{ID: 5, App: "Code", SpaceIndex: 1},
			{ID: 6, App: "Linear", SpaceIndex: 2},
			{ID: 7, App: "Arc", Title: "two", SpaceIndex: 2},
			{ID: 8, App: "Akiflow", SpaceIndex: 2},
		},
	})
}

func TestFocusSlot(t *testing.T) {
	t.Run("focuses the window the slot resolves to", func(t *testing.T) {
		driver := fake.New(fake.Seed{
			Displays: []fake.SeedDisplay{{Idx: 1}},
			Spaces:   []fake.SeedSpace{{DisplayIdx: 1}},
			Windows: []fake.SeedWindow{
				{ID: 10, App: "Ghostty", Title: "pc wave", SpaceIndex: 1},
				{ID: 11, App: "Arc", SpaceIndex: 1},
			},
		})
		if err := FocusSlot(t.Context(), testProfile(t), driver, 1); err != nil {
			t.Fatal(err)
		}
		focused, ok, err := driver.QueryFocusedWindow(t.Context())
		if err != nil || !ok || focused.ID != 10 {
			t.Fatalf("focused window = %+v, %t, %v; want id 10", focused, ok, err)
		}
	})
	t.Run("no-op when the slot resolves to nothing (no match)", func(t *testing.T) {
		driver := fake.New(fake.Seed{
			Displays: []fake.SeedDisplay{{Idx: 1}},
			Spaces:   []fake.SeedSpace{{DisplayIdx: 1}},
			Windows:  []fake.SeedWindow{{ID: 11, App: "Arc", SpaceIndex: 1}},
		})
		if err := FocusSlot(t.Context(), testProfile(t), driver, 1); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := driver.QueryFocusedWindow(t.Context()); err != nil || ok {
			t.Fatalf("focused = %t, %v; want none", ok, err)
		}
	})
}

func TestSnap(t *testing.T) {
	t.Run("50-50 splits the focused space's leaves into two stacked columns", func(t *testing.T) {
		driver := fake.New(fake.Seed{
			Displays: []fake.SeedDisplay{{Idx: 1}},
			Spaces:   []fake.SeedSpace{{DisplayIdx: 1}},
			Windows: []fake.SeedWindow{
				{ID: 1, App: "A", SpaceIndex: 1, Frame: &wm.Frame{W: 10, H: 10}},
				{ID: 2, App: "B", SpaceIndex: 1, Frame: &wm.Frame{X: 20, W: 10, H: 10}},
			},
		})
		_, _ = driver.FocusWindow(t.Context(), 1)
		if err := Snap(t.Context(), testProfile(t), driver, engine.SnapHalf); err != nil {
			t.Fatal(err)
		}
		spaces, err := driver.QuerySpaces(t.Context())
		if err != nil || spaces[0].Layout != wm.LayoutBSP || !slices.Equal(spaces[0].WindowIDs, []int{1, 2}) {
			t.Fatalf("space = %+v, %v; want BSP with both windows", spaces, err)
		}
	})
	t.Run("applies configured column weights to the seeded display frame", func(t *testing.T) {
		profile := testProfile(t)
		profile.Displays.G9.Width = 1000
		profile.Displays.G9.Weights = &config.WeightDefaults{Columns: map[int][]float64{2: {3, 2}}}
		driver := fake.New(fake.Seed{
			Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(1000, 800)}},
			Spaces:   []fake.SeedSpace{{DisplayIdx: 1}},
			Windows:  []fake.SeedWindow{{ID: 1, App: "A", SpaceIndex: 1}, {ID: 2, App: "A", SpaceIndex: 1}},
		})
		_, _ = driver.FocusWindow(t.Context(), 1)
		if err := Snap(t.Context(), profile, driver, engine.SnapHalf); err != nil {
			t.Fatal(err)
		}
		windows, err := driver.QueryWindows(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if windows[0].Frame.W != 600 || windows[1].Frame.W != 400 {
			t.Fatalf("window widths = %v, %v; want 600, 400", windows[0].Frame.W, windows[1].Frame.W)
		}
	})
	t.Run("no-op when no space is focused", func(t *testing.T) {
		driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}}, Windows: []fake.SeedWindow{{ID: 1, App: "A", SpaceIndex: 1}}})
		if err := Snap(t.Context(), testProfile(t), driver, engine.SnapColumns); err != nil {
			t.Fatal(err)
		}
		spaces, err := driver.QuerySpaces(t.Context())
		if err != nil || spaces[0].Layout != wm.LayoutBSP {
			t.Fatalf("spaces = %+v, %v; want unchanged BSP", spaces, err)
		}
	})
}

type navigationSpy struct {
	*fake.Driver
	focusCalls  []wm.FocusSelector
	resizeCalls []string
	primaryOK   bool
}

func (d *navigationSpy) FocusWindowDir(ctx context.Context, selector wm.FocusSelector) (bool, error) {
	d.focusCalls = append(d.focusCalls, selector)
	if len(d.focusCalls) == 1 {
		return d.primaryOK, ctx.Err()
	}
	return true, ctx.Err()
}

func (d *navigationSpy) ResizeWindow(ctx context.Context, edge wm.ResizeEdge, dx, _ int) (bool, error) {
	d.resizeCalls = append(d.resizeCalls, string(edge)+":"+strconv.Itoa(dx))
	if len(d.resizeCalls) == 1 {
		return d.primaryOK, ctx.Err()
	}
	return true, ctx.Err()
}

func TestStackCycle(t *testing.T) {
	t.Run("next: primary stack.next only when it succeeds", func(t *testing.T) {
		driver := &navigationSpy{Driver: fake.New(fake.Seed{}), primaryOK: true}
		if err := StackCycle(t.Context(), driver, CycleNext); err != nil || !slices.Equal(driver.focusCalls, []wm.FocusSelector{wm.FocusStackNext}) {
			t.Fatalf("calls = %v, %v; want only stack.next", driver.focusCalls, err)
		}
	})
	t.Run("next: falls back to stack.first when stack.next fails", func(t *testing.T) {
		driver := &navigationSpy{Driver: fake.New(fake.Seed{}), primaryOK: false}
		if err := StackCycle(t.Context(), driver, CycleNext); err != nil || !slices.Equal(driver.focusCalls, []wm.FocusSelector{wm.FocusStackNext, wm.FocusStackFirst}) {
			t.Fatalf("calls = %v, %v; want stack.next, stack.first", driver.focusCalls, err)
		}
	})
	t.Run("prev: falls back to stack.last when stack.prev fails", func(t *testing.T) {
		driver := &navigationSpy{Driver: fake.New(fake.Seed{}), primaryOK: false}
		if err := StackCycle(t.Context(), driver, CyclePrev); err != nil || !slices.Equal(driver.focusCalls, []wm.FocusSelector{wm.FocusStackPrev, wm.FocusStackLast}) {
			t.Fatalf("calls = %v, %v; want stack.prev, stack.last", driver.focusCalls, err)
		}
	})
}

func TestResize(t *testing.T) {
	t.Run("grow pushes the right edge out by +100", func(t *testing.T) {
		driver := &navigationSpy{Driver: fake.New(fake.Seed{}), primaryOK: true}
		if err := Resize(t.Context(), driver, ResizeGrow); err != nil || !slices.Equal(driver.resizeCalls, []string{"right:100"}) {
			t.Fatalf("calls = %v, %v; want right:100", driver.resizeCalls, err)
		}
	})
	t.Run("shrink falls back to the left edge when the right edge fails", func(t *testing.T) {
		driver := &navigationSpy{Driver: fake.New(fake.Seed{}), primaryOK: false}
		if err := Resize(t.Context(), driver, ResizeShrink); err != nil || !slices.Equal(driver.resizeCalls, []string{"right:-100", "left:100"}) {
			t.Fatalf("calls = %v, %v; want right:-100, left:100", driver.resizeCalls, err)
		}
	})
}

func TestMoveDisplay(t *testing.T) {
	t.Run("moves the focused window to the named display and focuses it", func(t *testing.T) {
		driver := fake.New(fake.Seed{
			Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(1728, 1117)}, {Idx: 2, Frame: displayFrame(3440, 1440)}},
			Spaces:   []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 2}},
			Windows:  []fake.SeedWindow{{ID: 5, App: "Arc", SpaceIndex: 1}},
		})
		_, _ = driver.FocusWindow(t.Context(), 5)
		if err := MoveDisplay(t.Context(), testProfile(t), driver, config.DisplayAW); err != nil {
			t.Fatal(err)
		}
		windows, err := driver.QueryWindows(t.Context())
		if err != nil || windows[0].DisplayIdx != 2 {
			t.Fatalf("display = %d, %v; want 2", windows[0].DisplayIdx, err)
		}
	})
	t.Run("no-op when the named display is absent", func(t *testing.T) {
		driver := fake.New(fake.Seed{
			Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(1728, 1117)}},
			Spaces:   []fake.SeedSpace{{DisplayIdx: 1}},
			Windows:  []fake.SeedWindow{{ID: 5, App: "Arc", SpaceIndex: 1}},
		})
		_, _ = driver.FocusWindow(t.Context(), 5)
		if err := MoveDisplay(t.Context(), testProfile(t), driver, config.DisplayG9); err != nil {
			t.Fatal(err)
		}
		windows, err := driver.QueryWindows(t.Context())
		if err != nil || windows[0].DisplayIdx != 1 {
			t.Fatalf("display = %d, %v; want unchanged display 1", windows[0].DisplayIdx, err)
		}
	})
}

func TestCycleDisplay(t *testing.T) {
	t.Run("moves + focuses across displays", func(t *testing.T) {
		driver := fake.New(fake.Seed{
			Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(1728, 1117)}, {Idx: 2, Frame: displayFrame(3440, 1440)}},
			Spaces:   []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 2}},
			Windows:  []fake.SeedWindow{{ID: 7, App: "Arc", SpaceIndex: 1}},
		})
		_, _ = driver.FocusWindow(t.Context(), 7)
		if err := CycleDisplay(t.Context(), driver, CycleNext); err != nil {
			t.Fatal(err)
		}
		windows, err := driver.QueryWindows(t.Context())
		if err != nil || windows[0].DisplayIdx != 2 {
			t.Fatalf("display = %d, %v; want 2", windows[0].DisplayIdx, err)
		}
	})
	t.Run("no-op when no window is focused", func(t *testing.T) {
		driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}}, Windows: []fake.SeedWindow{{ID: 7, App: "Arc", SpaceIndex: 1}}})
		if err := CycleDisplay(t.Context(), driver, CycleNext); err != nil {
			t.Fatal(err)
		}
		windows, err := driver.QueryWindows(t.Context())
		if err != nil || windows[0].DisplayIdx != 1 {
			t.Fatalf("display = %d, %v; want 1", windows[0].DisplayIdx, err)
		}
	})
}

func TestResetSplits(t *testing.T) {
	t.Run("sets abs ratio 0.5 on tiled visible windows only", func(t *testing.T) {
		calls := make([]string, 0)
		driver := fake.New(fake.Seed{
			Displays: []fake.SeedDisplay{{Idx: 1}},
			Spaces:   []fake.SeedSpace{{DisplayIdx: 1}},
			Windows: []fake.SeedWindow{
				{ID: 1, App: "A", SpaceIndex: 1},
				{ID: 2, App: "B", SpaceIndex: 1, Floating: true},
				{ID: 3, App: "C", SpaceIndex: 1, Minimized: true},
				{ID: 4, App: "D", SpaceIndex: 1, Visible: new(false)},
			},
		})
		spy := &ratioSpy{Driver: driver, calls: &calls}
		_, _ = spy.FocusWindow(t.Context(), 1)
		if err := ResetSplits(t.Context(), spy); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(calls, []string{"1:0.5"}) {
			t.Fatalf("ratio calls = %v; want [1:0.5]", calls)
		}
	})
}

type ratioSpy struct {
	*fake.Driver
	calls *[]string
}

func (d *ratioSpy) SetSplitRatio(ctx context.Context, id int, ratio float64) error {
	*d.calls = append(*d.calls, strconv.Itoa(id)+":"+strconv.FormatFloat(ratio, 'f', -1, 64))
	return ctx.Err()
}

func TestColumns(t *testing.T) {
	t.Run("flips every horizontal split to vertical then balances", func(t *testing.T) {
		driver := fake.New(fake.Seed{
			Displays: []fake.SeedDisplay{{Idx: 1}},
			Spaces:   []fake.SeedSpace{{DisplayIdx: 1}},
			Windows: []fake.SeedWindow{
				{ID: 1, App: "A", SpaceIndex: 1, SplitType: wm.SplitHorizontal},
				{ID: 2, App: "B", SpaceIndex: 1, SplitType: wm.SplitHorizontal},
				{ID: 3, App: "C", SpaceIndex: 1, SplitType: wm.SplitVertical},
			},
		})
		_, _ = driver.FocusWindow(t.Context(), 1)
		if err := Columns(t.Context(), driver); err != nil {
			t.Fatal(err)
		}
		windows, err := driver.QueryWindows(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		for _, window := range windows {
			if window.SplitType == wm.SplitHorizontal {
				t.Fatalf("window %d remains horizontal", window.ID)
			}
		}
	})
}

func TestApply(t *testing.T) {
	t.Run("labels each present display's home space, lays out the desk, nudges once", testApplyLabelsHomeSpacesAndNudges)
	t.Run("spawns a missing Ghostty once, places it, and reuses it on the next apply", testApplySpawnsGhosttyOnce)
	t.Run("does not spawn names belonging only to an absent display", testApplySkipsAbsentDisplaySpawns)
	t.Run("a spawn failure neither stops later spawns nor the layout", testApplyContinuesAfterSpawnFailure)
	t.Run("spawn wait gives up after its bounded polls when no window appears", testApplyStopsWaitingWithoutWindow)
	t.Run("no-op under lock contention (a live holder owns the lock)", testApplyNoopUnderLockContention)
	t.Run("foreign windows land on the laptop park; g9 keeps only its targets", testApplyParksForeignWindows)
}

func testApplyLabelsHomeSpacesAndNudges(t *testing.T) {
	driver := deskWorld()
	events := make([]string, 0)
	env := commandEnv(t)
	env.Nudge = func(_ context.Context, event string) error {
		events = append(events, event)
		return nil
	}
	if err := Apply(t.Context(), testProfile(t), driver, env); err != nil {
		t.Fatal(err)
	}
	spaces, err := driver.QuerySpaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	labels := make([]string, len(spaces))
	for index, space := range spaces {
		labels[index] = space.Label
	}
	for _, label := range []string{"main", "plan", "laptop"} {
		if !slices.Contains(labels, label) {
			t.Errorf("labels = %v; missing %q", labels, label)
		}
	}
	if !slices.Equal(events, []string{"yabai_spaces_changed"}) {
		t.Fatalf("nudges = %v", events)
	}
}

func testApplySpawnsGhosttyOnce(t *testing.T) {
	profile := testProfile(t)
	spec := profile.Windows["ghostty-wave"]
	spec.Spawn = []string{"open", "-na", "Ghostty"}
	profile.Windows["ghostty-wave"] = spec
	driver := fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 1}},
		Windows:  []fake.SeedWindow{{ID: 1, App: "Arc", Title: "one", SpaceIndex: 1}, {ID: 2, App: "Obsidian", SpaceIndex: 1}, {ID: 3, App: "Code", SpaceIndex: 1}},
	})
	env := commandEnv(t)
	if err := Apply(t.Context(), profile, driver, env); err != nil {
		t.Fatal(err)
	}
	if len(driver.SpawnCalls) != 1 || !slices.Equal(driver.SpawnCalls[0], spec.Spawn) {
		t.Fatalf("spawn calls = %v; want one Ghostty launch", driver.SpawnCalls)
	}
	windows, err := driver.QueryWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 4 {
		t.Fatalf("windows = %d; want spawned window in layout", len(windows))
	}
	if err := Apply(t.Context(), profile, driver, env); err != nil {
		t.Fatal(err)
	}
	if len(driver.SpawnCalls) != 1 {
		t.Fatalf("spawn calls = %d; want no second spawn", len(driver.SpawnCalls))
	}
}

func testApplySkipsAbsentDisplaySpawns(t *testing.T) {
	profile := testProfile(t)
	spec := profile.Windows["ghostty-wave"]
	spec.Spawn = []string{"open", "-a", "Ghostty"}
	profile.Windows["ghostty-wave"] = spec
	driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(3440, 1440)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}}})
	if err := Apply(t.Context(), profile, driver, commandEnv(t)); err != nil {
		t.Fatal(err)
	}
	if len(driver.SpawnCalls) != 0 {
		t.Fatalf("spawn calls = %v; want none", driver.SpawnCalls)
	}
}

func testApplyContinuesAfterSpawnFailure(t *testing.T) {
	profile := testProfile(t)
	linear := profile.Windows["linear"]
	linear.Spawn = []string{"open", "-a", "Linear"}
	profile.Windows["linear"] = linear
	akiflow := profile.Windows["akiflow"]
	akiflow.Spawn = []string{"open", "-a", "Akiflow"}
	profile.Windows["akiflow"] = akiflow
	driver := fake.New(fake.Seed{
		Displays:   []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(3440, 1440)}},
		Spaces:     []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 1}},
		Windows:    []fake.SeedWindow{{ID: 1, App: "Arc", SpaceIndex: 2}},
		SpawnFails: 1,
	})
	if err := Apply(t.Context(), profile, driver, commandEnv(t)); err != nil {
		t.Fatal(err)
	}
	if len(driver.SpawnCalls) != 2 {
		t.Fatalf("spawn calls = %v; want both launches attempted", driver.SpawnCalls)
	}
	spaces, err := driver.QuerySpaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(spaces, func(space wm.Space) bool { return space.Label == "plan" }) {
		t.Fatalf("spaces = %+v; want desk layout after spawn failure", spaces)
	}
}

func testApplyStopsWaitingWithoutWindow(t *testing.T) {
	profile := testProfile(t)
	arc := profile.Windows["arc"]
	arc.Spawn = []string{"open", "-a", "Arc"}
	profile.Windows["arc"] = arc
	driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(3440, 1440)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}}, SpawnNoWindow: true})
	env := commandEnv(t)
	env.Nudge = func(context.Context, string) error { return nil }
	if err := Apply(t.Context(), profile, driver, env); err != nil {
		t.Fatal(err)
	}
	if len(driver.SpawnCalls) == 0 {
		t.Fatal("no spawn was attempted")
	}
}

func testApplyNoopUnderLockContention(t *testing.T) {
	driver := deskWorld()
	env := commandEnv(t)
	lock, ok, err := effects.AcquireLock(env.Paths.ApplyLock)
	if err != nil || !ok {
		t.Fatalf("AcquireLock() = %t, %v", ok, err)
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			t.Error(err)
		}
	}()
	if err := Apply(t.Context(), testProfile(t), driver, env); err != nil {
		t.Fatal(err)
	}
	spaces, err := driver.QuerySpaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, space := range spaces {
		if space.Label != "" {
			t.Fatalf("contention changed space label: %+v", space)
		}
	}
}

func testApplyParksForeignWindows(t *testing.T) {
	driver := fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}, {Idx: 2, Frame: displayFrame(3440, 1440)}, {Idx: 3, Frame: displayFrame(1728, 1117)}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 2}, {DisplayIdx: 3}},
		Windows: []fake.SeedWindow{
			{ID: 1, App: "Arc", SpaceIndex: 1}, {ID: 2, App: "Obsidian", SpaceIndex: 1}, {ID: 3, App: "Ghostty", Title: "pc", SpaceIndex: 1}, {ID: 4, App: "Ghostty", Title: "mbp", SpaceIndex: 1}, {ID: 5, App: "Code", SpaceIndex: 1},
			{ID: 100, App: "Messages", SpaceIndex: 1}, {ID: 101, App: "Slack", SpaceIndex: 1},
			{ID: 6, App: "Linear", SpaceIndex: 2}, {ID: 7, App: "Arc", Title: "two", SpaceIndex: 2}, {ID: 8, App: "Akiflow", SpaceIndex: 2}, {ID: 102, App: "Finder", SpaceIndex: 2},
			{ID: 9, App: "Arc", Title: "three", SpaceIndex: 3}, {ID: 30, App: "Spotify", SpaceIndex: 3}, {ID: 31, App: "Discord", SpaceIndex: 3},
		},
	})
	if err := Apply(t.Context(), testProfile(t), driver, commandEnv(t)); err != nil {
		t.Fatal(err)
	}
	spaces, err := driver.QuerySpaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var main, laptop wm.Space
	for _, space := range spaces {
		if space.Label == "main" {
			main = space
		}
		if space.Label == "laptop" {
			laptop = space
		}
	}
	if !slices.Equal(main.WindowIDs, []int{1, 2, 3, 4, 5}) {
		t.Fatalf("main window ids = %v; want desk targets", main.WindowIDs)
	}
	for _, refugee := range []int{100, 101, 102} {
		if !slices.Contains(laptop.WindowIDs, refugee) {
			t.Errorf("laptop park lacks refugee %d: %v", refugee, laptop.WindowIDs)
		}
	}
	if laptop.Layout != wm.LayoutStack {
		t.Fatalf("laptop layout = %q; want stack", laptop.Layout)
	}
}

func TestLaptop(t *testing.T) {
	t.Run("converges the grid on a laptop-only world and persists the flex order", testLaptopConvergesAndPersists)
	t.Run("an explicit run spawns and clears blank spaces; a window-event converge does neither", testLaptopExplicitAndEventBehavior)
	t.Run("returns contended when a live holder owns the converge lock", testLaptopReturnsContendedUnderLock)
	t.Run("skipped when a monitor is attached (not laptop-only)", testLaptopSkipsWhenMonitorAttached)
}

func testLaptopConvergesAndPersists(t *testing.T) {
	driver := fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(1728, 1117)}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 1, Label: "laptop"}},
		Windows:  []fake.SeedWindow{{ID: 1, App: "Arc", Title: "a", SpaceIndex: 1}, {ID: 2, App: "Ghostty", Title: "pc", SpaceIndex: 1}, {ID: 3, App: "Spotify", SpaceIndex: 1}},
	})
	env := commandEnv(t)
	result, err := Laptop(t.Context(), testProfile(t), driver, env)
	if err != nil || result != LaptopRan {
		t.Fatalf("Laptop() = %q, %v; want ran", result, err)
	}
	spaces, err := driver.QuerySpaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var hasLap, homeStack bool
	for _, space := range spaces {
		hasLap = hasLap || strings.HasPrefix(space.Label, "lap-")
		homeStack = homeStack || (space.Label == "laptop" && space.Layout == wm.LayoutStack)
	}
	if !hasLap || !homeStack {
		t.Fatalf("laptop spaces = %+v; want lap grid and stack home", spaces)
	}
	if _, err := os.Stat(env.Paths.FlexOrder); err != nil {
		t.Fatalf("flex order was not persisted: %v", err)
	}
}

func testLaptopExplicitAndEventBehavior(t *testing.T) {
	profile := testProfile(t)
	spec := profile.Windows["ghostty-mbp"]
	spec.Spawn = []string{"open", "-na", "Ghostty"}
	profile.Windows["ghostty-mbp"] = spec
	seed := fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(1728, 1117)}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 1, Label: "laptop"}, {DisplayIdx: 1, Label: ""}},
		Windows:  []fake.SeedWindow{{ID: 1, App: "Arc", Title: "a", SpaceIndex: 1}},
	}
	explicit := fake.New(seed)
	if _, err := Laptop(t.Context(), profile, explicit, commandEnv(t)); err != nil {
		t.Fatal(err)
	}
	if len(explicit.SpawnCalls) != 1 {
		t.Fatalf("explicit spawn calls = %v; want one launch", explicit.SpawnCalls)
	}
	explicitSpaces, err := explicit.QuerySpaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, space := range explicitSpaces {
		if space.Label == "" {
			t.Fatal("explicit converge left an unrelated blank space")
		}
	}
	windowEvent := fake.New(seed)
	if _, err := RunFlexConverge(t.Context(), profile, windowEvent, commandEnv(t)); err != nil {
		t.Fatal(err)
	}
	if len(windowEvent.SpawnCalls) != 0 {
		t.Fatalf("event spawn calls = %v; want none", windowEvent.SpawnCalls)
	}
	spaces, err := windowEvent.QuerySpaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(spaces, func(space wm.Space) bool { return space.Label == "" }) {
		t.Fatal("window-event converge removed a blank space")
	}
}

func testLaptopReturnsContendedUnderLock(t *testing.T) {
	driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(1728, 1117)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1, Label: "laptop"}}, Windows: []fake.SeedWindow{{ID: 1, App: "Arc", SpaceIndex: 1}}})
	env := commandEnv(t)
	lock, ok, err := effects.AcquireLock(env.Paths.LaptopLock)
	if err != nil || !ok {
		t.Fatalf("AcquireLock() = %t, %v", ok, err)
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			t.Error(err)
		}
	}()
	result, err := Laptop(t.Context(), testProfile(t), driver, env)
	if err != nil || result != LaptopContended {
		t.Fatalf("Laptop() = %q, %v; want contended", result, err)
	}
}

func testLaptopSkipsWhenMonitorAttached(t *testing.T) {
	driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(1728, 1117)}, {Idx: 2, Frame: displayFrame(3440, 1440)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1, Label: "laptop"}, {DisplayIdx: 2, Label: "aw"}}, Windows: []fake.SeedWindow{{ID: 1, App: "Arc", SpaceIndex: 1}}})
	result, err := Laptop(t.Context(), testProfile(t), driver, commandEnv(t))
	if err != nil || result != LaptopSkipped {
		t.Fatalf("Laptop() = %q, %v; want skipped", result, err)
	}
}

func TestIsTileCandidate(t *testing.T) {
	base := wm.Window{Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true}
	if !IsTileCandidate(base) {
		t.Fatal("standard root window was not a tile candidate")
	}
	cases := []struct {
		name   string
		window wm.Window
	}{
		{name: "not root", window: func() wm.Window { w := base; w.Root = false; return w }()},
		{name: "dialog", window: func() wm.Window { w := base; w.Subrole = "AXDialog"; return w }()},
		{name: "immovable", window: func() wm.Window { w := base; w.CanMove = false; return w }()},
		{name: "sticky", window: func() wm.Window { w := base; w.Sticky = true; return w }()},
		{name: "non-normal level", window: func() wm.Window { w := base; w.Level = 101; return w }()},
		{name: "unknown role", window: func() wm.Window { w := base; w.Role = "AXUnknown"; return w }()},
		{name: "fixed width at threshold", window: func() wm.Window { w := base; w.CanResize = false; w.Frame = wm.Frame{W: 500, H: 501}; return w }()},
		{name: "fixed height at threshold", window: func() wm.Window { w := base; w.CanResize = false; w.Frame = wm.Frame{W: 501, H: 500}; return w }()},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if IsTileCandidate(test.window) {
				t.Fatal("exception classified as tile candidate")
			}
		})
	}
	base.CanResize = false
	base.Frame = wm.Frame{W: 501, H: 501}
	if !IsTileCandidate(base) {
		t.Fatal("large fixed-size standard window was not a tile candidate")
	}
}

func TestWindowPlacement(t *testing.T) {

	t.Run("desk arrival moves and unfloats only after focus verifies target", func(t *testing.T) {
		driver := deskWorld()
		env := commandEnv(t)
		before, err := driver.QueryWindows(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		window := wm.Window{ID: 90, Role: "AXWindow", Subrole: "AXStandardWindow", Root: true, CanMove: true, CanResize: true, Floating: true, SpaceID: "1", Frame: wm.Frame{W: 800, H: 600}}
		arrival := newPlacementDriver(driver, window)
		if err := HandleWindowCreated(t.Context(), testProfile(t), arrival, env, 90); err != nil {
			t.Fatal(err)
		}
		got, found, err := arrival.QueryWindow(t.Context(), 90)
		if err != nil || !found || got.SpaceID != "3" || got.Floating {
			t.Fatalf("placed window = %+v, found %t, err %v; want space 3 tiled", got, found, err)
		}
		if arrival.moves != 1 || arrival.focuses != 1 || arrival.toggles != 1 {
			t.Fatalf("move/focus/toggle = %d/%d/%d; want 1/1/1", arrival.moves, arrival.focuses, arrival.toggles)
		}
		if !slices.Equal(arrival.operations, []string{"move", "focus", "active-space", "toggle"}) {
			t.Fatalf("placement order = %v", arrival.operations)
		}
		after, err := driver.QueryWindows(t.Context())
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("arrival changed existing BSP windows: %v", err)
		}
	})

	t.Run("failed active-space verification leaves moved arrival floating", func(t *testing.T) {
		driver := deskWorld()
		env := commandEnv(t)
		arrival := newPlacementDriver(driver, wm.Window{ID: 91, Role: "AXWindow", Subrole: "AXStandardWindow", Root: true, CanMove: true, CanResize: true, Floating: true, SpaceID: "1", Frame: wm.Frame{W: 800, H: 600}})
		before, err := driver.QueryWindows(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		arrival.focusSpace = "1"
		if err := HandleWindowCreated(t.Context(), testProfile(t), arrival, env, 91); err != nil {
			t.Fatal(err)
		}
		if arrival.moves != 1 || arrival.toggles != 0 {
			t.Fatalf("move/toggle = %d/%d; want moved, not unfloated", arrival.moves, arrival.toggles)
		}
		got, found, err := arrival.QueryWindow(t.Context(), 91)
		if err != nil || !found || got.SpaceID != "3" || !got.Floating {
			t.Fatalf("failed verification state = %+v, %t, %v; want floating at laptop target", got, found, err)
		}
		after, err := driver.QueryWindows(t.Context())
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("failed verification changed existing BSP windows: %v", err)
		}
	})

	t.Run("stale ID and already tiled window do not mutate", func(t *testing.T) {
		driver := deskWorld()
		env := commandEnv(t)
		stale := newPlacementDriver(driver, wm.Window{})
		if err := HandleWindowCreated(t.Context(), testProfile(t), stale, env, 999); err != nil {
			t.Fatal(err)
		}
		tiled := wm.Window{ID: 92, Role: "AXWindow", Subrole: "AXStandardWindow", Root: true, CanMove: true, CanResize: true, Floating: false, SpaceID: "1"}
		already := newPlacementDriver(driver, tiled)
		if err := HandleWindowCreated(t.Context(), testProfile(t), already, env, 92); err != nil {
			t.Fatal(err)
		}
		if already.moves+already.focuses+already.toggles != 0 {
			t.Fatalf("already tiled window mutated: %+v", already)
		}
	})

}
func TestWindowPlacementFailedFocus(t *testing.T) {
	for _, focusErr := range []error{nil, errors.New("focus failed")} {
		name := "not focused"
		if focusErr != nil {
			name = "focus error"
		}
		t.Run(name, func(t *testing.T) {
			arrival := newPlacementDriver(deskWorld(), candidate())
			arrival.focusFailed, arrival.focusErr = true, focusErr
			err := HandleWindowCreated(t.Context(), testProfile(t), arrival, commandEnv(t), 90)
			if !errors.Is(err, focusErr) {
				t.Fatalf("focus error = %v; want %v", err, focusErr)
			}
			if !arrival.window.Floating || !slices.Equal(arrival.operations, []string{"move", "focus"}) {
				t.Fatalf("failed focus mutated window: %+v, %v", arrival.window, arrival.operations)
			}
		})
	}
}

type placementDriver struct {
	*fake.Driver
	mu          sync.Mutex
	window      wm.Window
	windowID    int
	activeSpace wm.SpaceID
	exists      bool
	moves       int
	focuses     int
	toggles     int
	operations  []string
	queries     int
	focusSpace  wm.SpaceID
	focusFailed bool
	focusErr    error
}

func newPlacementDriver(driver *fake.Driver, window wm.Window) *placementDriver {
	return &placementDriver{Driver: driver, window: window, windowID: window.ID, exists: window.ID > 0}
}

func (d *placementDriver) QueryWindow(ctx context.Context, id int) (wm.Window, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return wm.Window{}, false, err
	}
	d.queries++
	return d.window, d.exists && id == d.windowID, nil
}

func (d *placementDriver) MoveWindowToSpace(ctx context.Context, id int, space wm.SpaceID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	d.moves++
	d.operations = append(d.operations, "move")
	if id == d.windowID {
		d.window.SpaceID = space
	}
	return nil
}

func (d *placementDriver) FocusWindow(ctx context.Context, id int) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	d.focuses++
	d.operations = append(d.operations, "focus")
	if d.focusFailed {
		return false, d.focusErr
	}
	d.activeSpace = d.window.SpaceID
	if d.focusSpace != "" {
		d.activeSpace = d.focusSpace
	}
	return id == d.windowID, nil
}

func (d *placementDriver) QueryFocusedSpace(ctx context.Context) (wm.Space, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.operations = append(d.operations, "active-space")
	if err := ctx.Err(); err != nil {
		return wm.Space{}, false, err
	}
	return wm.Space{ID: d.activeSpace}, d.activeSpace != "", nil
}

func (d *placementDriver) ToggleFloat(ctx context.Context, id int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	d.toggles++
	d.operations = append(d.operations, "toggle")
	if id == d.windowID {
		d.window.Floating = !d.window.Floating
	}
	return nil
}

func TestRules(t *testing.T) {
	t.Run("laptop mode keeps placement and 1Password management exception", func(t *testing.T) {
		driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 3, Frame: displayFrame(1728, 1117)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 3, Label: "laptop"}}})
		recorder := &ruleRecorder{existing: []wm.Rule{{Label: "auto:old"}, {Label: "manual-keep"}}}
		withRules := &ruleDriver{Driver: driver, rules: recorder}
		if err := Rules(t.Context(), testProfile(t), withRules); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(recorder.removed, []string{"auto:old"}) {
			t.Fatalf("removed = %v", recorder.removed)
		}
		for _, label := range []string{
			"auto:default-laptop-stack", "auto:akiflow", "auto:manage-standard",
			"auto:finder", "auto:akiflow-dialog", "auto:system-settings",
		} {
			if !slices.ContainsFunc(recorder.added, func(rule wm.Rule) bool { return rule.Label == label }) {
				t.Errorf("missing rule %q", label)
			}
		}
		standard := slices.IndexFunc(recorder.added, func(rule wm.Rule) bool { return rule.Label == "auto:manage-standard" })
		if standard < 0 {
			t.Fatal("missing auto:manage-standard")
		}
		if rule := recorder.added[standard]; rule.ExcludeApp != "^1Password$" || rule.Manage == nil || !*rule.Manage {
			t.Errorf("manage-standard = %+v; want manage on except 1Password", rule)
		}
		if recorder.applied != 0 {
			t.Fatalf("apply calls = %d; want no bulk apply", recorder.applied)
		}
	})
	t.Run("desk mode adds only a catch-all manage-off rule", func(t *testing.T) {
		driver := deskWorld()
		recorder := &ruleRecorder{}
		withRules := &ruleDriver{Driver: driver, rules: recorder}
		if err := Rules(t.Context(), testProfile(t), withRules); err != nil {
			t.Fatal(err)
		}
		if len(recorder.added) != 1 || recorder.added[0].Label != "auto:desk-manage-off" || recorder.added[0].Manage == nil || *recorder.added[0].Manage {
			t.Fatalf("desk rules = %+v; want one catch-all manage-off rule", recorder.added)
		}
		if recorder.added[0].Space != nil || recorder.added[0].Display != nil {
			t.Fatalf("desk arrival rule has placement effects: %+v", recorder.added[0])
		}
		if recorder.applied != 0 {
			t.Fatalf("apply calls = %d; want no bulk apply", recorder.applied)
		}
	})
	t.Run("Rules leaves populated BSP management, space, and geometry unchanged", testRulesPreserveBSP)
	t.Run("no-op when the backend has no rules capability", func(t *testing.T) {
		if err := Rules(t.Context(), testProfile(t), fake.New(fake.Seed{})); err != nil {
			t.Fatal(err)
		}
	})
}

func testRulesPreserveBSP(t *testing.T) {
	t.Helper()
	driver := deskWorld()
	before, err := driver.QueryWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	spacesBefore, err := driver.QuerySpaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	recorder := &ruleRecorder{}
	if err := Rules(t.Context(), testProfile(t), &ruleDriver{Driver: driver, rules: recorder}); err != nil {
		t.Fatal(err)
	}
	after, err := driver.QueryWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	spacesAfter, err := driver.QuerySpaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(spacesBefore, spacesAfter) {
		t.Fatalf("Rules changed existing world: windows before=%+v after=%+v spaces before=%+v after=%+v", before, after, spacesBefore, spacesAfter)
	}
	if recorder.applied != 0 {
		t.Fatalf("apply calls = %d; want no bulk apply", recorder.applied)
	}
}

type ruleRecorder struct {
	existing []wm.Rule
	removed  []string
	added    []wm.Rule
	applied  int
}

type ruleDriver struct {
	*fake.Driver
	rules *ruleRecorder
}

func (d *ruleDriver) List(context.Context) ([]wm.Rule, error) { return d.rules.existing, nil }
func (d *ruleDriver) Remove(_ context.Context, label string) error {
	d.rules.removed = append(d.rules.removed, label)
	return nil
}
func (d *ruleDriver) Add(_ context.Context, rule wm.Rule) error {
	d.rules.added = append(d.rules.added, rule)
	return nil
}
func (d *ruleDriver) Apply(context.Context) error { d.rules.applied++; return nil }

func TestRun(t *testing.T) {
	t.Run("daemon command returns the CLI routing sentinel", func(t *testing.T) {
		_, err := Run(t.Context(), testProfile(t), DaemonCommand{}, fake.New(fake.Seed{}), commandEnv(t))
		if !errors.Is(err, ErrDaemonCommand) {
			t.Fatalf("Run() error = %v; want ErrDaemonCommand", err)
		}
	})
}

func TestWake(t *testing.T) {
	t.Run("writes wake request to the daemon socket", func(t *testing.T) {
		//nolint:usetesting // Short socket paths stay under the macOS limit.
		dir, err := os.MkdirTemp(commandsTempDir(), "tess-wake-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(dir); err != nil {
				t.Error(err)
			}
		})
		sock := filepath.Join(dir, "daemon.sock")
		listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := listener.Close(); err != nil {
				t.Error(err)
			}
		}()
		received := make(chan string, 1)
		go func() {
			conn, err := listener.Accept()
			if err != nil {
				received <- "accept error"
				return
			}
			defer func() {
				if err := conn.Close(); err != nil {
					t.Errorf("closing wake test socket: %v", err)
				}
			}()
			buf := make([]byte, 5)
			_, err = conn.Read(buf)
			if err != nil {
				received <- "read error"
				return
			}
			received <- string(buf)
		}()
		if err := Wake(t.Context(), testProfile(t), fake.New(fake.Seed{}), Env{Stderr: io.Discard}, sock); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-received:
			if got != "wake\n" {
				t.Fatalf("socket request = %q; want wake newline", got)
			}
		case <-time.After(time.Second):
			t.Fatal("daemon did not receive wake request")
		}
	})
	t.Run("daemon not running reports the frozen diagnostic", func(t *testing.T) {
		env := commandEnv(t)
		var stderr strings.Builder
		env.Stderr = &stderr
		err := Wake(t.Context(), testProfile(t), fake.New(fake.Seed{}), env, filepath.Join(t.TempDir(), "missing.sock"))
		if !errors.Is(err, ErrDaemonNotRunning) || stderr.String() != "tess: daemon not running\n" {
			t.Fatalf("Wake() = %v, stderr %q", err, stderr.String())
		}
	})
}
