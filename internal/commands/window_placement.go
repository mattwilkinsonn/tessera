package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

const placementLockInterval = 50 * time.Millisecond

// IsTileCandidate mirrors yabai's root-window creation heuristic.
func IsTileCandidate(window wm.Window) bool {
	rootAXWindow := window.Root && window.Role == "AXWindow" && window.Subrole == "AXStandardWindow"
	fixedAndUndersized := !window.CanResize && (window.Frame.W <= 500 || window.Frame.H <= 500)
	return rootAXWindow && window.Level == 0 && window.CanMove && !window.Sticky && !fixedAndUndersized
}

// HandoffDeskWindows moves floating desk arrivals before display convergence.
func HandoffDeskWindows(ctx context.Context, profile *config.Profile, driver wm.Driver) error {
	windows, err := driver.QueryWindows(ctx)
	if err != nil {
		return err
	}
	displays, err := driver.QueryDisplays(ctx)
	if err != nil {
		return err
	}
	target, ok := arrivalTargetFor(profile, displays)
	if !ok {
		return nil
	}
	for _, window := range windows {
		if !window.Floating || !IsTileCandidate(window) {
			continue
		}
		anchor := 0
		if target.display != config.DisplayLaptop {
			anchor = leftDeskSlotWindow(target, windows, window.ID)
		}
		if err := placeArrivalWindow(ctx, driver, window, target, anchor); err != nil {
			return fmt.Errorf("handing off window %d to arrival target: %w", window.ID, err)
		}
	}
	return nil
}

// RecoverDeskWindows restores floating tile candidates not claimed by desk slots.
func RecoverDeskWindows(ctx context.Context, profile *config.Profile, driver wm.Driver, env Env) error {
	paths, _, _, err := normalizedEnv(ctx, env)
	if err != nil {
		return err
	}
	release, err := lockPlacement(ctx, paths)
	if err != nil {
		return err
	}
	defer release()
	displays, err := driver.QueryDisplays(ctx)
	if err != nil {
		return err
	}
	shouldRecover := externalDeskOnly(profile, displays) || len(displays) > 1 && arrivalTargetIsDesk(profile, displays)
	if !shouldRecover {
		return nil
	}
	target, ok := arrivalTargetFor(profile, displays)
	if !ok {
		return nil
	}
	windows, err := driver.QueryWindows(ctx)
	if err != nil {
		return err
	}
	claimed := env.ReclaimedDeskWindows
	for _, window := range windows {
		if !window.Floating || !IsTileCandidate(window) {
			continue
		}
		if _, exists := claimed[window.ID]; exists {
			continue
		}
		anchor := 0
		if target.display != config.DisplayLaptop {
			anchor = leftDeskSlotWindow(target, windows, window.ID)
		}
		if err := placeArrivalWindow(ctx, driver, window, target, anchor); err != nil {
			return fmt.Errorf("recovering window %d to arrival target: %w", window.ID, err)
		}
	}
	return nil
}

// HandleWindowCreated places one arrival after display quiet and lock conditions settle.
func HandleWindowCreated(ctx context.Context, profile *config.Profile, driver wm.Driver, env Env, id int) error {
	if id <= 0 {
		return nil
	}
	placementCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	paths, _, _, err := normalizedEnv(placementCtx, env)
	if err != nil {
		return err
	}
	release, err := lockPlacement(placementCtx, paths)
	if err != nil {
		return err
	}
	defer release()
	if env.ArrivalReady != nil && !env.ArrivalReady() {
		return ErrDisplayPending
	}

	window, exists, err := driver.QueryWindow(placementCtx, id)
	if err != nil || !exists || !window.Floating || !IsTileCandidate(window) {
		return err
	}
	displays, err := driver.QueryDisplays(placementCtx)
	if err != nil {
		return err
	}
	deskTopology := len(displays) > 1 || externalDeskOnly(profile, displays)
	if !deskTopology {
		return nil
	}
	target, ok := arrivalTargetFor(profile, displays)
	if !ok {
		return nil
	}
	if env.ArrivalReady != nil && !env.ArrivalReady() {
		return ErrDisplayPending
	}
	if target.display != config.DisplayLaptop {
		windows, err := driver.QueryWindows(placementCtx)
		if err != nil {
			return err
		}
		anchor := leftDeskSlotWindow(target, windows, window.ID)
		if env.ArrivalReady != nil && !env.ArrivalReady() {
			return ErrDisplayPending
		}
		return placeArrivalWindow(placementCtx, driver, window, target, anchor)
	}
	return placeWindow(placementCtx, driver, window, target.space)
}

func acquirePlacementLocks(ctx context.Context, paths effects.Paths) (interface{ Unlock() error }, interface{ Unlock() error }, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if _, err := effects.SignalsSuppressed(paths.SignalGuard, time.Now()); err != nil {
			return nil, nil, err
		}
		apply, acquired, err := effects.AcquireLock(paths.ApplyLock)
		if err != nil {
			return nil, nil, err
		}
		if acquired && apply != nil {
			laptop, gotLaptop, err := effects.AcquireLock(paths.LaptopLock)
			if err != nil {
				unlock(apply)
				return nil, nil, err
			}
			if gotLaptop {
				return apply, laptop, nil
			}
			unlock(apply)
		}
		if err := waitPlacementLock(ctx); err != nil {
			return nil, nil, err
		}
	}
}

func waitPlacementLock(ctx context.Context) error {
	timer := time.NewTimer(placementLockInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func lockPlacement(ctx context.Context, paths effects.Paths) (func(), error) {
	applyLock, laptopLock, err := acquirePlacementLocks(ctx, paths)
	if err != nil {
		return nil, err
	}
	releaseLocks := func() {
		unlock(laptopLock)
		unlock(applyLock)
	}
	for {
		suppressed, err := effects.SignalsSuppressed(paths.SignalGuard, time.Now())
		if err != nil {
			releaseLocks()
			return nil, err
		}
		if !suppressed {
			break
		}
		if err := waitPlacementLock(ctx); err != nil {
			releaseLocks()
			return nil, err
		}
	}
	if err := effects.SuppressSignals(paths.SignalGuard); err != nil {
		releaseLocks()
		return nil, fmt.Errorf("suppressing placement signals: %w", err)
	}
	return func() {
		releaseGuard(paths.SignalGuard)
		releaseLocks()
	}, nil
}

type arrivalTarget struct {
	space   wm.SpaceID
	display config.DisplayName
}

func externalDeskOnly(profile *config.Profile, displays []wm.Display) bool {
	if len(displays) != 1 {
		return false
	}
	if _, hasLaptop := engine.ResolveDisplay(profile, config.DisplayLaptop, displays); hasLaptop {
		return false
	}
	return arrivalTargetIsDesk(profile, displays)
}

func arrivalTargetIsDesk(profile *config.Profile, displays []wm.Display) bool {
	_, hasAW := engine.ResolveDisplay(profile, config.DisplayAW, displays)
	_, hasG9 := engine.ResolveDisplay(profile, config.DisplayG9, displays)
	return hasAW || hasG9
}

func arrivalTargetFor(profile *config.Profile, displays []wm.Display) (arrivalTarget, bool) {
	for _, name := range [...]config.DisplayName{config.DisplayLaptop, config.DisplayAW, config.DisplayG9} {
		index, ok := engine.ResolveDisplay(profile, name, displays)
		if !ok {
			continue
		}
		for _, display := range displays {
			if display.Idx != index {
				continue
			}
			if len(display.SpaceIDs) == 0 {
				return arrivalTarget{}, false
			}
			return arrivalTarget{space: display.SpaceIDs[0], display: name}, true
		}
		return arrivalTarget{}, false
	}
	return arrivalTarget{}, false
}

func leftDeskSlotWindow(target arrivalTarget, windows []wm.Window, arrivingID int) int {
	var anchor wm.Window
	hasAnchor := false
	for _, window := range windows {
		if window.ID == arrivingID || window.SpaceID != target.space || window.Minimized || window.Floating || !IsTileCandidate(window) {
			continue
		}
		if !hasAnchor || window.Frame.X < anchor.Frame.X || window.Frame.X == anchor.Frame.X && window.ID < anchor.ID {
			anchor = window
			hasAnchor = true
		}
	}
	if !hasAnchor {
		return 0
	}
	return anchor.ID
}

func placeArrivalWindow(ctx context.Context, driver wm.Driver, window wm.Window, target arrivalTarget, leftSlotWindowID int) error {
	if leftSlotWindowID <= 0 {
		return placeWindow(ctx, driver, window, target.space)
	}
	if window.SpaceID != target.space {
		if err := driver.MoveWindowToSpace(ctx, window.ID, target.space); err != nil {
			return fmt.Errorf("moving arrival to left desk slot: %w", err)
		}
	}
	focused, err := driver.FocusWindow(ctx, window.ID)
	if err != nil {
		return fmt.Errorf("focusing arriving window: %w", err)
	}
	if !focused {
		return nil
	}
	active, exists, err := driver.QueryFocusedSpace(ctx)
	if err != nil {
		return fmt.Errorf("verifying active space: %w", err)
	}
	if !exists || active.ID != target.space {
		return nil
	}
	if err := driver.StackOnto(ctx, leftSlotWindowID, window.ID); err != nil {
		return fmt.Errorf("stacking arriving window in left desk slot: %w", err)
	}
	return nil
}

func placeWindow(ctx context.Context, driver wm.Driver, window wm.Window, target wm.SpaceID) error {
	if window.SpaceID != target {
		if err := driver.MoveWindowToSpace(ctx, window.ID, target); err != nil {
			return fmt.Errorf("moving window to arrival target: %w", err)
		}
	}
	focused, err := driver.FocusWindow(ctx, window.ID)
	if err != nil {
		return fmt.Errorf("focusing arriving window: %w", err)
	}
	if !focused {
		return nil
	}
	active, exists, err := driver.QueryFocusedSpace(ctx)
	if err != nil {
		return fmt.Errorf("verifying active space: %w", err)
	}
	if !exists || active.ID != target {
		return nil
	}
	if window.Floating {
		if err := driver.ToggleFloat(ctx, window.ID); err != nil {
			return fmt.Errorf("unfloating arriving window: %w", err)
		}
	}
	return nil
}
