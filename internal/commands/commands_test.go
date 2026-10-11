package commands

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/config/configtest"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
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
	t.Run("labels each present display's home space, lays out the desk, nudges once", func(t *testing.T) {
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
	})
	t.Run("spawns a missing Ghostty once, places it, and reuses it on the next apply", func(t *testing.T) {
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
	})
	t.Run("does not spawn names belonging only to an absent display", func(t *testing.T) {
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
	})
	t.Run("a spawn failure neither stops later spawns nor the layout", func(t *testing.T) {
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
	})
	t.Run("spawn wait gives up after its bounded polls when no window appears", func(t *testing.T) {
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
	})
	t.Run("no-op under lock contention (a live holder owns the lock)", func(t *testing.T) {
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
	})
	t.Run("foreign windows land on the laptop park; g9 keeps only its targets", func(t *testing.T) {
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
	})
}

func TestLaptop(t *testing.T) {
	t.Run("converges the grid on a laptop-only world and persists the flex order", func(t *testing.T) {
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
	})
	t.Run("an explicit run spawns and clears blank spaces; a window-event converge does neither", func(t *testing.T) {
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
	})
	t.Run("returns contended when a live holder owns the converge lock", func(t *testing.T) {
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
	})
	t.Run("skipped when a monitor is attached (not laptop-only)", func(t *testing.T) {
		driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(1728, 1117)}, {Idx: 2, Frame: displayFrame(3440, 1440)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1, Label: "laptop"}, {DisplayIdx: 2, Label: "aw"}}, Windows: []fake.SeedWindow{{ID: 1, App: "Arc", SpaceIndex: 1}}})
		result, err := Laptop(t.Context(), testProfile(t), driver, commandEnv(t))
		if err != nil || result != LaptopSkipped {
			t.Fatalf("Laptop() = %q, %v; want skipped", result, err)
		}
	})
}

func TestRules(t *testing.T) {
	t.Run("removes only auto: rules, re-adds the ported set catch-all first, applies", func(t *testing.T) {
		driver := deskWorld()
		recorder := &ruleRecorder{existing: []wm.Rule{{Label: "auto:old"}, {Label: "manual-keep"}}}
		withRules := &ruleDriver{Driver: driver, rules: recorder}
		if err := Rules(t.Context(), testProfile(t), withRules); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(recorder.removed, []string{"auto:old"}) {
			t.Fatalf("removed = %v", recorder.removed)
		}
		if len(recorder.added) != 6 || recorder.added[0].Label != "auto:default-laptop-stack" {
			t.Fatalf("added = %+v; catch-all should be first", recorder.added)
		}
		for _, label := range []string{"auto:code", "auto:akiflow", "auto:linear", "auto:finder", "auto:akiflow-dialog"} {
			if !slices.ContainsFunc(recorder.added, func(rule wm.Rule) bool { return rule.Label == label }) {
				t.Errorf("missing rule %q", label)
			}
		}
		if recorder.applied != 1 {
			t.Fatalf("apply calls = %d", recorder.applied)
		}
	})
	t.Run("no-op when the backend has no rules capability", func(t *testing.T) {
		if err := Rules(t.Context(), testProfile(t), fake.New(fake.Seed{})); err != nil {
			t.Fatal(err)
		}
	})
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
		sock := "/tmp/tess-wake-" + strconv.Itoa(os.Getpid()) + ".sock"
		listener, err := net.Listen("unix", sock)
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
