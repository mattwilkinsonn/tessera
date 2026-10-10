package engine

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

func TestSnapPlan(t *testing.T) {
	profile := engineTestProfile(t)
	spaceID := wm.SpaceID("sf")
	window := func(id int, x float64, options ...func(*wm.Window)) wm.Window {
		result := wm.Window{ID: id, Frame: wm.Frame{X: x, W: 100, H: 100}, SpaceID: spaceID}
		for _, apply := range options {
			apply(&result)
		}
		return result
	}
	world := func(windows ...wm.Window) World { return World{Windows: windows} }
	expect := func(t *testing.T, got []Op, want []Op) {
		t.Helper()
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("SnapPlan() mismatch (-want +got):\n%s", diff)
		}
	}
	t.Run("snap modes use nominal count defaults on focused display", func(t *testing.T) {
		profile := engineTestProfile(t)
		profile.Displays.G9.Weights = &config.WeightDefaults{Columns: map[int][]float64{2: {3, 2}, 3: {4, 3, 3}}}
		w := World{
			Windows:  []wm.Window{window(1, 0), window(2, 100), window(3, 200)},
			Spaces:   []wm.Space{{ID: spaceID, Label: "main", DisplayIdx: 1}},
			Displays: []wm.Display{{Idx: 1, Frame: wm.Frame{W: 5120}, SpaceIDs: []wm.SpaceID{spaceID}}},
		}
		expect(t, SnapPlan(profile, w, spaceID, SnapThreeColumns), []Op{RealizeLayoutOp{Space: spaceID, Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{1}, {2}, {3}}, Weights: []float64{4, 3, 3}}}})
		expect(t, SnapPlan(profile, w, spaceID, SnapHalf), []Op{RealizeLayoutOp{Space: spaceID, Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{1, 2}, {3}}, Weights: []float64{3, 2}}}})
	})
	t.Run("x-sort preserves left-to-right track order", func(t *testing.T) {
		expect(t, SnapPlan(profile, world(window(30, 300), window(10, 100), window(20, 200)), spaceID, SnapThreeColumns), []Op{RealizeLayoutOp{Space: spaceID, Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{10}, {20}, {30}}, Weights: []float64{3, 4, 3}}}})
	})
	t.Run("3col stacks excess leaves on third track", func(t *testing.T) {
		expect(t, SnapPlan(profile, world(window(0, 0), window(1, 100), window(2, 200), window(3, 300), window(4, 400)), spaceID, SnapThreeColumns), []Op{RealizeLayoutOp{Space: spaceID, Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{0}, {1}, {2, 3, 4}}, Weights: []float64{3, 4, 3}}}})
	})
	t.Run("3col with two leaves drops third track and weight", func(t *testing.T) {
		expect(t, SnapPlan(profile, world(window(0, 0), window(1, 100)), spaceID, SnapThreeColumns), []Op{RealizeLayoutOp{Space: spaceID, Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{0}, {1}}, Weights: []float64{3, 4}}}})
	})
	t.Run("50-50 splits leaves into balanced stacked halves", func(t *testing.T) {
		expect(t, SnapPlan(profile, world(window(0, 0), window(1, 100), window(2, 200), window(3, 300)), spaceID, SnapHalf), []Op{RealizeLayoutOp{Space: spaceID, Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{0, 1}, {2, 3}}, Weights: []float64{1, 1}}}})
		expect(t, SnapPlan(profile, world(window(0, 0), window(1, 100), window(2, 200), window(3, 300), window(4, 400)), spaceID, SnapHalf), []Op{RealizeLayoutOp{Space: spaceID, Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{0, 1, 2}, {3, 4}}, Weights: []float64{1, 1}}}})
	})
	t.Run("columns balances and empty focused space does nothing", func(t *testing.T) {
		expect(t, SnapPlan(profile, world(window(0, 0), window(1, 100)), spaceID, SnapColumns), []Op{BalanceSpaceOp{Space: spaceID}})
		expect(t, SnapPlan(profile, world(window(1, 100, func(w *wm.Window) { w.SpaceID = "other" })), spaceID, SnapThreeColumns), []Op{})
	})
	t.Run("floating and minimized leaves do not change tracks", func(t *testing.T) {
		expect(t, SnapPlan(profile, world(window(0, 0), window(1, 100, func(w *wm.Window) { w.Floating = true }), window(2, 200, func(w *wm.Window) { w.Minimized = true })), spaceID, SnapThreeColumns), []Op{RealizeLayoutOp{Space: spaceID, Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{0}}, Weights: []float64{3}}}})
	})
	t.Run("empty nominal middle track drops its matching weight", func(t *testing.T) {
		profile := engineTestProfile(t)
		profile.Displays.G9.Weights = &config.WeightDefaults{Columns: map[int][]float64{3: {3, 4, 3}}}
		w := World{Windows: []wm.Window{window(0, 0), window(2, 200)}, Spaces: []wm.Space{{ID: spaceID, DisplayIdx: 1}}, Displays: []wm.Display{{Idx: 1, Frame: wm.Frame{W: 5120}, SpaceIDs: []wm.SpaceID{spaceID}}}}
		want := []Op{RealizeLayoutOp{Space: spaceID, Target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{0}, {2}}, Weights: []float64{3, 4}}}}
		expect(t, SnapPlan(profile, w, spaceID, SnapThreeColumns), want)
	})
}
