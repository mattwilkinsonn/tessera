package fake

import (
	"reflect"
	"slices"
	"testing"

	"github.com/mattwilkinsonn/tessera/internal/wm"
)

var _ wm.Driver = (*Driver)(nil)

func seed() Seed {
	return Seed{
		Displays: []SeedDisplay{
			{Idx: 1, Frame: &wm.Frame{W: 3440, H: 1440}},
			{Idx: 2, Frame: &wm.Frame{W: 1728, H: 1117}},
		},
		Spaces: []SeedSpace{
			{DisplayIdx: 1, Label: "main"},
			{DisplayIdx: 1},
			{DisplayIdx: 1, Label: "lap-arc"},
			{DisplayIdx: 2, Label: "plan"},
		},
		Windows: []SeedWindow{
			{ID: 10, App: "Arc", SpaceIndex: 1},
			{ID: 11, App: "Ghostty", SpaceIndex: 2},
			{ID: 12, App: "Obsidian", SpaceIndex: 3},
			{ID: 20, App: "Linear", SpaceIndex: 4},
		},
	}
}

func newTestDriver(t *testing.T, seed Seed) *Driver {
	t.Helper()
	return New(seed)
}

func spaces(t *testing.T, driver *Driver) []wm.Space {
	t.Helper()
	got, err := driver.QuerySpaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func windows(t *testing.T, driver *Driver) []wm.Window {
	t.Helper()
	got, err := driver.QueryWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func displays(t *testing.T, driver *Driver) []wm.Display {
	t.Helper()
	got, err := driver.QueryDisplays(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func globalIndex(t *testing.T, driver *Driver, id wm.SpaceID) int {
	t.Helper()
	index := 0
	for _, display := range displays(t, driver) {
		for _, spaceID := range display.SpaceIDs {
			index++
			if spaceID == id {
				return index
			}
		}
	}
	return 0
}

func planSpace(t *testing.T, driver *Driver) wm.Space {
	t.Helper()
	for _, space := range spaces(t, driver) {
		if space.Label == "plan" {
			return space
		}
	}
	t.Fatal("missing plan space")
	return wm.Space{}
}

func windowWithID(t *testing.T, driver *Driver, id int) wm.Window {
	t.Helper()
	for _, window := range windows(t, driver) {
		if window.ID == id {
			return window
		}
	}
	t.Fatalf("missing window %d", id)
	return wm.Window{}
}

func TestStableSpaceIDSurvivesIndexRenumbering(t *testing.T) {
	t.Run("destroying a lower-index space renumbers later spaces but not their ids", testDestroySpaceKeepsLaterIDs)
	t.Run("createSpace mints a fresh id and appends within the display group", testCreateSpaceMintsFreshID)
	t.Run("moveSpaceToIndex reorders live index, id stable", testMoveSpaceKeepsID)
}

func testDestroySpaceKeepsLaterIDs(t *testing.T) {
	driver := newTestDriver(t, seed())
	before := spaces(t, driver)
	main, stray, lapArc := before[0].ID, before[1].ID, before[2].ID
	if got := globalIndex(t, driver, lapArc); got != 3 {
		t.Fatalf("lap-arc index = %d, want 3", got)
	}
	destroyed, err := driver.DestroySpace(t.Context(), stray)
	if err != nil || !destroyed {
		t.Fatalf("DestroySpace() = %t, %v; want true, nil", destroyed, err)
	}
	if !slices.ContainsFunc(spaces(t, driver), func(space wm.Space) bool { return space.ID == lapArc }) {
		t.Fatalf("space id %q changed or disappeared", lapArc)
	}
	if got := globalIndex(t, driver, lapArc); got != 2 {
		t.Fatalf("lap-arc index = %d, want 2", got)
	}
	if got := globalIndex(t, driver, main); got != 1 {
		t.Fatalf("main index = %d, want 1", got)
	}
}

func testCreateSpaceMintsFreshID(t *testing.T) {
	driver := newTestDriver(t, seed())
	created, added, err := driver.CreateSpace(t.Context(), 2)
	if err != nil || !added {
		t.Fatalf("CreateSpace() = %q, %t, %v; want id, true, nil", created, added, err)
	}
	var fresh *wm.Space
	for _, space := range spaces(t, driver) {
		if space.ID == created {
			fresh = &space
		}
	}
	if fresh == nil || fresh.DisplayIdx != 2 || fresh.Label != "" {
		t.Fatalf("created space = %+v, want display 2 and empty label", fresh)
	}
	for _, display := range displays(t, driver) {
		if display.Idx == 2 && (len(display.SpaceIDs) != 2 || display.SpaceIDs[1] != created) {
			t.Fatalf("display 2 spaces = %v, want new id last", display.SpaceIDs)
		}
	}
}

func testMoveSpaceKeepsID(t *testing.T) {
	driver := newTestDriver(t, seed())
	lapArc := spaces(t, driver)[2].ID
	if err := driver.MoveSpaceToIndex(t.Context(), lapArc, 1); err != nil {
		t.Fatal(err)
	}
	if got := globalIndex(t, driver, lapArc); got != 1 {
		t.Fatalf("lap-arc index = %d, want 1", got)
	}
	if !slices.ContainsFunc(spaces(t, driver), func(space wm.Space) bool { return space.ID == lapArc }) {
		t.Fatalf("space id %q changed or disappeared", lapArc)
	}
}

func TestSpaceLifecycle(t *testing.T) {
	t.Run("destroySpace relocates residual windows to an adjacent same-display space", func(t *testing.T) {
		driver := newTestDriver(t, seed())
		stray := spaces(t, driver)[1].ID
		destroyed, err := driver.DestroySpace(t.Context(), stray)
		if err != nil || !destroyed {
			t.Fatalf("DestroySpace() = %t, %v; want true, nil", destroyed, err)
		}
		window := windowWithID(t, driver, 11)
		if window.DisplayIdx != 1 || window.SpaceID == stray {
			t.Fatalf("window 11 after destroy = %+v, want surviving display 1 space", window)
		}
		for _, space := range spaces(t, driver) {
			if space.ID == stray {
				t.Fatalf("destroyed space %q remains", stray)
			}
		}
	})

	t.Run("refuses to destroy a display's sole space", func(t *testing.T) {
		driver := newTestDriver(t, seed())
		plan := planSpace(t, driver)
		destroyed, err := driver.DestroySpace(t.Context(), plan.ID)
		if err != nil || destroyed {
			t.Fatalf("DestroySpace() = %t, %v; want false, nil", destroyed, err)
		}
		if !slices.ContainsFunc(spaces(t, driver), func(space wm.Space) bool { return space.ID == plan.ID }) {
			t.Fatalf("sole space %q was removed", plan.ID)
		}
	})

	t.Run("destroySpace on an unknown id is a false no-op", func(t *testing.T) {
		driver := newTestDriver(t, seed())
		destroyed, err := driver.DestroySpace(t.Context(), "999")
		if err != nil || destroyed {
			t.Fatalf("DestroySpace() = %t, %v; want false, nil", destroyed, err)
		}
	})

	t.Run("labelSpace then restart clears labels", func(t *testing.T) {
		driver := newTestDriver(t, seed())
		stray := spaces(t, driver)[1].ID
		if err := driver.LabelSpace(t.Context(), stray, "lap-ghostty"); err != nil {
			t.Fatal(err)
		}
		if got := spaces(t, driver)[1].Label; got != "lap-ghostty" {
			t.Fatalf("label = %q, want lap-ghostty", got)
		}
		driver.Restart()
		for _, space := range spaces(t, driver) {
			if space.Label != "" {
				t.Errorf("space %q label after restart = %q, want empty", space.ID, space.Label)
			}
		}
	})
}

func TestSpawning(t *testing.T) {
	driver := newTestDriver(t, Seed{
		Displays: []SeedDisplay{{Idx: 1}, {Idx: 2}},
		Spaces:   []SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 2}},
		Windows:  []SeedWindow{{ID: 20, App: "Arc", SpaceIndex: 2}},
		SpawnApp: "Ghostty",
	})
	if focused, err := driver.FocusWindow(t.Context(), 20); err != nil || !focused {
		t.Fatalf("FocusWindow() = %t, %v; want true, nil", focused, err)
	}
	argv := []string{"open", "-na", "Ghostty"}
	if err := driver.SpawnWindow(t.Context(), argv); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(driver.SpawnCalls, [][]string{argv}) {
		t.Fatalf("SpawnCalls = %v, want %v", driver.SpawnCalls, [][]string{argv})
	}
	spawned := windowWithID(t, driver, 21)
	if spawned.Title != "" || spawned.SpaceID != displays(t, driver)[1].SpaceIDs[0] || spawned.Floating {
		t.Fatalf("spawned window = %+v, want blank, on focused space, tiled", spawned)
	}
}

func TestWindowPlacementAndQueries(t *testing.T) {
	t.Run("moveWindowToSpace updates the window's space and display", testMoveWindowToSpaceUpdatesDisplay)
	t.Run("queryWindowsOnSpace returns all windows after a move", testQueryWindowsOnSpaceAfterMove)
	t.Run("realizeSpaceLayout moves column windows onto the space and unfloats them", testRealizeColumnsMovesAndUnfloats)
	t.Run("realizeSpaceLayout cuts columns by weight and copies anchor frames", testRealizeColumnsUsesWeightedFrames)
	t.Run("realizeSpaceLayout stack preserves frames and weighted rows cut height", testRealizeStackAndRows)
	t.Run("realizeSpaceLayout drops empty tracks and uses exact offsets", testRealizeLayoutDropsEmptyTracks)
	t.Run("realizeSpaceLayout moves extras from every track", testRealizeLayoutMovesExtras)
	t.Run("toggleFloat flips floating and focusWindow sets focused window", testToggleFloatAndFocusWindow)
	t.Run("moveWindowToDisplay relocates to target display home space", testMoveWindowToDisplay)
	t.Run("settle unit defaults to zero", testSettleUnitDefaultsToZero)
}

func testMoveWindowToSpaceUpdatesDisplay(t *testing.T) {
	driver := newTestDriver(t, seed())
	plan := planSpace(t, driver)
	if err := driver.MoveWindowToSpace(t.Context(), 10, plan.ID); err != nil {
		t.Fatal(err)
	}
	window := windowWithID(t, driver, 10)
	if window.SpaceID != plan.ID || window.DisplayIdx != 2 {
		t.Fatalf("window after move = %+v, want space %q display 2", window, plan.ID)
	}
}

func testQueryWindowsOnSpaceAfterMove(t *testing.T) {
	driver := newTestDriver(t, seed())
	main := spaces(t, driver)[0].ID
	if err := driver.MoveWindowToSpace(t.Context(), 11, main); err != nil {
		t.Fatal(err)
	}
	got, err := driver.QueryWindowsOnSpace(t.Context(), main)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int, len(got))
	for i, window := range got {
		ids[i] = window.ID
	}
	slices.Sort(ids)
	if !reflect.DeepEqual(ids, []int{10, 11}) {
		t.Fatalf("window ids = %v, want [10 11]", ids)
	}
}

func testRealizeColumnsMovesAndUnfloats(t *testing.T) {
	driver := newTestDriver(t, seed())
	plan := planSpace(t, driver)
	if err := driver.ToggleFloat(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	if !windowWithID(t, driver, 10).Floating {
		t.Fatal("toggleFloat did not float window")
	}
	if err := driver.RealizeSpaceLayout(t.Context(), plan.ID, wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{10}, {11}}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{10, 11} {
		window := windowWithID(t, driver, id)
		if window.SpaceID != plan.ID || window.Floating {
			t.Errorf("window %d after realize = %+v, want tiled on %q", id, window, plan.ID)
		}
	}
}

func testRealizeColumnsUsesWeightedFrames(t *testing.T) {
	frame := &wm.Frame{X: 10, Y: 20, W: 1000, H: 800}
	driver := newTestDriver(t, Seed{
		Displays: []SeedDisplay{{Idx: 1, Frame: frame}},
		Spaces:   []SeedSpace{{DisplayIdx: 1, Label: "grid"}},
		Windows:  []SeedWindow{{ID: 1, App: "A", SpaceIndex: 1}, {ID: 2, App: "A", SpaceIndex: 1}, {ID: 3, App: "A", SpaceIndex: 1}, {ID: 4, App: "A", SpaceIndex: 1}},
	})
	space := spaces(t, driver)[0]
	err := driver.RealizeSpaceLayout(t.Context(), space.ID, wm.SpaceLayoutTarget{
		Kind: wm.LayoutColumns, Tracks: [][]int{{1, 4}, {2}, {3}}, Weights: []float64{3, 4, 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []wm.Frame{{X: 10, Y: 20, W: 300, H: 800}, {X: 310, Y: 20, W: 400, H: 800}, {X: 710, Y: 20, W: 300, H: 800}}
	for i, id := range []int{1, 2, 3} {
		if got := windowWithID(t, driver, id).Frame; got != want[i] {
			t.Errorf("window %d frame = %+v, want %+v", id, got, want[i])
		}
	}
	if got := windowWithID(t, driver, 4).Frame; got != want[0] {
		t.Errorf("extra window frame = %+v, want anchor frame %+v", got, want[0])
	}
}

func testRealizeStackAndRows(t *testing.T) {
	driver := newTestDriver(t, Seed{
		Displays: []SeedDisplay{{Idx: 1, Frame: &wm.Frame{X: 10, Y: 20, W: 1000, H: 800}}},
		Spaces:   []SeedSpace{{DisplayIdx: 1, Label: "grid"}},
		Windows: []SeedWindow{
			{ID: 1, App: "A", SpaceIndex: 1, Frame: &wm.Frame{X: 1, Y: 2, W: 3, H: 4}},
			{ID: 2, App: "B", SpaceIndex: 1, Frame: &wm.Frame{X: 5, Y: 6, W: 7, H: 8}},
		},
	})
	space := spaces(t, driver)[0]
	before := []wm.Frame{windowWithID(t, driver, 1).Frame, windowWithID(t, driver, 2).Frame}
	if err := driver.RealizeSpaceLayout(t.Context(), space.ID, wm.SpaceLayoutTarget{Kind: wm.TargetStack, Tracks: [][]int{{1, 2}}}); err != nil {
		t.Fatal(err)
	}
	for i, id := range []int{1, 2} {
		if got := windowWithID(t, driver, id).Frame; got != before[i] {
			t.Errorf("stack frame for window %d = %+v, want %+v", id, got, before[i])
		}
	}
	if err := driver.RealizeSpaceLayout(t.Context(), space.ID, wm.SpaceLayoutTarget{Kind: wm.LayoutRows, Tracks: [][]int{{1}, {2}}, Weights: []float64{1, 3}}); err != nil {
		t.Fatal(err)
	}
	want := []wm.Frame{{X: 10, Y: 20, W: 1000, H: 200}, {X: 10, Y: 220, W: 1000, H: 600}}
	for i, id := range []int{1, 2} {
		if got := windowWithID(t, driver, id).Frame; got != want[i] {
			t.Errorf("row frame for window %d = %+v, want %+v", id, got, want[i])
		}
	}
}

func testRealizeLayoutDropsEmptyTracks(t *testing.T) {
	driver := newTestDriver(t, Seed{
		Displays: []SeedDisplay{{Idx: 1, Frame: &wm.Frame{W: 1728, H: 1000}}},
		Spaces:   []SeedSpace{{DisplayIdx: 1, Label: "grid"}},
		Windows:  []SeedWindow{{ID: 1, App: "A", SpaceIndex: 1}, {ID: 2, App: "A", SpaceIndex: 1}, {ID: 3, App: "A", SpaceIndex: 1}, {ID: 4, App: "A", SpaceIndex: 1}},
	})
	space := spaces(t, driver)[0]
	if err := driver.RealizeSpaceLayout(t.Context(), space.ID, wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{1}, {}, {2}, {3}, {4}}, Weights: []float64{3, 4, 3, 2}}); err != nil {
		t.Fatal(err)
	}
	want := []float64{0, 432, 1008, 1440}
	for i, id := range []int{1, 2, 3, 4} {
		if got := windowWithID(t, driver, id).Frame.X; got != want[i] {
			t.Errorf("window %d x = %v, want %v", id, got, want[i])
		}
	}
}

func testRealizeLayoutMovesExtras(t *testing.T) {
	driver := newTestDriver(t, seed())
	plan := planSpace(t, driver)
	if err := driver.RealizeSpaceLayout(t.Context(), plan.ID, wm.SpaceLayoutTarget{Kind: wm.LayoutRows, Tracks: [][]int{{10, 20}, {11}}, Weights: []float64{1, 2}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{10, 11, 20} {
		if got := windowWithID(t, driver, id).SpaceID; got != plan.ID {
			t.Errorf("window %d space = %q, want %q", id, got, plan.ID)
		}
	}
}

func testToggleFloatAndFocusWindow(t *testing.T) {
	driver := newTestDriver(t, seed())
	focused, found, err := driver.QueryFocusedWindow(t.Context())
	if err != nil || found {
		t.Fatalf("QueryFocusedWindow() = %v, %t, %v; want absent, nil error", focused, found, err)
	}
	ok, err := driver.FocusWindow(t.Context(), 20)
	if err != nil || !ok {
		t.Fatalf("FocusWindow() = %t, %v; want true, nil", ok, err)
	}
	focused, found, err = driver.QueryFocusedWindow(t.Context())
	if err != nil || !found || focused.ID != 20 {
		t.Fatalf("focused window = %v, %t, %v; want id 20", focused, found, err)
	}
	ok, err = driver.FocusWindow(t.Context(), 999)
	if err != nil || ok {
		t.Fatalf("FocusWindow(unknown) = %t, %v; want false, nil", ok, err)
	}
}

func testMoveWindowToDisplay(t *testing.T) {
	driver := newTestDriver(t, seed())
	ok, err := driver.MoveWindowToDisplay(t.Context(), 20, wm.DisplaySelector{Index: 1})
	if err != nil || !ok {
		t.Fatalf("MoveWindowToDisplay() = %t, %v; want true, nil", ok, err)
	}
	window := windowWithID(t, driver, 20)
	if window.DisplayIdx != 1 || window.SpaceID != displays(t, driver)[0].SpaceIDs[0] {
		t.Fatalf("window after move = %+v, want display 1 home", window)
	}
}

func testSettleUnitDefaultsToZero(t *testing.T) {
	if got := New(Seed{}).SettleUnit(); got != 0 {
		t.Fatalf("SettleUnit() = %s, want 0", got)
	}
}
