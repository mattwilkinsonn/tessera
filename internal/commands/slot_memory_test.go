package commands

import (
	"context"
	"regexp"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
	"gotest.tools/v3/assert"
)

type observingDriver struct {
	*fake.Driver
	beforeQueryWindows func()
	queryOnce          sync.Once
}

func (d *observingDriver) QueryWindows(ctx context.Context) ([]wm.Window, error) {
	d.queryOnce.Do(func() {
		if d.beforeQueryWindows != nil {
			d.beforeQueryWindows()
		}
	})
	return d.Driver.QueryWindows(ctx)
}

func TestApplyRemembersWindowAssignments(t *testing.T) {
	env := commandEnv(t)
	profile := testProfile(t)
	first := deskWorld()
	assert.NilError(t, Apply(t.Context(), profile, first, env))
	memory, err := effects.ReadSlots(env.Paths.WindowSlots)
	assert.NilError(t, err)
	assert.Assert(t, len(memory.Slots) > 0)

	second := deskWorld()
	assert.NilError(t, Apply(t.Context(), profile, second, env))
	fresh, err := second.QueryWindows(t.Context())
	assert.NilError(t, err)
	for key, ref := range memory.Slots {
		assert.Assert(t, slices.ContainsFunc(fresh, func(window wm.Window) bool { return window.ID == ref.ID }))
		assert.Assert(t, key != "")
	}
}

func TestLaptopReadsAndWritesSlotMemoryWithoutChangingDeskKeys(t *testing.T) {
	env := commandEnv(t)
	profile := testProfile(t)
	assert.NilError(t, effects.WriteSlots(env.Paths.WindowSlots, engine.SlotMemory{Seq: 4, Slots: map[engine.SlotKey]engine.WindowRef{
		"desk:g9/arc#1": {ID: 1, App: "Arc", Displays: "g9", Seq: 4},
	}}))
	driver := fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(1728, 1117)}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 1, Label: "laptop"}},
		Windows:  []fake.SeedWindow{{ID: 1, App: "Arc", SpaceIndex: 1}},
	})
	result, err := Laptop(t.Context(), profile, driver, env)
	assert.NilError(t, err)
	assert.Equal(t, result, LaptopRan)
	memory, err := effects.ReadSlots(env.Paths.WindowSlots)
	assert.NilError(t, err)
	assert.Equal(t, memory.Seq, uint64(5))
	assert.Equal(t, memory.Slots["desk:g9/arc#1"].ID, 1)
	assert.Assert(t, memory.Slots["laptop:laptop/arc#1"].ID != 0)
}

func TestApplyAndLaptopShareSlotLock(t *testing.T) {
	for _, test := range []struct {
		name       string
		applyFirst bool
	}{
		{name: "apply blocks laptop query", applyFirst: true},
		{name: "laptop blocks apply query", applyFirst: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := commandEnv(t)
			profile := testProfile(t)
			releaseFirst := make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(releaseFirst) }) }
			defer release()
			firstEntered := make(chan struct{})
			secondEntered := make(chan struct{}, 1)
			desk := deskWorld()
			lap := fake.New(fake.Seed{
				Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(1728, 1117)}},
				Spaces:   []fake.SeedSpace{{DisplayIdx: 1, Label: "laptop"}},
				Windows:  []fake.SeedWindow{{ID: 1, App: "Arc", SpaceIndex: 1}},
			})
			firstDriver := &observingDriver{Driver: lap, beforeQueryWindows: func() { close(firstEntered); <-releaseFirst }}
			secondDriver := &observingDriver{Driver: desk, beforeQueryWindows: func() { secondEntered <- struct{}{} }}
			if test.applyFirst {
				firstDriver.Driver = desk
				secondDriver.Driver = lap
			}
			run := func(apply bool, driver *observingDriver) error {
				if apply {
					return Apply(t.Context(), profile, driver, env)
				}
				_, err := Laptop(t.Context(), profile, driver, env)
				return err
			}
			firstDone := make(chan error, 1)
			go func() { firstDone <- run(test.applyFirst, firstDriver) }()
			select {
			case <-firstEntered:
			case err := <-firstDone:
				t.Fatalf("first command returned before entering query: %v", err)
			case <-time.After(time.Second):
				t.Fatal("first command did not reach query")
			}
			secondDone := make(chan error, 1)
			go func() { secondDone <- run(!test.applyFirst, secondDriver) }()
			select {
			case <-secondEntered:
				t.Fatal("second command queried windows while first held slot lock")
			case err := <-secondDone:
				t.Fatalf("second command returned while first held slot lock: %v", err)
			case <-time.After(40 * time.Millisecond):
			}
			release()
			assert.NilError(t, <-firstDone)
			assert.NilError(t, <-secondDone)
		})
	}
}

func TestApplyDropsRestartedWindowIds(t *testing.T) {
	env := commandEnv(t)
	profile := testProfile(t)
	assert.NilError(t, effects.WriteSlots(env.Paths.WindowSlots, engine.SlotMemory{Seq: 5, Slots: map[engine.SlotKey]engine.WindowRef{
		"desk:g9/arc#1": {ID: 99, App: "Arc", Displays: "g9", Seq: 5},
	}}))
	driver := fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 1, Label: "main"}},
		Windows: []fake.SeedWindow{
			{ID: 101, App: "Arc", SpaceIndex: 1}, {ID: 102, App: "Obsidian", SpaceIndex: 1},
			{ID: 103, App: "Ghostty", Title: "pc", SpaceIndex: 1}, {ID: 104, App: "Ghostty", Title: "mbp", SpaceIndex: 1}, {ID: 105, App: "Code", SpaceIndex: 1},
		},
	})
	assert.NilError(t, Apply(t.Context(), profile, driver, env))
	memory, err := effects.ReadSlots(env.Paths.WindowSlots)
	assert.NilError(t, err)
	assert.Assert(t, memory.Slots["desk:g9/arc#1"].ID != 99)
}

func TestApplyRestoresWindowSlotsAfterReorderedQueries(t *testing.T) {
	env := commandEnv(t)
	profile := testProfile(t)
	first := fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{
			{Idx: 1, Frame: displayFrame(5120, 2160)},
			{Idx: 2, Frame: displayFrame(3440, 1440)},
			{Idx: 3, Frame: displayFrame(1728, 1117)},
		},
		Spaces: []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 2}, {DisplayIdx: 3}},
		Windows: []fake.SeedWindow{
			{ID: 1, App: "Arc", SpaceIndex: 1}, {ID: 2, App: "Obsidian", SpaceIndex: 1},
			{ID: 3, App: "Ghostty", Title: "pc", SpaceIndex: 1}, {ID: 4, App: "Ghostty", Title: "mbp", SpaceIndex: 1},
			{ID: 5, App: "Code", SpaceIndex: 1}, {ID: 6, App: "Linear", SpaceIndex: 2},
			{ID: 7, App: "Arc", SpaceIndex: 2}, {ID: 8, App: "Akiflow", SpaceIndex: 2}, {ID: 9, App: "Arc", SpaceIndex: 2},
		},
	})
	assert.NilError(t, Apply(t.Context(), profile, first, env))
	memory, err := effects.ReadSlots(env.Paths.WindowSlots)
	assert.NilError(t, err)
	assert.Equal(t, memory.Slots["desk:aw/arc#1"].ID, 7)
	assert.Equal(t, memory.Slots["desk:aw/arc#2"].ID, 9)
	second := fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{
			{Idx: 1, Frame: displayFrame(5120, 2160)},
			{Idx: 2, Frame: displayFrame(3440, 1440)},
			{Idx: 3, Frame: displayFrame(1728, 1117)},
		},
		Spaces: []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 2}, {DisplayIdx: 3}},
		Windows: []fake.SeedWindow{
			{ID: 1, App: "Arc", SpaceIndex: 1}, {ID: 2, App: "Obsidian", SpaceIndex: 1},
			{ID: 3, App: "Ghostty", Title: "pc", SpaceIndex: 1}, {ID: 4, App: "Ghostty", Title: "mbp", SpaceIndex: 1},
			{ID: 5, App: "Code", SpaceIndex: 1}, {ID: 6, App: "Linear", SpaceIndex: 2},
			{ID: 7, App: "Arc", SpaceIndex: 2}, {ID: 8, App: "Akiflow", SpaceIndex: 2},
		},
	})
	secondDriver := &reorderWindowsDriver{Driver: second}
	assert.NilError(t, Apply(t.Context(), profile, secondDriver, env))
	windows, err := secondDriver.QueryWindows(t.Context())
	assert.NilError(t, err)
	assert.Assert(t, slices.ContainsFunc(windows, func(window wm.Window) bool {
		return window.ID == memory.Slots["desk:g9/arc#1"].ID && window.SpaceID == "1"
	}))
	assert.Equal(t, memory.Slots["desk:g9/arc#1"].ID, 1)
}

func TestLaptopUsesPersistedPinnedSlotsAfterReorderedQueries(t *testing.T) {
	env := commandEnv(t)
	profile := testProfile(t)
	driver := fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(1728, 1117)}},
		Spaces: []fake.SeedSpace{
			{DisplayIdx: 1, Label: "laptop"}, {DisplayIdx: 1, Label: "lap-arc"},
			{DisplayIdx: 1, Label: "lap-ghostty-wave"}, {DisplayIdx: 1, Label: "lap-arc-2"},
			{DisplayIdx: 1, Label: "lap-ghostty-mbp"}, {DisplayIdx: 1, Label: "lap-linear"},
			{DisplayIdx: 1, Label: "lap-obsidian"}, {DisplayIdx: 1, Label: "lap-arc-3"},
			{DisplayIdx: 1, Label: "lap-akiflow"}, {DisplayIdx: 1, Label: "lap-arc-4"},
		},
		Windows: []fake.SeedWindow{
			{ID: 1, App: "Arc", SpaceIndex: 2}, {ID: 2, App: "Ghostty", Title: "pc", SpaceIndex: 3},
			{ID: 3, App: "Arc", SpaceIndex: 4}, {ID: 4, App: "Ghostty", Title: "mbp", SpaceIndex: 5},
			{ID: 5, App: "Linear", SpaceIndex: 6}, {ID: 6, App: "Obsidian", SpaceIndex: 7},
			{ID: 7, App: "Arc", SpaceIndex: 8}, {ID: 8, App: "Akiflow", SpaceIndex: 9},
			{ID: 9, App: "Arc", SpaceIndex: 10},
		},
	})
	first := &reorderWindowsDriver{Driver: driver}
	assert.NilError(t, effects.WriteSlots(env.Paths.WindowSlots, engine.SlotMemory{Seq: 4, Slots: map[engine.SlotKey]engine.WindowRef{
		"desk:g9/arc#1":       {ID: 1, App: "Arc", Displays: "g9", Seq: 4},
		"laptop:laptop/arc#1": {ID: 1, App: "Arc", Displays: "laptop", Seq: 3},
		"laptop:laptop/arc#2": {ID: 3, App: "Arc", Displays: "laptop", Seq: 3},
		"laptop:laptop/arc#3": {ID: 7, App: "Arc", Displays: "laptop", Seq: 3},
		"laptop:laptop/arc#4": {ID: 9, App: "Arc", Displays: "laptop", Seq: 3},
	}}))
	result, err := Laptop(t.Context(), profile, first, env)
	assert.NilError(t, err)
	assert.Equal(t, result, LaptopRan)
	memory, err := effects.ReadSlots(env.Paths.WindowSlots)
	assert.NilError(t, err)
	assert.Equal(t, memory.Slots["laptop:laptop/arc#1"].ID, 1)
	assert.Equal(t, memory.Slots["laptop:laptop/arc#2"].ID, 3)
	assert.Equal(t, memory.Slots["desk:g9/arc#1"].ID, 1)

}

func TestApplyPersistsTitleDriftUnderCurrentWindowSpec(t *testing.T) {
	env := commandEnv(t)
	profile := testProfile(t)
	profile.Windows["ghostty-pc"] = config.WindowSpec{App: "Ghostty", AppRE: regexp.MustCompile("^Ghostty$"), Title: "pc", TitleRE: regexp.MustCompile("^pc$")}
	profile.Windows["ghostty-other"] = config.WindowSpec{App: "Ghostty", AppRE: regexp.MustCompile("^Ghostty$"), Title: "other", TitleRE: regexp.MustCompile("^other$")}
	profile.Desk = []config.DeskLayout{{Display: config.DisplayG9, Label: "main", Kind: config.LayoutColumns, Tracks: [][]string{{"ghostty-pc", "ghostty-other"}}}}
	driver := fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 1, Label: "main"}},
		Windows:  []fake.SeedWindow{{ID: 1, App: "Ghostty", Title: "other", SpaceIndex: 1}},
	})
	assert.NilError(t, effects.WriteSlots(env.Paths.WindowSlots, engine.SlotMemory{Slots: map[engine.SlotKey]engine.WindowRef{
		"desk:g9/ghostty-pc#1": {ID: 1, App: "Ghostty", Displays: "g9", Seq: 1},
	}}))
	assert.NilError(t, Apply(t.Context(), profile, driver, env))
	memory, err := effects.ReadSlots(env.Paths.WindowSlots)
	assert.NilError(t, err)
	assert.Equal(t, len(memory.Slots), 1)
	assert.Equal(t, memory.Slots["desk:g9/ghostty-other#1"].ID, 1)
	assert.Equal(t, memory.Slots["desk:g9/ghostty-other#1"].Displays, "g9")

	returned := fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 1, Label: "main"}},
		Windows:  []fake.SeedWindow{{ID: 1, App: "Ghostty", Title: "pc", SpaceIndex: 1}},
	})
	assert.NilError(t, Apply(t.Context(), profile, returned, env))
	memory, err = effects.ReadSlots(env.Paths.WindowSlots)
	assert.NilError(t, err)
	assert.Equal(t, len(memory.Slots), 1)
	assert.Equal(t, memory.Slots["desk:g9/ghostty-pc#1"].ID, 1)
	assert.Equal(t, memory.Slots["desk:g9/ghostty-pc#1"].Displays, "g9")

}

type reorderWindowsDriver struct {
	*fake.Driver
	queryCount int
}

func (d *reorderWindowsDriver) QueryWindows(ctx context.Context) ([]wm.Window, error) {
	d.queryCount++
	windows, err := d.Driver.QueryWindows(ctx)
	if err != nil {
		return nil, err
	}
	if d.queryCount%2 == 0 {
		for left, right := 0, len(windows)-1; left < right; left, right = left+1, right-1 {
			windows[left], windows[right] = windows[right], windows[left]
		}
	}
	return windows, nil
}
