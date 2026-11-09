package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/executor"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

type stagingPlan struct {
	path      string
	staged    map[int]engine.StagedWindow
	attempted map[int]struct{}
}

func runDeskPlan(ctx context.Context, profile *config.Profile, driver wm.Driver, world engine.World, deskTopology bool, spawned []string, before []wm.Window, guard string, staging *stagingPlan) (engine.World, map[engine.SlotKey]int, error) {
	beforeOp := func(op engine.Op) error { return staging.before(ctx, driver, op) }
	ops, filled := engine.DeskPlan(profile, world)
	if deskTopology && len(spawned) > 0 {
		return reconcileSpawnedWindows(ctx, profile, driver, spawned, before, world, filled, beforeOp)
	}
	err := executor.RunPlan(ctx, driver, ops, func(op engine.Op) error {
		if err := effects.SuppressSignals(guard); err != nil {
			return err
		}
		return beforeOp(op)
	})
	return world, filled, err
}

func loadLaptopStaging(ctx context.Context, path string, driver wm.Driver, windows []wm.Window) (*stagingPlan, error) {
	staging, err := loadStaging(path, windows)
	if err != nil {
		return nil, err
	}
	if err := staging.recoverLaptop(ctx, driver, windows); err != nil {
		return nil, err
	}
	return staging, nil
}

func (s *stagingPlan) snapshot(ctx context.Context, driver wm.Driver) ([]wm.Window, error) {
	windows, err := driver.QueryWindows(ctx)
	if err != nil {
		return nil, fmt.Errorf("querying windows after layout: %w", err)
	}
	if err := s.prune(windows); err != nil {
		return nil, err
	}
	return windows, nil
}

func laptopStep(profile *config.Profile, world engine.World, state engine.ConvergeState) (engine.ConvergeAction, engine.ConvergeState, bool) {
	for {
		action, next, emitted := engine.LaptopConvergeStep(profile, world, state)
		if emitted {
			return action, next, false
		}
		if next.Phase == state.Phase {
			return nil, next, true
		}
		state = next
	}
}

func loadStaging(path string, windows []wm.Window) (*stagingPlan, error) {
	staged, err := effects.ReadStagedWindows(path)
	if err != nil {
		return nil, err
	}
	plan := &stagingPlan{path: path, staged: staged, attempted: make(map[int]struct{})}
	if err := plan.prune(windows); err != nil {
		return nil, err
	}
	return plan, nil
}

func (s *stagingPlan) recoverLaptop(ctx context.Context, driver wm.Driver, windows []wm.Window) error {
	var errs []error
	for _, window := range windows {
		ref, exists := s.staged[window.ID]
		if !exists || ref.App != window.App || !window.Floating || window.Minimized {
			continue
		}
		if err := placeWindow(ctx, driver, window, window.SpaceID); err != nil {
			slog.WarnContext(ctx, "recovering staged window", "window", window.ID, "error", err)
			errs = append(errs, fmt.Errorf("recovering window %d: %w", window.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *stagingPlan) before(ctx context.Context, driver wm.Driver, op engine.Op) error {
	float, ok := op.(engine.FloatWindowOp)
	if !ok || !float.Staging {
		return nil
	}
	window, found, err := driver.QueryWindow(ctx, float.WindowID)
	if err != nil {
		return fmt.Errorf("querying staged window %d: %w", float.WindowID, err)
	}
	if !found {
		return nil
	}
	s.staged[float.WindowID] = engine.StagedWindow{App: window.App, Title: window.Title}
	if err := effects.WriteStagedWindows(s.path, s.staged); err != nil {
		return fmt.Errorf("persisting staged windows: %w", err)
	}
	s.attempted[float.WindowID] = struct{}{}
	return nil
}

func (s *stagingPlan) prune(windows []wm.Window) error {
	floating := make(map[int]string, len(windows))
	for _, window := range windows {
		if window.Floating {
			floating[window.ID] = window.App
		}
	}
	changed := false
	for id, ref := range s.staged {
		if app, exists := floating[id]; !exists || app != ref.App {
			delete(s.staged, id)
			changed = true
		}
	}
	if changed {
		return effects.WriteStagedWindows(s.path, s.staged)
	}
	return nil
}

func (s *stagingPlan) rollback(ctx context.Context, driver wm.Driver) {
	ctx = context.WithoutCancel(ctx)
	for id := range s.attempted {
		window, found, err := driver.QueryWindow(ctx, id)
		ref, staged := s.staged[id]
		if err == nil && found && staged && ref.App == window.App && window.Floating {
			var placed bool
			placed, err = placeWindowOutcome(ctx, driver, window, window.SpaceID)
			if err == nil && !placed {
				err = fmt.Errorf("window %d could not be tiled", id)
			}
		}
		if err != nil {
			slog.WarnContext(ctx, "rolling back staged window", "window", id, "error", err)
		}
	}
	windows, err := driver.QueryWindows(ctx)
	if err == nil {
		err = s.prune(windows)
	}
	if err != nil {
		slog.WarnContext(ctx, "pruning staged windows after rollback", "error", err)
	}
}
