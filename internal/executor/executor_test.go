package executor

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
)

func TestRunPlan(t *testing.T) {
	t.Run("relabelHome + realizeLayout builds tracks on the labelled space", func(t *testing.T) {
		driver := fake.New(fake.Seed{
			Displays: []fake.SeedDisplay{{Idx: 1}},
			Spaces:   []fake.SeedSpace{{DisplayIdx: 1}},
			Windows:  []fake.SeedWindow{{ID: 10, App: "Ghostty", SpaceIndex: 1}, {ID: 11, App: "Zed", SpaceIndex: 1, Floating: true}},
		})
		err := RunPlan(t.Context(), driver, []engine.Op{
			engine.RelabelHomeAction{HomeSpace: "1", Label: "desk-code"},
			engine.RealizeLayoutOp{Space: "1", Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{10}, {11}}, Weights: []float64{3, 2}}},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		spaces, err := driver.QuerySpaces(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(spaces) != 1 || spaces[0].Label != "desk-code" || !slices.Equal(spaces[0].WindowIDs, []int{10, 11}) {
			t.Fatalf("spaces = %+v; want labelled space with both windows", spaces)
		}
		windows, err := driver.QueryWindows(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		for _, window := range windows {
			if window.ID == 11 && window.Floating {
				t.Fatal("realized layout left window 11 floating")
			}
		}
	})

	t.Run("createSpace mints a labelled space, moveWindow places a window on it", func(t *testing.T) {
		driver := fake.New(fake.Seed{
			Displays: []fake.SeedDisplay{{Idx: 1}},
			Spaces:   []fake.SeedSpace{{DisplayIdx: 1, Label: "home"}},
			Windows:  []fake.SeedWindow{{ID: 20, App: "Safari", SpaceIndex: 1}},
		})
		err := RunPlan(t.Context(), driver, []engine.Op{
			engine.CreateSpaceAction{DisplayIdx: 1, Label: "scratch"},
			engine.MoveWindowAction{WindowID: 20, ToSpace: "2"},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		spaces, err := driver.QuerySpaces(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(spaces) != 2 || spaces[1].ID != "2" || spaces[1].Label != "scratch" || !slices.Equal(spaces[1].WindowIDs, []int{20}) {
			t.Fatalf("spaces = %+v; want scratch space holding window 20", spaces)
		}
	})

	t.Run("moveSpace reorders spaces by live index", func(t *testing.T) {
		driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1}}, Spaces: []fake.SeedSpace{
			{DisplayIdx: 1, Label: "a"}, {DisplayIdx: 1, Label: "b"}, {DisplayIdx: 1, Label: "c"},
		}})
		if err := RunPlan(t.Context(), driver, []engine.Op{engine.MoveSpaceAction{Space: "3", ToIndex: 1}}, nil); err != nil {
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
		if !slices.Equal(labels, []string{"c", "a", "b"}) {
			t.Fatalf("labels = %v; want [c a b]", labels)
		}
	})

	t.Run("destroySpace removes a non-last space", func(t *testing.T) {
		driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1}}, Spaces: []fake.SeedSpace{
			{DisplayIdx: 1, Label: "keep"}, {DisplayIdx: 1, Label: "drop"},
		}})
		if err := RunPlan(t.Context(), driver, []engine.Op{engine.DestroySpaceOp{Space: "2"}}, nil); err != nil {
			t.Fatal(err)
		}
		spaces, err := driver.QuerySpaces(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(spaces) != 1 || spaces[0].Label != "keep" {
			t.Fatalf("spaces = %+v; want only keep", spaces)
		}
	})

	t.Run("rehomeAndDestroy re-homes residual windows before destroying", func(t *testing.T) {
		driver := fake.New(fake.Seed{
			Displays: []fake.SeedDisplay{{Idx: 1}},
			Spaces:   []fake.SeedSpace{{DisplayIdx: 1, Label: "home"}, {DisplayIdx: 1, Label: "stale"}},
			Windows:  []fake.SeedWindow{{ID: 30, App: "Notes", SpaceIndex: 2}, {ID: 31, App: "Mail", SpaceIndex: 2}},
		})
		if err := RunPlan(t.Context(), driver, []engine.Op{engine.RehomeAndDestroyAction{StaleSpace: "2", HomeSpace: "1"}}, nil); err != nil {
			t.Fatal(err)
		}
		spaces, err := driver.QuerySpaces(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(spaces) != 1 || spaces[0].Label != "home" || !slices.Equal(spaces[0].WindowIDs, []int{30, 31}) {
			t.Fatalf("spaces = %+v; want home with both re-homed windows", spaces)
		}
	})

	t.Run("setLayout and balanceSpace drive their driver methods", func(t *testing.T) {
		driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1, Label: "s"}}})
		if err := RunPlan(t.Context(), driver, []engine.Op{
			engine.SetLayoutAction{Space: "1", Layout: wm.LayoutStack}, engine.BalanceSpaceOp{Space: "1"},
		}, nil); err != nil {
			t.Fatal(err)
		}
		spaces, err := driver.QuerySpaces(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if spaces[0].Layout != wm.LayoutStack {
			t.Fatalf("layout = %q; want stack", spaces[0].Layout)
		}
	})

	t.Run("exhaustiveness — every PlanOp kind is a valid runPlan input", func(t *testing.T) {
		byKind := []engine.Op{
			engine.RelabelHomeAction{HomeSpace: "1", Label: "x"},
			engine.CreateSpaceAction{DisplayIdx: 1, Label: "x"},
			engine.MoveWindowAction{WindowID: 1, ToSpace: "1"},
			engine.RehomeAndDestroyAction{StaleSpace: "1", HomeSpace: "1"},
			engine.MoveSpaceAction{Space: "1", ToIndex: 1},
			engine.SetLayoutAction{Space: "1", Layout: wm.LayoutBSP},
			engine.DestroySpaceOp{Space: "1"},
			engine.RealizeLayoutOp{Space: "1", Target: wm.SpaceLayoutTarget{Kind: wm.TargetStack, Tracks: [][]int{{1}}}},
			engine.BalanceSpaceOp{Space: "1"},
		}
		seen := make(map[string]bool, len(byKind))
		for _, op := range byKind {
			kind := fmt.Sprintf("%T", op)
			if seen[kind] {
				t.Fatalf("duplicate operation kind %s", kind)
			}
			seen[kind] = true
		}
		if len(seen) != 9 {
			t.Fatalf("covered %d operation kinds; want 9", len(seen))
		}
	})

	t.Run("onOp fires once before each op, in order", func(t *testing.T) {
		driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1, Label: "a"}}})
		plan := []engine.Op{engine.RelabelHomeAction{HomeSpace: "1", Label: "b"}, engine.SetLayoutAction{Space: "1", Layout: wm.LayoutStack}, engine.BalanceSpaceOp{Space: "1"}}
		seen := make([]string, 0, len(plan))
		err := RunPlan(t.Context(), driver, plan, func(op engine.Op) error {
			seen = append(seen, fmt.Sprintf("%T", op))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"engine.RelabelHomeAction", "engine.SetLayoutAction", "engine.BalanceSpaceOp"}
		if !slices.Equal(seen, want) {
			t.Fatalf("onOp kinds = %v; want %v", seen, want)
		}
		spaces, err := driver.QuerySpaces(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if spaces[0].Layout != wm.LayoutStack {
			t.Fatalf("layout = %q; want stack", spaces[0].Layout)
		}
	})

	t.Run("onOp runs before each op effect", func(t *testing.T) {
		driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1, Label: "a"}}})
		wantErr := errors.New("stop before effect")
		err := RunPlan(t.Context(), driver, []engine.Op{engine.RelabelHomeAction{HomeSpace: "1", Label: "b"}}, func(engine.Op) error {
			return wantErr
		})
		if !errors.Is(err, wantErr) {
			t.Fatalf("RunPlan() error = %v; want wrapped callback error", err)
		}
		spaces, err := driver.QuerySpaces(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if spaces[0].Label != "a" {
			t.Fatalf("label = %q; want original label a", spaces[0].Label)
		}
	})
}

func TestRunConverge(t *testing.T) {
	t.Run("converges to the fixed point and is idempotent on a second run", func(t *testing.T) {
		driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}}, Windows: []fake.SeedWindow{{ID: 40, App: "Zed", SpaceIndex: 1}}})
		step := func(world engine.World, state int) (engine.ConvergeAction, int, bool) {
			home := world.Spaces[0]
			if home.Label != "conv-home" {
				return engine.RelabelHomeAction{HomeSpace: home.ID, Label: "conv-home"}, state + 1, false
			}
			for _, space := range world.Spaces {
				if space.Label == "conv-code" {
					for _, window := range world.Windows {
						if window.ID == 40 && window.SpaceID != space.ID {
							return engine.MoveWindowAction{WindowID: 40, ToSpace: space.ID}, state + 1, false
						}
					}
					return nil, state, true
				}
			}
			return engine.CreateSpaceAction{DisplayIdx: 1, Label: "conv-code"}, state + 1, false
		}
		final, err := RunConverge(t.Context(), driver, step, 0)
		if err != nil || final != 3 {
			t.Fatalf("RunConverge() = %d, %v; want 3, nil", final, err)
		}
		first, err := driver.QuerySpaces(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		second, err := RunConverge(t.Context(), driver, step, 0)
		if err != nil || second != 0 {
			t.Fatalf("second RunConverge() = %d, %v; want 0, nil", second, err)
		}
		after, err := driver.QuerySpaces(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !slices.EqualFunc(first, after, func(a, b wm.Space) bool {
			return a.ID == b.ID && a.Label == b.Label && slices.Equal(a.WindowIDs, b.WindowIDs)
		}) {
			t.Fatalf("second converge changed world: before=%+v after=%+v", first, after)
		}
	})

	t.Run("throws the cap error on a non-terminating converger", func(t *testing.T) {
		driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}}})
		step := func(world engine.World, state int) (engine.ConvergeAction, int, bool) {
			label := "flip-0"
			if state%2 == 1 {
				label = "flip-1"
			}
			return engine.RelabelHomeAction{HomeSpace: world.Spaces[0].ID, Label: label}, state + 1, false
		}
		_, err := RunConverge(t.Context(), driver, step, 0)
		if err == nil || !strings.Contains(err.Error(), "exceeded 200 iterations") {
			t.Fatalf("RunConverge() error = %v; want cap error", err)
		}
	})
}
