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
	var batchAnchor int
	for _, window := range windows {
		if !window.Floating || !IsTileCandidate(window) {
			continue
		}
		anchor := batchAnchor
		if anchor == 0 && target.display != config.DisplayLaptop {
			anchor = leftDeskSlotWindow(target, windows, window.ID)
		}
		if err := placeArrivalWindow(ctx, driver, window, target, anchor); err != nil {
			return fmt.Errorf("handing off window %d to arrival target: %w", window.ID, err)
		}
		if target.display != config.DisplayLaptop && anchor == 0 {
			batchAnchor, err = placedArrivalAnchor(ctx, driver, target, window.ID)
			if err != nil {
				return fmt.Errorf("refreshing handoff anchor for window %d: %w", window.ID, err)
			}
		}
	}
	return nil
}

func stackDisplacedDeskWindows(ctx context.Context, driver wm.Driver, target arrivalTarget, windows []wm.Window, displaced, claimed map[int]struct{}) (map[int]struct{}, error) {
	consumed := make(map[int]struct{}, len(displaced))
	for id := range displaced {
		consumed[id] = struct{}{}
	}
	plannedWindows := make([]wm.Window, 0, len(claimed))
	for _, window := range windows {
		if _, planned := claimed[window.ID]; planned {
			plannedWindows = append(plannedWindows, window)
		}
		if _, candidate := displaced[window.ID]; candidate {
			delete(consumed, window.ID)
		}
	}
	leftSlotWindowID := leftDeskSlotWindow(target, plannedWindows, 0)
	var batchAnchor int
	for _, window := range windows {
		if _, displaced := displaced[window.ID]; !displaced {
			continue
		}
		if window.Minimized || window.Floating || !IsTileCandidate(window) {
			consumed[window.ID] = struct{}{}
			continue
		}
		if _, exists := claimed[window.ID]; exists || window.ID == leftSlotWindowID {
			consumed[window.ID] = struct{}{}
			continue
		}
		anchor := leftSlotWindowID
		if anchor == 0 {
			anchor = batchAnchor
		}
		placed, err := placeArrivalWindowOutcome(ctx, driver, window, target, anchor)
		if err != nil {
			return consumed, fmt.Errorf("stacking displaced desk window %d: %w", window.ID, err)
		}
		if !placed {
			delete(consumed, window.ID)
			continue
		}
		consumed[window.ID] = struct{}{}
		if anchor == 0 {
			var err error
			batchAnchor, err = placedArrivalAnchor(ctx, driver, target, window.ID)
			if err != nil {
				return consumed, fmt.Errorf("refreshing displaced window anchor %d: %w", window.ID, err)
			}
		}
	}
	return consumed, nil
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
	target, hasTarget := arrivalTargetFor(profile, displays)
	shouldRecover := externalDeskOnly(profile, displays) || len(displays) > 1 && hasTarget
	if !shouldRecover || !hasTarget {
		return nil
	}
	windows, err := driver.QueryWindows(ctx)
	if err != nil {
		return err
	}
	claimed := env.ReclaimedDeskWindows
	var batchAnchor int
	for _, window := range windows {
		if !window.Floating || !IsTileCandidate(window) {
			continue
		}
		if _, exists := claimed[window.ID]; exists {
			continue
		}
		anchor := batchAnchor
		if anchor == 0 && target.display != config.DisplayLaptop {
			anchor = leftDeskSlotWindow(target, windows, window.ID)
		}
		placed, err := placeArrivalWindowOutcome(ctx, driver, window, target, anchor)
		if err != nil {
			return fmt.Errorf("recovering window %d to arrival target: %w", window.ID, err)
		}
		if placed && target.display == config.DisplayLaptop {
			env.Topology.RecordLaptopHomeWindow(window.ID)
		}
		if target.display != config.DisplayLaptop && anchor == 0 {
			batchAnchor, err = placedArrivalAnchor(ctx, driver, target, window.ID)
			if err != nil {
				return fmt.Errorf("refreshing recovery anchor for window %d: %w", window.ID, err)
			}
		}
	}
	if target.display == config.DisplayLaptop {
		world, err := worldSnapshot(ctx, driver)
		if err != nil {
			return err
		}
		recordLaptopHomeWindows(profile, env.Topology, world)
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
	if err := placeWindow(placementCtx, driver, window, target.space); err != nil {
		return err
	}
	world, err := worldSnapshot(placementCtx, driver)
	if err != nil {
		return err
	}
	recordLaptopHomeWindows(profile, env.Topology, world)
	return nil
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

func placedArrivalAnchor(ctx context.Context, driver wm.Driver, target arrivalTarget, placedID int) (int, error) {
	window, exists, err := driver.QueryWindow(ctx, placedID)
	if err != nil {
		return 0, err
	}
	if !exists || window.Floating || window.Minimized || !IsTileCandidate(window) || window.SpaceID != target.space {
		return 0, nil
	}
	return window.ID, nil
}

func placeArrivalWindow(ctx context.Context, driver wm.Driver, window wm.Window, target arrivalTarget, leftSlotWindowID int) error {
	_, err := placeArrivalWindowOutcome(ctx, driver, window, target, leftSlotWindowID)
	return err
}

func placeArrivalWindowOutcome(ctx context.Context, driver wm.Driver, window wm.Window, target arrivalTarget, leftSlotWindowID int) (bool, error) {
	if leftSlotWindowID <= 0 {
		return placeWindowOutcome(ctx, driver, window, target.space)
	}
	allowed, err := wm.CanStack(ctx, driver, leftSlotWindowID, window.ID)
	if err != nil {
		return false, err
	}
	if !allowed {
		if !window.Floating {
			if err := driver.ToggleFloat(ctx, window.ID); err != nil {
				return false, fmt.Errorf("floating rejected stack window: %w", err)
			}
		}
		return false, nil
	}
	if window.SpaceID != target.space {
		if err := driver.MoveWindowToSpace(ctx, window.ID, target.space); err != nil {
			return false, fmt.Errorf("moving arrival to left desk slot: %w", err)
		}
	}
	focused, err := driver.FocusWindow(ctx, window.ID)
	if err != nil {
		return false, fmt.Errorf("focusing arriving window: %w", err)
	}
	if !focused {
		return false, nil
	}
	active, exists, err := driver.QueryFocusedSpace(ctx)
	if err != nil {
		return false, fmt.Errorf("verifying active space: %w", err)
	}
	if !exists || active.ID != target.space {
		return false, nil
	}
	if err := driver.StackOnto(ctx, leftSlotWindowID, window.ID); err != nil {
		return false, fmt.Errorf("stacking arriving window in left desk slot: %w", err)
	}
	return true, nil
}

func placeWindow(ctx context.Context, driver wm.Driver, window wm.Window, target wm.SpaceID) error {
	_, err := placeWindowOutcome(ctx, driver, window, target)
	return err
}

func placeWindowOutcome(ctx context.Context, driver wm.Driver, window wm.Window, target wm.SpaceID) (bool, error) {
	allowed, err := wm.CanAddToSpace(ctx, driver, target, window.ID)
	if err != nil {
		return false, err
	}
	if !allowed {
		if !window.Floating {
			if err := driver.ToggleFloat(ctx, window.ID); err != nil {
				return false, fmt.Errorf("floating rejected arrival: %w", err)
			}
		}
		return false, nil
	}
	if window.SpaceID != target {
		if err := driver.MoveWindowToSpace(ctx, window.ID, target); err != nil {
			return false, fmt.Errorf("moving window to arrival target: %w", err)
		}
	}
	focused, err := driver.FocusWindow(ctx, window.ID)
	if err != nil {
		return false, fmt.Errorf("focusing arriving window: %w", err)
	}
	if !focused {
		return false, nil
	}
	active, exists, err := driver.QueryFocusedSpace(ctx)
	if err != nil {
		return false, fmt.Errorf("verifying active space: %w", err)
	}
	if !exists || active.ID != target {
		return false, nil
	}
	if window.Floating {
		if err := driver.ToggleFloat(ctx, window.ID); err != nil {
			return false, fmt.Errorf("unfloating arriving window: %w", err)
		}
	}
	return true, nil
}
