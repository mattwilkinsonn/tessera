package engine

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

func TestResolveSlot(t *testing.T) {
	profile := engineTestProfile(t)
	window := func(id int, app, title string, display int, minimized bool) wm.Window {
		return wm.Window{ID: id, App: app, Title: title, DisplayIdx: display, Minimized: minimized}
	}
	displays := func(widths ...float64) []wm.Display {
		result := make([]wm.Display, len(widths))
		for index, width := range widths {
			result[index] = wm.Display{Idx: index + 1, Frame: wm.Frame{W: width}}
		}
		return result
	}
	t.Run("bare-name slot returns first matching non-minimized window", func(t *testing.T) {
		world := World{Windows: []wm.Window{window(10, "Arc", "", 1, false), window(11, "Linear", "Inbox", 1, false)}}
		if got, ok := ResolveSlot(profile, world, 6); !ok || got != 11 {
			t.Fatalf("ResolveSlot() = %d, %t; want 11, true", got, ok)
		}
	})
	t.Run("bare-name slot honours title disambiguation", func(t *testing.T) {
		world := World{Windows: []wm.Window{window(20, "Ghostty", "mbp | mbp", 1, false), window(21, "Ghostty", "pc | foo", 1, false)}}
		if got, ok := ResolveSlot(profile, world, 1); !ok || got != 21 {
			t.Fatalf("ResolveSlot() = %d, %t; want 21, true", got, ok)
		}
	})
	t.Run("display slot prefers candidate on resolved display", func(t *testing.T) {
		world := World{Windows: []wm.Window{window(30, "Arc", "", 1, false), window(31, "Arc", "", 2, false)}, Displays: displays(3440, 5120)}
		if got, ok := ResolveSlot(profile, world, 2); !ok || got != 31 {
			t.Fatalf("ResolveSlot() = %d, %t; want 31, true", got, ok)
		}
	})
	t.Run("display slot falls back when target display has no candidate", func(t *testing.T) {
		world := World{Windows: []wm.Window{window(40, "Arc", "", 1, false), window(41, "Arc", "", 1, false)}, Displays: displays(5120, 3440)}
		if got, ok := ResolveSlot(profile, world, 2); !ok || got != 40 {
			t.Fatalf("ResolveSlot() = %d, %t; want 40, true", got, ok)
		}
	})
	t.Run("display slot with absent display falls back", func(t *testing.T) {
		world := World{Windows: []wm.Window{window(50, "Arc", "", 1, false)}, Displays: displays(3440)}
		if got, ok := ResolveSlot(profile, world, 2); !ok || got != 50 {
			t.Fatalf("ResolveSlot() = %d, %t; want 50, true", got, ok)
		}
	})
	t.Run("minimized match resolves to no window", func(t *testing.T) {
		world := World{Windows: []wm.Window{window(60, "Linear", "Inbox", 1, true)}}
		if _, ok := ResolveSlot(profile, world, 6); ok {
			t.Fatal("minimized-only window resolved")
		}
	})
	t.Run("skips minimized candidate in favour of live match", func(t *testing.T) {
		world := World{Windows: []wm.Window{window(70, "Linear", "Inbox", 1, true), window(71, "Linear", "Roadmap", 1, false)}}
		if got, ok := ResolveSlot(profile, world, 6); !ok || got != 71 {
			t.Fatalf("ResolveSlot() = %d, %t; want 71, true", got, ok)
		}
	})
	t.Run("out-of-range slot numbers return no window", func(t *testing.T) {
		world := World{Windows: []wm.Window{window(80, "Linear", "", 1, false)}}
		for _, slot := range []int{0, -1, 10} {
			if _, ok := ResolveSlot(profile, world, slot); ok {
				t.Errorf("ResolveSlot(%d) unexpectedly succeeded", slot)
			}
		}
	})
	t.Run("slot with absent app returns no window", func(t *testing.T) {
		world := World{Windows: []wm.Window{window(90, "Arc", "", 1, false)}}
		if _, ok := ResolveSlot(profile, world, 7); ok {
			t.Fatal("slot resolved although Obsidian was absent")
		}
	})
}

func TestReconcileFlexOrder(t *testing.T) {
	for _, test := range []struct {
		name      string
		persisted []string
		current   []string
		wantOrder []string
		wantWrite []string
	}{
		{name: "empty persisted seeds order and file", current: []string{"a", "b", "c"}, wantOrder: []string{"a", "b", "c"}, wantWrite: []string{"a", "b", "c"}},
		{name: "subset current follows persisted file order", persisted: []string{"a", "b", "c"}, current: []string{"c", "a"}, wantOrder: []string{"a", "c"}, wantWrite: []string{"a", "b", "c"}},
		{name: "new current slug appends", persisted: []string{"a"}, current: []string{"a", "b"}, wantOrder: []string{"a", "b"}, wantWrite: []string{"a", "b"}},
		{name: "duplicate current slug is appended once", current: []string{"a", "a"}, wantOrder: []string{"a"}, wantWrite: []string{"a"}},
		{name: "empty string is retained only in persisted data", persisted: []string{"", "a"}, current: []string{"a", ""}, wantOrder: []string{"a"}, wantWrite: []string{"", "a"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := ReconcileFlexOrder(test.persisted, test.current)
			if diff := cmp.Diff(test.wantOrder, got.Order); diff != "" {
				t.Errorf("order mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantWrite, got.ToPersist); diff != "" {
				t.Errorf("toPersist mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLaptopFlexWindows(t *testing.T) {
	profile := engineTestProfile(t)
	window := func(id int, app string, options ...func(*wm.Window)) wm.Window {
		result := wm.Window{ID: id, App: app}
		for _, apply := range options {
			apply(&result)
		}
		return result
	}
	pinnedArcs := 0
	for _, name := range profile.LaptopPinned {
		if name == "arc" {
			pinnedArcs++
		}
	}
	t.Run("flex Arc continues pinned occurrence sequence", func(t *testing.T) {
		got := LaptopFlexWindows(profile, []wm.Window{window(1, "Arc")}, nil)
		if len(got) != 1 || got[0].Label != "arc-"+itoa(pinnedArcs+1) {
			t.Fatalf("LaptopFlexWindows() = %v, want next arc occurrence", got)
		}
	})
	t.Run("laptopStackApps app is omitted", func(t *testing.T) {
		got := LaptopFlexWindows(profile, []wm.Window{window(1, "Slack"), window(2, "Arc")}, nil)
		if len(got) != 1 || got[0].ID != 2 {
			t.Fatalf("LaptopFlexWindows() = %v, want only Arc window", got)
		}
	})
	t.Run("claimed id is omitted", func(t *testing.T) {
		got := LaptopFlexWindows(profile, []wm.Window{window(1, "Arc"), window(2, "Arc")}, map[int]struct{}{1: {}})
		if len(got) != 1 || got[0].ID != 2 {
			t.Fatalf("LaptopFlexWindows() = %v, want only id 2", got)
		}
	})
	t.Run("windows process in ascending id order", func(t *testing.T) {
		got := LaptopFlexWindows(profile, []wm.Window{window(3, "Arc"), window(1, "Arc"), window(2, "Arc")}, nil)
		want := []FlexRow{{ID: 1, Label: "arc-5"}, {ID: 2, Label: "arc-6"}, {ID: 3, Label: "arc-7"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("LaptopFlexWindows() mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("minimized and floating windows are filtered", func(t *testing.T) {
		got := LaptopFlexWindows(profile, []wm.Window{window(1, "Arc", func(w *wm.Window) { w.Minimized = true }), window(2, "Arc", func(w *wm.Window) { w.Floating = true }), window(3, "Arc")}, nil)
		if len(got) != 1 || got[0].ID != 3 {
			t.Fatalf("LaptopFlexWindows() = %v, want only id 3", got)
		}
	})
	t.Run("labels do not include lap prefix", func(t *testing.T) {
		got := LaptopFlexWindows(profile, []wm.Window{window(1, "Arc")}, nil)
		if len(got) != 1 || len(got[0].Label) >= 4 && got[0].Label[:4] == "lap-" {
			t.Fatalf("LaptopFlexWindows() = %v, expected bare slug label", got)
		}
	})
}

func itoa(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = digits[value%10]
		value /= 10
	}
	return string(buffer[index:])
}

func TestTeardownLabels(t *testing.T) {
	world := World{
		Displays: []wm.Display{{Idx: 1, SpaceIDs: []wm.SpaceID{"home", "lap-arc", "main"}}, {Idx: 2, SpaceIDs: []wm.SpaceID{"lap-old"}}},
		Spaces:   []wm.Space{{ID: "home", Label: "laptop"}, {ID: "lap-arc", Label: "lap-arc"}, {ID: "main", Label: "main"}, {ID: "lap-old", Label: "lap-old"}},
	}
	if got, want := TeardownLabels(world, "lap-"), []wm.SpaceID{"lap-arc", "lap-old"}; cmp.Diff(want, got) != "" {
		t.Fatalf("TeardownLabels() = %v, want %v", got, want)
	}
	if got := TeardownLabels(World{Spaces: []wm.Space{{ID: "blank", Label: ""}}}, "lap-"); len(got) != 0 {
		t.Fatalf("TeardownLabels() = %v, want empty", got)
	}
}

func TestStraySpaces(t *testing.T) {
	tests := []struct {
		name    string
		world   World
		wantIDs []wm.SpaceID
	}{
		{name: "empty unlabelled non-last space is reaped", world: strayWorld([]wm.Space{{ID: "s1", Label: "main"}, {ID: "s2"}}), wantIDs: []wm.SpaceID{"s2"}},
		{name: "labelled space is retained", world: strayWorld([]wm.Space{{ID: "s1", Label: "main"}, {ID: "s2", Label: "lap-arc"}}), wantIDs: []wm.SpaceID{}},
		{name: "non-sticky window retains blank space", world: strayWorld([]wm.Space{{ID: "s1", Label: "main"}, {ID: "s2", WindowIDs: []int{10}}}, wm.Window{ID: 10}), wantIDs: []wm.SpaceID{}},
		{name: "sticky-only space is empty", world: strayWorld([]wm.Space{{ID: "s1", Label: "main"}, {ID: "s2", WindowIDs: []int{10}}}, wm.Window{ID: 10, Sticky: true}), wantIDs: []wm.SpaceID{"s2"}},
		{name: "sole blank space is retained", world: strayWorld([]wm.Space{{ID: "s1"}}), wantIDs: []wm.SpaceID{}},
		{name: "all-stray display keeps last space", world: strayWorld([]wm.Space{{ID: "s1"}, {ID: "s2"}, {ID: "s3"}}), wantIDs: []wm.SpaceID{"s1", "s2"}},
		{name: "each display is evaluated independently", world: strayWorld([]wm.Space{{ID: "s1", Label: "main"}, {ID: "s2"}, {ID: "s3", Label: "main"}, {ID: "s4"}}), wantIDs: []wm.SpaceID{"s2", "s4"}},
		{name: "unreported non-sticky window retains space", world: strayWorldWithIDs([]wm.Space{{ID: "s1", Label: "main"}, {ID: "s2", WindowIDs: []int{99}}}, nil), wantIDs: []wm.SpaceID{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := StraySpaces(test.world); cmp.Diff(test.wantIDs, got) != "" {
				t.Fatalf("StraySpaces() mismatch (-want +got):\n%s", cmp.Diff(test.wantIDs, got))
			}
		})
	}
}

func strayWorld(spaces []wm.Space, windows ...wm.Window) World {
	ids := make([]wm.SpaceID, len(spaces))
	for index, space := range spaces {
		ids[index] = space.ID
	}
	return strayWorldWithIDs(spaces, []wm.Display{{Idx: 1, SpaceIDs: ids}}, windows...)
}

func strayWorldWithIDs(spaces []wm.Space, displays []wm.Display, windows ...wm.Window) World {
	if displays == nil {
		ids := make([]wm.SpaceID, len(spaces))
		for index, space := range spaces {
			ids[index] = space.ID
		}
		displays = []wm.Display{{Idx: 1, SpaceIDs: ids}}
	}
	return World{Spaces: spaces, Displays: displays, Windows: windows}
}

func TestSpawnsNeeded(t *testing.T) {
	profile := engineTestProfile(t)
	spec := profile.Windows["ghostty-wave"]
	spec.Spawn = []string{"open", "-na", "Ghostty"}
	profile.Windows["ghostty-wave"] = spec
	t.Run("returns only names without a matching or blank candidate", func(t *testing.T) {
		windows := []wm.Window{{ID: 1, App: "Ghostty", Title: "blank"}}
		got := SpawnsNeeded(profile, []string{"ghostty-wave", "ghostty-wave"}, windows)
		if diff := cmp.Diff([]string{"ghostty-wave"}, got); diff != "" {
			t.Fatalf("SpawnsNeeded() mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("does not request spawn when blank app window can be claimed", func(t *testing.T) {
		windows := []wm.Window{{ID: 1, App: "Ghostty"}}
		if got := SpawnsNeeded(profile, []string{"ghostty-wave"}, windows); len(got) != 0 {
			t.Fatalf("SpawnsNeeded() = %v, want empty", got)
		}
	})
}
