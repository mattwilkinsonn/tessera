package engine

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

type rawWindow struct {
	App   string `json:"app"`
	Title string `json:"title"`
}

func TestMatchesSpec(t *testing.T) {
	profile := engineTestProfile(t)
	tests := []struct {
		name  string
		spec  string
		app   string
		title string
		want  bool
	}{
		{name: "app-only spec matches any title", spec: "arc", app: "Arc", title: "some window", want: true},
		{name: "empty title matches any", spec: "arc", app: "Arc", title: "mbp | bar", want: true},
		{name: "title-present spec must match", spec: "ghostty-wave", app: "Ghostty", title: "pc | model-evals", want: true},
		{name: "title mismatch rejected", spec: "ghostty-wave", app: "Ghostty", title: "mbp | bar", want: false},
		{name: "substring app matching", spec: "vscode", app: "Visual Studio Code", want: true},
		{name: "app mismatch rejected", spec: "arc", app: "Ferdium", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := MatchesSpec(profile.Windows[test.spec], test.app, test.title); got != test.want {
				t.Fatalf("MatchesSpec() = %t, want %t", got, test.want)
			}
		})
	}
	t.Run("titleInvert matches when title does not match", func(t *testing.T) {
		spec := profile.Windows["ghostty-wave"]
		spec.TitleInvert = true
		if !MatchesSpec(spec, "Ghostty", "mbp | bar") || MatchesSpec(spec, "Ghostty", "pc | foo") {
			t.Fatal("inverted title matching did not invert the predicate")
		}
	})
}

func TestSlugForWindow(t *testing.T) {
	profile := engineTestProfile(t)
	tests := []struct {
		name  string
		app   string
		title string
		want  string
	}{
		{name: "sorted first matching key with title disambiguation", app: "Ghostty", title: "pc | model-evals", want: "ghostty-wave"},
		{name: "fallback squeezes punctuation runs", app: "Activity Monitor", want: "activity-monitor"},
		{name: "fallback for non-profile app", app: "1Password", want: "1password"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SlugForWindow(profile, test.app, test.title); got != test.want {
				t.Fatalf("SlugForWindow() = %q, want %q", got, test.want)
			}
		})
	}
	t.Run("sorted-key precedence breaks a genuine collision", func(t *testing.T) {
		profile := engineTestProfile(t)
		profile.Windows["alpha"] = profile.Windows["arc"]
		profile.Windows["zeta"] = profile.Windows["arc"]
		if got := SlugForWindow(profile, "Arc", ""); got != "alpha" {
			t.Fatalf("SlugForWindow() = %q, want alpha", got)
		}
	})
}

func TestLiveWindowFixtureClassifications(t *testing.T) {
	path := filepath.Join("testdata", "windows.live.json")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var windows []rawWindow
	if err := json.Unmarshal(source, &windows); err != nil {
		t.Fatal(err)
	}
	profile := engineTestProfile(t)
	find := func(appPrefix, titlePrefix string) rawWindow {
		t.Helper()
		for _, window := range windows {
			if strings.HasPrefix(window.App, appPrefix) && strings.HasPrefix(window.Title, titlePrefix) {
				return window
			}
		}
		t.Fatalf("fixture has no %q / %q window", appPrefix, titlePrefix)
		return rawWindow{}
	}
	t.Run("the two Ghostty windows disambiguate by title", func(t *testing.T) {
		pc := find("Ghostty", "pc")
		mbp := find("Ghostty", "mbp")
		wave, mbpSpec := profile.Windows["ghostty-wave"], profile.Windows["ghostty-mbp"]
		if !MatchesSpec(wave, pc.App, pc.Title) || MatchesSpec(mbpSpec, pc.App, pc.Title) || !MatchesSpec(mbpSpec, mbp.App, mbp.Title) || MatchesSpec(wave, mbp.App, mbp.Title) {
			t.Fatal("Ghostty fixture titles do not separate wave and mbp specs")
		}
		if SlugForWindow(profile, pc.App, pc.Title) != "ghostty-wave" || SlugForWindow(profile, mbp.App, mbp.Title) != "ghostty-mbp" {
			t.Fatal("Ghostty fixture windows resolve to wrong profile names")
		}
	})
	t.Run("every Arc window matches the arc spec", func(t *testing.T) {
		count := 0
		for _, window := range windows {
			if window.App == "Arc" {
				count++
				if !MatchesSpec(profile.Windows["arc"], window.App, window.Title) {
					t.Errorf("Arc window %q did not match", window.Title)
				}
			}
		}
		if count != 6 {
			t.Fatalf("Arc fixture rows = %d, want 6", count)
		}
	})
	t.Run("a Code window matches the vscode spec", func(t *testing.T) {
		code := find("Code", "")
		if !MatchesSpec(profile.Windows["vscode"], code.App, code.Title) {
			t.Fatal("Code fixture window did not match vscode spec")
		}
	})
	t.Run("non-profile apps match none of the profile specs", func(t *testing.T) {
		for _, app := range []string{"1Password", "Ferdium", "Messages", "Notion"} {
			window := find(app, "")
			for name, spec := range profile.Windows {
				if MatchesSpec(spec, window.App, window.Title) {
					t.Errorf("%s window matched profile spec %q", app, name)
				}
			}
		}
	})
	t.Run("slugForWindow on a non-profile app returns squeezed fallback", func(t *testing.T) {
		window := find("1Password", "")
		if got := SlugForWindow(profile, window.App, window.Title); got != "1password" {
			t.Fatalf("SlugForWindow() = %q, want 1password", got)
		}
	})
}

func TestResolveDisplay(t *testing.T) {
	profile := engineTestProfile(t)
	displays := []wm.Display{
		{Idx: 1, Frame: wm.Frame{W: 3440}},
		{Idx: 2, Frame: wm.Frame{W: 5120}},
		{Idx: 3, Frame: wm.Frame{W: 1728}},
	}
	for _, test := range []struct {
		name string
		key  config.DisplayName
		want int
		ok   bool
	}{
		{name: "matches connected display by stable width", key: config.DisplayG9, want: 2, ok: true},
		{name: "matches AW after index reorder", key: config.DisplayAW, want: 1, ok: true},
		{name: "matches laptop by stable width", key: config.DisplayLaptop, want: 3, ok: true},
		{name: "absent display returns false", key: config.DisplayLaptop, ok: true},
	}[:3] {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ResolveDisplay(profile, test.key, displays)
			if got != test.want || ok != test.ok {
				t.Fatalf("ResolveDisplay() = %d, %t; want %d, %t", got, ok, test.want, test.ok)
			}
		})
	}
	t.Run("returns false for absent display", func(t *testing.T) {
		got, ok := ResolveDisplay(profile, config.DisplayLaptop, displays[:2])
		if ok || got != 0 {
			t.Fatalf("ResolveDisplay() = %d, %t; want 0, false", got, ok)
		}
	})
	t.Run("returns first display when width repeats", func(t *testing.T) {
		got, ok := ResolveDisplay(profile, config.DisplayG9, []wm.Display{{Idx: 1, Frame: wm.Frame{W: 5120}}, {Idx: 2, Frame: wm.Frame{W: 5120}}})
		if !ok || got != 1 {
			t.Fatalf("ResolveDisplay() = %d, %t; want 1, true", got, ok)
		}
	})
}

func TestResolveDesk(t *testing.T) {
	base := engineTestProfile(t)
	display := func(idx int, width float64) wm.Display { return wm.Display{Idx: idx, Frame: wm.Frame{W: width}} }
	t.Run("no topologies always uses default desk", func(t *testing.T) {
		got := ResolveDesk(base, []wm.Display{display(1, 5120), display(2, 1728)})
		if diff := cmp.Diff(base.Desk, got); diff != "" {
			t.Fatalf("ResolveDesk() mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("exact present set selects matching topology", func(t *testing.T) {
		profile := engineTestProfile(t)
		topologyDesk := []config.DeskLayout{{Display: config.DisplayAW, Label: "solo", Kind: config.LayoutColumns, Tracks: [][]string{{"arc"}}}}
		profile.Topologies = []config.Topology{{Name: "aw-laptop", Displays: []config.DisplayName{config.DisplayAW, config.DisplayLaptop}, Desk: topologyDesk}}
		got := ResolveDesk(profile, []wm.Display{display(2, 3440), display(3, 1728)})
		if diff := cmp.Diff(topologyDesk, got); diff != "" {
			t.Fatalf("ResolveDesk() mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("superset and subset do not match", func(t *testing.T) {
		profile := engineTestProfile(t)
		profile.Topologies = []config.Topology{{Name: "aw-laptop", Displays: []config.DisplayName{config.DisplayAW, config.DisplayLaptop}, Desk: []config.DeskLayout{{Display: config.DisplayAW, Label: "solo", Kind: config.LayoutColumns, Tracks: [][]string{{"arc"}}}}}}
		for _, displays := range [][]wm.Display{{display(1, 5120), display(2, 3440), display(3, 1728)}, {display(2, 3440)}} {
			if got := ResolveDesk(profile, displays); cmp.Diff(profile.Desk, got) != "" {
				t.Fatalf("ResolveDesk() did not use default desk: %s", cmp.Diff(profile.Desk, got))
			}
		}
	})
	t.Run("first matching topology wins regardless of display declaration order", func(t *testing.T) {
		profile := engineTestProfile(t)
		first := []config.DeskLayout{{Display: config.DisplayAW, Label: "first", Kind: config.LayoutColumns, Tracks: [][]string{{"arc"}}}}
		second := []config.DeskLayout{{Display: config.DisplayAW, Label: "second", Kind: config.LayoutColumns, Tracks: [][]string{{"arc"}}}}
		profile.Topologies = []config.Topology{
			{Name: "a", Displays: []config.DisplayName{config.DisplayAW, config.DisplayLaptop}, Desk: first},
			{Name: "b", Displays: []config.DisplayName{config.DisplayLaptop, config.DisplayAW}, Desk: second},
		}
		got := ResolveDesk(profile, []wm.Display{display(2, 3440), display(3, 1728)})
		if diff := cmp.Diff(first, got); diff != "" {
			t.Fatalf("ResolveDesk() mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("unknown or duplicate widths fall back to default", func(t *testing.T) {
		profile := engineTestProfile(t)
		profile.Topologies = []config.Topology{{Name: "aw-laptop", Displays: []config.DisplayName{config.DisplayAW, config.DisplayLaptop}}}
		cases := [][]wm.Display{
			{display(2, 3440), display(3, 1728), display(9, 2560)},
			{display(2, 3440), display(4, 3440), display(3, 1728)},
		}
		for _, displays := range cases {
			if got := ResolveDesk(profile, displays); cmp.Diff(profile.Desk, got) != "" {
				t.Fatalf("ResolveDesk() did not use default desk: %s", cmp.Diff(profile.Desk, got))
			}
		}
	})
}

func TestWeightsFor(t *testing.T) {
	profile := engineTestProfile(t)
	profile.Weights = &config.WeightDefaults{Columns: map[int][]float64{3: {7, 2, 1}}}
	profile.Displays.G9.Weights = &config.WeightDefaults{Columns: map[int][]float64{3: {4, 3, 3}}}
	for _, test := range []struct {
		name     string
		kind     config.LayoutKind
		count    int
		display  config.DisplayName
		override []float64
		want     []float64
	}{
		{name: "layout override wins", kind: config.LayoutColumns, count: 3, display: config.DisplayG9, override: []float64{1, 2, 3}, want: []float64{1, 2, 3}},
		{name: "display defaults before profile", kind: config.LayoutColumns, count: 3, display: config.DisplayG9, want: []float64{4, 3, 3}},
		{name: "profile defaults used for another display", kind: config.LayoutColumns, count: 3, display: config.DisplayAW, want: []float64{7, 2, 1}},
		{name: "equal defaults for unmatched count", kind: config.LayoutColumns, count: 4, display: config.DisplayG9, want: []float64{1, 1, 1, 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := WeightsFor(profile, test.kind, test.count, test.display, test.override); cmp.Diff(test.want, got) != "" {
				t.Fatalf("WeightsFor() mismatch (-want +got):\n%s", cmp.Diff(test.want, got))
			}
		})
	}
	t.Run("only uses defaults with requested kind and count", func(t *testing.T) {
		profile.Weights = &config.WeightDefaults{Columns: map[int][]float64{2: {5, 1}}, Rows: map[int][]float64{2: {2, 3}}}
		profile.Displays.G9.Weights = &config.WeightDefaults{Columns: map[int][]float64{3: {4, 3, 3}}, Rows: map[int][]float64{3: {1, 2, 1}}}
		cases := []struct {
			kind  config.LayoutKind
			count int
			want  []float64
		}{
			{kind: config.LayoutColumns, count: 2, want: []float64{5, 1}},
			{kind: config.LayoutRows, count: 2, want: []float64{2, 3}},
			{kind: config.LayoutColumns, count: 4, want: []float64{1, 1, 1, 1}},
			{kind: config.LayoutRows, count: 3, want: []float64{1, 2, 1}},
			{kind: config.LayoutColumns, count: 3, want: []float64{4, 3, 3}},
		}
		for _, test := range cases {
			if got := WeightsFor(profile, test.kind, test.count, config.DisplayG9, nil); cmp.Diff(test.want, got) != "" {
				t.Errorf("WeightsFor(%q, %d) mismatch (-want +got):\n%s", test.kind, test.count, cmp.Diff(test.want, got))
			}
		}
	})
}
