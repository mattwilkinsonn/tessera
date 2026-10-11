package engine

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

const (
	deskG9     = 5120
	deskAW     = 3440
	deskLaptop = 1728
)

func TestDeskPlan(t *testing.T) {
	profile := engineTestProfile(t)
	window, space, display := deskFactories()
	expectOps := func(t *testing.T, got, want []Op) {
		t.Helper()
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("DeskPlan() mismatch (-want +got):\n%s", diff)
		}
	}
	t.Run("full app set golden — three displays, every desk app present", func(t *testing.T) {
		w := World{
			Displays: []wm.Display{display(1, deskG9, "s1"), display(2, deskAW, "s2"), display(3, deskLaptop, "s3")},
			Spaces:   []wm.Space{space("s1", "main", 1), space("s2", "plan", 2), space("s3", "laptop", 3)},
			Windows: []wm.Window{
				window(10, "Arc", 1, "s1", ""), window(11, "Obsidian", 1, "s1", ""), window(12, "Ghostty", 1, "s1", "pc"), window(13, "Ghostty", 1, "s1", "mbp"), window(14, "Code", 1, "s1", ""),
				window(20, "Linear", 2, "s2", ""), window(21, "Arc", 2, "s2", ""), window(22, "Arc", 2, "s2", ""), window(23, "Akiflow", 2, "s2", ""),
				window(30, "Arc", 3, "s3", ""), window(31, "Spotify", 3, "s3", ""), window(32, "Discord", 3, "s3", ""), window(33, "Qalculate", 3, "s3", ""),
			},
		}
		want := []Op{
			MoveWindowAction{WindowID: 10, ToSpace: "s3"}, MoveWindowAction{WindowID: 11, ToSpace: "s3"}, MoveWindowAction{WindowID: 12, ToSpace: "s3"}, MoveWindowAction{WindowID: 13, ToSpace: "s3"}, MoveWindowAction{WindowID: 14, ToSpace: "s3"},
			MoveWindowAction{WindowID: 20, ToSpace: "s3"}, MoveWindowAction{WindowID: 21, ToSpace: "s3"}, MoveWindowAction{WindowID: 22, ToSpace: "s3"}, MoveWindowAction{WindowID: 23, ToSpace: "s3"},
			RelabelHomeAction{HomeSpace: "s1", Label: "main"}, RealizeLayoutOp{Space: "s1", Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{10, 11}, {12}, {13, 14}}, Weights: []float64{3, 4, 3}}},
			RelabelHomeAction{HomeSpace: "s2", Label: "plan"}, RealizeLayoutOp{Space: "s2", Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{20, 21}, {22, 23}}, Weights: []float64{1, 1}}},
			RelabelHomeAction{HomeSpace: "s3", Label: "laptop"}, RealizeLayoutOp{Space: "s3", Target: wm.SpaceLayoutTarget{Kind: wm.TargetStack, Tracks: [][]int{{30, 31, 32, 33}}, Weights: []float64{}}},
		}
		expectOps(t, DeskPlan(profile, w), want)
	})
	t.Run("missing windows skipped — columns shrink, empty columns dropped", func(t *testing.T) {
		w := World{Displays: []wm.Display{display(1, deskG9, "s1")}, Spaces: []wm.Space{space("s1", "main", 1)}, Windows: []wm.Window{window(10, "Arc", 1, "s1", ""), window(14, "Code", 1, "s1", "")}}
		expectOps(t, DeskPlan(profile, w), []Op{RelabelHomeAction{HomeSpace: "s1", Label: "main"}, RealizeLayoutOp{Space: "s1", Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{10}, {14}}, Weights: []float64{3, 3}}}})
	})
	t.Run("display with zero claimed windows gets relabelHome but no realizeLayout", func(t *testing.T) {
		w := World{Displays: []wm.Display{display(1, deskG9, "s1")}, Spaces: []wm.Space{space("s1", "main", 1)}, Windows: []wm.Window{window(99, "Finder", 1, "s1", "")}}
		expectOps(t, DeskPlan(profile, w), []Op{RelabelHomeAction{HomeSpace: "s1", Label: "main"}})
	})
	t.Run("absent display skipped — laptop-only world emits only the laptop stack", func(t *testing.T) {
		w := World{Displays: []wm.Display{display(3, deskLaptop, "s3")}, Spaces: []wm.Space{space("s3", "laptop", 3)}, Windows: []wm.Window{window(30, "Arc", 3, "s3", ""), window(31, "Spotify", 3, "s3", ""), window(32, "Discord", 3, "s3", ""), window(33, "Qalculate", 3, "s3", "")}}
		expectOps(t, DeskPlan(profile, w), []Op{RelabelHomeAction{HomeSpace: "s3", Label: "laptop"}, RealizeLayoutOp{Space: "s3", Target: wm.SpaceLayoutTarget{Kind: wm.TargetStack, Tracks: [][]int{{30, 31, 32, 33}}, Weights: []float64{}}}})
	})
	t.Run("a matching topology replaces the default desk layout", func(t *testing.T) {
		p := engineTestProfile(t)
		p.Topologies = []config.Topology{{Name: "aw-laptop", Displays: []config.DisplayName{config.DisplayAW, config.DisplayLaptop}, Desk: []config.DeskLayout{{Display: config.DisplayAW, Label: "solo", Kind: config.LayoutColumns, Tracks: [][]string{{"arc"}, {"linear"}, {"akiflow"}}}, {Display: config.DisplayLaptop, Label: "laptop", Kind: config.LayoutStack, Tracks: [][]string{{"akiflow"}}}}}}
		w := World{Displays: []wm.Display{display(2, deskAW, "s2"), display(3, deskLaptop, "s3")}, Spaces: []wm.Space{space("s2", "plan", 2), space("s3", "laptop", 3)}, Windows: []wm.Window{window(21, "Arc", 2, "s2", ""), window(20, "Linear", 2, "s2", ""), window(23, "Akiflow", 2, "s2", ""), window(30, "Akiflow", 3, "s3", "")}}
		want := []Op{MoveWindowAction{WindowID: 21, ToSpace: "s3"}, MoveWindowAction{WindowID: 20, ToSpace: "s3"}, MoveWindowAction{WindowID: 23, ToSpace: "s3"}, RelabelHomeAction{HomeSpace: "s2", Label: "solo"}, RealizeLayoutOp{Space: "s2", Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{21}, {20}, {23}}, Weights: []float64{3, 4, 3}}}, RelabelHomeAction{HomeSpace: "s3", Label: "laptop"}, RealizeLayoutOp{Space: "s3", Target: wm.SpaceLayoutTarget{Kind: wm.TargetStack, Tracks: [][]int{{30}}, Weights: []float64{}}}}
		expectOps(t, DeskPlan(p, w), want)
	})
	t.Run("a non-matching topology leaves the default desk in force", func(t *testing.T) {
		p := engineTestProfile(t)
		p.Topologies = []config.Topology{{Name: "aw-laptop", Displays: []config.DisplayName{config.DisplayAW, config.DisplayLaptop}, Desk: []config.DeskLayout{{Display: config.DisplayAW, Label: "solo", Kind: config.LayoutColumns, Tracks: [][]string{{"arc"}}}}}}
		w := World{Displays: []wm.Display{display(1, deskG9, "s1")}, Spaces: []wm.Space{space("s1", "main", 1)}, Windows: []wm.Window{window(10, "Arc", 1, "s1", "")}}
		expectOps(t, DeskPlan(p, w), []Op{RelabelHomeAction{HomeSpace: "s1", Label: "main"}, RealizeLayoutOp{Space: "s1", Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{10}}, Weights: []float64{3}}}})
	})
	t.Run("layout, display, profile, and equal weights resolve by kind and count", func(t *testing.T) {
		p := engineTestProfile(t)
		p.Weights = &config.WeightDefaults{Columns: map[int][]float64{2: {5, 1}, 3: {7, 2, 1}}, Rows: map[int][]float64{2: {1, 3}}}
		p.Displays.G9.Weights = &config.WeightDefaults{Columns: map[int][]float64{3: {4, 3, 3}}}
		p.Displays.AW.Weights = &config.WeightDefaults{Columns: map[int][]float64{2: {3, 2}}}
		p.Desk = []config.DeskLayout{{Display: config.DisplayG9, Label: "main", Kind: config.LayoutColumns, Tracks: [][]string{{"arc"}, {"ghostty-wave"}, {"ghostty-mbp"}}}, {Display: config.DisplayAW, Label: "plan", Kind: config.LayoutColumns, Tracks: [][]string{{"linear"}, {"akiflow"}}, Weights: []float64{1, 4}}, {Display: config.DisplayLaptop, Label: "rows", Kind: config.LayoutRows, Tracks: [][]string{{"spotify"}, {"discord"}}}, {Display: config.DisplayLaptop, Label: "profile", Kind: config.LayoutColumns, Tracks: [][]string{{"obsidian"}, {"vscode"}}}, {Display: config.DisplayLaptop, Label: "equal", Kind: config.LayoutColumns, Tracks: [][]string{{"akiflow"}, {"qalculate"}, {"spotify"}, {"discord"}}}}
		w := World{Displays: []wm.Display{display(1, deskG9, "s1"), display(2, deskAW, "s2"), display(3, deskLaptop, "s3")}, Spaces: []wm.Space{space("s1", "main", 1), space("s2", "plan", 2), space("s3", "rows", 3)}, Windows: []wm.Window{window(10, "Arc", 1, "s1", ""), window(11, "Ghostty", 1, "s1", "pc"), window(12, "Ghostty", 1, "s1", "mbp"), window(20, "Linear", 2, "s2", ""), window(21, "Akiflow", 2, "s2", ""), window(30, "Spotify", 3, "s3", ""), window(31, "Discord", 3, "s3", ""), window(32, "Obsidian", 3, "s3", ""), window(33, "Code", 3, "s3", ""), window(34, "Akiflow", 3, "s3", ""), window(35, "Qalculate", 3, "s3", ""), window(36, "Spotify", 3, "s3", ""), window(37, "Discord", 3, "s3", "")}}
		want := []wm.SpaceLayoutTarget{{Kind: wm.LayoutColumns, Tracks: [][]int{{10}, {11}, {12}}, Weights: []float64{4, 3, 3}}, {Kind: wm.LayoutColumns, Tracks: [][]int{{20}, {21}}, Weights: []float64{1, 4}}, {Kind: wm.LayoutRows, Tracks: [][]int{{30}, {31}}, Weights: []float64{1, 3}}, {Kind: wm.LayoutColumns, Tracks: [][]int{{32}, {33}}, Weights: []float64{5, 1}}, {Kind: wm.LayoutColumns, Tracks: [][]int{{34}, {35}, {36}, {37}}, Weights: []float64{1, 1, 1, 1}}}
		if diff := cmp.Diff(want, realizedTargets(DeskPlan(p, w))); diff != "" {
			t.Fatalf("layout targets mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("empty tracks drop the matching configured weights", func(t *testing.T) {
		testEmptyDeskTracks(t, window, space, display)
	})
}

func deskFactories() (func(int, string, int, wm.SpaceID, string) wm.Window, func(wm.SpaceID, string, int, ...int) wm.Space, func(int, float64, ...wm.SpaceID) wm.Display) {
	window := func(id int, app string, display int, space wm.SpaceID, title string) wm.Window {
		return wm.Window{ID: id, App: app, Title: title, DisplayIdx: display, SpaceID: space}
	}
	space := func(id wm.SpaceID, label string, display int, windowIDs ...int) wm.Space {
		return wm.Space{ID: id, Label: label, DisplayIdx: display, WindowIDs: windowIDs, Layout: wm.LayoutBSP}
	}
	display := func(idx int, width float64, spaces ...wm.SpaceID) wm.Display {
		return wm.Display{Idx: idx, Frame: wm.Frame{W: width, H: 1000}, SpaceIDs: spaces}
	}
	return window, space, display
}

func realizedTargets(ops []Op) []wm.SpaceLayoutTarget {
	targets := make([]wm.SpaceLayoutTarget, 0)
	for _, op := range ops {
		if realized, ok := op.(RealizeLayoutOp); ok {
			targets = append(targets, realized.Target)
		}
	}
	return targets
}

func testEmptyDeskTracks(t *testing.T, window func(int, string, int, wm.SpaceID, string) wm.Window, space func(wm.SpaceID, string, int, ...int) wm.Space, display func(int, float64, ...wm.SpaceID) wm.Display) {
	t.Helper()
	p := engineTestProfile(t)
	p.Desk = []config.DeskLayout{{Display: config.DisplayG9, Label: "main", Kind: config.LayoutColumns, Tracks: [][]string{{"arc"}, {"missing"}, {"akiflow"}}, Weights: []float64{3, 4, 3}}}
	w := World{Displays: []wm.Display{display(1, deskG9, "s1")}, Spaces: []wm.Space{space("s1", "main", 1)}, Windows: []wm.Window{window(10, "Arc", 1, "s1", ""), window(12, "Akiflow", 1, "s1", "")}}
	want := RealizeLayoutOp{Space: "s1", Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{10}, {12}}, Weights: []float64{3, 3}}}
	if !containsOp(DeskPlan(p, w), want) {
		t.Fatalf("DeskPlan() missing layout %#v", want)
	}
	p.Desk[0].Tracks = [][]string{{"missing"}, {"arc"}, {"akiflow"}}
	want.Target.Weights = []float64{4, 3}
	if !containsOp(DeskPlan(p, w), want) {
		t.Fatalf("DeskPlan() missing shifted-weight layout %#v", want)
	}
}

func TestDeskPlanEvacuatesRebuildWindows(t *testing.T) {
	profile := engineTestProfile(t)
	window, space, display := deskFactories()
	w := World{Displays: []wm.Display{display(1, deskG9, "s1"), display(2, deskAW, "s2"), display(3, deskLaptop, "s3")}, Spaces: []wm.Space{space("s1", "main", 1), space("s2", "plan", 2), space("s3", "laptop", 3)}, Windows: []wm.Window{
		window(10, "Arc", 1, "s1", ""), window(40, "Messages", 1, "s1", ""), window(12, "Ghostty", 1, "s1", "pc"), window(41, "Slack", 1, "s1", ""), window(14, "Code", 1, "s1", ""),
		window(20, "Linear", 2, "s2", ""), window(21, "Arc", 2, "s2", ""), window(42, "Finder", 2, "s2", ""), window(23, "Akiflow", 2, "s2", ""),
		window(30, "Arc", 3, "s3", ""), window(43, "Notion", 3, "s3", ""), window(31, "Spotify", 3, "s3", ""),
	}}
	got := DeskPlan(profile, w)
	var moves []MoveWindowAction
	firstRealize := len(got)
	for index, op := range got {
		if move, ok := op.(MoveWindowAction); ok {
			moves = append(moves, move)
		}
		if _, ok := op.(RealizeLayoutOp); ok && firstRealize == len(got) {
			firstRealize = index
		}
	}
	want := []MoveWindowAction{{WindowID: 10, ToSpace: "s3"}, {WindowID: 40, ToSpace: "s3"}, {WindowID: 12, ToSpace: "s3"}, {WindowID: 41, ToSpace: "s3"}, {WindowID: 14, ToSpace: "s3"}, {WindowID: 20, ToSpace: "s3"}, {WindowID: 21, ToSpace: "s3"}, {WindowID: 42, ToSpace: "s3"}, {WindowID: 23, ToSpace: "s3"}}
	if diff := cmp.Diff(want, moves); diff != "" {
		t.Fatalf("evacuation moves mismatch (-want +got):\n%s", diff)
	}
	for index, op := range got {
		if _, ok := op.(MoveWindowAction); ok && index >= firstRealize {
			t.Fatalf("move operation at index %d followed realization at %d", index, firstRealize)
		}
	}
	for _, move := range moves {
		if slices.Contains([]int{30, 31, 43}, move.WindowID) {
			t.Errorf("park window %d was evacuated", move.WindowID)
		}
	}
	var target *wm.SpaceLayoutTarget
	for _, op := range got {
		if realized, ok := op.(RealizeLayoutOp); ok && realized.Space == "s1" {
			target = &realized.Target
		}
	}
	if target == nil || cmp.Diff(wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{10}, {12}, {14}}, Weights: []float64{3, 4, 3}}, *target) != "" {
		t.Fatalf("g9 target = %#v", target)
	}
}

func TestDeskPlanParkingVariants(t *testing.T) {
	profile := engineTestProfile(t)
	window, space, display := deskFactories()
	t.Run("single display rebuilds in place without evacuation", func(t *testing.T) {
		w := World{Displays: []wm.Display{display(1, deskG9, "s1")}, Spaces: []wm.Space{space("s1", "main", 1)}, Windows: []wm.Window{window(10, "Arc", 1, "s1", ""), window(40, "Messages", 1, "s1", ""), window(14, "Code", 1, "s1", "")}}
		for _, op := range DeskPlan(profile, w) {
			if _, ok := op.(MoveWindowAction); ok {
				t.Fatal("single-display plan evacuated a window")
			}
		}
	})
	t.Run("two displays park on AW", func(t *testing.T) {
		w := World{Displays: []wm.Display{display(1, deskG9, "s1"), display(2, deskAW, "s2")}, Spaces: []wm.Space{space("s1", "main", 1), space("s2", "plan", 2)}, Windows: []wm.Window{window(10, "Arc", 1, "s1", ""), window(40, "Messages", 1, "s1", ""), window(14, "Code", 1, "s1", ""), window(20, "Linear", 2, "s2", ""), window(21, "Arc", 2, "s2", ""), window(41, "Finder", 2, "s2", ""), window(23, "Akiflow", 2, "s2", "")}}
		var got []MoveWindowAction
		for _, op := range DeskPlan(profile, w) {
			if move, ok := op.(MoveWindowAction); ok {
				got = append(got, move)
			}
		}
		want := []MoveWindowAction{{WindowID: 10, ToSpace: "s2"}, {WindowID: 40, ToSpace: "s2"}, {WindowID: 14, ToSpace: "s2"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("evacuation moves mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("g9 window claimed as AW target evacuates to park", func(t *testing.T) {
		w := World{Displays: []wm.Display{display(1, deskG9, "s1"), display(2, deskAW, "s2"), display(3, deskLaptop, "s3")}, Spaces: []wm.Space{space("s1", "main", 1), space("s2", "plan", 2), space("s3", "laptop", 3)}, Windows: []wm.Window{window(10, "Arc", 1, "s1", ""), window(14, "Code", 1, "s1", ""), window(50, "Linear", 1, "s1", ""), window(21, "Arc", 2, "s2", ""), window(30, "Arc", 3, "s3", ""), window(31, "Spotify", 3, "s3", "")}}
		got := DeskPlan(profile, w)
		if !containsOp(got, MoveWindowAction{WindowID: 50, ToSpace: "s3"}) {
			t.Fatal("AW-claimed Linear did not evacuate to park")
		}
		for _, op := range got {
			if realized, ok := op.(RealizeLayoutOp); ok && realized.Space == "s2" && realized.Target.Tracks[0][0] != 50 {
				t.Fatalf("AW first track = %v, want window 50", realized.Target.Tracks[0])
			}
		}
	})
}

func containsOp(ops []Op, expected Op) bool {
	for _, op := range ops {
		if diff := cmp.Diff(expected, op); diff == "" {
			return true
		}
	}
	return false
}

func TestLaptopConvergeStep(t *testing.T) {
	profile := engineTestProfile(t)
	laptopConvergeCreationCases(t, profile)
	laptopConvergeOrderCases(t, profile)
	laptopConvergeCleanupCases(t, profile)
}

func laptopConvergeCreationCases(t *testing.T, profile *config.Profile) {
	t.Helper()
	t.Run("5th Arc creates lap-arc-5", func(t *testing.T) {
		world := newLaptopWorld([]wm.Window{{ID: 1, App: "Arc"}, {ID: 2, App: "Arc"}, {ID: 3, App: "Arc"}, {ID: 4, App: "Arc"}, {ID: 5, App: "Arc"}}, nil, true)
		runLaptopConverge(t, profile, world, true)
		if !containsString(laptopLabels(world), "lap-arc-5") {
			t.Fatalf("laptop labels = %v", laptopLabels(world))
		}
	})
	t.Run("absent pinned app creates no space", func(t *testing.T) {
		world := newLaptopWorld([]wm.Window{{ID: 1, App: "Arc"}, {ID: 2, App: "Arc"}}, nil, true)
		runLaptopConverge(t, profile, world, true)
		for _, absent := range []string{"lap-linear", "lap-obsidian", "lap-ghostty-wave"} {
			if containsString(laptopLabels(world), absent) {
				t.Errorf("absent label %q was created", absent)
			}
		}
	})
	t.Run("CREATE_FAILED aborts destructive reconcile", func(t *testing.T) {
		world := newLaptopWorld([]wm.Window{{ID: 1, App: "Arc"}}, []string{"lap-oldapp"}, false)
		actions := runLaptopConverge(t, profile, world, true)
		for _, action := range actions {
			if actionIsDestructive(action) {
				t.Fatalf("failed creation allowed destructive action %T", action)
			}
		}
		if !containsString(laptopLabels(world), "lap-oldapp") {
			t.Fatal("stale grid was destroyed after failed create")
		}
	})
	t.Run("reconcile destroys only untargeted lap space", func(t *testing.T) {
		world := newLaptopWorld([]wm.Window{{ID: 1, App: "Arc"}, {ID: 2, App: "Arc"}}, []string{"lap-oldapp"}, true)
		stale := world.order[1]
		actions := runLaptopConverge(t, profile, world, true)
		var destroys []RehomeAndDestroyAction
		for _, action := range actions {
			if value, ok := action.(RehomeAndDestroyAction); ok {
				destroys = append(destroys, value)
			}
		}
		if len(destroys) != 1 || destroys[0].StaleSpace != stale || destroys[0].HomeSpace != "home" {
			t.Fatalf("rehome-and-destroy actions = %v", destroys)
		}
		labels := laptopLabels(world)
		if containsString(labels, "lap-oldapp") || !containsString(labels, "lap-arc") || !containsString(labels, "laptop") {
			t.Fatalf("labels after reconcile = %v", labels)
		}
	})
}

func laptopConvergeOrderCases(t *testing.T, profile *config.Profile) {
	t.Helper()
	t.Run("final order is home then pinned core and flex tail", func(t *testing.T) {
		world := newLaptopWorld([]wm.Window{{ID: 1, App: "Arc"}, {ID: 2, App: "Arc"}, {ID: 3, App: "Arc"}, {ID: 4, App: "Arc"}, {ID: 5, App: "Arc"}, {ID: 6, App: "Spotify"}}, nil, true)
		runLaptopConverge(t, profile, world, true)
		want := []string{"laptop", "lap-arc", "lap-arc-2", "lap-arc-3", "lap-arc-4", "lap-arc-5", "lap-spotify"}
		if diff := cmp.Diff(want, laptopLabels(world)); diff != "" {
			t.Fatalf("label order mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("space ids remain stable throughout converge", func(t *testing.T) {
		world := newLaptopWorld([]wm.Window{{ID: 1, App: "Arc"}, {ID: 2, App: "Arc"}, {ID: 3, App: "Spotify"}}, nil, true)
		state := InitialConvergeState("home", nil, true)
		referenced := make(map[wm.SpaceID]struct{})
		for range 1000 {
			action, next, ok := LaptopConvergeStep(profile, laptopSnapshot(world), state)
			if !ok {
				break
			}
			if value, ok := action.(MoveWindowAction); ok {
				referenced[value.ToSpace] = struct{}{}
			}
			if value, ok := action.(MoveSpaceAction); ok {
				referenced[value.Space] = struct{}{}
			}
			applyLaptopAction(world, action)
			state = next
		}
		live := make(map[wm.SpaceID]struct{}, len(world.spaces))
		for _, space := range world.spaces {
			live[space.ID] = struct{}{}
		}
		for id := range referenced {
			if _, ok := live[id]; !ok {
				t.Errorf("referenced id %q did not survive", id)
			}
		}
		if _, ok := live["home"]; !ok {
			t.Fatal("home space identity did not survive")
		}
	})
	t.Run("phase D reorders existing out-of-order lap spaces", func(t *testing.T) {
		world := newLaptopWorld([]wm.Window{{ID: 1, App: "Arc"}, {ID: 2, App: "Arc"}, {ID: 3, App: "Arc"}}, []string{"lap-arc-3", "lap-arc-2", "lap-arc"}, true)
		actions := runLaptopConverge(t, profile, world, true)
		var indexes []int
		for _, action := range actions {
			if move, ok := action.(MoveSpaceAction); ok {
				indexes = append(indexes, move.ToIndex)
			}
		}
		if diff := cmp.Diff([]int{2, 3}, indexes); diff != "" {
			t.Fatalf("move indexes mismatch (-want +got):\n%s", diff)
		}
		want := []string{"laptop", "lap-arc", "lap-arc-2", "lap-arc-3"}
		if diff := cmp.Diff(want, laptopLabels(world)); diff != "" {
			t.Fatalf("label order mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("reconverging a settled grid terminates without moves", func(t *testing.T) {
		world := newLaptopWorld([]wm.Window{{ID: 1, App: "Arc"}, {ID: 2, App: "Arc"}, {ID: 3, App: "Arc"}, {ID: 4, App: "Arc"}, {ID: 5, App: "Arc"}, {ID: 6, App: "Spotify"}}, nil, true)
		runLaptopConverge(t, profile, world, true)
		settled := laptopLabels(world)
		actions := runLaptopConverge(t, profile, world, true)
		want := []ConvergeAction{RelabelHomeAction{HomeSpace: "home", Label: "laptop"}, SetLayoutAction{Space: "home", Layout: wm.LayoutStack}}
		if diff := cmp.Diff(want, actions); diff != "" {
			t.Fatalf("settled actions mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(settled, laptopLabels(world)); diff != "" {
			t.Fatalf("settled order changed (-want +got):\n%s", diff)
		}
	})
}

func laptopConvergeCleanupCases(t *testing.T, profile *config.Profile) {
	t.Helper()
	t.Run("reaps blank spaces after yabai restart", func(t *testing.T) {
		world := newLaptopWorld([]wm.Window{{ID: 1, App: "Arc"}, {ID: 2, App: "Arc"}}, []string{"", "", ""}, true)
		runLaptopConverge(t, profile, world, true)
		want := []string{"laptop", "lap-arc", "lap-arc-2"}
		if diff := cmp.Diff(want, laptopLabels(world)); diff != "" {
			t.Fatalf("labels mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("keeps blank space that holds a window", func(t *testing.T) {
		world := newLaptopWorld([]wm.Window{{ID: 1, App: "Arc"}, {ID: 9, App: "Finder", SpaceID: "s-1", Floating: true}}, []string{""}, true)
		runLaptopConverge(t, profile, world, true)
		if !containsSpace(world, "s-1") {
			t.Fatal("space holding a window was destroyed")
		}
	})
	t.Run("keeps blank space with unreported window", func(t *testing.T) {
		world := newLaptopWorld([]wm.Window{{ID: 1, App: "Arc"}, {ID: 9, App: "Finder", SpaceID: "s-1", Floating: true}}, []string{""}, true)
		world.unreported[9] = struct{}{}
		runLaptopConverge(t, profile, world, true)
		if !containsSpace(world, "s-1") {
			t.Fatal("space holding unreported window was destroyed")
		}
	})
	t.Run("window-event converge leaves blank spaces alone", func(t *testing.T) {
		world := newLaptopWorld([]wm.Window{{ID: 1, App: "Arc"}}, []string{""}, true)
		actions := runLaptopConverge(t, profile, world, false)
		for _, action := range actions {
			if value, ok := action.(DestroySpaceOp); ok {
				t.Fatalf("window event destroyed unrelated blank space: %v", value)
			}
		}
		if !containsString(laptopLabels(world), "") {
			t.Fatal("blank space was removed")
		}
	})
	t.Run("refused destroy is tried once and converge finishes", func(t *testing.T) {
		world := newLaptopWorld([]wm.Window{{ID: 1, App: "Arc"}}, []string{"", "lap-oldapp"}, true)
		blank, stale := world.order[1], world.order[2]
		world.undestroyable[blank] = struct{}{}
		world.undestroyable[stale] = struct{}{}
		actions := runLaptopConverge(t, profile, world, true)
		tries := 0
		for _, action := range actions {
			if value, ok := action.(DestroySpaceOp); ok && value.Space == blank {
				tries++
			}
			if value, ok := action.(RehomeAndDestroyAction); ok && value.StaleSpace == stale {
				tries++
			}
		}
		if tries != 2 {
			t.Fatalf("destroy attempts = %d, want 2", tries)
		}
		if _, ok := actions[len(actions)-1].(SetLayoutAction); !ok {
			t.Fatalf("last action = %T", actions[len(actions)-1])
		}
	})
}

func (world *laptopTestWorld) destroy(id wm.SpaceID) {
	if _, refused := world.undestroyable[id]; refused {
		return
	}
	for index, space := range world.spaces {
		if space.ID == id {
			world.spaces = append(world.spaces[:index], world.spaces[index+1:]...)
			break
		}
	}
	for index, spaceID := range world.order {
		if spaceID == id {
			world.order = append(world.order[:index], world.order[index+1:]...)
			break
		}
	}
}

func runLaptopConverge(t *testing.T, profile *config.Profile, world *laptopTestWorld, reap bool) []ConvergeAction {
	t.Helper()
	state := InitialConvergeState("home", nil, reap)
	actions := make([]ConvergeAction, 0)
	for range 1000 {
		action, next, ok := LaptopConvergeStep(profile, laptopSnapshot(world), state)
		if !ok {
			return actions
		}
		actions = append(actions, action)
		applyLaptopAction(world, action)
		state = next
	}
	t.Fatal("converge did not terminate within 1000 steps")
	return actions
}

func laptopLabels(world *laptopTestWorld) []string {
	labels := make([]string, 0, len(world.order))
	for _, id := range world.order {
		for _, space := range world.spaces {
			if space.ID == id {
				labels = append(labels, space.Label)
				break
			}
		}
	}
	return labels
}

func containsSpace(world *laptopTestWorld, id wm.SpaceID) bool {
	for _, space := range world.spaces {
		if space.ID == id {
			return true
		}
	}
	return false
}

func containsString(values []string, wanted string) bool { return slices.Contains(values, wanted) }

func actionIsDestructive(action ConvergeAction) bool {
	switch action.(type) {
	case DestroySpaceOp, RehomeAndDestroyAction, MoveSpaceAction:
		return true
	case RelabelHomeAction, CreateSpaceAction, MoveWindowAction, SetLayoutAction:
		return false
	}
	return false
}
