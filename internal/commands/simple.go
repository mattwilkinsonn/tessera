package commands

import (
	"context"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/executor"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

func worldSnapshot(ctx context.Context, driver wm.Driver) (engine.World, error) {
	world, err := wm.Snapshot(ctx, driver)
	if err != nil {
		return engine.World{}, err
	}
	return engine.World{Windows: world.Windows, Spaces: world.Spaces, Displays: world.Displays}, nil
}

// FocusSlot focuses the window selected by a one-based desk slot.
func FocusSlot(ctx context.Context, profile *config.Profile, driver wm.Driver, number int) error {
	world, err := worldSnapshot(ctx, driver)
	if err != nil {
		return err
	}
	id, ok := engine.ResolveSlot(profile, world, number)
	if !ok {
		return nil
	}
	_, err = driver.FocusWindow(ctx, id)
	return err
}

// Snap reshapes the focused space into the selected column layout.
func Snap(ctx context.Context, profile *config.Profile, driver wm.Driver, mode engine.SnapMode) error {
	focused, ok, err := driver.QueryFocusedSpace(ctx)
	if err != nil || !ok {
		return err
	}
	world, err := worldSnapshot(ctx, driver)
	if err != nil {
		return err
	}
	return executor.RunPlan(ctx, driver, engine.SnapPlan(profile, world, focused.ID, mode), nil)
}

// StackCycle advances focus within a stack and wraps at its ends.
func StackCycle(ctx context.Context, driver wm.Driver, direction CycleDir) error {
	primary, fallback := wm.FocusStackNext, wm.FocusStackFirst
	if direction == CyclePrev {
		primary, fallback = wm.FocusStackPrev, wm.FocusStackLast
	}
	ok, err := driver.FocusWindowDir(ctx, primary)
	if err != nil || ok {
		return err
	}
	_, err = driver.FocusWindowDir(ctx, fallback)
	return err
}

// Resize adjusts the focused window column by one hundred pixels.
func Resize(ctx context.Context, driver wm.Driver, direction ResizeDir) error {
	delta := 100
	if direction == ResizeShrink {
		delta = -100
	}
	ok, err := driver.ResizeWindow(ctx, wm.EdgeRight, delta, 0)
	if err != nil || ok {
		return err
	}
	_, err = driver.ResizeWindow(ctx, wm.EdgeLeft, -delta, 0)
	return err
}

// MoveDisplay moves the focused window to a named profile display.
func MoveDisplay(ctx context.Context, profile *config.Profile, driver wm.Driver, name config.DisplayName) error {
	displays, err := driver.QueryDisplays(ctx)
	if err != nil {
		return err
	}
	index, ok := engine.ResolveDisplay(profile, name, displays)
	if !ok {
		return nil
	}
	focused, ok, err := driver.QueryFocusedWindow(ctx)
	if err != nil || !ok {
		return err
	}
	if _, err := driver.MoveWindowToDisplay(ctx, focused.ID, wm.DisplaySelector{Index: index}); err != nil {
		return err
	}
	_, err = driver.FocusDisplay(ctx, wm.DisplaySelector{Index: index})
	return err
}

// CycleDisplay moves the focused window and focus to the next or previous display.
func CycleDisplay(ctx context.Context, driver wm.Driver, direction CycleDir) error {
	focused, ok, err := driver.QueryFocusedWindow(ctx)
	if err != nil || !ok {
		return err
	}
	position, fallback := wm.DisplayNext, wm.DisplayFirst
	if direction == CyclePrev {
		position, fallback = wm.DisplayPrev, wm.DisplayLast
	}
	moved, err := driver.MoveWindowToDisplay(ctx, focused.ID, wm.DisplaySelector{Position: position})
	if err != nil {
		return err
	}
	if !moved {
		if _, err := driver.MoveWindowToDisplay(ctx, focused.ID, wm.DisplaySelector{Position: fallback}); err != nil {
			return err
		}
	}
	focusedDisplay, err := driver.FocusDisplay(ctx, wm.DisplaySelector{Position: position})
	if err != nil {
		return err
	}
	if !focusedDisplay {
		_, err = driver.FocusDisplay(ctx, wm.DisplaySelector{Position: fallback})
	}
	return err
}

// ResetSplits sets every visible, tiled window ratio in the focused space to one half.
func ResetSplits(ctx context.Context, driver wm.Driver) error {
	focused, ok, err := driver.QueryFocusedSpace(ctx)
	if err != nil || !ok {
		return err
	}
	windows, err := driver.QueryWindowsOnSpace(ctx, focused.ID)
	if err != nil {
		return err
	}
	for _, window := range windows {
		if window.Floating || window.Minimized || !window.Visible {
			continue
		}
		if err := driver.SetSplitRatio(ctx, window.ID, 0.5); err != nil {
			return err
		}
	}
	return nil
}

// Columns flattens horizontal splits in the focused space and balances it.
func Columns(ctx context.Context, driver wm.Driver) error {
	focused, ok, err := driver.QueryFocusedSpace(ctx)
	if err != nil || !ok {
		return err
	}
	for range columnsIterationCap {
		windows, err := driver.QueryWindowsOnSpace(ctx, focused.ID)
		if err != nil {
			return err
		}
		var horizontal *wm.Window
		for index := range windows {
			window := &windows[index]
			if !window.Floating && !window.Minimized && window.Visible && window.SplitType == wm.SplitHorizontal {
				horizontal = window
				break
			}
		}
		if horizontal == nil {
			break
		}
		if err := driver.ToggleSplit(ctx, horizontal.ID); err != nil {
			return err
		}
	}
	return driver.BalanceSpace(ctx, &focused.ID)
}

// Insert arms the focused window for the next insertion direction.
func Insert(ctx context.Context, driver wm.Driver, direction InsertDir) error {
	focused, ok, err := driver.QueryFocusedWindow(ctx)
	if err != nil || !ok {
		return err
	}
	return driver.ArmInsert(ctx, focused.ID, wm.Direction(direction))
}

// ToggleFloat toggles floating on the focused window.
func ToggleFloat(ctx context.Context, driver wm.Driver) error {
	focused, ok, err := driver.QueryFocusedWindow(ctx)
	if err != nil || !ok {
		return err
	}
	return driver.ToggleFloat(ctx, focused.ID)
}

// SpaceLayout sets the focused space's layout.
func SpaceLayout(ctx context.Context, driver wm.Driver, layout SpaceLayoutMode) error {
	focused, ok, err := driver.QueryFocusedSpace(ctx)
	if err != nil || !ok {
		return err
	}
	return driver.SetSpaceLayout(ctx, focused.ID, wm.Layout(layout))
}
