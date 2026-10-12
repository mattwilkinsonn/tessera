package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/commands"
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/config/configtest"
	"github.com/mattwilkinsonn/tessera/internal/daemon"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
	"github.com/mattwilkinsonn/tessera/internal/wm/yabai"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("/tmp", "tess-cli-test-")
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	stub := filepath.Join(dir, "stubyabai")
	// TestMain has no testing context; this is the root for its setup command.
	build := exec.CommandContext(context.Background(), "go", "build", "-o", stub, "../wm/yabai/testdata/stubyabai")
	if output, err := build.CombinedOutput(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "building stub yabai: %v\n%s", err, output)
		os.Exit(1)
	}
	if err := os.Setenv("TESS_YABAI", stub); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fixture := filepath.Join("..", "wm", "yabai", "testdata", "stubyabai")
	for name, file := range map[string]string{
		"TESS_YABAI_WINDOWS":  "windows.live.json",
		"TESS_YABAI_SPACES":   "spaces.live.json",
		"TESS_YABAI_DISPLAYS": "displays.live.json",
	} {
		if err := os.Setenv(name, filepath.Join(fixture, file)); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	status := m.Run()
	if err := os.RemoveAll(dir); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		status = 1
	}
	os.Exit(status)
}

func TestLiteralChoicesMatchDeclaredCLIGrammar(t *testing.T) {
	choices := map[string][]string{}
	for _, entry := range kinds {
		if len(entry.choices) > 0 {
			choices[entry.name] = entry.choices
		}
	}
	want := map[string][]string{
		"snap":          {"3col", "50-50", "columns"},
		"stack-cycle":   {"next", "prev"},
		"resize":        {"grow", "shrink"},
		"move-display":  {"g9", "aw", "laptop"},
		"cycle-display": {"next", "prev"},
		"focus":         {"west", "south", "north", "east"},
		"swap":          {"west", "south", "north", "east"},
		"warp":          {"west", "south", "north", "east"},
		"insert":        {"east", "west", "north", "south", "stack"},
		"space":         {"bsp", "stack"},
	}
	assert.DeepEqual(t, choices, want)
}

func TestIntegerTokensMatchEffectCLIAcceptedNumericForms(t *testing.T) {
	for _, token := range []string{"7", "007", "+1"} {
		t.Run("valid "+token, func(t *testing.T) {
			command, err := Parse(t.Context(), []string{"focus-slot", token}, "test", io.Discard, io.Discard)
			assert.NilError(t, err)
			slot, ok := command.(commands.FocusSlotCommand)
			if !ok || slot.Number <= 0 {
				t.Fatalf("Parse() = %#v; want a positive FocusSlotCommand", command)
			}
		})
	}
	for _, token := range []string{"2.0", "2e0", "0x10", "", " 1", "1.5", "abc", "9223372036854775808"} {
		t.Run("reject "+token, func(t *testing.T) {
			_, err := Parse(t.Context(), []string{"focus-slot", token}, "test", io.Discard, io.Discard)
			if _, ok := errors.AsType[*UsageError](err); !ok {
				t.Fatalf("Parse() error = %v; want UsageError", err)
			}
		})
	}
}

func TestEffectCLICommands(t *testing.T) {
	dispatchCases := []struct {
		name string
		argv []string
		want commands.Command
	}{
		{name: "apply", argv: []string{"apply"}, want: commands.ApplyCommand{}},
		{name: "laptop", argv: []string{"laptop"}, want: commands.LaptopCommand{}},
		{name: "rules", argv: []string{"rules"}, want: commands.RulesCommand{}},
		{name: "display-setup", argv: []string{"display-setup"}, want: commands.DisplaySetupCommand{}},
		{name: "focus-slot", argv: []string{"focus-slot", "1"}, want: commands.FocusSlotCommand{Number: 1}},
		{name: "snap", argv: []string{"snap", "3col"}, want: commands.SnapCommand{Mode: engine.SnapThreeColumns}},
		{name: "stack-cycle", argv: []string{"stack-cycle", "next"}, want: commands.StackCycleCommand{Direction: commands.CycleNext}},
		{name: "resize", argv: []string{"resize", "grow"}, want: commands.ResizeCommand{Direction: commands.ResizeGrow}},
		{name: "move-display", argv: []string{"move-display", "g9"}, want: commands.MoveDisplayCommand{Display: config.DisplayG9}},
		{name: "cycle-display", argv: []string{"cycle-display", "next"}, want: commands.CycleDisplayCommand{Direction: commands.CycleNext}},
		{name: "reset-splits", argv: []string{"reset-splits"}, want: commands.ResetSplitsCommand{}},
		{name: "columns", argv: []string{"columns"}, want: commands.ColumnsCommand{}},
		{name: "focus", argv: []string{"focus", "west"}, want: commands.FocusCommand{Direction: wm.DirectionWest}},
		{name: "swap", argv: []string{"swap", "west"}, want: commands.SwapCommand{Direction: wm.DirectionWest}},
		{name: "warp", argv: []string{"warp", "west"}, want: commands.WarpCommand{Direction: wm.DirectionWest}},
		{name: "insert", argv: []string{"insert", "east"}, want: commands.InsertCommand{Direction: commands.InsertEast}},
		{name: "toggle-float", argv: []string{"toggle-float"}, want: commands.ToggleFloatCommand{}},
		{name: "balance", argv: []string{"balance"}, want: commands.BalanceCommand{}},
		{name: "space", argv: []string{"space", "bsp"}, want: commands.SpaceCommand{Layout: commands.SpaceBSP}},
		{name: "daemon", argv: []string{"daemon", "--no-bootstrap"}, want: commands.DaemonCommand{NoBootstrap: true}},
		{name: "wake", argv: []string{"wake"}, want: commands.WakeCommand{}},
	}
	assert.Equal(t, len(dispatchCases), len(kinds))
	for _, test := range dispatchCases {
		t.Run("dispatches "+test.name+" through the shared router", func(t *testing.T) {
			got, err := Parse(t.Context(), test.argv, "test", io.Discard, io.Discard)
			assert.NilError(t, err)
			assert.DeepEqual(t, got, test.want)
		})
	}
}

func TestRootCommandRegistersEveryGrammarSubcommandAndInit(t *testing.T) {
	var stdout bytes.Buffer
	_, err := Parse(t.Context(), []string{"--help"}, "test", &stdout, io.Discard)
	assert.NilError(t, err)
	for _, entry := range kinds {
		assert.Assert(t, strings.Contains(stdout.String(), entry.name), entry.name)
	}
	assert.Assert(t, !strings.Contains(stdout.String(), "display-event"))
	assert.Assert(t, !strings.Contains(stdout.String(), "flex-event"))
}

func TestValidSimpleCommandChangesTheFakeDriverWorld(t *testing.T) {
	profile := cliTestProfile(t)
	driver := fake.New(fake.Seed{Spaces: []fake.SeedSpace{{DisplayIdx: 1}}, Windows: []fake.SeedWindow{{ID: 7, App: "Arc", SpaceIndex: 1}}})
	if _, err := driver.FocusWindow(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	if code := runFake(t, profile, driver, []string{"toggle-float"}); code != 0 {
		t.Fatalf("Main() = %d; want 0", code)
	}
	window, ok, err := driver.QueryFocusedWindow(t.Context())
	if err != nil || !ok || !window.Floating {
		t.Fatalf("focused window = %#v, %t, %v; want floating", window, ok, err)
	}
}

func TestSubcommandAndRootHelpDoNotBuildRunDepsAndValidCommandBuildsItOnce(t *testing.T) {
	builds := 0
	deps := Deps{Version: "test", Run: func(context.Context, commands.Command) (int, error) {
		builds++
		return 0, nil
	}}
	for _, argv := range [][]string{{"snap", "--help"}, {"--help"}, {}} {
		assert.Equal(t, Main(t.Context(), argv, io.Discard, io.Discard, deps), 0)
	}
	assert.Equal(t, builds, 0)
	assert.Equal(t, Main(t.Context(), []string{"snap", "3col"}, io.Discard, io.Discard, deps), 0)
	assert.Equal(t, builds, 1)
}

func TestInvalidLiteralsAndInvalidDaemonFlagsCarryParseErrorsWithExit1(t *testing.T) {
	for _, argv := range [][]string{{"snap", "bogus"}, {"daemon", "--bad"}} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			var stderr bytes.Buffer
			code := Main(t.Context(), argv, io.Discard, &stderr, Deps{Version: "test"})
			if code != 1 || !strings.Contains(stderr.String(), "tess:") || !strings.Contains(stderr.String(), "usage: tess") {
				t.Fatalf("Main(%q) = %d, stderr %q", argv, code, stderr.String())
			}
		})
	}
}

func TestMalformedArgvFailsBeforeCallingTheDriver(t *testing.T) {
	for _, argv := range [][]string{{"bogus"}, {"snap"}, {"focus-slot"}, {"snap", "3col", "extra"}, {"focus", "west", "east"}, {"focus-slot", "x"}, {"snap", "bad"}, {"focus-slot", "2.0"}} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			called := false
			code := Main(t.Context(), argv, io.Discard, io.Discard, Deps{Version: "test", Run: func(context.Context, commands.Command) (int, error) { called = true; return 0, nil }})
			if code != 1 || called {
				t.Fatalf("Main(%q) = %d, driver called %t", argv, code, called)
			}
		})
	}
}

func TestMissingAndThrowingHandlersUseTypedOneLineErrorWrappers(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(old)
	var stderr bytes.Buffer
	code := Main(t.Context(), []string{"focus", "east"}, io.Discard, &stderr, Deps{Version: "test", Run: func(context.Context, commands.Command) (int, error) { return 1, errors.New("driver gone") }})
	if code != 1 || stderr.Len() != 0 || !strings.Contains(logs.String(), "driver gone") {
		t.Fatalf("Main() = %d, stderr %q, logs %q", code, stderr.String(), logs.String())
	}
}

func TestLaptopContentionReturnsExitCode1Silently(t *testing.T) {
	profile := cliTestProfile(t)
	root := t.TempDir()
	lock, acquired, err := effects.AcquireLock(filepath.Join(root, "laptop.lock"))
	if err != nil || !acquired {
		t.Fatalf("AcquireLock() = %t, %v", acquired, err)
	}
	defer func() { assert.NilError(t, lock.Unlock()) }()
	var stderr bytes.Buffer
	code := Main(t.Context(), []string{"laptop"}, io.Discard, &stderr, Deps{Version: "test", Run: func(ctx context.Context, command commands.Command) (int, error) {
		env := commands.Env{Paths: effects.Paths{LaptopLock: filepath.Join(root, "laptop.lock"), SignalGuard: filepath.Join(root, "guard"), FlexOrder: filepath.Join(root, "flex")}, Nudge: func(context.Context, string) error { return nil }, Stderr: &stderr}
		return commands.Run(ctx, profile, command, fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}}}), env)
	}})
	if code != 1 || stderr.Len() != 0 {
		t.Fatalf("Main() = %d, stderr %q", code, stderr.String())
	}
}

func TestLaptopOnMultiDisplayWorldSkippedExit0(t *testing.T) {
	profile := cliTestProfile(t)
	driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1}, {Idx: 2}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 2}}})
	root := t.TempDir()
	var stderr bytes.Buffer
	code := Main(t.Context(), []string{"laptop"}, io.Discard, &stderr, Deps{Version: "test", Run: func(ctx context.Context, command commands.Command) (int, error) {
		env := commands.Env{Paths: effects.Paths{LaptopLock: filepath.Join(root, "laptop.lock"), SignalGuard: filepath.Join(root, "guard"), FlexOrder: filepath.Join(root, "flex")}, Nudge: func(context.Context, string) error { return nil }, Stderr: &stderr}
		return commands.Run(ctx, profile, command, driver, env)
	}})
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("Main() = %d, stderr %q; want 0 and silent", code, stderr.String())
	}
}

func TestHelpAndVersionReturnSuccess(t *testing.T) {
	for _, test := range []struct {
		name string
		argv []string
		want string
	}{
		{name: "bare tess", argv: []string{}, want: "Usage:"},
		{name: "--help", argv: []string{"--help"}, want: "Usage:"},
		{name: "snap --help", argv: []string{"snap", "--help"}, want: "tess snap {3col|50-50|columns}"},
		{name: "--version", argv: []string{"--version"}, want: "tess v1.2.3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			code := Main(t.Context(), test.argv, &stdout, io.Discard, Deps{Version: "1.2.3"})
			if code != 0 || !strings.Contains(stdout.String(), test.want) {
				t.Fatalf("Main() = %d, stdout %q; want %q", code, stdout.String(), test.want)
			}
		})
	}
}

func TestCompletionZshExits0AndCompleteSnapListsThreeChoices(t *testing.T) {
	var stdout bytes.Buffer
	code := Main(t.Context(), []string{"completion", "zsh"}, &stdout, io.Discard, Deps{Version: "test"})
	if code != 0 || !strings.Contains(stdout.String(), "#compdef") {
		t.Fatalf("completion zsh = %d, output begins %q", code, stdout.String())
	}
	stdout.Reset()
	code = Main(t.Context(), []string{"__complete", "snap", ""}, &stdout, io.Discard, Deps{Version: "test"})
	if code != 0 {
		t.Fatalf("__complete = %d, output %q", code, stdout.String())
	}
	for _, choice := range []string{"3col", "50-50", "columns"} {
		if !strings.Contains(stdout.String(), choice) {
			t.Fatalf("__complete output %q lacks %q", stdout.String(), choice)
		}
	}
}

func TestStubYabaiCorpus(t *testing.T) {
	//nolint:usetesting // /tmp keeps socket-related fixture paths under macOS's path limit.
	root, err := os.MkdirTemp("/tmp", "tess-cli-corpus-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { assert.NilError(t, os.RemoveAll(root)) }()
	log := filepath.Join(root, "argv.log")
	profile := cliTestProfile(t)
	t.Setenv("TESS_YABAI_LOG", log)
	var corpus strings.Builder
	for _, test := range cliArgvCases() {
		ok := t.Run("dispatches "+test.name+" to stub yabai", func(t *testing.T) {
			if err := os.WriteFile(log, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			code := Main(t.Context(), test.argv, io.Discard, io.Discard, Deps{Version: "test", Run: func(ctx context.Context, command commands.Command) (int, error) {
				env := commands.Env{Paths: effects.Paths{ApplyLock: filepath.Join(root, "apply.lock"), LaptopLock: filepath.Join(root, "laptop.lock"), SignalGuard: filepath.Join(root, "guard"), FlexOrder: filepath.Join(root, "flex")}, Nudge: func(context.Context, string) error { return nil }, Stderr: io.Discard}
				return commands.Run(ctx, profile, command, yabai.New(os.Getenv("TESS_YABAI")), env)
			}})
			if code != 0 {
				t.Fatalf("Main() = %d", code)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if len(data) == 0 {
				t.Fatalf("stub yabai recorded no argv for %q", test.argv)
			}
			corpus.WriteString("## tess " + strings.Join(test.argv, " ") + "\n")
			corpus.WriteString(canonicalArgv(string(data)))
		})
		if !ok {
			return
		}
	}
	// The exact yabai argv each command emits; regenerate with -update.
	golden.Assert(t, corpus.String(), "argv/cli-corpus.golden")
}

// canonicalArgv sorts each run of adjacent queries: wm.Snapshot issues them concurrently.
func canonicalArgv(log string) string {
	calls := strings.SplitAfter(log, "\n\n")
	for start := 0; start < len(calls); {
		end := start
		for end < len(calls) && strings.HasPrefix(calls[end], "-m\nquery\n") {
			end++
		}
		slices.Sort(calls[start:end])
		start = max(end, start+1)
	}
	return strings.Join(calls, "")
}

func TestSIGTERMDuringStubBlockedApplyExits130AndLeavesLockFree(t *testing.T) {
	//nolint:usetesting // /tmp keeps socket-related fixture paths under macOS's path limit.
	root, err := os.MkdirTemp("/tmp", "tess-term-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { assert.NilError(t, os.RemoveAll(root)) }()
	binary := filepath.Join(root, "tess")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../../cmd/tess")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building tess: %v\n%s", err, output)
	}
	started := filepath.Join(root, "started")
	stub := filepath.Join(root, "blocked-yabai")
	stubCode := []byte("#!/bin/sh\n: > \"$TESS_BLOCK_STARTED\"\nexec /usr/bin/yes\n")
	if err := os.WriteFile(stub, stubCode, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, "tessera-apply.lock")
	command := exec.CommandContext(t.Context(), binary, "apply")
	// TESSERA_PROFILE= overrides any host profile, so the default profile loads.
	command.Env = append(os.Environ(), "HOME="+root, "TMPDIR="+root, "XDG_CACHE_HOME="+root, "XDG_CONFIG_HOME="+root, "TESSERA_PROFILE=", "TESS_YABAI="+stub, "TESS_BLOCK_STARTED="+started)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	startedTicker := time.NewTicker(10 * time.Millisecond)
	defer startedTicker.Stop()
	startedTimeout := time.NewTimer(15 * time.Second)
	defer startedTimeout.Stop()
	for {
		if _, err := os.Stat(started); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
			t.Fatal(err)
		}
		select {
		case <-startedTicker.C:
		case <-startedTimeout.C:
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
			t.Fatal("stub invocation was not observed")
		}
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		if killErr := command.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			t.Errorf("killing tess after SIGTERM failure: %v", killErr)
		}
		if waitErr := command.Wait(); waitErr != nil {
			if _, ok := errors.AsType[*exec.ExitError](waitErr); !ok {
				t.Errorf("waiting for tess after SIGTERM failure: %v", waitErr)
			}
		}
		t.Fatal(err)
	}
	waitErr := command.Wait()
	exitErr, ok := errors.AsType[*exec.ExitError](waitErr)
	if !ok || exitErr.ExitCode() != 130 {
		t.Fatalf("tess exit = %v; want 130", waitErr)
	}
	lock, acquired, err := effects.AcquireLock(lockPath)
	if err != nil || !acquired {
		t.Fatalf("apply lock after SIGTERM = %t, %v", acquired, err)
	}
	assert.NilError(t, lock.Unlock())
}

func TestSecondSIGTERMDuringShutdownKillsByDefault(t *testing.T) {
	//nolint:usetesting // /tmp keeps socket-related fixture paths under macOS's path limit.
	root, err := os.MkdirTemp("/tmp", "tess-term2-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { assert.NilError(t, os.RemoveAll(root)) }()
	binary := filepath.Join(root, "tess")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../../cmd/tess")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building tess: %v\n%s", err, output)
	}
	// The orphan holds yabai's stdout, so after the first SIGTERM shutdown waits out WaitDelay.
	orphanPID := filepath.Join(root, "orphan.pid")
	stub := filepath.Join(root, "lingering-yabai")
	stubCode := []byte("#!/bin/sh\n/bin/sleep 600 &\necho $! > \"$TESS_ORPHAN_PID\"\nexec /bin/sleep 600\n")
	if err := os.WriteFile(stub, stubCode, 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), binary, "apply")
	command.Env = append(os.Environ(), "HOME="+root, "TMPDIR="+root, "XDG_CACHE_HOME="+root, "XDG_CONFIG_HOME="+root, "TESSERA_PROFILE=", "TESS_YABAI="+stub, "TESS_ORPHAN_PID="+orphanPID)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var pid []byte
	for len(pid) == 0 {
		<-ticker.C
		data, err := os.ReadFile(orphanPID)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		pid = data
	}
	orphan, err := strconv.Atoi(strings.TrimSpace(string(pid)))
	assert.NilError(t, err)
	defer func() {
		if err := syscall.Kill(orphan, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Error(err)
		}
	}()
	// Keep signalling: the first starts shutdown, a later one must kill tess outright.
	var waitErr error
	for done := false; !done; {
		select {
		case waitErr = <-waited:
			done = true
		case <-ticker.C:
			if err := command.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Fatal(err)
			}
		}
	}
	exitErr, ok := errors.AsType[*exec.ExitError](waitErr)
	assert.Assert(t, ok, "tess exit = %v", waitErr)
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	assert.Assert(t, ok && status.Signaled() && status.Signal() == syscall.SIGTERM, "tess exit = %v; want killed by SIGTERM", waitErr)
}

func TestWakeWithNoDaemonExits1(t *testing.T) {
	//nolint:usetesting // /tmp keeps socket-related fixture paths under macOS's path limit.
	root, err := os.MkdirTemp("/tmp", "tess-wake-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { assert.NilError(t, os.RemoveAll(root)) }()
	var stderr bytes.Buffer
	code := Main(t.Context(), []string{"wake"}, io.Discard, &stderr, Deps{Version: "test", Run: func(ctx context.Context, command commands.Command) (int, error) {
		env := commands.Env{Paths: effects.Paths{DaemonSocket: filepath.Join(root, "d.sock")}, Stderr: &stderr}
		return commands.Run(ctx, cliTestProfile(t), command, fake.New(fake.Seed{}), env)
	}})
	if code != 1 || !strings.Contains(stderr.String(), "tess: daemon not running") {
		t.Fatalf("Main() = %d, stderr %q", code, stderr.String())
	}
}

func TestUsageErrorNamesUnknownCommandAndMainExits1(t *testing.T) {
	_, err := Parse(t.Context(), []string{"bogus"}, "test", io.Discard, io.Discard)
	usage, ok := errors.AsType[*UsageError](err)
	if !ok || !strings.Contains(usage.Msg, "bogus") {
		t.Fatalf("Parse() error = %v; want UsageError naming bogus", err)
	}
	var stderr bytes.Buffer
	code := Main(t.Context(), []string{"bogus"}, io.Discard, &stderr, Deps{Version: "test"})
	if code != 1 || !strings.Contains(stderr.String(), "bogus") {
		t.Fatalf("Main() = %d, stderr %q", code, stderr.String())
	}
}

func TestDefaultCompletionCommandSupportsAllShells(t *testing.T) {
	for _, shell := range []string{"bash", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			var stdout bytes.Buffer
			code := Main(t.Context(), []string{"completion", shell}, &stdout, io.Discard, Deps{Version: "test"})
			if code != 0 || stdout.Len() == 0 {
				t.Fatalf("completion %s = %d, bytes=%d", shell, code, stdout.Len())
			}
		})
	}
}

func TestDaemonAlreadyRunningExits0(t *testing.T) {
	//nolint:usetesting // /tmp keeps socket-related fixture paths under macOS's path limit.
	root, err := os.MkdirTemp("/tmp", "tess-daemon-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { assert.NilError(t, os.RemoveAll(root)) }()
	socket := filepath.Join(root, "d.sock")
	listener, err := daemon.Listen(t.Context(), socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { assert.NilError(t, listener.Close()) }()
	var stderr bytes.Buffer
	code := Main(t.Context(), []string{"daemon", "--no-bootstrap"}, io.Discard, &stderr, Deps{
		Version: "test",
		Run: func(ctx context.Context, command commands.Command) (int, error) {
			return commands.Run(ctx, cliTestProfile(t), command, fake.New(fake.Seed{}), commands.Env{Stderr: io.Discard})
		},
		DaemonOptions: func(_ context.Context, command commands.DaemonCommand) (daemon.Options, error) {
			return daemon.Options{Socket: socket, Bootstrap: !command.NoBootstrap}, nil
		},
	})
	assert.Equal(t, code, 0)
	assert.Equal(t, stderr.String(), "tess: daemon already running\n")
}

func TestParseAlwaysUsesNonNilArgs(t *testing.T) {
	command, err := Parse(t.Context(), nil, "test", io.Discard, io.Discard)
	assert.NilError(t, err)
	assert.Assert(t, command == nil)
}

func cliTestProfile(t *testing.T) *config.Profile {
	t.Helper()
	profile := configtest.Profile()
	if err := config.Validate(profile); err != nil {
		t.Fatal(err)
	}
	return profile
}

func runFake(t *testing.T, profile *config.Profile, driver *fake.Driver, argv []string) int {
	t.Helper()
	return Main(t.Context(), argv, io.Discard, io.Discard, Deps{Version: "test", Run: func(ctx context.Context, command commands.Command) (int, error) {
		return commands.Run(ctx, profile, command, driver, commands.Env{Stderr: io.Discard})
	}})
}

func cliArgvCases() []struct {
	name string
	argv []string
} {
	return []struct {
		name string
		argv []string
	}{
		{name: "focus-slot", argv: []string{"focus-slot", "1"}},
		{name: "snap-3col", argv: []string{"snap", "3col"}}, {name: "snap-50-50", argv: []string{"snap", "50-50"}}, {name: "snap-columns", argv: []string{"snap", "columns"}},
		{name: "stack-cycle-next", argv: []string{"stack-cycle", "next"}}, {name: "stack-cycle-prev", argv: []string{"stack-cycle", "prev"}},
		{name: "resize-grow", argv: []string{"resize", "grow"}}, {name: "resize-shrink", argv: []string{"resize", "shrink"}},
		{name: "move-display-g9", argv: []string{"move-display", "g9"}}, {name: "move-display-aw", argv: []string{"move-display", "aw"}}, {name: "move-display-laptop", argv: []string{"move-display", "laptop"}},
		{name: "cycle-display-next", argv: []string{"cycle-display", "next"}}, {name: "cycle-display-prev", argv: []string{"cycle-display", "prev"}},
		{name: "focus-west", argv: []string{"focus", "west"}}, {name: "focus-south", argv: []string{"focus", "south"}}, {name: "focus-north", argv: []string{"focus", "north"}}, {name: "focus-east", argv: []string{"focus", "east"}},
		{name: "swap-west", argv: []string{"swap", "west"}}, {name: "swap-south", argv: []string{"swap", "south"}}, {name: "swap-north", argv: []string{"swap", "north"}}, {name: "swap-east", argv: []string{"swap", "east"}},
		{name: "warp-west", argv: []string{"warp", "west"}}, {name: "warp-south", argv: []string{"warp", "south"}}, {name: "warp-north", argv: []string{"warp", "north"}}, {name: "warp-east", argv: []string{"warp", "east"}},
		{name: "insert-east", argv: []string{"insert", "east"}}, {name: "insert-west", argv: []string{"insert", "west"}}, {name: "insert-north", argv: []string{"insert", "north"}}, {name: "insert-south", argv: []string{"insert", "south"}}, {name: "insert-stack", argv: []string{"insert", "stack"}},
		{name: "space-bsp", argv: []string{"space", "bsp"}}, {name: "space-stack", argv: []string{"space", "stack"}}, {name: "toggle-float", argv: []string{"toggle-float"}}, {name: "balance", argv: []string{"balance"}}, {name: "reset-splits", argv: []string{"reset-splits"}}, {name: "columns", argv: []string{"columns"}},
	}
}

func TestFocusSlotFocusesTheResolvedSlotWindow(t *testing.T) {
	profile := cliTestProfile(t)
	driver := fake.New(fake.Seed{Spaces: []fake.SeedSpace{{DisplayIdx: 1}}, Windows: []fake.SeedWindow{{ID: 42, App: "Ghostty", Title: "pc", SpaceIndex: 1}, {ID: 43, App: "Arc", Title: "x", SpaceIndex: 1}}})
	if code := runFake(t, profile, driver, []string{"focus-slot", "1"}); code != 0 {
		t.Fatalf("Main() = %d; want 0", code)
	}
	window, ok, err := driver.QueryFocusedWindow(t.Context())
	if err != nil || !ok || window.ID != 42 {
		t.Fatalf("focused window = %#v, %t, %v; want id 42", window, ok, err)
	}
}

func TestToggleFloatFlipsTheFocusedWindowsFloat(t *testing.T) {
	profile := cliTestProfile(t)
	driver := fake.New(fake.Seed{Spaces: []fake.SeedSpace{{DisplayIdx: 1}}, Windows: []fake.SeedWindow{{ID: 7, App: "Arc", SpaceIndex: 1}}})
	if _, err := driver.FocusWindow(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	if code := runFake(t, profile, driver, []string{"toggle-float"}); code != 0 {
		t.Fatalf("Main() = %d; want 0", code)
	}
	window, ok, err := driver.QueryFocusedWindow(t.Context())
	if err != nil || !ok || !window.Floating {
		t.Fatalf("focused window = %#v, %t, %v; want floating", window, ok, err)
	}
}

func TestSpaceSetsTheFocusedSpacesLayout(t *testing.T) {
	profile := cliTestProfile(t)
	driver := fake.New(fake.Seed{Spaces: []fake.SeedSpace{{DisplayIdx: 1, Layout: wm.LayoutBSP}}, Windows: []fake.SeedWindow{{ID: 9, App: "Arc", SpaceIndex: 1}}})
	if _, err := driver.FocusWindow(t.Context(), 9); err != nil {
		t.Fatal(err)
	}
	if code := runFake(t, profile, driver, []string{"space", "stack"}); code != 0 {
		t.Fatalf("Main() = %d; want 0", code)
	}
	space, ok, err := driver.QueryFocusedSpace(t.Context())
	if err != nil || !ok || space.Layout != wm.LayoutStack {
		t.Fatalf("focused space = %#v, %t, %v; want stack", space, ok, err)
	}
}

func TestRawPassthroughFocusIsNoopAndExits0(t *testing.T) {
	profile := cliTestProfile(t)
	driver := fake.New(fake.Seed{Spaces: []fake.SeedSpace{{DisplayIdx: 1}}, Windows: []fake.SeedWindow{{ID: 1, App: "Arc", SpaceIndex: 1}}})
	before, err := driver.QuerySpaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if code := runFake(t, profile, driver, []string{"focus", "west"}); code != 0 {
		t.Fatalf("Main() = %d; want 0", code)
	}
	after, err := driver.QuerySpaces(t.Context())
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("spaces after focus = %#v, %v; want %#v", after, err, before)
	}
}

func TestEveryCommandKindDispatchesWithoutThrowingOnABenignWorld(t *testing.T) {
	profile := cliTestProfile(t)
	for _, test := range []struct {
		name string
		argv []string
	}{
		{name: "apply", argv: []string{"apply"}}, {name: "laptop", argv: []string{"laptop"}}, {name: "rules", argv: []string{"rules"}}, {name: "display-setup", argv: []string{"display-setup"}},
		{name: "focus-slot", argv: []string{"focus-slot", "1"}}, {name: "snap", argv: []string{"snap", "3col"}}, {name: "stack-cycle", argv: []string{"stack-cycle", "next"}}, {name: "resize", argv: []string{"resize", "grow"}},
		{name: "move-display", argv: []string{"move-display", "g9"}}, {name: "cycle-display", argv: []string{"cycle-display", "next"}}, {name: "reset-splits", argv: []string{"reset-splits"}}, {name: "columns", argv: []string{"columns"}},
		{name: "focus", argv: []string{"focus", "west"}}, {name: "swap", argv: []string{"swap", "west"}}, {name: "warp", argv: []string{"warp", "west"}}, {name: "insert", argv: []string{"insert", "east"}},
		{name: "toggle-float", argv: []string{"toggle-float"}}, {name: "balance", argv: []string{"balance"}}, {name: "space", argv: []string{"space", "bsp"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			command, err := Parse(t.Context(), test.argv, "test", io.Discard, io.Discard)
			assert.NilError(t, err)
			code, err := commands.Run(t.Context(), profile, command, fake.New(fake.Seed{}), commands.Env{Nudge: func(context.Context, string) error { return nil }, Stderr: io.Discard})
			if err != nil || code < 0 {
				t.Fatalf("Run() = %d, %v", code, err)
			}
		})
	}
}
