package yabai

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/commands"
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/config/configtest"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/executor"
	"github.com/mattwilkinsonn/tessera/internal/wm"
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

func TestQueryFocusedWindowForRebuild(t *testing.T) {
	t.Run("no focus is not a failed query", func(t *testing.T) {
		driver, _ := unfloatSafetyDriver(t)
		ctx := wm.TrackQueryFailures(t.Context())
		_, found, err := driver.QueryFocusedWindow(ctx)
		if err != nil || found || wm.QueryFailed(ctx) {
			t.Fatalf("no focus = found %t, error %v, query failed %t", found, err, wm.QueryFailed(ctx))
		}
	})
	t.Run("query failure is not missing focus", func(t *testing.T) {
		driver, _ := unfloatSafetyDriver(t)
		t.Setenv("TESS_YABAI_WINDOWS_EXIT_CODE", "1")
		ctx := wm.TrackQueryFailures(t.Context())
		_, found, err := driver.QueryFocusedWindow(ctx)
		if err == nil || found || !wm.QueryFailed(ctx) {
			t.Fatalf("query failure = found %t, error %v, query failed %t", found, err, wm.QueryFailed(ctx))
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
				if window.Floating || window.SpaceID != test.space || window.DisplayIdx != test.index {
					t.Errorf("window %d: floating=%t, space=%s, display=%d; want tiled on %s, display %d",
						id, window.Floating, window.SpaceID, window.DisplayIdx, test.space, test.index)
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
		display := map[wm.SpaceID]int{"3": 1, "22": 2, "25": 3}[space]
		for _, track := range tracks {
			for _, id := range track {
				window, found, err := driver.QueryWindow(t.Context(), id)
				if err != nil || !found || window.Floating || window.SpaceID != space || window.DisplayIdx != display {
					t.Errorf("window %d = %+v, found=%t, err=%v; want tiled on %s, display %d",
						id, window, found, err, space, display)
				}
			}
		}
	}
}

func TestUnfloatOneWaitsForFocusedSpace(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	const staleQueries = 2
	t.Setenv("TESS_YABAI_FOCUS_DELAY", strconv.Itoa(staleQueries))
	if err := driver.unfloatOne(t.Context(), 210); err != nil {
		t.Fatalf("unfloatOne: %v", err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(data), "-m\nquery\n--spaces\n--space\n\n"); count != staleQueries+1 {
		t.Errorf("focused-space queries = %d, want %d", count, staleQueries+1)
	}
	if !strings.Contains(string(data), "--toggle\nfloat") {
		t.Errorf("missing unfloat toggle in argv:\n%s", data)
	}
}

func TestUnfloatOneZeroWaitSettingsUseDefaults(t *testing.T) {
	driver, _ := unfloatSafetyDriver(t)
	driver.unfloatFocusTimeout = 0
	driver.unfloatFocusPoll = 0
	var waits []time.Duration
	driver.unfloatWait = func(ctx context.Context, duration time.Duration) error {
		waits = append(waits, duration)
		return waitContext(ctx, duration)
	}
	t.Setenv("TESS_YABAI_FOCUS_DELAY", "3")
	start := time.Now()
	if err := driver.unfloatOne(t.Context(), 210); err != nil {
		t.Fatalf("unfloatOne with zero wait settings: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("unfloatOne returned after %s, want at least 100ms", elapsed)
	}
	if len(waits) != 3 {
		t.Fatalf("poll waits = %d, want 3", len(waits))
	}
	for _, duration := range waits {
		if duration != 50*time.Millisecond {
			t.Errorf("poll duration = %s, want 50ms", duration)
		}
	}
}

func TestUnfloatOneTransientFocusedSpaceFailure(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	t.Setenv("TESS_YABAI_FOCUS_FAILURES", "1")
	ctx := wm.TrackQueryFailures(t.Context())
	if err := driver.unfloatOne(ctx, 210); err != nil {
		t.Fatalf("unfloatOne: %v", err)
	}
	if wm.QueryFailed(ctx) {
		t.Error("transient focused-space failure marked the apply context")
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "--toggle\nfloat") {
		t.Errorf("missing unfloat toggle in argv:\n%s", data)
	}
}

func TestUnfloatOneCancelDuringFocusedSpaceQuery(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	driver.unfloatFocusTimeout = 10 * time.Second
	var config net.ListenConfig
	listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TESS_YABAI_SPACE_NOTIFY", listener.Addr().String())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- driver.unfloatOne(ctx, 210) }()
	conn, err := listener.Accept()
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Errorf("unfloatOne = %v, want context.Canceled", err)
	}
	assertNoUnfloatToggle(t, log)
}

func TestUnfloatOneFocusedSpaceFailuresMarkOnTimeout(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	t.Setenv("TESS_YABAI_FOCUS_FAILURES", "9999")
	ctx := wm.TrackQueryFailures(t.Context())
	err := driver.unfloatOne(ctx, 210)
	if err == nil || !strings.Contains(err.Error(), "did not activate target space") {
		t.Errorf("unfloatOne = %v, want target-space timeout", err)
	}
	if !wm.QueryFailed(ctx) {
		t.Error("focused-space failures were not marked on timeout")
	}
	assertNoUnfloatToggle(t, log)
}

func TestUnfloatOneRecoveredQueryBeforeTimeout(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	t.Setenv("TESS_YABAI_FOCUS_FAILURES", "1")
	t.Setenv("TESS_YABAI_FOCUS_DELAY", "9999")
	ctx := wm.TrackQueryFailures(t.Context())
	err := driver.unfloatOne(ctx, 210)
	if err == nil || !strings.Contains(err.Error(), "did not activate target space") {
		t.Errorf("unfloatOne = %v, want target-space timeout", err)
	}
	if wm.QueryFailed(ctx) {
		t.Error("recovered focused-space query marked the apply context")
	}
	assertNoUnfloatToggle(t, log)
}

func TestUnfloatOneCancelDuringPollWait(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	t.Setenv("TESS_YABAI_FOCUS_STALE", "1")
	t.Setenv("TESS_YABAI_FOCUS_FAILURES", "1")
	driver.unfloatFocusTimeout = 10 * time.Second
	driver.unfloatFocusPoll = 10 * time.Second
	waiting := make(chan struct{})
	driver.unfloatWait = func(ctx context.Context, duration time.Duration) error {
		close(waiting)
		return waitContext(ctx, duration)
	}
	ctx, cancel := context.WithCancel(wm.TrackQueryFailures(t.Context()))
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- driver.unfloatOne(ctx, 210) }()
	select {
	case <-waiting:
		cancel()
	case err := <-result:
		t.Fatalf("unfloatOne returned before poll wait: %v", err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Errorf("unfloatOne = %v, want context.Canceled", err)
	}
	if wm.QueryFailed(ctx) {
		t.Error("canceled poll marked the apply context")
	}
	assertNoUnfloatToggle(t, log)
}

func TestUnfloatOneFocusedSpaceTimeout(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	t.Setenv("TESS_YABAI_FOCUS_STALE", "1")
	start := time.Now()
	err := driver.unfloatOne(t.Context(), 210)
	if err == nil || !strings.Contains(err.Error(), "did not activate target space") {
		t.Errorf("unfloatOne = %v, want target-space timeout", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("unfloatOne returned after %s, before the deadline", elapsed)
	}
	assertNoUnfloatToggle(t, log)
}

func TestUnfloatOneFocusSafety(t *testing.T) {
	for _, test := range []struct {
		name string
		env  string
		want string
	}{
		{name: "successful focus leaves old space active", env: "TESS_YABAI_FOCUS_STALE", want: "target space"},
		{name: "focus exits nonzero", env: "TESS_YABAI_FOCUS_FAIL", want: "focusing window 210"},
	} {
		t.Run(test.name, func(t *testing.T) {
			driver, log := unfloatSafetyDriver(t)
			t.Setenv(test.env, "1")
			err := driver.unfloatOne(t.Context(), 210)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("unfloatOne = %v, want error containing %q", err, test.want)
			}
			assertNoUnfloatToggle(t, log)
		})
	}
	t.Run("context canceled during focus subprocess", func(t *testing.T) {
		driver, log := unfloatSafetyDriver(t)
		var config net.ListenConfig
		listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
			t.Fatal(err)
		}
		t.Setenv("TESS_YABAI_FOCUS_NOTIFY", listener.Addr().String())
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		result := make(chan error, 1)
		go func() { result <- driver.unfloatOne(ctx, 210) }()
		conn, err := listener.Accept()
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Errorf("unfloatOne = %v, want context.Canceled", err)
		}
		assertNoUnfloatToggle(t, log)
	})
	t.Run("unknown focus ID fails", func(t *testing.T) {
		driver, log := unfloatSafetyDriver(t)
		focused, err := driver.FocusWindow(t.Context(), 999)
		if err != nil || focused {
			t.Errorf("FocusWindow(999) = %t, %v; want false, nil", focused, err)
		}
		assertNoUnfloatToggle(t, log)
	})
}

func TestRealizeBSPInsertRecovery(t *testing.T) {
	for _, direction := range []string{"east", "south", "stack"} {
		t.Run(direction, func(t *testing.T) {
			driver, log := unfloatSafetyDriver(t)
			t.Setenv("TESS_YABAI_FOCUS_STALE", "1")
			steps := []BspStep{
				{Args: ArmInsertArgs(210, direction)},
				{Args: MoveWindowToSpaceArgs(210, 2), Unfloat: 210, HasUnfloat: true},
			}
			err := driver.realizeBSP(t.Context(), steps)
			if err == nil || !strings.Contains(err.Error(), "did not activate target space") {
				t.Fatalf("realizeBSP = %v, want target-space timeout", err)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			arm := "-m\nwindow\n210\n--insert\n" + direction + "\n\n"
			if strings.Count(string(data), arm) != 2 || !strings.HasSuffix(string(data), arm) {
				t.Errorf("argv does not end with matching insert toggle:\n%s", data)
			}
		})
	}
}

func TestRealizeBSPInsertSuccess(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	steps := []BspStep{
		{Args: ArmInsertArgs(210, "east")},
		{Args: MoveWindowToSpaceArgs(210, 2), Unfloat: 210, HasUnfloat: true},
	}
	if err := driver.realizeBSP(t.Context(), steps); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "--insert\n"); got != 1 {
		t.Errorf("insert count = %d, want 1", got)
	}
}

func TestRealizeBSPAlreadyTiledWindow(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	if err := os.WriteFile(os.Getenv("TESS_YABAI_WINDOWS"), []byte(`[{"id":210,"space":2,"is-floating":false},{"id":211,"space":2,"is-floating":false}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	steps := []BspStep{
		{Args: ArmInsertArgs(210, "east")},
		{Args: MoveWindowToSpaceArgs(211, 2), Unfloat: 211, HasUnfloat: true},
	}
	if err := driver.realizeBSP(t.Context(), steps); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "--insert\n"); got != 2 {
		t.Errorf("insert count = %d, want 2", got)
	}
}

func TestRealizeBSPPreMoveQueryFailureClearsArm(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	t.Setenv("TESS_YABAI_WINDOWS_EXIT_CODE", "1")
	steps := []BspStep{{Args: ArmInsertArgs(210, "east")}, {Args: MoveWindowToSpaceArgs(210, 2), Unfloat: 210, HasUnfloat: true}}
	if err := driver.realizeBSP(t.Context(), steps); err == nil {
		t.Fatal("realizeBSP succeeded, want query failure")
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	arm := "-m\nwindow\n210\n--insert\neast\n\n"
	if got := strings.Count(string(data), arm); got != 2 {
		t.Errorf("insert count = %d, want 2", got)
	}
	if strings.Contains(string(data), "--space\n") {
		t.Errorf("move sent after query failure: %s", data)
	}
	state, err := os.ReadFile(os.Getenv("TESS_YABAI_ARM"))
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 0 {
		t.Errorf("arm remains set: %s", state)
	}
}

func TestRealizeBSPManagedMoveConsumesArmBeforeQueryFailure(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	arm := filepath.Join(t.TempDir(), "arm")
	t.Setenv("TESS_YABAI_ARM", arm)
	t.Setenv("TESS_YABAI_FAIL_WINDOWS_AFTER_MOVE", "1")
	windows := `[{"id":210,"space":2,"is-floating":false},{"id":211,"space":3,"root-window":true,"is-floating":false,"is-sticky":false,"is-minimized":false,"is-hidden":false,"role":"AXWindow","subrole":"AXStandardWindow","level":0,"can-move":true}]`
	if err := os.WriteFile(os.Getenv("TESS_YABAI_WINDOWS"), []byte(windows), 0600); err != nil {
		t.Fatal(err)
	}
	steps := []BspStep{{Args: ArmInsertArgs(210, "east")}, {Args: MoveWindowToSpaceArgs(211, 2), Unfloat: 211, HasUnfloat: true}}
	if err := driver.realizeBSP(t.Context(), steps); err == nil {
		t.Fatal("realizeBSP succeeded, want query failure")
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "--insert\n"); got != 1 {
		t.Errorf("insert count = %d, want 1", got)
	}
	state, err := os.ReadFile(arm)
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 0 {
		t.Errorf("arm remains set: %s", state)
	}
}

func TestRealizeBSPUnmanageableMoveClearsArm(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	windows := `[{"id":210,"space":2,"is-floating":false},{"id":211,"space":3,"root-window":true,"is-floating":false,"is-sticky":true,"is-minimized":false,"is-hidden":false,"role":"AXWindow","subrole":"AXStandardWindow","level":0,"can-move":true}]`
	if err := os.WriteFile(os.Getenv("TESS_YABAI_WINDOWS"), []byte(windows), 0600); err != nil {
		t.Fatal(err)
	}
	steps := []BspStep{{Args: ArmInsertArgs(210, "east")}, {Args: MoveWindowToSpaceArgs(211, 2), Unfloat: 211, HasUnfloat: true}}
	if err := driver.realizeBSP(t.Context(), steps); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "--insert\n"); got != 2 {
		t.Errorf("insert count = %d, want 2", got)
	}
}

func TestRealizeBSPRuleManagedMoveConsumesArm(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	t.Setenv("TESS_YABAI_RULE_MANAGED", "211")
	windows := `[{"id":210,"space":2,"is-floating":false},{"id":211,"space":3,"root-window":true,"is-floating":false,"role":"AXDialog"}]`
	if err := os.WriteFile(os.Getenv("TESS_YABAI_WINDOWS"), []byte(windows), 0600); err != nil {
		t.Fatal(err)
	}
	steps := []BspStep{{Args: ArmInsertArgs(210, "east")}, {Args: MoveWindowToSpaceArgs(211, 2), Unfloat: 211, HasUnfloat: true}}
	if err := driver.realizeBSP(t.Context(), steps); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "--insert\n"); got != 1 {
		t.Errorf("insert count = %d, want 1", got)
	}
}

func TestRealizeBSPStackMoveConsumesArm(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	windows := `[{"id":210,"space":2,"has-ax-reference":true,"is-floating":false},{"id":211,"space":3,"has-ax-reference":true,"root-window":true,"is-floating":false,"role":"AXWindow","subrole":"AXStandardWindow","level":0,"can-move":true}]`
	if err := os.WriteFile(os.Getenv("TESS_YABAI_WINDOWS"), []byte(windows), 0600); err != nil {
		t.Fatal(err)
	}
	steps := []BspStep{{Args: ArmInsertArgs(210, "stack")}, {Args: MoveWindowToSpaceArgs(211, 2), Unfloat: 211, HasUnfloat: true}}
	if err := driver.realizeBSP(t.Context(), steps); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "--insert\n"); got != 1 {
		t.Errorf("insert count = %d, want 1", got)
	}
	state, err := os.ReadFile(os.Getenv("TESS_YABAI_ARM"))
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 0 {
		t.Errorf("arm remains set: %s", state)
	}
}

func TestRealizeBSPMissingSplitChild(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	t.Setenv("TESS_YABAI_MISSING_SPLIT_CHILD", "1")
	steps := []BspStep{{Args: ArmInsertArgs(210, "east")}, {Args: MoveWindowToSpaceArgs(210, 2), Unfloat: 210, HasUnfloat: true}}
	if err := driver.realizeBSP(t.Context(), steps); err == nil || !strings.Contains(err.Error(), "split-child") {
		t.Fatalf("realizeBSP = %v, want missing split-child error", err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "--insert\n"); got != 1 {
		t.Errorf("insert count = %d, want 1", got)
	}
	if err := os.WriteFile(os.Getenv("TESS_YABAI_ARM"), nil, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRealizeBSPSkipsArmAfterFullUnfloat(t *testing.T) {
	driver, log := capacityDriver(t, 24)
	t.Setenv("TESS_YABAI_MUTATE", "1")
	t.Setenv("TESS_YABAI_INSERT_EXIT_CODE", "1")
	steps := []BspStep{{Args: MoveWindowToSpaceArgs(90, 1), Unfloat: 90, HasUnfloat: true}, {Args: ArmInsertArgs(90, "east")}}
	if err := driver.realizeBSP(t.Context(), steps); err != nil {
		t.Fatalf("realizeBSP: %v", err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "--insert\n") {
		t.Errorf("insert sent for floating anchor: %s", data)
	}
}

func TestRealizeBSPVerbFailures(t *testing.T) {
	for _, test := range []struct {
		name        string
		env         string
		wantInserts int
	}{
		{name: "insert", env: "TESS_YABAI_INSERT_EXIT_CODE", wantInserts: 1},
		{name: "space", env: "TESS_YABAI_SPACE_EXIT_CODE", wantInserts: 2},
		{name: "toggle", env: "TESS_YABAI_TOGGLE_EXIT_CODE", wantInserts: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			driver, log := unfloatSafetyDriver(t)
			t.Setenv(test.env, "1")
			steps := []BspStep{{Args: ArmInsertArgs(210, "east")}, {Args: MoveWindowToSpaceArgs(210, 2), Unfloat: 210, HasUnfloat: true}}
			if err := driver.realizeBSP(t.Context(), steps); err == nil {
				t.Fatal("realizeBSP succeeded, want error")
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(string(data), "--insert\n"); got != test.wantInserts {
				t.Errorf("insert count = %d, want %d", got, test.wantInserts)
			}
		})
	}
}

func TestRealizeBSPCanceledCleanup(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	var config net.ListenConfig
	listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TESS_YABAI_FOCUS_NOTIFY", listener.Addr().String())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	steps := []BspStep{{Args: ArmInsertArgs(210, "east")}, {Args: MoveWindowToSpaceArgs(210, 2), Unfloat: 210, HasUnfloat: true}}
	result := make(chan error, 1)
	go func() { result <- driver.realizeBSP(ctx, steps) }()
	conn, err := listener.Accept()
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("realizeBSP = %v, want context.Canceled", err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "--insert\n"); got != 2 {
		t.Errorf("insert count = %d, want 2", got)
	}
}

func TestRealizeBSPFailureBeforeInsert(t *testing.T) {
	driver, log := unfloatSafetyDriver(t)
	t.Setenv("TESS_YABAI_WINDOWS_EXIT_CODE", "1")
	steps := []BspStep{{Args: ArmInsertArgs(210, "stack")}}
	if err := driver.realizeBSP(t.Context(), steps); err == nil {
		t.Fatal("realizeBSP succeeded, want error")
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "--insert\n") {
		t.Errorf("unexpected insert in argv:\n%s", data)
	}
}

func unfloatSafetyDriver(t *testing.T) (*Driver, string) {
	t.Helper()
	root := t.TempDir()
	windows := filepath.Join(root, "windows.json")
	spaces := filepath.Join(root, "spaces.json")
	for path, data := range map[string]string{
		windows: `[{"id":210,"app":"Zen","display":2,"space":2,"has-ax-reference":true,"is-floating":true}]`,
		spaces: `[{"id":22,"index":2,"display":2,"has-focus":false},
			{"id":25,"index":3,"display":3,"has-focus":true}]`,
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(root, "argv.log")
	t.Setenv("TESS_YABAI_WINDOWS", windows)
	t.Setenv("TESS_YABAI_SPACES", spaces)
	t.Setenv("TESS_YABAI_LOG", log)
	t.Setenv("TESS_YABAI_EXIT_CODE", "")
	t.Setenv("TESS_YABAI_MUTATE", "1")
	t.Setenv("TESS_YABAI_ACTIVE_UNFLOAT", "1")
	driver := New(os.Getenv("TESS_YABAI"))
	driver.unfloatFocusTimeout = 150 * time.Millisecond
	driver.unfloatFocusPoll = 5 * time.Millisecond
	arm := filepath.Join(root, "arm")
	t.Setenv("TESS_YABAI_ARM", arm)
	t.Cleanup(func() {
		state, err := os.ReadFile(arm)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("reading arm: %v", err)
		} else if len(state) != 0 {
			t.Errorf("arm remains set: %s", state)
		}
	})
	return driver, log
}

func assertNoUnfloatToggle(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "--toggle\nfloat") {
		t.Errorf("unexpected unfloat toggle in argv:\n%s", data)
	}
}
