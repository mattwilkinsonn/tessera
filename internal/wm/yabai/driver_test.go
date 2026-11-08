package yabai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattwilkinsonn/tessera/internal/commands"
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/config/configtest"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/executor"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"reflect"
)

func TestQueryWindow(t *testing.T) {
	root := t.TempDir()
	windows := filepath.Join(root, "windows.json")
	spaces := filepath.Join(root, "spaces.json")
	if err := os.WriteFile(windows, []byte(`[{"id":90,"app":"Code","role":"AXWindow","subrole":"AXStandardWindow","root-window":true,"level":0,"can-move":true,"can-resize":true,"has-ax-reference":true,"is-floating":true,"space":2}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spaces, []byte(`[{"id":47,"index":2}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TESS_YABAI_WINDOWS", windows)
	t.Setenv("TESS_YABAI_SPACES", spaces)
	t.Setenv("TESS_YABAI_LOG", filepath.Join(root, "argv.log"))
	t.Setenv("TESS_YABAI_EXIT_CODE", "")
	driver := New(os.Getenv("TESS_YABAI"))
	t.Run("classification and stable space ID", func(t *testing.T) {
		window, found, err := driver.QueryWindow(t.Context(), 90)
		if err != nil || !found {
			t.Fatalf("QueryWindow = %t, %v", found, err)
		}
		if window.SpaceID != wm.SpaceID("47") || !window.Floating || !window.Root || window.Role != "AXWindow" || window.Subrole != "AXStandardWindow" || !window.CanMove || !window.CanResize || window.Level != 0 {
			t.Fatalf("window = %+v", window)
		}
	})
	t.Run("missing ID", func(t *testing.T) {
		_, found, err := driver.QueryWindow(t.Context(), 999)
		if err != nil || found {
			t.Fatalf("missing = %t, %v", found, err)
		}
	})
	t.Run("query errors are not missing windows", func(t *testing.T) {
		t.Setenv("TESS_YABAI_WINDOWS_EXIT_CODE", "1")
		_, found, err := driver.QueryWindow(t.Context(), 90)
		if err == nil || found || !strings.Contains(err.Error(), "windows query failed") {
			t.Fatalf("failed query = %t, %v", found, err)
		}
	})
	t.Run("spaces query error", func(t *testing.T) {
		t.Setenv("TESS_YABAI_SPACES", filepath.Join(root, "missing.json"))
		_, found, err := driver.QueryWindow(t.Context(), 90)
		if err == nil || found {
			t.Fatalf("failed spaces = %t, %v", found, err)
		}
	})
	t.Run("canceled query", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, found, err := driver.QueryWindow(ctx, 90)
		if found || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled = %t, %v", found, err)
		}
	})
}

func TestRulesAddOnly(t *testing.T) {
	root := t.TempDir()
	windows := filepath.Join(root, "windows.json")
	spaces := filepath.Join(root, "spaces.json")
	displays := filepath.Join(root, "displays.json")
	log := filepath.Join(root, "argv.log")
	for path, data := range map[string]string{
		windows:  `[{"id":90,"app":"Code","space":1,"has-ax-reference":true,"is-floating":false,"frame":{"x":0,"y":0,"w":2500,"h":2000}}]`,
		spaces:   `[{"id":47,"index":1,"display":1,"type":"bsp"},{"id":48,"index":2,"display":2,"type":"stack"}]`,
		displays: `[{"index":1,"frame":{"w":5120},"spaces":[1]},{"index":2,"frame":{"w":1728},"spaces":[2]}]`,
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TESS_YABAI_WINDOWS", windows)
	t.Setenv("TESS_YABAI_SPACES", spaces)
	t.Setenv("TESS_YABAI_DISPLAYS", displays)
	t.Setenv("TESS_YABAI_LOG", log)
	t.Setenv("TESS_YABAI_EXIT_CODE", "")
	profile := configtest.Profile()
	if err := config.Validate(profile); err != nil {
		t.Fatal(err)
	}
	driver := New(os.Getenv("TESS_YABAI"))
	before, err := driver.QueryWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := commands.Rules(t.Context(), profile, driver); err != nil {
		t.Fatal(err)
	}
	after, err := driver.QueryWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rules changed populated BSP state")
	}
	argv, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(argv), "--apply") || strings.Contains(string(argv), "window\n90\n--") || strings.Contains(string(argv), "space=") || strings.Contains(string(argv), "display=") {
		t.Fatalf("rules applied effects to open windows: %s", argv)
	}
	if !strings.Contains(string(argv), "manage=off") {
		t.Fatalf("desk rule missing: %s", argv)
	}
}

func TestRealizeSpaceLayoutKeepsUnfloatedWindowsOnTargetDisplay(t *testing.T) {
	// Captured display geometry from the three-display wake incident. The stub
	// models yabai's unfloat behavior: a floating window tiles on the active
	// space, even after --space assigned it to another display.
	profileData, err := os.ReadFile("testdata/profile.three-displays.cue")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := config.Parse("profile.three-displays.cue", profileData)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		space wm.SpaceID
		index int
		kind  wm.LayoutKind
	}{
		{name: "AW columns while G9 focused", space: "22", index: 2, kind: wm.LayoutColumns},
		{name: "G9 columns while AW focused", space: "25", index: 3, kind: wm.LayoutColumns},
		{name: "laptop stack while G9 focused", space: "3", index: 1, kind: wm.TargetStack},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			windowsPath := filepath.Join(root, "windows.json")
			spacesPath := filepath.Join(root, "spaces.json")
			focusedSpace := 3
			if test.index == focusedSpace {
				focusedSpace = 2
			}
			windows := fmt.Sprintf(`[
				{"id":210,"app":"Zen","display":%d,"space":%d,"has-ax-reference":true,"is-floating":true},
				{"id":220,"app":"Zen","display":%d,"space":%d,"has-ax-reference":true,"is-floating":true},
				{"id":40,"app":"Ghostty","display":%d,"space":%d,"has-ax-reference":true,"has-focus":true}
			]`, test.index, test.index, test.index, test.index, focusedSpace, focusedSpace)
			for path, data := range map[string]string{
				windowsPath: windows,
				spacesPath: `[{"id":3,"index":1,"display":1,"type":"stack"},
					{"id":22,"index":2,"display":2,"type":"bsp"},
					{"id":25,"index":3,"display":3,"type":"bsp"}]`,
			} {
				if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("TESS_YABAI_WINDOWS", windowsPath)
			t.Setenv("TESS_YABAI_SPACES", spacesPath)
			t.Setenv("TESS_YABAI_DISPLAYS", "testdata/displays.three-displays.json")
			t.Setenv("TESS_YABAI_LOG", filepath.Join(root, "argv.log"))
			t.Setenv("TESS_YABAI_EXIT_CODE", "")
			t.Setenv("TESS_YABAI_MUTATE", "1")
			t.Setenv("TESS_YABAI_ACTIVE_UNFLOAT", "1")
			driver := New(os.Getenv("TESS_YABAI"))
			displays, err := driver.QueryDisplays(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if got := engine.ResolveDesk(profile, displays); !reflect.DeepEqual(got, profile.Desk) {
				t.Fatalf("three displays selected a subset topology: %+v", got)
			}
			target := wm.SpaceLayoutTarget{Kind: test.kind, Tracks: [][]int{{210}, {220}}, Weights: []float64{1, 1}}
			if err := driver.RealizeSpaceLayout(t.Context(), test.space, target); err != nil {
				t.Fatal(err)
			}
			for _, id := range []int{210, 220} {
				window, found, err := driver.QueryWindow(t.Context(), id)
				if err != nil || !found {
					t.Fatalf("QueryWindow(%d) = %t, %v", id, found, err)
				}
				if window.Floating || window.SpaceID != test.space {
					t.Errorf("window %d: floating=%t, space=%s; want tiled on %s", id, window.Floating, window.SpaceID, test.space)
				}
			}
		})
	}
}

func TestThreeDisplayWakePlan(t *testing.T) {
	root := t.TempDir()
	profileData, err := os.ReadFile("testdata/profile.three-displays.cue")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := config.Parse("profile.three-displays.cue", profileData)
	if err != nil {
		t.Fatal(err)
	}
	// The nine desk windows start on G9, as after the reported wake.
	// Give the wave terminal its configured title so every desk slot can claim.
	windowsPath := filepath.Join(root, "windows.json")
	spacesPath := filepath.Join(root, "spaces.json")
	for path, data := range map[string]string{
		windowsPath: `[
			{"id":210,"app":"Zen","space":3,"display":3,"has-ax-reference":true},
			{"id":87,"app":"Obsidian","space":3,"display":3,"has-ax-reference":true},
			{"id":2282,"app":"Ghostty","title":"devbox","space":3,"display":3,"has-ax-reference":true},
			{"id":262,"app":"Zen","space":3,"display":3,"has-ax-reference":true},
			{"id":97,"app":"Code","space":3,"display":3,"has-ax-reference":true},
			{"id":221,"app":"Zen","space":3,"display":3,"has-ax-reference":true},
			{"id":220,"app":"Zen","space":3,"display":3,"has-ax-reference":true},
			{"id":114,"app":"Linear","space":3,"display":3,"has-ax-reference":true},
			{"id":40,"app":"Ghostty","title":"mbp","space":3,"display":3,"has-ax-reference":true,"has-focus":true}
		]`,
		spacesPath: `[{"id":3,"index":1,"display":1,"type":"stack"},
			{"id":22,"index":2,"display":2,"type":"bsp"},
			{"id":25,"index":3,"display":3,"type":"bsp"}]`,
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TESS_YABAI_WINDOWS", windowsPath)
	t.Setenv("TESS_YABAI_SPACES", spacesPath)
	t.Setenv("TESS_YABAI_DISPLAYS", "testdata/displays.three-displays.json")
	t.Setenv("TESS_YABAI_LOG", filepath.Join(root, "argv.log"))
	t.Setenv("TESS_YABAI_EXIT_CODE", "")
	t.Setenv("TESS_YABAI_MUTATE", "1")
	t.Setenv("TESS_YABAI_ACTIVE_UNFLOAT", "1")
	driver := New(os.Getenv("TESS_YABAI"))
	world, err := wm.Snapshot(t.Context(), driver)
	if err != nil {
		t.Fatal(err)
	}
	ops, filled := engine.DeskPlan(profile, engine.World{
		Displays: world.Displays, Spaces: world.Spaces, Windows: world.Windows,
	})
	wantTracks := map[wm.SpaceID][][]int{
		"25": {{210, 87}, {2282, 262, 97}, {40}},
		"22": {{221}, {220, 114}},
		"3":  {},
	}
	for _, op := range ops {
		if layout, ok := op.(engine.RealizeLayoutOp); ok {
			if !reflect.DeepEqual(layout.Target.Tracks, wantTracks[layout.Space]) {
				t.Fatalf("space %s tracks = %v, want %v", layout.Space, layout.Target.Tracks, wantTracks[layout.Space])
			}
			t.Logf("planned space %s: tracks=%v weights=%v", layout.Space, layout.Target.Tracks, layout.Target.Weights)
		}
	}
	if len(filled) != 9 {
		t.Fatalf("claimed %d desk windows, want 9", len(filled))
	}
	if err := executor.RunPlan(t.Context(), driver, ops, nil); err != nil {
		t.Fatal(err)
	}
	for space, tracks := range wantTracks {
		for _, track := range tracks {
			for _, id := range track {
				window, found, err := driver.QueryWindow(t.Context(), id)
				if err != nil || !found || window.Floating || window.SpaceID != space {
					t.Errorf("window %d = %+v, found=%t, err=%v; want tiled on %s", id, window, found, err, space)
				}
			}
		}
	}
}
