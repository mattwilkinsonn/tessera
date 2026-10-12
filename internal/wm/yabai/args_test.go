package yabai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/wm"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tess-yabai-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	path := filepath.Join(dir, "stubyabai")
	command := exec.CommandContext(context.Background(), "go", "build", "-o", path, "./testdata/stubyabai")
	output, err := command.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "building stub yabai: %v\n%s", err, output)
		os.Exit(1)
	}
	if err := os.Setenv("TESS_YABAI", path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	status := m.Run()
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		status = 1
	}
	os.Exit(status)
}

func TestYabaiArgs(t *testing.T) {

	display := 1
	space := 2
	manage := false
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{name: "queries", got: QueryWindowsArgs(), want: []string{"-m", "query", "--windows"}},
		{name: "windows on space", got: QueryWindowsOnSpaceArgs(3), want: []string{"-m", "query", "--windows", "--space", "3"}},
		{name: "window", got: QueryWindowArgs(2383), want: []string{"-m", "query", "--windows", "--window", "2383"}},
		{name: "focused window", got: QueryFocusedWindowArgs(), want: []string{"-m", "query", "--windows", "--window"}},
		{name: "spaces", got: QuerySpacesArgs(), want: []string{"-m", "query", "--spaces"}},
		{name: "focused space", got: QueryFocusedSpaceArgs(), want: []string{"-m", "query", "--spaces", "--space"}},
		{name: "displays", got: QueryDisplaysArgs(), want: []string{"-m", "query", "--displays"}},
		{name: "create space", got: CreateSpaceArgs(), want: []string{"-m", "space", "--create"}},
		{name: "destroy space", got: DestroySpaceArgs(4), want: []string{"-m", "space", "4", "--destroy"}},
		{name: "label space", got: LabelSpaceArgs(4, "lap-arc"), want: []string{"-m", "space", "4", "--label", "lap-arc"}},
		{name: "set space layout", got: SetSpaceLayoutArgs(4, wm.LayoutBSP), want: []string{"-m", "space", "4", "--layout", "bsp"}},
		{name: "move space", got: MoveSpaceToIndexArgs(4, 2), want: []string{"-m", "space", "4", "--move", "2"}},
		{name: "balance all", got: BalanceSpaceArgs(nil), want: []string{"-m", "space", "--balance"}},
		{name: "balance one", got: BalanceSpaceArgs(&space), want: []string{"-m", "space", "2", "--balance"}},
		{name: "move window space", got: MoveWindowToSpaceArgs(78, 3), want: []string{"-m", "window", "78", "--space", "3"}},
		{name: "move window display index", got: MoveWindowToDisplayArgs(78, wm.DisplaySelector{Index: 2}), want: []string{"-m", "window", "78", "--display", "2"}},
		{name: "move window display position", got: MoveWindowToDisplayArgs(78, wm.DisplaySelector{Position: wm.DisplayNext}), want: []string{"-m", "window", "78", "--display", "next"}},
		{name: "split ratio", got: SetSplitRatioArgs(78, 0.3), want: []string{"-m", "window", "78", "--ratio", "abs:0.3"}},
		{name: "ratio argument", got: SetSplitRatioArgArgs(78, "abs:0.3000"), want: []string{"-m", "window", "78", "--ratio", "abs:0.3000"}},
		{name: "toggle split", got: ToggleSplitArgs(78), want: []string{"-m", "window", "78", "--toggle", "split"}},
		{name: "toggle float", got: ToggleFloatArgs(78), want: []string{"-m", "window", "78", "--toggle", "float"}},
		{name: "insert east", got: ArmInsertArgs(78, "east"), want: []string{"-m", "window", "78", "--insert", "east"}},
		{name: "insert stack", got: ArmInsertArgs(78, "stack"), want: []string{"-m", "window", "78", "--insert", "stack"}},
		{name: "swap windows", got: SwapWindowsArgs(wm.DirectionWest), want: []string{"-m", "window", "--swap", "west"}},
		{name: "warp window", got: WarpWindowArgs(wm.DirectionEast), want: []string{"-m", "window", "--warp", "east"}},
		{name: "resize window", got: ResizeWindowArgs(wm.EdgeRight, 100, 0), want: []string{"-m", "window", "--resize", "right:100:0"}},
		{name: "focus window", got: FocusWindowArgs(78), want: []string{"-m", "window", "--focus", "78"}},
		{name: "focus direction", got: FocusWindowDirArgs(wm.FocusEast), want: []string{"-m", "window", "--focus", "east"}},
		{name: "focus stack", got: FocusWindowDirArgs(wm.FocusStackNext), want: []string{"-m", "window", "--focus", "stack.next"}},
		{name: "focus display index", got: FocusDisplayArgs(wm.DisplaySelector{Index: 2}), want: []string{"-m", "display", "--focus", "2"}},
		{name: "focus display position", got: FocusDisplayArgs(wm.DisplaySelector{Position: wm.DisplayNext}), want: []string{"-m", "display", "--focus", "next"}},
		{name: "list rules", got: RuleListArgs(), want: []string{"-m", "rule", "--list"}},
		{name: "remove rule", got: RuleRemoveArgs("auto:code"), want: []string{"-m", "rule", "--remove", "auto:code"}},
		{name: "add display rule", got: RuleAddArgs(RuleAdd{Label: "auto:code", App: "^Code$", Display: &display}), want: []string{"-m", "rule", "--add", "label=auto:code", "app=^Code$", "display=1"}},
		{name: "add space rule", got: RuleAddArgs(RuleAdd{Label: "auto:default", App: "^.*$", Space: &space}), want: []string{"-m", "rule", "--add", "label=auto:default", "app=^.*$", "space=2"}},
		{name: "add manage rule", got: RuleAddArgs(RuleAdd{Label: "auto:akiflow-dialog", App: "^Akiflow$", Subrole: "AXDialog", Manage: &manage}), want: []string{"-m", "rule", "--add", "label=auto:akiflow-dialog", "app=^Akiflow$", "subrole=AXDialog", "manage=off"}},
		{name: "apply rules", got: RuleApplyArgs(), want: []string{"-m", "rule", "--apply"}},
		{name: "add labelled signal", got: SignalAddArgs(wm.EventDisplayAdded, "tessera-display_added", "echo display_added | nc -U /tmp/tessera-daemon.sock"), want: []string{"-m", "signal", "--add", "event=display_added", "label=tessera-display_added", "action=echo display_added | nc -U /tmp/tessera-daemon.sock"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.DeepEqual(t, test.got, test.want)
		})
	}
}

func TestRatioArg(t *testing.T) {
	tests := []struct {
		name  string
		ratio float64
		want  string
		warn  string
	}{
		{name: "stable precision", ratio: 1.0 / 3, want: "abs:0.3333"},
		{name: "lower clamp", ratio: 0.05, want: "abs:0.1000", warn: "ratio 0.05 clamped to 0.1000"},
		{name: "upper clamp", ratio: 0.95, want: "abs:0.9000", warn: "ratio 0.95 clamped to 0.9000"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var warnings []string
			got := RatioArg(test.ratio, func(line string) { warnings = append(warnings, line) })
			assert.Equal(t, got, test.want)
			if test.warn == "" {
				assert.Equal(t, len(warnings), 0)
				return
			}
			assert.DeepEqual(t, warnings, []string{test.warn})
		})
	}
}

func TestBSPSteps(t *testing.T) {
	tests := []struct {
		name   string
		target wm.SpaceLayoutTarget
		want   []BspStep
	}{
		{
			name:   "columns stack extras and unfloat moves",
			target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{10, 11}, {12}, {13, 14}}, Weights: []float64{3, 4, 3}},
			want: []BspStep{
				{Args: []string{"-m", "space", "7", "--layout", "bsp"}},
				{Args: []string{"-m", "window", "10", "--space", "7"}, Settle: 400, Unfloat: 10, HasUnfloat: true},
				{Args: []string{"-m", "window", "10", "--insert", "east"}, Settle: 150},
				{Args: []string{"-m", "window", "12", "--space", "7"}, Settle: 400, Unfloat: 12, HasUnfloat: true},
				{Args: []string{"-m", "window", "12", "--insert", "east"}, Settle: 150},
				{Args: []string{"-m", "window", "13", "--space", "7"}, Settle: 400, Unfloat: 13, HasUnfloat: true},
				{Args: []string{"-m", "window", "10", "--ratio", "abs:0.3000"}, Settle: 150},
				{Args: []string{"-m", "window", "12", "--ratio", "abs:0.5714"}, Settle: 150},
				{Args: []string{"-m", "window", "10", "--insert", "stack"}, Settle: 150},
				{Args: []string{"-m", "window", "11", "--space", "7"}, Settle: 350, Unfloat: 11, HasUnfloat: true},
				{Args: []string{"-m", "window", "13", "--insert", "stack"}, Settle: 150},
				{Args: []string{"-m", "window", "14", "--space", "7"}, Settle: 350, Unfloat: 14, HasUnfloat: true},
			},
		},
		{
			name:   "equal columns rows and one track",
			target: wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{1}, {2}, {3}, {4}}, Weights: []float64{1, 1, 1, 1}},
		},
		{
			name:   "empty tracks",
			target: wm.SpaceLayoutTarget{Kind: wm.LayoutRows, Tracks: [][]int{{}, {}}},
			want:   nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := BSPSteps(7, test.target, nil)
			if test.name == "equal columns rows and one track" {
				var ratios []string
				var inserts []string
				for _, step := range got {
					for index, arg := range step.Args {
						if arg == "--ratio" {
							ratios = append(ratios, step.Args[index+1])
						}
						if arg == "--insert" {
							inserts = append(inserts, step.Args[index+1])
						}
					}
				}
				assert.DeepEqual(t, ratios, []string{"abs:0.2500", "abs:0.3333", "abs:0.5000"})
				assert.DeepEqual(t, inserts, []string{"east", "east", "east"})
				rows := BSPSteps(2, wm.SpaceLayoutTarget{Kind: wm.LayoutRows, Tracks: [][]int{{1}, {2}}, Weights: []float64{2, 1}}, nil)
				assert.DeepEqual(t, rows[2].Args, []string{"-m", "window", "1", "--insert", "south"})
				assert.DeepEqual(t, rows[4].Args, []string{"-m", "window", "1", "--ratio", "abs:0.6667"})
				single := BSPSteps(2, wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{1}}}, nil)
				assert.Equal(t, len(single), 2)
				return
			}
			assert.DeepEqual(t, got, test.want)
		})
	}
	t.Run("clamps out of range chain ratio with target context", func(t *testing.T) {
		var warnings []string
		steps := BSPSteps(4, wm.SpaceLayoutTarget{Kind: wm.LayoutColumns, Tracks: [][]int{{1}, {2}}, Weights: []float64{9, 0.5}}, func(line string) { warnings = append(warnings, line) })
		assert.DeepEqual(t, steps[4].Args, []string{"-m", "window", "1", "--ratio", "abs:0.9000"})
		assert.DeepEqual(t, warnings, []string{"space 4: ratio 0.9473684210526315 clamped to 0.9000"})
	})
}

func TestNormalizeWindows(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "stubyabai", "windows.live.json"))
	assert.NilError(t, err)
	var rawWindows []RawYabaiWindow
	assert.NilError(t, json.Unmarshal(fixture, &rawWindows))
	rawSpaces := make([]RawYabaiSpace, 17)
	for index := range rawSpaces {
		rawSpaces[index] = RawYabaiSpace{ID: index + 101, Index: index + 1}
	}
	windows, err := NormalizeWindows(rawWindows, rawSpaces)
	assert.NilError(t, err)

	t.Run("resolves .space index to the stable SpaceId", func(t *testing.T) {
		window := windowWithID(t, windows, 2383)
		assert.Equal(t, window.SpaceID, wm.SpaceID("103"))
		assert.Equal(t, window.DisplayIdx, 1)
		assert.Equal(t, window.Title, "pc | π ! model-evals")
	})
	t.Run("Ghostty flags and frame normalize to camelCase", func(t *testing.T) {
		window := windowWithID(t, windows, 2383)
		assert.Equal(t, window.Minimized, false)
		assert.Equal(t, window.Floating, false)
		assert.Equal(t, window.Sticky, false)
		assert.Equal(t, window.Visible, true)
		assert.Equal(t, window.SplitType, wm.SplitNone)
		assert.DeepEqual(t, window.Frame, wm.Frame{Y: 32, W: 1728, H: 1085})
	})
	t.Run("Arc window normalizes with its own space resolution", func(t *testing.T) {
		window := windowWithID(t, windows, 2444)
		assert.Equal(t, window.SpaceID, wm.SpaceID("102"))
		assert.Equal(t, window.Title, "Rigel Development")
		assert.Equal(t, window.Minimized, false)
		assert.Equal(t, window.Floating, false)
	})
	t.Run("throws on a window referencing an unknown space index", func(t *testing.T) {
		_, err := NormalizeWindows([]RawYabaiWindow{{ID: 999, Space: 999, HasAXReference: true}}, rawSpaces)
		assert.ErrorContains(t, err, "window 999 references unknown space index 999")
	})
	t.Run("keeps addressable non-standard windows", func(t *testing.T) {
		ids := make(map[int]struct{}, len(windows))
		for _, window := range windows {
			ids[window.ID] = struct{}{}
		}
		for _, id := range []int{104, 8047, 7273} {
			_, found := ids[id]
			assert.Assert(t, found)
		}
	})
	t.Run("drops a phantom window yabai holds no AX reference for", func(t *testing.T) {
		got, err := NormalizeWindows([]RawYabaiWindow{{ID: 10937, Space: 999}}, nil)
		assert.NilError(t, err)
		assert.DeepEqual(t, got, []wm.Window{})
	})
}

func windowWithID(t *testing.T, windows []wm.Window, id int) wm.Window {
	t.Helper()
	for _, window := range windows {
		if window.ID == id {
			return window
		}
	}
	t.Fatalf("window %d not found", id)
	return wm.Window{}
}

func TestNormalizeSpaceAndDisplay(t *testing.T) {
	got := NormalizeSpace(RawYabaiSpace{ID: 42, Index: 3, Label: "main", Display: 2, Windows: []int{78, 2444, 141}, Type: "stack"})
	assert.DeepEqual(t, got, wm.Space{ID: "42", Label: "main", DisplayIdx: 2, WindowIDs: []int{78, 2444, 141}, Layout: wm.LayoutStack})
	assert.Equal(t, NormalizeSpace(RawYabaiSpace{Type: "float"}).Layout, wm.LayoutFloat)
	assert.Equal(t, NormalizeSpace(RawYabaiSpace{Type: "weird"}).Layout, wm.LayoutBSP)
	spaceIDs := IndexToSpaceID([]RawYabaiSpace{{ID: 201, Index: 1}, {ID: 202, Index: 2}})
	display, err := NormalizeDisplay(RawYabaiDisplay{Index: 1, Frame: wm.Frame{W: 5120, H: 1440}, Spaces: []int{1, 2}}, spaceIDs)
	assert.NilError(t, err)
	assert.DeepEqual(t, display, wm.Display{Idx: 1, Frame: wm.Frame{W: 5120, H: 1440}, SpaceIDs: []wm.SpaceID{"201", "202"}})
	_, err = NormalizeDisplay(RawYabaiDisplay{Index: 2, Spaces: []int{3}}, spaceIDs)
	assert.ErrorContains(t, err, "display 2 references unknown space index 3")
}
func TestSpawnWindow(t *testing.T) {
	t.Run("runs argv directly and reports non-zero exit", func(t *testing.T) {
		driver := New("yabai")
		err := driver.SpawnWindow(t.Context(), []string{"/usr/bin/false"})
		assert.ErrorContains(t, err, "spawning window failed (exit 1): /usr/bin/false")
	})
	t.Run("reports stderr from an early launcher exit", func(t *testing.T) {
		driver := New("yabai")
		err := driver.SpawnWindow(t.Context(), []string{"/bin/sh", "-c", "echo boom >&2; exit 1"})
		assert.ErrorContains(t, err, "boom")
	})
	t.Run("returns after caller cancellation", func(t *testing.T) {
		driver := New("yabai")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err := driver.SpawnWindow(ctx, []string{"/bin/sleep", "2"})
		assert.Assert(t, errors.Is(err, context.Canceled))
	})
	t.Run("returns after the launcher timeout", func(t *testing.T) {
		driver := New("yabai")
		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
		defer cancel()
		err := driver.SpawnWindow(ctx, []string{"/bin/sleep", "2"})
		assert.Assert(t, errors.Is(err, context.DeadlineExceeded))
	})
	t.Run("a launcher outliving the wait survives caller cancellation", func(t *testing.T) {
		dir := t.TempDir()
		gate, started, out := filepath.Join(dir, "gate"), filepath.Join(dir, "started"), filepath.Join(dir, "out")
		for _, path := range []string{gate, started} {
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := syscall.Mkfifo(out, 0o600); err != nil {
			t.Fatal(err)
		}
		startedRead := make(chan error, 1)
		go func() {
			f, err := os.Open(started)
			if err != nil {
				startedRead <- err
				return
			}
			defer f.Close()
			var marker [1]byte
			_, err = f.Read(marker[:])
			startedRead <- err
		}()
		driver := &Driver{path: "yabai", spawnWait: 0}
		ctx, cancel := context.WithCancel(t.Context())
		script := `printf x > "$3"; read x < "$1"; echo alive > "$2"`
		if err := driver.SpawnWindow(ctx, []string{"/bin/sh", "-c", script, "sh", gate, out, started}); err != nil {
			t.Fatal(err)
		}
		cancel()
		select {
		case err := <-startedRead:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("launcher did not reach the event gate")
		}
		gateOpened := make(chan error, 1)
		go func() {
			f, err := os.OpenFile(gate, os.O_WRONLY, 0)
			if err == nil {
				_, err = f.WriteString("go\n")
				err = errors.Join(err, f.Close())
			}
			gateOpened <- err
		}()
		select {
		case err := <-gateOpened:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("launcher did not survive caller cancellation")
		}
		got, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "alive\n" {
			t.Fatalf("launcher wrote %q; want alive", got)
		}
	})
}

func TestRecordingStub(t *testing.T) {
	windowsFixture := filepath.Join("testdata", "stubyabai", "windows.live.json")
	log := filepath.Join(t.TempDir(), "argv.log")
	command := exec.CommandContext(t.Context(), os.Getenv("TESS_YABAI"), "-m", "query", "--windows", "--space", "3")
	command.Env = append(os.Environ(), "TESS_YABAI_LOG="+log, "TESS_YABAI_WINDOWS="+windowsFixture)
	output, err := command.Output()
	assert.NilError(t, err)
	var windows []json.RawMessage
	assert.NilError(t, json.Unmarshal(output, &windows))
	assert.Assert(t, len(windows) > 0)
	args, err := os.ReadFile(log)
	assert.NilError(t, err)
	golden.Assert(t, string(args), "stub-query-windows.golden")
}

func TestArgvCorpus(t *testing.T) {
	space := 4
	cases := [][]string{
		QueryWindowsArgs(), QueryWindowsOnSpaceArgs(3), QueryWindowArgs(78), QueryFocusedWindowArgs(), QuerySpacesArgs(), QueryFocusedSpaceArgs(), QueryDisplaysArgs(),
		CreateSpaceArgs(), DestroySpaceArgs(4), LabelSpaceArgs(4, "lap-arc"), SetSpaceLayoutArgs(4, wm.LayoutBSP), SetSpaceLayoutArgs(4, wm.LayoutStack), SetSpaceLayoutArgs(4, wm.LayoutFloat),
		MoveSpaceToIndexArgs(4, 2), BalanceSpaceArgs(nil), BalanceSpaceArgs(&space), MoveWindowToSpaceArgs(78, 3),
		MoveWindowToDisplayArgs(78, wm.DisplaySelector{Index: 2}), MoveWindowToDisplayArgs(78, wm.DisplaySelector{Position: wm.DisplayNext}),
		SetSplitRatioArgs(78, 0.3), SetSplitRatioArgArgs(78, "abs:0.3000"), ToggleSplitArgs(78), ToggleFloatArgs(78),
		ArmInsertArgs(78, "east"), ArmInsertArgs(78, "west"), ArmInsertArgs(78, "north"), ArmInsertArgs(78, "south"), ArmInsertArgs(78, "stack"),
		SwapWindowsArgs(wm.DirectionWest), SwapWindowsArgs(wm.DirectionSouth), SwapWindowsArgs(wm.DirectionNorth), SwapWindowsArgs(wm.DirectionEast),
		WarpWindowArgs(wm.DirectionWest), WarpWindowArgs(wm.DirectionSouth), WarpWindowArgs(wm.DirectionNorth), WarpWindowArgs(wm.DirectionEast),
		ResizeWindowArgs(wm.EdgeRight, 100, 0), ResizeWindowArgs(wm.EdgeLeft, -100, 0), ResizeWindowArgs(wm.EdgeTop, 0, -100), ResizeWindowArgs(wm.EdgeBottom, 0, 100),
		FocusWindowArgs(78), FocusWindowDirArgs(wm.FocusWest), FocusWindowDirArgs(wm.FocusSouth), FocusWindowDirArgs(wm.FocusNorth), FocusWindowDirArgs(wm.FocusEast),
		FocusWindowDirArgs(wm.FocusStackNext), FocusWindowDirArgs(wm.FocusStackPrev), FocusWindowDirArgs(wm.FocusStackFirst), FocusWindowDirArgs(wm.FocusStackLast),
		FocusDisplayArgs(wm.DisplaySelector{Index: 2}), FocusDisplayArgs(wm.DisplaySelector{Position: wm.DisplayNext}),
		RuleListArgs(), RuleRemoveArgs("auto:code"), RuleAddArgs(RuleAdd{Label: "auto:code", App: "^Code$"}),
		RuleAddArgs(RuleAdd{Label: "auto:code", App: "^Code$", Display: new(1)}),
		RuleAddArgs(RuleAdd{Label: "auto:default", App: "^.*$", Space: new(2)}),
		RuleAddArgs(RuleAdd{Label: "auto:akiflow-dialog", App: "^Akiflow$", Subrole: "AXDialog", Manage: new(false)}),
		RuleApplyArgs(), SignalAddArgs(wm.EventDisplayAdded, "tessera-display_added", "echo display_added | nc -U /tmp/tessera-daemon.sock"),
	}
	log := filepath.Join(t.TempDir(), "argv.log")
	for _, args := range cases {
		command := exec.CommandContext(t.Context(), os.Getenv("TESS_YABAI"), args...)
		command.Env = append(os.Environ(), "TESS_YABAI_LOG="+log, "TESS_YABAI_EXIT_CODE=0")
		assert.NilError(t, command.Run())
	}
	actual, err := os.ReadFile(log)
	assert.NilError(t, err)
	golden.Assert(t, string(actual), "argv/corpus.golden")
}
