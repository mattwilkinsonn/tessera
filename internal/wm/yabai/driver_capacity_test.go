package yabai

import (
	"encoding/json"
	"errors"
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
)

func capacityDriver(t *testing.T, count int) (*Driver, string) {
	t.Helper()
	root := t.TempDir()
	windows := make([]RawYabaiWindow, 0, count+1)
	for id := 1; id <= count; id++ {
		windows = append(windows, RawYabaiWindow{ID: id, Space: 1, HasAXReference: true})
	}
	windows = append(windows, RawYabaiWindow{ID: 90, Space: 2, HasAXReference: true, IsFloating: true})
	data, err := json.Marshal(windows)
	if err != nil {
		t.Fatal(err)
	}
	windowPath := filepath.Join(root, "windows.json")
	spacePath := filepath.Join(root, "spaces.json")
	if err := os.WriteFile(windowPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spacePath, []byte(`[{"id":47,"index":1,"type":"bsp"},{"id":48,"index":2,"type":"bsp"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "argv.log")
	t.Setenv("TESS_YABAI_WINDOWS", windowPath)
	t.Setenv("TESS_YABAI_SPACES", spacePath)
	t.Setenv("TESS_YABAI_LOG", log)
	t.Setenv("TESS_YABAI_EXIT_CODE", "")
	return New(os.Getenv("TESS_YABAI")), log
}

func TestDriverCapacityMutations(t *testing.T) {
	for _, test := range []struct {
		name     string
		run      func(*Driver) error
		mutation string
		full     bool
	}{
		{name: "move out of full source", run: func(d *Driver) error { return d.MoveWindowToSpace(t.Context(), 1, "48") }, mutation: "--space\n2"},
		{name: "float tiled full-space member", run: func(d *Driver) error { return d.ToggleFloat(t.Context(), 1) }, mutation: "--toggle\nfloat"},
		{name: "reject incoming move", run: func(d *Driver) error { return d.MoveWindowToSpace(t.Context(), 90, "47") }, full: true},
		{name: "allow same-space tiled member", run: func(d *Driver) error { return d.MoveWindowToSpace(t.Context(), 1, "47") }, mutation: "--space\n1"},
		{name: "reject incoming stack", run: func(d *Driver) error { return d.StackOnto(t.Context(), 1, 90) }, full: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			driver, log := capacityDriver(t, 24)
			err := test.run(driver)
			if errors.Is(err, wm.ErrSpaceFull) != test.full {
				t.Fatalf("error = %v, full = %v", err, test.full)
			}
			if !test.full && err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			argv := string(data)
			if test.full {
				if strings.Contains(argv, "--space\n") || strings.Contains(argv, "--stack\n") || strings.Contains(argv, "--toggle\n") {
					t.Fatalf("rejected mutation emitted: %s", argv)
				}
			} else if !strings.Contains(argv, test.mutation) {
				t.Fatalf("missing mutation %q: %s", test.mutation, argv)
			}
		})
	}
}

func TestRejectedRehomeKeepsSourceSpace(t *testing.T) {
	driver, log := capacityDriver(t, 24)
	err := executor.RunPlan(t.Context(), driver, []engine.Op{engine.RehomeAndDestroyAction{HomeSpace: "47", StaleSpace: "48"}}, nil)
	if !errors.Is(err, wm.ErrSpaceFull) {
		t.Fatalf("error = %v", err)
	}
	spaces, err := driver.QuerySpaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(spaces) != 2 {
		t.Fatalf("spaces = %v", spaces)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "--destroy") || strings.Contains(string(data), "--space\n1") {
		t.Fatalf("destructive argv after rejected move: %s", data)
	}
}

func TestOverCapSpaceIsNotConverted(t *testing.T) {
	for _, realize := range []bool{false, true} {
		name := "set layout"
		if realize {
			name = "realize layout"
		}
		t.Run(name, func(t *testing.T) {
			driver, log := capacityDriver(t, 25)
			var err error
			if realize {
				err = driver.RealizeSpaceLayout(t.Context(), "47", wm.SpaceLayoutTarget{Kind: wm.TargetStack})
			} else {
				err = driver.SetSpaceLayout(t.Context(), "47", wm.LayoutStack)
			}
			if realize && err != nil || !realize && !errors.Is(err, wm.ErrSpaceFull) {
				t.Fatalf("error = %v", err)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "--layout") {
				t.Fatalf("converted over-cap space: %s", data)
			}
		})
	}
}

func TestDisplaySetupKeepsOverCapLayout(t *testing.T) {
	driver, log := capacityDriver(t, 25)
	path := filepath.Join(t.TempDir(), "displays.json")
	if err := os.WriteFile(path, []byte(`[{"index":1,"frame":{"w":1728,"h":1117},"spaces":[1]}]`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TESS_YABAI_DISPLAYS", path)
	profile := configtest.Profile()
	if err := config.Validate(profile); err != nil {
		t.Fatal(err)
	}
	if err := commands.DisplaySetup(t.Context(), profile, driver); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "--layout") {
		t.Fatalf("converted over-cap laptop space: %s", data)
	}
}

func TestRealizeStackSkipsOverflowMembers(t *testing.T) {
	driver, _ := capacityDriver(t, 0)
	t.Setenv("TESS_YABAI_MUTATE", "1")
	t.Setenv("TESS_YABAI_ACTIVE_UNFLOAT", "1")
	windows := make([]RawYabaiWindow, 0, 25)
	first := make([]int, 0, 24)
	for id := range 25 {
		windows = append(windows, RawYabaiWindow{ID: id + 1, Space: 2, HasAXReference: true, IsFloating: true})
		if id < 24 {
			first = append(first, id+1)
		}
	}
	data, err := json.Marshal(windows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("TESS_YABAI_WINDOWS"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := driver.RealizeSpaceLayout(t.Context(), "47", wm.SpaceLayoutTarget{Kind: wm.TargetStack, Tracks: [][]int{first, {25}}}); err != nil {
		t.Fatal(err)
	}
	current, err := driver.QueryWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	tiled, floating := 0, 0
	for _, window := range current {
		if window.Floating {
			floating++
		} else if window.SpaceID == "47" {
			tiled++
		}
	}
	if tiled != 24 || floating != 1 {
		t.Fatalf("tiled %d, floating %d; want 24, 1", tiled, floating)
	}
	spaces, err := driver.QuerySpaces(t.Context())
	if err != nil || len(spaces) != 2 {
		t.Fatalf("spaces = %v, %v", spaces, err)
	}
}
