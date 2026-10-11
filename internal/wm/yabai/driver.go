package yabai

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec" //nolint:depguard // yabai and configured launchers require subprocess execution.
	"strconv"
	"strings"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/wm"
)

// spawnTimeout is how long SpawnWindow waits for a launcher before leaving it running.
const spawnTimeout = 5 * time.Second

type Driver struct {
	path      string
	spawnWait time.Duration
}

func New(yabaiPath string) *Driver {
	return &Driver{path: yabaiPath, spawnWait: spawnTimeout}
}

func (d *Driver) QueryWindows(ctx context.Context) ([]wm.Window, error) {
	var raw []RawYabaiWindow
	if err := d.query(ctx, QueryWindowsArgs(), &raw); err != nil {
		return nil, err
	}
	var spaces []RawYabaiSpace
	if err := d.query(ctx, QuerySpacesArgs(), &spaces); err != nil {
		return nil, err
	}
	return NormalizeWindows(raw, spaces)
}

func (d *Driver) QueryWindowsOnSpace(ctx context.Context, id wm.SpaceID) ([]wm.Window, error) {
	index, found, err := d.resolveIndex(ctx, id)
	if err != nil || !found {
		return nil, err
	}
	var raw []RawYabaiWindow
	if err := d.query(ctx, QueryWindowsOnSpaceArgs(index), &raw); err != nil {
		return nil, err
	}
	var spaces []RawYabaiSpace
	if err := d.query(ctx, QuerySpacesArgs(), &spaces); err != nil {
		return nil, err
	}
	return NormalizeWindows(raw, spaces)
}

func (d *Driver) QuerySpaces(ctx context.Context) ([]wm.Space, error) {
	var raw []RawYabaiSpace
	if err := d.query(ctx, QuerySpacesArgs(), &raw); err != nil {
		return nil, err
	}
	spaces := make([]wm.Space, len(raw))
	for index, space := range raw {
		spaces[index] = NormalizeSpace(space)
	}
	return spaces, nil
}

func (d *Driver) QueryDisplays(ctx context.Context) ([]wm.Display, error) {
	var rawDisplays []RawYabaiDisplay
	if err := d.query(ctx, QueryDisplaysArgs(), &rawDisplays); err != nil {
		return nil, err
	}
	var rawSpaces []RawYabaiSpace
	if err := d.query(ctx, QuerySpacesArgs(), &rawSpaces); err != nil {
		return nil, err
	}
	spaceIDs := IndexToSpaceID(rawSpaces)
	displays := make([]wm.Display, len(rawDisplays))
	for index, display := range rawDisplays {
		normalized, err := NormalizeDisplay(display, spaceIDs)
		if err != nil {
			return nil, err
		}
		displays[index] = normalized
	}
	return displays, nil
}

func (d *Driver) QueryFocusedSpace(ctx context.Context) (wm.Space, bool, error) {
	var raw RawYabaiSpace
	found, err := d.queryOrNull(ctx, QueryFocusedSpaceArgs(), &raw)
	if err != nil || !found {
		return wm.Space{}, false, err
	}
	return NormalizeSpace(raw), true, nil
}

func (d *Driver) QueryFocusedWindow(ctx context.Context) (wm.Window, bool, error) {
	var raw RawYabaiWindow
	found, err := d.queryOrNull(ctx, QueryFocusedWindowArgs(), &raw)
	if err != nil || !found {
		return wm.Window{}, false, err
	}
	var spaces []RawYabaiSpace
	if err := d.query(ctx, QuerySpacesArgs(), &spaces); err != nil {
		return wm.Window{}, false, err
	}
	window, err := NormalizeWindow(raw, IndexToSpaceID(spaces))
	return window, err == nil, err
}

func (d *Driver) CreateSpace(ctx context.Context, _ int) (wm.SpaceID, bool, error) {
	var before []RawYabaiSpace
	if err := d.query(ctx, QuerySpacesArgs(), &before); err != nil {
		return "", false, err
	}
	beforeIDs := make(map[int]struct{}, len(before))
	for _, space := range before {
		beforeIDs[space.ID] = struct{}{}
	}
	if _, _, err := d.run(ctx, CreateSpaceArgs()); err != nil {
		return "", false, err
	}
	for attempt := range 10 {
		if attempt > 0 {
			if err := waitContext(ctx, 50*time.Millisecond); err != nil {
				return "", false, err
			}
		}
		var after []RawYabaiSpace
		if err := d.query(ctx, QuerySpacesArgs(), &after); err != nil {
			return "", false, err
		}
		for _, space := range after {
			if _, exists := beforeIDs[space.ID]; !exists {
				return wm.SpaceID(strconv.Itoa(space.ID)), true, nil
			}
		}
	}
	return "", false, nil
}

func (d *Driver) DestroySpace(ctx context.Context, id wm.SpaceID) (bool, error) {
	index, found, err := d.resolveIndex(ctx, id)
	if err != nil || !found {
		return false, err
	}
	_, exitCode, err := d.run(ctx, DestroySpaceArgs(index))
	return exitCode == 0, err
}

func (d *Driver) LabelSpace(ctx context.Context, id wm.SpaceID, label string) error {
	index, found, err := d.resolveIndex(ctx, id)
	if err != nil || !found {
		return err
	}
	if _, _, err := d.run(ctx, LabelSpaceArgs(index, label)); err != nil {
		return err
	}
	return waitContext(ctx, 50*time.Millisecond)
}

func (d *Driver) SetSpaceLayout(ctx context.Context, id wm.SpaceID, layout wm.Layout) error {
	index, found, err := d.resolveIndex(ctx, id)
	if err != nil || !found {
		return err
	}
	_, _, err = d.run(ctx, SetSpaceLayoutArgs(index, layout))
	return err
}

func (d *Driver) MoveSpaceToIndex(ctx context.Context, id wm.SpaceID, toIndex int) error {
	index, found, err := d.resolveIndex(ctx, id)
	if err != nil || !found {
		return err
	}
	_, _, err = d.run(ctx, MoveSpaceToIndexArgs(index, toIndex))
	return err
}

func (d *Driver) BalanceSpace(ctx context.Context, id *wm.SpaceID) error {
	if id == nil {
		_, _, err := d.run(ctx, BalanceSpaceArgs(nil))
		return err
	}
	index, found, err := d.resolveIndex(ctx, *id)
	if err != nil || !found {
		return err
	}
	_, _, err = d.run(ctx, BalanceSpaceArgs(&index))
	return err
}

func (d *Driver) RealizeSpaceLayout(ctx context.Context, id wm.SpaceID, target wm.SpaceLayoutTarget) error {
	index, found, err := d.resolveIndex(ctx, id)
	if err != nil || !found {
		return err
	}
	if target.Kind == wm.TargetStack {
		for _, track := range target.Tracks {
			for _, windowID := range track {
				if _, _, err := d.run(ctx, MoveWindowToSpaceArgs(windowID, index)); err != nil {
					return err
				}
				if err := waitContext(ctx, 350*time.Millisecond); err != nil {
					return err
				}
				if err := d.unfloatOne(ctx, windowID); err != nil {
					return err
				}
			}
		}
		_, _, err := d.run(ctx, SetSpaceLayoutArgs(index, wm.LayoutStack))
		return err
	}
	for _, step := range BSPSteps(index, target, nil) {
		if _, _, err := d.run(ctx, step.Args); err != nil {
			return err
		}
		if step.Settle > 0 {
			if err := waitContext(ctx, time.Duration(step.Settle)*time.Millisecond); err != nil {
				return err
			}
		}
		if step.HasUnfloat {
			if err := d.unfloatOne(ctx, step.Unfloat); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *Driver) MoveWindowToSpace(ctx context.Context, windowID int, id wm.SpaceID) error {
	index, found, err := d.resolveIndex(ctx, id)
	if err != nil || !found {
		return err
	}
	if _, _, err := d.run(ctx, MoveWindowToSpaceArgs(windowID, index)); err != nil {
		return err
	}
	return waitContext(ctx, 100*time.Millisecond)
}

func (d *Driver) MoveWindowToDisplay(ctx context.Context, windowID int, selector wm.DisplaySelector) (bool, error) {
	_, exitCode, err := d.run(ctx, MoveWindowToDisplayArgs(windowID, selector))
	return exitCode == 0, err
}

func (d *Driver) SetSplitRatio(ctx context.Context, windowID int, ratio float64) error {
	_, _, err := d.run(ctx, SetSplitRatioArgs(windowID, ratio))
	return err
}

func (d *Driver) ToggleSplit(ctx context.Context, windowID int) error {
	_, _, err := d.run(ctx, ToggleSplitArgs(windowID))
	return err
}

func (d *Driver) ToggleFloat(ctx context.Context, windowID int) error {
	_, _, err := d.run(ctx, ToggleFloatArgs(windowID))
	return err
}

func (d *Driver) ArmInsert(ctx context.Context, windowID int, direction wm.Direction) error {
	_, _, err := d.run(ctx, ArmInsertArgs(windowID, string(direction)))
	return err
}

func (d *Driver) StackOnto(ctx context.Context, targetWindowID, windowID int) error {
	var target RawYabaiWindow
	found, err := d.queryOrNull(ctx, QueryWindowArgs(targetWindowID), &target)
	if err != nil || !found {
		return err
	}
	if _, _, err := d.run(ctx, ArmInsertArgs(targetWindowID, "stack")); err != nil {
		return err
	}
	_, _, err = d.run(ctx, MoveWindowToSpaceArgs(windowID, target.Space))
	return err
}

func (d *Driver) SwapWindows(ctx context.Context, direction wm.Direction) (bool, error) {
	_, exitCode, err := d.run(ctx, SwapWindowsArgs(direction))
	return exitCode == 0, err
}

func (d *Driver) WarpWindow(ctx context.Context, direction wm.Direction) (bool, error) {
	_, exitCode, err := d.run(ctx, WarpWindowArgs(direction))
	return exitCode == 0, err
}

func (d *Driver) ResizeWindow(ctx context.Context, edge wm.ResizeEdge, dx, dy int) (bool, error) {
	_, exitCode, err := d.run(ctx, ResizeWindowArgs(edge, dx, dy))
	return exitCode == 0, err
}

// SpawnWindow runs the launcher and waits up to spawnWait for it. A launcher still running then is left
// alive and reaped in the background; only caller cancellation during the wait kills it.
func (d *Driver) SpawnWindow(ctx context.Context, argv []string) error {
	if len(argv) == 0 {
		return errors.New("spawning window: empty argv")
	}
	// A file, not a pipe: a lingering launcher must not block or SIGPIPE on stderr.
	stderr, err := os.CreateTemp("", "tess-spawn-*.err")
	if err != nil {
		return fmt.Errorf("creating spawn stderr file: %w", err)
	}
	//nolint:gosec // The profile's spawn argv is the intended launcher interface.
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return errors.Join(fmt.Errorf("spawning window %s: %w", argv[0], err), discardSpawnStderr(stderr))
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	timer := time.NewTimer(d.spawnWait)
	defer timer.Stop()
	select {
	case waitErr := <-done:
		return spawnResult(argv, stderr, waitErr)
	case <-ctx.Done():
		killErr := command.Process.Kill()
		if errors.Is(killErr, os.ErrProcessDone) {
			killErr = nil
		}
		<-done
		return errors.Join(fmt.Errorf("spawning window: %w", ctx.Err()), killErr, discardSpawnStderr(stderr))
	case <-timer.C:
		go func() {
			<-done
			if err := discardSpawnStderr(stderr); err != nil {
				slog.Warn("cleaning up after a detached launcher", "error", err)
			}
		}()
		return nil
	}
}

// spawnResult turns a finished launcher's exit into an error carrying its stderr.
func spawnResult(argv []string, stderr *os.File, waitErr error) error {
	content, readErr := os.ReadFile(stderr.Name())
	if cleanupErr := discardSpawnStderr(stderr); cleanupErr != nil {
		return errors.Join(waitErr, cleanupErr)
	}
	if waitErr == nil {
		return nil
	}
	exitError, ok := errors.AsType[*exec.ExitError](waitErr)
	if !ok {
		return fmt.Errorf("spawning window %s: %w", argv[0], waitErr)
	}
	if readErr != nil {
		return fmt.Errorf("reading spawn stderr: %w", readErr)
	}
	if message := strings.TrimSpace(string(content)); message != "" {
		return fmt.Errorf("spawning window failed (exit %d): %s: %s", exitError.ExitCode(), strings.Join(argv, " "), message)
	}
	return fmt.Errorf("spawning window failed (exit %d): %s", exitError.ExitCode(), strings.Join(argv, " "))
}

func discardSpawnStderr(stderr *os.File) error {
	closeErr := stderr.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("closing spawn stderr file: %w", closeErr)
	}
	removeErr := os.Remove(stderr.Name())
	if removeErr != nil {
		removeErr = fmt.Errorf("removing spawn stderr file: %w", removeErr)
	}
	return errors.Join(closeErr, removeErr)
}

func (d *Driver) FocusWindow(ctx context.Context, windowID int) (bool, error) {
	_, exitCode, err := d.run(ctx, FocusWindowArgs(windowID))
	return exitCode == 0, err
}

func (d *Driver) FocusWindowDir(ctx context.Context, selector wm.FocusSelector) (bool, error) {
	_, exitCode, err := d.run(ctx, FocusWindowDirArgs(selector))
	return exitCode == 0, err
}

func (d *Driver) FocusDisplay(ctx context.Context, selector wm.DisplaySelector) (bool, error) {
	_, exitCode, err := d.run(ctx, FocusDisplayArgs(selector))
	return exitCode == 0, err
}

func (d *Driver) SettleUnit() time.Duration { return 150 * time.Millisecond }

func (d *Driver) List(ctx context.Context) ([]wm.Rule, error) {
	var raw []struct {
		Label string `json:"label"`
	}
	if err := d.query(ctx, RuleListArgs(), &raw); err != nil {
		return nil, err
	}
	rules := make([]wm.Rule, len(raw))
	for index, rule := range raw {
		rules[index] = wm.Rule{Label: rule.Label}
	}
	return rules, nil
}

func (d *Driver) Remove(ctx context.Context, label string) error {
	_, _, err := d.run(ctx, RuleRemoveArgs(label))
	return err
}

func (d *Driver) Add(ctx context.Context, rule wm.Rule) error {
	_, _, err := d.run(ctx, RuleAddArgs(RuleAdd{
		Label: rule.Label, App: rule.App, Subrole: rule.Subrole,
		Display: rule.Display, Space: rule.Space, Manage: rule.Manage,
	}))
	return err
}

func (d *Driver) Apply(ctx context.Context) error {
	_, _, err := d.run(ctx, RuleApplyArgs())
	return err
}

func (d *Driver) AddSignal(ctx context.Context, event wm.Event, label, action string) error {
	_, _, err := d.run(ctx, SignalAddArgs(event, label, action))
	return err
}

func waitContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (d *Driver) run(ctx context.Context, args []string) ([]byte, int, error) {
	//nolint:gosec // The configured yabai executable and arguments are the CLI interface.
	command := exec.CommandContext(ctx, d.path, args...)
	// Bound Wait if a descendant keeps the output pipes open after yabai exits or is killed.
	command.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return stdout.Bytes(), 0, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return stdout.Bytes(), -1, fmt.Errorf("running yabai %s: %w", strings.Join(args, " "), ctxErr)
	}
	if exitError, ok := errors.AsType[*exec.ExitError](err); ok {
		return stdout.Bytes(), exitError.ExitCode(), nil
	}
	if message := strings.TrimSpace(stderr.String()); message != "" {
		return stdout.Bytes(), -1, fmt.Errorf("running yabai %s: %s: %w", strings.Join(args, " "), message, err)
	}
	return stdout.Bytes(), -1, fmt.Errorf("running yabai %s: %w", strings.Join(args, " "), err)
}

func (d *Driver) query(ctx context.Context, args []string, target any) error {
	stdout, exitCode, err := d.run(ctx, args)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("yabai query failed (exit %d): %s", exitCode, strings.Join(args, " "))
	}
	if err := json.Unmarshal(stdout, target); err != nil {
		return fmt.Errorf("decoding yabai query %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func (d *Driver) queryOrNull(ctx context.Context, args []string, target any) (bool, error) {
	stdout, exitCode, err := d.run(ctx, args)
	if err != nil {
		return false, err
	}
	if exitCode != 0 || len(bytes.TrimSpace(stdout)) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(stdout, target); err != nil {
		return false, fmt.Errorf("decoding yabai query %s: %w", strings.Join(args, " "), err)
	}
	return true, nil
}

func (d *Driver) resolveIndex(ctx context.Context, id wm.SpaceID) (int, bool, error) {
	var spaces []RawYabaiSpace
	if err := d.query(ctx, QuerySpacesArgs(), &spaces); err != nil {
		return 0, false, err
	}
	for _, space := range spaces {
		if strconv.Itoa(space.ID) == string(id) {
			return space.Index, true, nil
		}
	}
	return 0, false, nil
}

func (d *Driver) unfloatOne(ctx context.Context, windowID int) error {
	var raw RawYabaiWindow
	found, err := d.queryOrNull(ctx, QueryWindowArgs(windowID), &raw)
	if err != nil || !found || !raw.IsFloating {
		return err
	}
	if _, _, err := d.run(ctx, ToggleFloatArgs(windowID)); err != nil {
		return err
	}
	return waitContext(ctx, 150*time.Millisecond)
}
