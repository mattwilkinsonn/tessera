package yabai

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"os/exec" //nolint:depguard // yabai requires subprocess execution.
	"strconv"
	"strings"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/spawn"
)

// spawnTimeout is how long SpawnWindow waits for a launcher before leaving it running.
const spawnTimeout = 5 * time.Second

const (
	unfloatFocusTimeout = 2 * time.Second
	unfloatFocusPoll    = 50 * time.Millisecond
)

type Driver struct {
	path                string
	spawnWait           time.Duration
	unfloatFocusTimeout time.Duration
	unfloatFocusPoll    time.Duration
	unfloatWait         func(context.Context, time.Duration) error
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

func (d *Driver) QueryWindow(ctx context.Context, id int) (wm.Window, bool, error) {
	var raw RawYabaiWindow
	stdout, stderr, exitCode, err := d.runOutput(ctx, QueryWindowArgs(id))
	if err != nil {
		return wm.Window{}, false, err
	}
	if exitCode != 0 {
		if strings.Contains(stderr, "could not locate window") || strings.Contains(stderr, "could not retrieve window details") {
			return wm.Window{}, false, nil
		}
		return wm.Window{}, false, fmt.Errorf("querying window %d (exit %d): %s", id, exitCode, strings.TrimSpace(stderr))
	}
	if len(bytes.TrimSpace(stdout)) == 0 || bytes.Equal(bytes.TrimSpace(stdout), []byte("null")) {
		return wm.Window{}, false, nil
	}
	if err := json.Unmarshal(stdout, &raw); err != nil {
		return wm.Window{}, false, fmt.Errorf("decoding window %d: %w", id, err)
	}
	if raw.ID != id || !raw.HasAXReference {
		return wm.Window{}, false, nil
	}
	var spaces []RawYabaiSpace
	if err := d.query(ctx, QuerySpacesArgs(), &spaces); err != nil {
		return wm.Window{}, false, err
	}
	window, err := NormalizeWindow(raw, IndexToSpaceID(spaces))
	return window, err == nil, err
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

func (d *Driver) QueryFocusedWindow(ctx context.Context) (_ wm.Window, found bool, err error) {
	defer func() {
		if err != nil {
			wm.MarkQueryFailed(ctx)
		}
	}()
	stdout, stderr, exitCode, err := d.runOutput(ctx, QueryFocusedWindowArgs())
	if err != nil {
		return wm.Window{}, false, err
	}
	if exitCode != 0 {
		if strings.Contains(stderr, "could not locate window") || strings.Contains(stderr, "could not retrieve window details") {
			return wm.Window{}, false, nil
		}
		return wm.Window{}, false, fmt.Errorf("querying focused window (exit %d): %s", exitCode, strings.TrimSpace(stderr))
	}
	if len(bytes.TrimSpace(stdout)) == 0 || bytes.Equal(bytes.TrimSpace(stdout), []byte("null")) {
		return wm.Window{}, false, nil
	}
	var raw RawYabaiWindow
	if err := json.Unmarshal(stdout, &raw); err != nil {
		return wm.Window{}, false, fmt.Errorf("decoding focused window: %w", err)
	}
	if raw.ID == 0 || !raw.HasAXReference {
		return wm.Window{}, false, nil
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
	if layout == wm.LayoutStack {
		if err := wm.CheckStackConversion(ctx, d, id); err != nil {
			return err
		}
	}
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
		return d.realizeStack(ctx, id, index, target)
	}
	return d.realizeBSP(ctx, BSPSteps(index, target, nil))
}

func (d *Driver) MoveWindowToSpace(ctx context.Context, windowID int, id wm.SpaceID) error {
	index, found, err := d.resolveIndex(ctx, id)
	if err != nil || !found {
		return err
	}
	allowed, err := wm.CanAddToSpace(ctx, d, id, windowID)
	if err != nil || !allowed {
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
	window, exists, err := d.QueryWindow(ctx, windowID)
	if err != nil || !exists {
		return err
	}
	if window.Floating {
		allowed, err := wm.CanAddToSpace(ctx, d, window.SpaceID, windowID)
		if err != nil || !allowed {
			return err
		}
	}
	_, _, err = d.run(ctx, ToggleFloatArgs(windowID))
	return err
}

func (d *Driver) ArmInsert(ctx context.Context, windowID int, direction wm.Direction) error {
	if string(direction) == "stack" {
		allowed, err := wm.CanStack(ctx, d, windowID, 0)
		if err != nil || !allowed {
			return err
		}
	}
	_, _, err := d.run(ctx, ArmInsertArgs(windowID, string(direction)))
	return err
}

func (d *Driver) StackOnto(ctx context.Context, targetWindowID, windowID int) error {
	allowed, err := wm.CanStack(ctx, d, targetWindowID, windowID)
	if err != nil || !allowed {
		return err
	}
	_, exitCode, err := d.run(ctx, StackWindowArgs(targetWindowID, windowID))
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("stacking window %d onto %d failed (exit %d)", windowID, targetWindowID, exitCode)
	}
	return nil
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

// SpawnWindow runs the launcher through spawn.Run, independent of yabai.
func (d *Driver) SpawnWindow(ctx context.Context, argv []string) error {
	return spawn.Run(ctx, argv, d.spawnWait)
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
	if rule.App != "" && rule.ExcludeApp != "" {
		return errors.New("rule cannot set both app and excludeApp")
	}
	_, _, err := d.run(ctx, RuleAddArgs(RuleAdd{
		Label: rule.Label, App: rule.App, ExcludeApp: rule.ExcludeApp, Subrole: rule.Subrole,
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
	stdout, _, exitCode, err := d.runOutput(ctx, args)
	return stdout, exitCode, err
}

func (d *Driver) runOutput(ctx context.Context, args []string) ([]byte, string, int, error) {
	//nolint:gosec // The configured yabai executable and arguments are the CLI interface.
	command := exec.CommandContext(ctx, d.path, args...)
	// Bound Wait if a descendant keeps the output pipes open after yabai exits or is killed.
	command.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return stdout.Bytes(), stderr.String(), 0, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return stdout.Bytes(), stderr.String(), -1, fmt.Errorf("running yabai %s: %w", strings.Join(args, " "), ctxErr)
	}
	if exitError, ok := errors.AsType[*exec.ExitError](err); ok {
		return stdout.Bytes(), stderr.String(), exitError.ExitCode(), nil
	}
	if message := strings.TrimSpace(stderr.String()); message != "" {
		return stdout.Bytes(), stderr.String(), -1, fmt.Errorf("running yabai %s: %s: %w", strings.Join(args, " "), message, err)
	}
	return stdout.Bytes(), stderr.String(), -1, fmt.Errorf("running yabai %s: %w", strings.Join(args, " "), err)
}

func (d *Driver) query(ctx context.Context, args []string, target any) (err error) {
	defer func() {
		if err != nil {
			wm.MarkQueryFailed(ctx)
		}
	}()
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

func (d *Driver) queryOrNull(ctx context.Context, args []string, target any) (found bool, err error) {
	found, failed, err := d.queryOrNullUntracked(ctx, args, target)
	if failed || err != nil {
		wm.MarkQueryFailed(ctx)
	}
	return found, err
}

func (d *Driver) queryOrNullUntracked(ctx context.Context, args []string, target any) (found, failed bool, err error) {
	stdout, exitCode, err := d.run(ctx, args)
	if err != nil {
		return false, false, err
	}
	if exitCode != 0 || len(bytes.TrimSpace(stdout)) == 0 {
		return false, exitCode != 0, nil
	}
	if err := json.Unmarshal(stdout, target); err != nil {
		return false, false, fmt.Errorf("decoding yabai query %s: %w", strings.Join(args, " "), err)
	}
	return true, false, nil
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
	_, err := d.unfloatOneConsumed(ctx, windowID)
	return err
}

func (d *Driver) unfloatOneConsumed(ctx context.Context, windowID int) (bool, error) {
	var raw RawYabaiWindow
	found, err := d.queryOrNull(ctx, QueryWindowArgs(windowID), &raw)
	if err != nil || !found || !raw.IsFloating {
		return false, err
	}
	var spaces []RawYabaiSpace
	if err := d.query(ctx, QuerySpacesArgs(), &spaces); err != nil {
		return false, err
	}
	space := IndexToSpaceID(spaces)[raw.Space]
	allowed, err := wm.CanAddToSpace(ctx, d, space, windowID)
	if err != nil || !allowed {
		return false, err
	}
	// yabai tiles an unfloated window on the active space, not necessarily
	// the space assigned by --space. Focus it before rebuilding that tree.
	focused, err := d.FocusWindow(ctx, windowID)
	if err != nil {
		return false, err
	}
	if !focused {
		return false, fmt.Errorf("focusing window %d before unfloat", windowID)
	}
	// Focusing a window switches spaces asynchronously.
	timeout, poll := d.unfloatFocusTimeout, d.unfloatFocusPoll
	if timeout == 0 {
		timeout = unfloatFocusTimeout
	}
	if poll == 0 {
		poll = unfloatFocusPoll
	}
	wait := d.unfloatWait
	if wait == nil {
		wait = waitContext
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// Only completed queries update lastFailed; a timed-out query leaves it unchanged.
	// Parent cancellation never marks query health.
	lastFailed := false
	for {
		var rawSpace RawYabaiSpace
		found, failed, err := d.queryOrNullUntracked(waitCtx, QueryFocusedSpaceArgs(), &rawSpace)
		if err == nil {
			lastFailed = failed
		}
		if err == nil && found && NormalizeSpace(rawSpace).ID == space {
			break
		}
		if err == nil {
			err = wait(waitCtx, poll)
		} else if ctx.Err() == nil && waitCtx.Err() == nil {
			wm.MarkQueryFailed(ctx)
			return false, err
		}
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			if lastFailed {
				wm.MarkQueryFailed(ctx)
			}
			return false, fmt.Errorf("window %d did not activate target space %s before unfloat", windowID, space)
		}
	}
	_, code, err := d.run(ctx, ToggleFloatArgs(windowID))
	if err != nil {
		return false, err
	}
	if code != 0 {
		return false, fmt.Errorf("unfloating window %d: yabai exited %d", windowID, code)
	}
	return true, waitContext(ctx, 150*time.Millisecond)
}
func (d *Driver) realizeStack(ctx context.Context, id wm.SpaceID, index int, target wm.SpaceLayoutTarget) error {
	for _, track := range target.Tracks {
		for _, windowID := range track {
			allowed, err := wm.CanAddToSpace(ctx, d, id, windowID)
			if errors.Is(err, wm.ErrSpaceFull) {
				continue
			}
			if err != nil {
				return err
			}
			if !allowed {
				continue
			}
			if _, _, err := d.run(ctx, MoveWindowToSpaceArgs(windowID, index)); err != nil {
				return err
			}
			if err := waitContext(ctx, 350*time.Millisecond); err != nil {
				return err
			}
			if err := d.unfloatOne(ctx, windowID); err != nil {
				if errors.Is(err, wm.ErrSpaceFull) {
					continue
				}
				return err
			}
		}
	}
	if err := d.SetSpaceLayout(ctx, id, wm.LayoutStack); err != nil && !errors.Is(err, wm.ErrSpaceFull) {
		return err
	}
	return nil
}

type insertArm struct {
	anchor    int
	direction string
}

func (d *Driver) realizeBSP(ctx context.Context, steps []BspStep) (result error) {
	var pending *insertArm
	clearPending := func() {
		d.clearInsert(ctx, pending)
		pending = nil
	}
	defer func() {
		if result != nil {
			clearPending()
		}
	}()
	for i := 0; i < len(steps); i++ {
		step := steps[i]
		allowed, err := d.allowBSPStep(ctx, steps, i)
		if err != nil {
			return err
		}
		if !allowed {
			i++
			continue
		}
		consumesOnMove, err := d.moveConsumesPending(ctx, step, pending)
		if err != nil {
			return err
		}
		_, code, err := d.run(ctx, step.Args)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("running yabai %s: exit %d", strings.Join(step.Args, " "), code)
		}
		if consumesOnMove {
			pending = nil
		}
		pending, err = pendingAfterStep(pending, step.Args)
		if err != nil {
			return err
		}
		if step.Settle > 0 {
			if err := waitContext(ctx, time.Duration(step.Settle)*time.Millisecond); err != nil {
				return err
			}
		}
		if step.HasUnfloat {
			consumed, err := d.unfloatOneConsumed(ctx, step.Unfloat)
			if consumed {
				pending = nil
			}
			if errors.Is(err, wm.ErrSpaceFull) {
				clearPending()
				if nextArmsWindow(steps, i, step.Unfloat) {
					i++
				}
				continue
			}
			if err != nil {
				return err
			}
			clearPending()
		}
	}
	return nil
}

func pendingAfterStep(pending *insertArm, args []string) (*insertArm, error) {
	if len(args) < 5 || args[3] != insertArg {
		return pending, nil
	}
	anchor, err := strconv.Atoi(args[2])
	if err != nil {
		return pending, err
	}
	return &insertArm{anchor: anchor, direction: args[4]}, nil
}

func (d *Driver) moveConsumesPending(ctx context.Context, step BspStep, pending *insertArm) (bool, error) {
	if pending == nil || !step.HasUnfloat {
		return false, nil
	}
	destination, ok, err := moveSpaceDestination(step.Args)
	if err != nil || !ok {
		return false, err
	}
	return d.moveConsumesInsert(ctx, step.Unfloat, destination)
}

func (d *Driver) clearInsert(ctx context.Context, arm *insertArm) {
	if arm == nil {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_, code, err := d.run(cleanupCtx, ArmInsertArgs(arm.anchor, arm.direction))
	if err != nil || code != 0 {
		slog.Warn("clearing pending yabai insert", "anchor", arm.anchor, "direction", arm.direction, "exit_code", code, "error", err)
	}
}

func nextArmsWindow(steps []BspStep, index, windowID int) bool {
	if index+1 >= len(steps) {
		return false
	}
	args := steps[index+1].Args
	return len(args) >= 5 && args[3] == insertArg && args[2] == strconv.Itoa(windowID)
}

func moveSpaceDestination(args []string) (int, bool, error) {
	if len(args) != 5 || args[0] != "-m" || args[1] != windowMode || args[3] != "--space" {
		return 0, false, nil
	}
	destination, err := strconv.Atoi(args[4])
	return destination, true, err
}

func (d *Driver) moveConsumesInsert(ctx context.Context, windowID, destination int) (bool, error) {
	var moving struct {
		Space     int    `json:"space"`
		Root      *bool  `json:"root-window"`
		Floating  *bool  `json:"is-floating"`
		Sticky    *bool  `json:"is-sticky"`
		Minimized *bool  `json:"is-minimized"`
		Hidden    *bool  `json:"is-hidden"`
		Role      string `json:"role"`
		Subrole   string `json:"subrole"`
		Level     *int   `json:"level"`
		CanMove   *bool  `json:"can-move"`
	}
	if err := d.query(ctx, QueryWindowArgs(windowID), &moving); err != nil {
		return false, err
	}
	if moving.Root == nil || moving.Floating == nil || moving.Sticky == nil || moving.Minimized == nil || moving.Hidden == nil || moving.Level == nil || moving.CanMove == nil {
		return false, nil
	}
	return moving.Space != destination && *moving.Root && !*moving.Floating && !*moving.Sticky && !*moving.Minimized && !*moving.Hidden && moving.Role == "AXWindow" && moving.Subrole == "AXStandardWindow" && *moving.Level == 0 && *moving.CanMove, nil
}
func (d *Driver) allowBSPStep(ctx context.Context, steps []BspStep, index int) (bool, error) {
	step := steps[index]
	if len(step.Args) < 5 || step.Args[3] != insertArg || step.Args[4] != string(wm.LayoutStack) {
		return true, nil
	}
	anchor, err := strconv.Atoi(step.Args[2])
	if err != nil {
		return false, err
	}
	windowID := 0
	if index+1 < len(steps) {
		windowID = steps[index+1].Unfloat
	}
	allowed, err := wm.CanStack(ctx, d, anchor, windowID)
	if errors.Is(err, wm.ErrSpaceFull) {
		return false, nil
	}
	return allowed, err
}
