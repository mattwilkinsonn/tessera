package engine

import (
	"regexp"
	"testing"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/samber/mo"
	"gotest.tools/v3/assert"
)

func slotTestProfile() *config.Profile {
	return &config.Profile{
		Displays: config.Displays{
			G9:     config.DisplaySpec{Width: 1000},
			AW:     config.DisplaySpec{Width: 2000},
			Laptop: config.DisplaySpec{Width: 3000},
		},
		Windows: map[string]config.WindowSpec{
			"browser":       {App: "Zen", AppRE: regexp.MustCompile("^Zen$")},
			"ghostty-pc":    {App: "Ghostty", AppRE: regexp.MustCompile("^Ghostty$"), Title: "pc", TitleRE: regexp.MustCompile("^pc$")},
			"ghostty-other": {App: "Ghostty", AppRE: regexp.MustCompile("^Ghostty$"), Title: "other", TitleRE: regexp.MustCompile("^other$")},
			"zen-alias":     {App: "Arc|Zen", AppRE: regexp.MustCompile("^(Arc|Zen)$"), Spawn: []string{"zen"}},
			"spawn-alias":   {App: "Zen", AppRE: regexp.MustCompile("^Zen$"), Spawn: []string{"zen"}},
		},
		Desk: []config.DeskLayout{
			{Display: config.DisplayG9, Label: "g9", Kind: config.LayoutColumns, Tracks: [][]string{{"browser", "browser"}}},
			{Display: config.DisplayAW, Label: "aw", Kind: config.LayoutColumns, Tracks: [][]string{{"browser", "browser"}}},
		},
		LaptopPinned: []string{"browser", "browser"},
	}
}

func TestSlotKeys(t *testing.T) {
	keys := SlotKeys("desk", config.DisplayAW, []string{"browser", "arc", "browser"})
	assert.DeepEqual(t, keys, []SlotKey{"desk:aw/browser#1", "desk:aw/arc#1", "desk:aw/browser#2"})
}

func TestReserveAndClaimSlot(t *testing.T) {
	profile := slotTestProfile()
	key := SlotKey("desk:aw/browser#1")
	windows := []wm.Window{
		{ID: 10, App: "Zen", DisplayIdx: 1},
		{ID: 20, App: "Zen", DisplayIdx: 2},
	}
	memory := SlotMemory{Slots: map[SlotKey]WindowRef{key: {ID: 20, App: "Zen", Displays: "aw,g9"}}}
	reserved := Reserve(profile, memory, []SlotKey{key}, "aw,g9", windows)
	claims := NewClaimSet(profile, memory, reserved)
	id, ok := claims.ClaimSlot(windows, key, "browser", someDisplay(1))
	assert.Assert(t, ok)
	assert.Equal(t, id, 20)
}

func TestClaimSlotKeepsMinimizedSlotEmpty(t *testing.T) {
	profile := slotTestProfile()
	keys := []SlotKey{"desk:aw/browser#1", "desk:aw/browser#2"}
	windows := []wm.Window{
		{ID: 1, App: "Zen", DisplayIdx: 2, Minimized: true},
		{ID: 2, App: "Zen", DisplayIdx: 2},
	}
	memory := SlotMemory{Slots: map[SlotKey]WindowRef{
		keys[0]: {ID: 1, App: "Zen"},
		keys[1]: {ID: 2, App: "Zen"},
	}}
	claims := NewClaimSet(profile, memory, Reserve(profile, memory, keys, "aw", windows))
	_, firstOK := claims.ClaimSlot(windows, keys[0], "browser", someDisplay(2))
	second, secondOK := claims.ClaimSlot(windows, keys[1], "browser", someDisplay(2))
	assert.Assert(t, !firstOK)
	assert.Assert(t, secondOK)
	assert.Equal(t, second, 2)
}

func TestClaimSlotDoesNotTakeAnotherKeysReservation(t *testing.T) {
	profile := slotTestProfile()
	owner := SlotKey("desk:g9/browser#1")
	other := SlotKey("desk:aw/spawn-alias#1")
	window := wm.Window{ID: 1, App: "Zen", DisplayIdx: 1}
	claims := NewClaimSet(profile, SlotMemory{}, map[int]SlotKey{window.ID: owner})
	_, ok := claims.ClaimSlot([]wm.Window{window}, other, "spawn-alias", someDisplay(2))
	assert.Assert(t, !ok)
}

func TestReserveChoosesCurrentDisplaySetBeforeSequence(t *testing.T) {
	profile := slotTestProfile()
	windows := []wm.Window{{ID: 1, App: "Zen"}}
	first := SlotKey("desk:g9/browser#1")
	second := SlotKey("desk:aw/browser#1")
	memory := SlotMemory{Slots: map[SlotKey]WindowRef{
		first:  {ID: 1, App: "Zen", Displays: "other", Seq: 99},
		second: {ID: 1, App: "Zen", Displays: "aw,g9", Seq: 1},
	}}
	reserved := Reserve(profile, memory, []SlotKey{first, second}, "aw,g9", windows)
	assert.Equal(t, reserved[1], second)
}

func TestReserveUsesSequenceWhenDisplaySetsTie(t *testing.T) {
	profile := slotTestProfile()
	windows := []wm.Window{{ID: 1, App: "Zen"}}
	first := SlotKey("desk:g9/browser#1")
	second := SlotKey("desk:aw/browser#1")
	for _, test := range []struct {
		name     string
		displays string
	}{
		{name: "neither display set matches", displays: "other"},
		{name: "both display sets match", displays: "aw,g9"},
	} {
		t.Run(test.name, func(t *testing.T) {
			memory := SlotMemory{Slots: map[SlotKey]WindowRef{
				first:  {ID: 1, App: "Zen", Displays: test.displays, Seq: 1},
				second: {ID: 1, App: "Zen", Displays: test.displays, Seq: 2},
			}}
			reserved := Reserve(profile, memory, []SlotKey{first, second}, "aw,g9", windows)
			assert.Equal(t, reserved[1], second)
		})
	}
}

func TestReserveIgnoresChangedSpecAndApp(t *testing.T) {
	profile := slotTestProfile()
	ghosttyKey := SlotKey("desk:g9/ghostty-pc#1")
	aliasKey := SlotKey("desk:g9/zen-alias#1")
	memory := SlotMemory{Slots: map[SlotKey]WindowRef{
		ghosttyKey: {ID: 1, App: "Ghostty"},
		aliasKey:   {ID: 2, App: "Arc"},
	}}
	windows := []wm.Window{
		{ID: 1, App: "Ghostty", Title: "other"},
		{ID: 2, App: "Zen"},
		{ID: 3, App: "Zen"},
	}
	reserved := Reserve(profile, memory, []SlotKey{ghosttyKey, aliasKey}, "g9", windows)
	assert.Equal(t, len(reserved), 0)
	claims := NewClaimSet(profile, memory, reserved)
	id, ok := claims.ClaimSlot(windows, SlotKey("desk:g9/ghostty-other#1"), "ghostty-other", noneDisplay())
	assert.Assert(t, ok)
	assert.Equal(t, id, 1)

	changedKey := SlotKey("desk:g9/browser#1")
	changed := SlotMemory{Slots: map[SlotKey]WindowRef{changedKey: {ID: 3, App: "Arc"}}}
	changedReserved := Reserve(profile, changed, []SlotKey{changedKey}, "g9", windows)
	assert.Equal(t, len(changedReserved), 0)
	changedClaims := NewClaimSet(profile, changed, changedReserved)
	changedID, changedOK := changedClaims.ClaimSlot(windows, changedKey, "browser", noneDisplay())
	assert.Assert(t, changedOK)
	assert.Equal(t, changedID, 2)
}

func TestDeskPlanReturnsStableSlotsAcrossTopologies(t *testing.T) {
	profile := slotTestProfile()
	profile.Desk = []config.DeskLayout{
		{Display: config.DisplayG9, Label: "g9", Kind: config.LayoutColumns, Tracks: [][]string{{"browser", "browser"}}},
		{Display: config.DisplayAW, Label: "aw", Kind: config.LayoutColumns, Tracks: [][]string{{"browser", "browser"}}},
	}
	profile.Topologies = []config.Topology{{
		Name: "aw-laptop", Displays: []config.DisplayName{config.DisplayAW, config.DisplayLaptop},
		Desk: []config.DeskLayout{
			{Display: config.DisplayAW, Label: "aw", Kind: config.LayoutColumns, Tracks: [][]string{{"browser", "browser", "browser", "browser"}}},
		},
	}}
	windows := []wm.Window{
		{ID: 1, App: "Zen", DisplayIdx: 1}, {ID: 2, App: "Zen", DisplayIdx: 1},
		{ID: 3, App: "Zen", DisplayIdx: 2}, {ID: 4, App: "Zen", DisplayIdx: 2},
	}
	full := slotWorld(windows, true, true, true)
	_, filled := DeskPlan(profile, full)
	memory := RecordSlots(SlotMemory{Slots: map[SlotKey]WindowRef{}}, filled, DisplaySet(profile, full.Displays), windows)
	assert.Equal(t, memory.Slots["desk:aw/browser#1"].ID, 3)
	assert.Equal(t, memory.Slots["desk:aw/browser#2"].ID, 4)
	assert.Equal(t, memory.Slots["desk:g9/browser#1"].ID, 1)
	assert.Equal(t, memory.Slots["desk:g9/browser#2"].ID, 2)

	awLaptop := slotWorld([]wm.Window{windows[3], windows[1], windows[2], windows[0]}, false, true, true)
	awLaptop.Slots = PruneSlots(profile, memory, awLaptop.Windows)
	_, filled = DeskPlan(profile, awLaptop)
	memory = RecordSlots(awLaptop.Slots, filled, DisplaySet(profile, awLaptop.Displays), awLaptop.Windows)
	assert.Equal(t, memory.Slots["desk:aw/browser#1"].ID, 3)
	assert.Equal(t, memory.Slots["desk:aw/browser#2"].ID, 4)

	full.Windows = []wm.Window{windows[3], windows[1], windows[0], windows[2]}
	full.Slots = PruneSlots(profile, memory, full.Windows)
	_, filled = DeskPlan(profile, full)
	assert.Equal(t, filled["desk:aw/browser#1"], 3)
	assert.Equal(t, filled["desk:aw/browser#2"], 4)
	assert.Equal(t, filled["desk:g9/browser#1"], 1)
	assert.Equal(t, filled["desk:g9/browser#2"], 2)
}

func TestDeskPlanReturnsG9WindowFromAwSlotAfterClose(t *testing.T) {
	profile := slotTestProfile()
	profile.Desk = []config.DeskLayout{
		{Display: config.DisplayG9, Label: "g9", Kind: config.LayoutColumns, Tracks: [][]string{{"browser", "browser"}}},
		{Display: config.DisplayAW, Label: "aw", Kind: config.LayoutColumns, Tracks: [][]string{{"browser", "browser"}}},
	}
	profile.Topologies = []config.Topology{{
		Name: "aw-laptop", Displays: []config.DisplayName{config.DisplayAW, config.DisplayLaptop},
		Desk: []config.DeskLayout{{Display: config.DisplayAW, Label: "aw", Kind: config.LayoutColumns, Tracks: [][]string{{"browser", "browser"}}}},
	}}
	windows := []wm.Window{
		{ID: 1, App: "Zen", DisplayIdx: 1}, {ID: 2, App: "Zen", DisplayIdx: 1},
		{ID: 3, App: "Zen", DisplayIdx: 2}, {ID: 4, App: "Zen", DisplayIdx: 2},
	}
	full := slotWorld(windows, true, true, true)
	_, filled := DeskPlan(profile, full)
	memory := RecordSlots(SlotMemory{Slots: map[SlotKey]WindowRef{}}, filled, DisplaySet(profile, full.Displays), windows)
	awLaptop := slotWorld([]wm.Window{windows[2], windows[0], windows[1]}, false, true, true)
	awLaptop.Slots = PruneSlots(profile, memory, awLaptop.Windows)
	_, filled = DeskPlan(profile, awLaptop)
	assert.Equal(t, filled["desk:aw/browser#1"], 3)
	assert.Equal(t, filled["desk:aw/browser#2"], 1)
	memory = RecordSlots(awLaptop.Slots, filled, DisplaySet(profile, awLaptop.Displays), awLaptop.Windows)
	full.Windows = []wm.Window{windows[1], windows[2], windows[0]}
	full.Slots = PruneSlots(profile, memory, full.Windows)
	_, filled = DeskPlan(profile, full)
	assert.Equal(t, filled["desk:g9/browser#1"], 1)
	assert.Equal(t, filled["desk:g9/browser#2"], 2)
	assert.Assert(t, filled["desk:aw/browser#2"] != 1)
}

func TestDeskPlanDoesNotMoveG9WindowIntoClosedAwSlot(t *testing.T) {
	profile := slotTestProfile()
	profile.Desk = []config.DeskLayout{
		{Display: config.DisplayG9, Label: "g9", Kind: config.LayoutColumns, Tracks: [][]string{{"browser", "browser"}}},
		{Display: config.DisplayAW, Label: "aw", Kind: config.LayoutColumns, Tracks: [][]string{{"browser", "browser"}}},
	}
	windows := []wm.Window{
		{ID: 1, App: "Zen", DisplayIdx: 1}, {ID: 2, App: "Zen", DisplayIdx: 1},
		{ID: 3, App: "Zen", DisplayIdx: 2}, {ID: 4, App: "Zen", DisplayIdx: 2},
	}
	world := slotWorld(windows, true, true, true)
	_, filled := DeskPlan(profile, world)
	memory := RecordSlots(SlotMemory{Slots: map[SlotKey]WindowRef{}}, filled, DisplaySet(profile, world.Displays), windows)
	world.Windows = []wm.Window{windows[0], windows[2], windows[1]}
	world.Slots = PruneSlots(profile, memory, world.Windows)
	_, first := DeskPlan(profile, world)
	_, second := DeskPlan(profile, world)
	assert.Equal(t, first["desk:g9/browser#1"], 1)
	assert.Equal(t, first["desk:g9/browser#2"], 2)
	assert.Assert(t, first["desk:aw/browser#2"] != 1 && first["desk:aw/browser#2"] != 2)
	assert.DeepEqual(t, second, first)
}

func TestDeskPlanKeepsSlotsAcrossLaptopModeAndSharedDisplays(t *testing.T) {
	profile := slotTestProfile()
	profile.Desk = []config.DeskLayout{
		{Display: config.DisplayLaptop, Label: "laptop-a", Kind: config.LayoutColumns, Tracks: [][]string{{"browser"}}},
		{Display: config.DisplayLaptop, Label: "laptop-b", Kind: config.LayoutColumns, Tracks: [][]string{{"browser"}}},
	}
	world := slotWorld([]wm.Window{{ID: 1, App: "Zen", DisplayIdx: 3}, {ID: 2, App: "Zen", DisplayIdx: 3}}, false, false, true)
	_, deskFilled := DeskPlan(profile, world)
	deskMemory := RecordSlots(SlotMemory{Slots: map[SlotKey]WindowRef{}}, deskFilled, "laptop", world.Windows)
	world.Slots = deskMemory
	world.Spaces = append(world.Spaces,
		wm.Space{ID: "pin-1", Label: "lap-browser", DisplayIdx: 3},
		wm.Space{ID: "pin-2", Label: "lap-browser-2", DisplayIdx: 3},
	)
	world.Windows[0].SpaceID = "pin-1"
	world.Windows[1].SpaceID = "pin-2"
	state := ConvergeState{Phase: PhaseA, LaptopIdx: 3, HomeSpace: "s3", Filled: make(map[SlotKey]int)}
	_, state, emitted := stepPinned(profile, world, state)
	assert.Assert(t, !emitted)
	assert.Equal(t, state.Filled["laptop:laptop/browser#1"], 1)
	assert.Equal(t, state.Filled["laptop:laptop/browser#2"], 2)
	laptopMemory := RecordSlots(deskMemory, state.Filled, "laptop", world.Windows)
	assert.Equal(t, laptopMemory.Slots["desk:laptop/browser#1"], deskMemory.Slots["desk:laptop/browser#1"])
	assert.Equal(t, laptopMemory.Slots["desk:laptop/browser#2"], deskMemory.Slots["desk:laptop/browser#2"])
	world.Slots = laptopMemory
	_, filled := DeskPlan(profile, world)
	assert.Equal(t, filled["desk:laptop/browser#1"], 1)
	assert.Equal(t, filled["desk:laptop/browser#2"], 2)
}

func TestTitleDriftMovesMemoryToCurrentSpec(t *testing.T) {
	profile := slotTestProfile()
	profile.Windows["ghostty-pc"] = config.WindowSpec{App: "Ghostty", AppRE: regexp.MustCompile("^Ghostty$"), Title: "pc", TitleRE: regexp.MustCompile("^pc$")}
	profile.Windows["ghostty-other"] = config.WindowSpec{App: "Ghostty", AppRE: regexp.MustCompile("^Ghostty$"), Title: "other", TitleRE: regexp.MustCompile("^other$")}
	profile.Desk = []config.DeskLayout{{Display: config.DisplayG9, Label: "g9", Kind: config.LayoutColumns, Tracks: [][]string{{"ghostty-other"}}}}
	oldKey := SlotKey("desk:g9/ghostty-pc#1")
	newKey := SlotKey("desk:g9/ghostty-other#1")
	memory := SlotMemory{Slots: map[SlotKey]WindowRef{oldKey: {ID: 1, App: "Ghostty"}}}
	windows := []wm.Window{{ID: 1, App: "Ghostty", Title: "other"}}
	world := slotWorld(windows, true, false, false)
	world.Slots = PruneSlots(profile, memory, windows)
	_, filled := DeskPlan(profile, world)
	assert.Equal(t, filled[newKey], 1)
	updated := PruneSlots(profile, RecordSlots(world.Slots, filled, DisplaySet(profile, world.Displays), windows), windows)
	assert.Equal(t, len(updated.Slots), 1)
	assert.Equal(t, updated.Slots[newKey].ID, 1)
	assert.Assert(t, updated.Slots[oldKey].ID == 0)
}

func TestDeskPlanUsesDistinctKeysForLayoutsOnOneDisplay(t *testing.T) {
	profile := slotTestProfile()
	profile.Desk = []config.DeskLayout{
		{Display: config.DisplayG9, Label: "first", Kind: config.LayoutColumns, Tracks: [][]string{{"browser"}}},
		{Display: config.DisplayG9, Label: "second", Kind: config.LayoutColumns, Tracks: [][]string{{"browser"}}},
	}
	world := slotWorld([]wm.Window{{ID: 1, App: "Zen", DisplayIdx: 1}, {ID: 2, App: "Zen", DisplayIdx: 1}}, true, false, false)
	_, filled := DeskPlan(profile, world)
	assert.Equal(t, filled["desk:g9/browser#1"], 1)
	assert.Equal(t, filled["desk:g9/browser#2"], 2)
}

func TestPruneSlotsAndRecordSlots(t *testing.T) {
	profile := slotTestProfile()
	keys := []SlotKey{
		"desk:g9/browser#1", "desk:g9/browser#2", "desk:g9/ghostty-pc#1", "desk:g9/ghostty-other#1", "desk:g9/zen-alias#1",
	}
	memory := SlotMemory{Seq: 7, Slots: map[SlotKey]WindowRef{
		keys[0]: {ID: 1, App: "Zen"},
		keys[1]: {ID: 6, App: "Zen"},
		keys[2]: {ID: 2, App: "Ghostty"},
		keys[3]: {ID: 3, App: "Ghostty"},
		keys[4]: {ID: 4, App: "Arc"},
	}}
	windows := []wm.Window{
		{ID: 1, App: "Zen", Minimized: true},
		{ID: 2, App: "Ghostty", Title: "pc", Floating: true},
		{ID: 3, App: "Ghostty", Title: "other"},
		{ID: 4, App: "Zen"},
		{ID: 5, App: "Zen"},
	}
	pruned := PruneSlots(profile, memory, windows)
	assert.Equal(t, len(pruned.Slots), 3)
	assert.Assert(t, pruned.Slots[keys[0]].ID == 1 && pruned.Slots[keys[2]].ID == 2)
	filled := map[SlotKey]int{keys[0]: 5}
	recorded := RecordSlots(pruned, filled, "aw,g9", windows)
	assert.Equal(t, recorded.Seq, uint64(8))
	assert.Equal(t, recorded.Slots[keys[0]].ID, 5)
	assert.Equal(t, recorded.Slots[keys[0]].Displays, "aw,g9")
	assert.Equal(t, recorded.Slots[keys[0]].Seq, uint64(8))
	assert.Equal(t, recorded.Slots[keys[2]].ID, 2)
	assert.Equal(t, len(recorded.Slots), 3)
}

func slotWorld(windows []wm.Window, g9, aw, laptop bool) World {
	world := World{Windows: windows}
	for index, display := range []struct {
		present bool
		width   float64
	}{
		{present: g9, width: 1000},
		{present: aw, width: 2000},
		{present: laptop, width: 3000},
	} {
		if !display.present {
			continue
		}
		spaceID := wm.SpaceID("s" + string(rune('1'+index)))
		world.Displays = append(world.Displays, wm.Display{Idx: index + 1, Frame: wm.Frame{W: display.width}, SpaceIDs: []wm.SpaceID{spaceID}})
		world.Spaces = append(world.Spaces, wm.Space{ID: spaceID, DisplayIdx: index + 1})
	}
	return world
}

func someDisplay(index int) mo.Option[int] { return mo.Some(index) }

func noneDisplay() mo.Option[int] { return mo.None[int]() }

func TestDisplaySetNamesSameWidthDisplaysByDeviceID(t *testing.T) {
	profile := slotTestProfile()
	profile.Displays.AW = config.DisplaySpec{Width: 1000, DeviceID: "device-aw"}
	displays := []wm.Display{
		{Idx: 1, DeviceID: "device-g9", Frame: wm.Frame{W: 1000}},
		{Idx: 2, DeviceID: "device-aw", Frame: wm.Frame{W: 1000}},
	}
	assert.Equal(t, DisplaySet(profile, displays), "aw,g9")
}
