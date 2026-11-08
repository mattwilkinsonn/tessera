package yabai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattwilkinsonn/tessera/internal/commands"
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/daemon"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

func TestRebuildRestoresOriginalFocus(t *testing.T) {
	for _, entry := range []string{"wake", "apply"} {
		t.Run(entry, func(t *testing.T) {
			for _, scenario := range []string{
				"G9 focus survives rebuild",
				"focused window closes",
				"nothing focused",
				"realization and restore fail",
				"restore rejected",
			} {
				t.Run(scenario, func(t *testing.T) {
					testRebuildFocus(t, entry, scenario)
				})
			}
		})
	}
}

func testRebuildFocus(t *testing.T, entry, scenario string) {
	t.Helper()
	profile, driver, env, log := rebuildFocusFixture(t, scenario != "nothing focused")
	realizeErr := errors.New("realization interrupted")
	restoreErr := errors.New("restore interrupted")
	driver.afterRealize = func() error {
		switch scenario {
		case "focused window closes":
			removeRebuildWindow(t, 900)
		case "realization and restore fail":
			driver.restoreErr = restoreErr
			return realizeErr
		case "restore rejected":
			t.Setenv("TESS_YABAI_FOCUS_FAIL", "1")
		}
		return nil
	}
	var err error
	if entry == "wake" {
		err = daemon.WakeRoutine(profile, driver, env, filepath.Join(t.TempDir(), "daemon.sock"))(t.Context())
	} else {
		err = commands.Apply(t.Context(), profile, driver, env)
	}
	switch scenario {
	case "realization and restore fail":
		if !errors.Is(err, realizeErr) || !errors.Is(err, restoreErr) {
			t.Errorf("rebuild error = %v, want both realization and restore errors", err)
		}
	case "restore rejected":
		if err == nil || !strings.Contains(err.Error(), "restoring focus") {
			t.Errorf("rebuild error = %v, want rejected restore error", err)
		}
	default:
		if err != nil {
			t.Fatal(err)
		}
	}
	if driver.realizations == 0 {
		t.Fatal("test did not run layout realization")
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	argv := string(data)
	if got := strings.Count(argv, "-m\nquery\n--windows\n--window\n\n"); got != 1 {
		t.Errorf("focused-window snapshots = %d, want exactly one at outer %s boundary", got, entry)
	}
	wantRestores := 1
	if scenario == "nothing focused" || scenario == "focused window closes" {
		wantRestores = 0
	}
	if got := strings.Count(argv, "-m\nwindow\n--focus\n900\n\n"); got != wantRestores {
		t.Errorf("original-window focus calls = %d, want %d", got, wantRestores)
	}
	if scenario == "G9 focus survives rebuild" {
		focused, found, err := driver.QueryFocusedWindow(t.Context())
		if err != nil || !found || focused.ID != 900 || focused.DisplayIdx != 3 {
			t.Errorf("final focus = %+v, found=%t, err=%v; want original G9 window 900", focused, found, err)
		}
		if !strings.HasSuffix(argv, "-m\nwindow\n--focus\n900\n\n") {
			t.Error("restore was not the last yabai operation")
		}
	}
}

type rebuildFocusDriver struct {
	*Driver
	afterRealize func() error
	realizations int
	restoreErr   error
}

func (d *rebuildFocusDriver) RealizeSpaceLayout(ctx context.Context, id wm.SpaceID, target wm.SpaceLayoutTarget) error {
	if err := d.Driver.RealizeSpaceLayout(ctx, id, target); err != nil {
		return err
	}
	d.realizations++
	if d.afterRealize != nil {
		after := d.afterRealize
		d.afterRealize = nil
		return after()
	}
	return nil
}

func (d *rebuildFocusDriver) FocusWindow(ctx context.Context, id int) (bool, error) {
	focused, err := d.Driver.FocusWindow(ctx, id)
	if id == 900 && d.restoreErr != nil {
		return false, errors.Join(err, d.restoreErr)
	}
	return focused, err
}

func rebuildFocusFixture(t *testing.T, focused bool) (*config.Profile, *rebuildFocusDriver, commands.Env, string) {
	t.Helper()
	root := t.TempDir()
	profileData, err := os.ReadFile("testdata/profile.three-displays.cue")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := config.Parse("profile.three-displays.cue", profileData)
	if err != nil {
		t.Fatal(err)
	}
	for name, spec := range profile.Windows {
		spec.Spawn = nil
		profile.Windows[name] = spec
	}
	windows := filepath.Join(root, "windows.json")
	spaces := filepath.Join(root, "spaces.json")
	for path, data := range map[string]string{
		windows: `[
			{"id":900,"app":"User","space":3,"display":3,"has-ax-reference":true,"is-floating":true,"has-focus":FOCUSED},
			{"id":210,"app":"Zen","space":3,"display":3,"has-ax-reference":true},
			{"id":114,"app":"Linear","space":2,"display":2,"has-ax-reference":true},
			{"id":901,"app":"Extra","space":2,"display":2,"has-ax-reference":true,"is-floating":true,"root-window":true,"role":"AXWindow","subrole":"AXStandardWindow","can-move":true,"can-resize":true}
		]`,
		spaces: `[{"id":3,"index":1,"display":1,"type":"stack","label":"laptop"},
			{"id":22,"index":2,"display":2,"type":"bsp","label":"plan"},
			{"id":25,"index":3,"display":3,"type":"bsp","label":"main","has-focus":true}]`,
	} {
		focus := "false"
		if focused {
			focus = "true"
		}
		if err := os.WriteFile(path, []byte(strings.ReplaceAll(data, "FOCUSED", focus)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(root, "argv.log")
	t.Setenv("TESS_YABAI_WINDOWS", windows)
	t.Setenv("TESS_YABAI_SPACES", spaces)
	t.Setenv("TESS_YABAI_DISPLAYS", "testdata/displays.three-displays.json")
	t.Setenv("TESS_YABAI_LOG", log)
	t.Setenv("TESS_YABAI_EXIT_CODE", "")
	t.Setenv("TESS_YABAI_MUTATE", "1")
	t.Setenv("TESS_YABAI_ACTIVE_UNFLOAT", "1")
	env := commands.Env{
		Paths: effects.Paths{
			ApplyLock: filepath.Join(root, "apply.lock"), LaptopLock: filepath.Join(root, "laptop.lock"),
			SignalGuard: filepath.Join(root, "guard"), WindowSlots: filepath.Join(root, "slots.json"),
			FlexOrder: filepath.Join(root, "flex.json"), SpawnLedger: filepath.Join(root, "spawns.json"),
		},
		Nudge: func(context.Context, string) error { return nil }, Stderr: io.Discard,
	}
	return profile, &rebuildFocusDriver{Driver: New(os.Getenv("TESS_YABAI"))}, env, log
}

func removeRebuildWindow(t *testing.T, id int) {
	t.Helper()
	path := os.Getenv("TESS_YABAI_WINDOWS")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var windows []map[string]any
	if err := json.Unmarshal(data, &windows); err != nil {
		t.Fatal(err)
	}
	remaining := make([]map[string]any, 0, len(windows))
	for _, window := range windows {
		if window["id"] != float64(id) {
			remaining = append(remaining, window)
		}
	}
	data, err = json.Marshal(remaining)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
