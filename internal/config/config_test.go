package config_test

import (
	"context"
	"encoding/json/v2"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/config/configtest"
	"gotest.tools/v3/assert"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		valid bool
	}{
		{name: "default profile", src: mustRead(t, "default.json"), valid: true},
		{name: "unknown profile key", src: `{"extra":true}`},
		{name: "unknown nested key", src: `{"displays":{"g9":{"width":1,"extra":true}}}`},
		{name: "no width", src: `{"displays":{"g9":{},"aw":{"width":1},"laptop":{"width":1}},"windows":{},"desk":[{"display":"g9","label":"main","kind":"stack","tracks":[["app"]]}],"deskSlots":[],"laptopPinned":[],"laptopStackApps":[]}`},
		{name: "no app", src: `{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"windows":{"app":{}},"desk":[{"display":"g9","label":"main","kind":"stack","tracks":[["app"]]}],"deskSlots":[],"laptopPinned":[],"laptopStackApps":[]}`},
		{name: "missing track list", src: `{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"windows":{"app":{"app":"App"}},"desk":[{"display":"g9","label":"main","kind":"stack"}],"deskSlots":[],"laptopPinned":[],"laptopStackApps":[]}`},
		{name: "empty track list", src: `{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"windows":{"app":{"app":"App"}},"desk":[{"display":"g9","label":"main","kind":"stack","tracks":[]}],"deskSlots":[],"laptopPinned":[],"laptopStackApps":[]}`},
		{name: "empty track", src: `{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"windows":{"app":{"app":"App"}},"desk":[{"display":"g9","label":"main","kind":"stack","tracks":[[]]}],"deskSlots":[],"laptopPinned":[],"laptopStackApps":[]}`},
		{name: "missing desk", src: `{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"windows":{},"deskSlots":[],"laptopPinned":[],"laptopStackApps":[]}`},
		{name: "empty desk", src: `{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"windows":{},"desk":[],"deskSlots":[],"laptopPinned":[],"laptopStackApps":[]}`},
		{name: "missing windows", src: `{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"desk":[{"display":"g9","label":"main","kind":"stack","tracks":[["app"]]}],"deskSlots":[],"laptopPinned":[],"laptopStackApps":[]}`},
		{name: "missing slots", src: `{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"windows":{"app":{"app":"App"}},"desk":[{"display":"g9","label":"main","kind":"stack","tracks":[["app"]]}],"laptopPinned":[],"laptopStackApps":[]}`},
		{name: "missing pinned", src: `{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"windows":{"app":{"app":"App"}},"desk":[{"display":"g9","label":"main","kind":"stack","tracks":[["app"]]}],"deskSlots":[],"laptopStackApps":[]}`},
		{name: "missing stack app list", src: `{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"windows":{"app":{"app":"App"}},"desk":[{"display":"g9","label":"main","kind":"stack","tracks":[["app"]]}],"deskSlots":[],"laptopPinned":[]}`},
		{name: "empty required collections", src: `{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"windows":{"app":{"app":"App"}},"desk":[{"display":"g9","label":"main","kind":"stack","tracks":[["app"]]}],"deskSlots":[],"laptopPinned":[],"laptopStackApps":[]}`, valid: true},
		{name: "invalid RE2 lookbehind", src: `{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"windows":{"app":{"app":"(?<=x)y"}},"desk":[{"display":"g9","label":"main","kind":"stack","tracks":[["app"]]}],"deskSlots":[],"laptopPinned":[],"laptopStackApps":[]}`},
		{name: "unknown track name", src: `{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"windows":{"app":{"app":"App"}},"desk":[{"display":"g9","label":"main","kind":"stack","tracks":[["missing"]]}],"deskSlots":[],"laptopPinned":[],"laptopStackApps":[]}`},
		{name: "stack with two tracks", src: `{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"windows":{"app":{"app":"App"}},"desk":[{"display":"g9","label":"main","kind":"stack","tracks":[["app"],["app"]]}],"deskSlots":[],"laptopPinned":[],"laptopStackApps":[]}`},
		{name: "invalid display enum", src: `{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"windows":{"app":{"app":"App"}},"desk":[{"display":"external","label":"main","kind":"stack","tracks":[["app"]]}],"deskSlots":[],"laptopPinned":[],"laptopStackApps":[]}`},
		{name: "invalid layout kind", src: `{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"windows":{"app":{"app":"App"}},"desk":[{"display":"g9","label":"main","kind":"grid","tracks":[["app"]]}],"deskSlots":[],"laptopPinned":[],"laptopStackApps":[]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := config.Parse("test.json", []byte(test.src))
			if test.valid {
				assert.NilError(t, err)
			} else if err == nil {
				t.Fatal("expected parse error")
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.Profile)
		want   string
	}{
		{name: "missing required window map", mutate: func(p *config.Profile) { p.Windows = nil }, want: "windows must be set"},
		{name: "missing required slots", mutate: func(p *config.Profile) { p.DeskSlots = nil }, want: "deskSlots must be set"},
		{name: "missing required pinned list", mutate: func(p *config.Profile) { p.LaptopPinned = nil }, want: "laptopPinned must be set"},
		{name: "missing required stack apps", mutate: func(p *config.Profile) { p.LaptopStackApps = nil }, want: "laptopStackApps must be set"},
		{name: "empty desk", mutate: func(p *config.Profile) { p.Desk = []config.DeskLayout{} }, want: "desk must not be empty"},
		{name: "invalid track ratio subsequence", mutate: func(p *config.Profile) { p.Weights.Columns = map[int][]float64{3: {9, 0.5, 0.5}} }, want: "weight chain ratios"},
		{name: "topology without declared layout", mutate: func(p *config.Profile) {
			p.Topologies = []config.Topology{{Name: "dual", Displays: []config.DisplayName{config.DisplayAW}, Desk: []config.DeskLayout{}}}
		}, want: "no layout for aw"},
		{name: "duplicate topology display layout", mutate: func(p *config.Profile) {
			p.Topologies = []config.Topology{{Name: "dual", Displays: []config.DisplayName{config.DisplayG9}, Desk: []config.DeskLayout{{Display: config.DisplayG9, Label: "one", Kind: config.LayoutStack, Tracks: [][]string{{"alpha"}}}, {Display: config.DisplayG9, Label: "two", Kind: config.LayoutStack, Tracks: [][]string{{"alpha"}}}}}}
		}, want: "duplicate layout for g9"},
		{name: "unknown desk slot window", mutate: func(p *config.Profile) { p.DeskSlots[0].Name = "missing" }, want: "unknown window"},
		{name: "unknown pinned window", mutate: func(p *config.Profile) { p.LaptopPinned[0] = "missing" }, want: "unknown window"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := configtest.Profile()
			test.mutate(profile)
			if err := config.Validate(profile); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}

	profile := configtest.Profile()
	assert.NilError(t, config.Validate(profile))
	for name, spec := range profile.Windows {
		assert.Assert(t, spec.AppRE != nil, name+" app pattern was not compiled")
		if spec.Title != "" {
			assert.Assert(t, spec.TitleRE != nil, name+" title pattern was not compiled")
		}
	}
}

func TestValidateLayoutRules(t *testing.T) {
	tests := []struct {
		name   string
		layout config.DeskLayout
		want   string
	}{
		{name: "empty tracks", layout: config.DeskLayout{Display: config.DisplayAW, Label: "empty", Kind: config.LayoutStack, Tracks: [][]string{}}, want: "stack must have exactly one track"},
		{name: "empty track", layout: config.DeskLayout{Display: config.DisplayAW, Label: "empty", Kind: config.LayoutStack, Tracks: [][]string{{}}}, want: "track 0 must have at least one window name"},
		{name: "stack with two tracks", layout: config.DeskLayout{Display: config.DisplayAW, Label: "stack", Kind: config.LayoutStack, Tracks: [][]string{{"arc"}, {"arc"}}}, want: "stack must have exactly one track"},
		{name: "stack weights", layout: config.DeskLayout{Display: config.DisplayAW, Label: "stack", Kind: config.LayoutStack, Tracks: [][]string{{"arc"}}, Weights: []float64{1}}, want: "stack cannot set weights"},
		{name: "one-track columns weights", layout: config.DeskLayout{Display: config.DisplayAW, Label: "columns", Kind: config.LayoutColumns, Tracks: [][]string{{"arc"}}, Weights: []float64{1}}, want: "one-track layout cannot set weights"},
		{name: "one-track rows weights", layout: config.DeskLayout{Display: config.DisplayAW, Label: "rows", Kind: config.LayoutRows, Tracks: [][]string{{"arc"}}, Weights: []float64{1}}, want: "one-track layout cannot set weights"},
		{name: "weights track count mismatch", layout: config.DeskLayout{Display: config.DisplayAW, Label: "main", Kind: config.LayoutColumns, Tracks: [][]string{{"arc"}, {"arc"}}, Weights: []float64{1}}, want: "weights length must match tracks length"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := configtest.Profile()
			profile.Desk = []config.DeskLayout{test.layout}
			err := config.Validate(profile)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestValidateWeights(t *testing.T) {
	t.Run("non-positive and non-finite weights are rejected", func(t *testing.T) {
		for _, value := range []struct {
			name   string
			weight float64
		}{
			{name: "zero", weight: 0},
			{name: "negative", weight: -1},
			{name: "nan", weight: math.NaN()},
			{name: "infinity", weight: math.Inf(1)},
		} {
			t.Run(value.name, func(t *testing.T) {
				profile := configtest.Profile()
				profile.Weights = &config.WeightDefaults{Columns: map[int][]float64{2: {value.weight, 1}}}
				err := config.Validate(profile)
				if err == nil || !strings.Contains(err.Error(), "weights must be finite and greater than 0") {
					t.Fatalf("Validate() error = %v, want invalid weight", err)
				}
			})
		}
	})

	tests := []struct {
		name    string
		weights []float64
		valid   bool
	}{
		{name: "lower boundary", weights: []float64{1, 9}, valid: true},
		{name: "upper boundary", weights: []float64{9, 1}, valid: true},
		{name: "equal huge weights", weights: []float64{1e308, 1e308}, valid: true},
		{name: "overflowing sum boundary", weights: []float64{1.797803617755409e307, 9 * 1.797803617755409e307}, valid: true},
		{name: "out-of-range full vector", weights: []float64{9.01, 1}},
		{name: "out-of-range subsequence", weights: []float64{9, 0.5, 0.5}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := configtest.Profile()
			profile.Weights = &config.WeightDefaults{Columns: map[int][]float64{len(test.weights): test.weights}}
			err := config.Validate(profile)
			if test.valid && err != nil {
				t.Fatalf("Validate() error = %v, want valid weights", err)
			}
			if !test.valid && (err == nil || !strings.Contains(err.Error(), "weight chain ratios")) {
				t.Fatalf("Validate() error = %v, want chain ratio violation", err)
			}
		})
	}

	for _, test := range []struct {
		name    string
		key     int
		weights []float64
	}{
		{name: "key differs from vector length", key: 3, weights: []float64{1, 1}},
		{name: "key below two", key: 1, weights: []float64{1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile := configtest.Profile()
			profile.Weights = &config.WeightDefaults{Columns: map[int][]float64{test.key: test.weights}}
			err := config.Validate(profile)
			if err == nil || !strings.Contains(err.Error(), "key must equal vector length and be at least 2") {
				t.Fatalf("Validate() error = %v, want invalid weight key", err)
			}
		})
	}
}

func TestProfileFixture(t *testing.T) {
	profile := configtest.Profile()
	assert.DeepEqual(t, profile.Displays, config.Displays{
		G9:     config.DisplaySpec{Width: 5120},
		AW:     config.DisplaySpec{Width: 3440},
		Laptop: config.DisplaySpec{Width: 1728},
	})
	assert.DeepEqual(t, profile.Weights, &config.WeightDefaults{
		Columns: map[int][]float64{3: {3, 4, 3}},
	})
	assert.DeepEqual(t, []string{
		"akiflow", "arc", "discord", "ghostty-mbp", "ghostty-wave",
		"linear", "obsidian", "qalculate", "spotify", "vscode",
	}, sortedKeys(profile.Windows))
	assert.Equal(t, profile.Windows["ghostty-wave"].Title, "pc")
	assert.Equal(t, profile.Windows["ghostty-mbp"].Title, "mbp")
	assert.DeepEqual(t, profile.DeskSlots, []config.DeskSlot{
		{Name: "ghostty-wave"}, {Name: "arc", OnDisplay: config.DisplayG9},
		{Name: "ghostty-mbp"}, {Name: "arc", OnDisplay: config.DisplayAW},
		{Name: "arc", OnDisplay: config.DisplayAW}, {Name: "linear"},
		{Name: "obsidian"}, {Name: "akiflow"}, {Name: "vscode"},
	})
	assert.DeepEqual(t, profile.LaptopPinned, []string{
		"arc", "ghostty-wave", "arc", "ghostty-mbp", "linear", "obsidian",
		"arc", "akiflow", "arc",
	})
	assert.DeepEqual(t, profile.LaptopStackApps, []string{
		"Messages", "System Settings", "Slack", "Discord", "1Password", "Activity Monitor",
	})
	assert.NilError(t, config.Validate(profile))
}

func TestParseFullProfileDrift(t *testing.T) {
	data := mustRead(t, "testdata/full.json")
	got, err := config.Parse("testdata/full.json", []byte(data))
	assert.NilError(t, err)
	want := configtest.Full()
	assert.DeepEqual(t, got, want, cmp.Options{
		cmp.FilterPath(func(path cmp.Path) bool {
			field, ok := path.Last().(cmp.StructField)
			return ok && (field.Name() == "AppRE" || field.Name() == "TitleRE")
		}, cmp.Ignore()),
	})
}

func TestLoad(t *testing.T) {
	t.Run("explicit profile override", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "override.json")
		data := mustRead(t, "default.json")
		assert.NilError(t, os.WriteFile(path, []byte(data), 0o600))
		profile, err := config.Load(context.Background(), func(key string) string {
			if key == "TESSERA_PROFILE" {
				return path
			}
			return ""
		}, t.TempDir())
		assert.NilError(t, err)
		assert.Equal(t, profile.Displays.Laptop.Width, 1512)
	})

	t.Run("missing profile falls back to embedded default", func(t *testing.T) {
		profile, err := config.Load(context.Background(), func(string) string { return "" }, t.TempDir())
		assert.NilError(t, err)
		assert.Equal(t, profile.Displays.Laptop.Width, 1512)
	})

	t.Run("well-known JSON profile wins", func(t *testing.T) {
		home := t.TempDir()
		configHome := filepath.Join(home, "config")
		path := filepath.Join(configHome, "tessera", "profile.json")
		assert.NilError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		data := mustRead(t, "default.json")
		assert.NilError(t, os.WriteFile(path, []byte(data), 0o600))
		profile, err := config.Load(context.Background(), func(key string) string {
			if key == "XDG_CONFIG_HOME" {
				return configHome
			}
			return ""
		}, home)
		assert.NilError(t, err)
		assert.Equal(t, profile.Displays.G9.Width, 3440)
	})

	t.Run("broken well-known profile errors", func(t *testing.T) {
		home := t.TempDir()
		configHome := filepath.Join(home, "config")
		path := filepath.Join(configHome, "tessera", "profile.json")
		assert.NilError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		assert.NilError(t, os.WriteFile(path, []byte("{"), 0o600))
		_, err := config.Load(context.Background(), func(key string) string {
			if key == "XDG_CONFIG_HOME" {
				return configHome
			}
			return ""
		}, home)
		assert.Assert(t, err != nil)
	})

	t.Run("TypeScript override requires rendering", func(t *testing.T) {
		_, err := config.Load(context.Background(), func(key string) string {
			if key == "TESSERA_PROFILE" {
				return "profile.ts"
			}
			return ""
		}, t.TempDir())
		assert.Assert(t, err != nil && strings.Contains(err.Error(), "render"))
	})

	t.Run("legacy TypeScript profile requires JSON render", func(t *testing.T) {
		home := t.TempDir()
		configHome := filepath.Join(home, "config")
		directory := filepath.Join(configHome, "tessera")
		assert.NilError(t, os.MkdirAll(directory, 0o700))
		assert.NilError(t, os.WriteFile(filepath.Join(directory, "profile.ts"), nil, 0o600))
		_, err := config.Load(context.Background(), func(key string) string {
			if key == "XDG_CONFIG_HOME" {
				return configHome
			}
			return ""
		}, home)
		assert.Assert(t, err != nil && strings.Contains(err.Error(), "render"))
	})
}

func TestDecodeRequiredEmptyCollections(t *testing.T) {
	src := []byte(`{"displays":{"g9":{"width":1},"aw":{"width":1},"laptop":{"width":1}},"windows":{},"desk":[{"display":"g9","label":"main","kind":"stack","tracks":[["app"]]}],"deskSlots":[],"laptopPinned":[],"laptopStackApps":[]}`)
	var profile config.Profile
	assert.NilError(t, json.Unmarshal(src, &profile, json.RejectUnknownMembers(true)))
	assert.Assert(t, profile.Windows != nil)
	assert.Equal(t, len(profile.Windows), 0)
	if err := config.Validate(&profile); err == nil || strings.Contains(err.Error(), "windows must be set") {
		t.Fatalf("Validate() error = %v, want unrelated profile invariant errors", err)
	}
}

func TestSnapshotDecodeOptions(t *testing.T) {
	var value map[string]any
	assert.NilError(t, json.Unmarshal([]byte(`{"known":true}`), &value, json.RejectUnknownMembers(true)))
}

func mustRead(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	assert.NilError(t, err)
	return string(data)
}

func sortedKeys(values map[string]config.WindowSpec) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
