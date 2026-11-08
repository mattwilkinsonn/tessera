// Package executor realizes engine plans against a window-manager driver.
package executor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

const convergeIterationCap = 200

// RunPlan executes plan operations in order. onOp runs before each operation.
func RunPlan(ctx context.Context, driver wm.Driver, plan []engine.Op, onOp func(engine.Op) error) error {
	for _, op := range plan {
		if err := ctx.Err(); err != nil {
			return err
		}
		if onOp != nil {
			if err := onOp(op); err != nil {
				return fmt.Errorf("before %T: %w", op, err)
			}
		}
		if err := applyOp(ctx, driver, op); err != nil {
			return fmt.Errorf("applying %T: %w", op, err)
		}
	}
	return nil
}

func applyOp(ctx context.Context, driver wm.Driver, op engine.Op) error {
	switch op := op.(type) {
	case engine.RelabelHomeAction:
		return driver.LabelSpace(ctx, op.HomeSpace, op.Label)
	case engine.CreateSpaceAction:
		id, created, err := driver.CreateSpace(ctx, op.DisplayIdx)
		if err != nil || !created {
			return err
		}
		if err := driver.LabelSpace(ctx, id, op.Label); err != nil {
			return err
		}
		return driver.SetSpaceLayout(ctx, id, wm.LayoutBSP)
	case engine.MoveWindowAction:
		err := driver.MoveWindowToSpace(ctx, op.WindowID, op.ToSpace)
		if op.Evacuation && errors.Is(err, wm.ErrSpaceFull) {
			return floatOverflow(ctx, driver, op.WindowID, false)
		}
		return err
	case engine.RehomeAndDestroyAction:
		windows, err := driver.QueryWindowsOnSpace(ctx, op.StaleSpace)
		if err != nil {
			return fmt.Errorf("querying stale-space windows: %w", err)
		}
		for _, window := range windows {
			if err := driver.MoveWindowToSpace(ctx, window.ID, op.HomeSpace); err != nil {
				return fmt.Errorf("moving window %d home: %w", window.ID, err)
			}
		}
		_, err = driver.DestroySpace(ctx, op.StaleSpace)
		return err
	case engine.MoveSpaceAction:
		return driver.MoveSpaceToIndex(ctx, op.Space, op.ToIndex)
	case engine.SetLayoutAction:
		return driver.SetSpaceLayout(ctx, op.Space, op.Layout)
	case engine.DestroySpaceOp:
		_, err := driver.DestroySpace(ctx, op.Space)
		return err
	case engine.RealizeLayoutOp:
		return driver.RealizeSpaceLayout(ctx, op.Space, op.Target)
	case engine.BalanceSpaceOp:
		return driver.BalanceSpace(ctx, &op.Space)
	case engine.FloatWindowOp:
		return floatOverflow(ctx, driver, op.WindowID, op.Staging)
	default:
		return fmt.Errorf("unhandled plan operation %T", op)
	}
}

func floatOverflow(ctx context.Context, driver wm.Driver, id int, staging bool) error {
	window, found, err := driver.QueryWindow(ctx, id)
	if err != nil || !found {
		return err
	}
	if !window.Floating {
		if err := driver.ToggleFloat(ctx, id); err != nil {
			return fmt.Errorf("floating evacuation overflow %d: %w", id, err)
		}
	}
	if staging {
		slog.DebugContext(ctx, "staging desk slot owner", "window", id)
	} else {
		slog.WarnContext(ctx, "floating evacuation overflow", "window", id)
	}
	return nil
}

// RunConverge queries a fresh world for each step and realizes each corrective action.
func RunConverge[S any](ctx context.Context, driver wm.Driver, step func(engine.World, S) (engine.ConvergeAction, S, bool), initial S) (S, error) {
	state := initial
	for range convergeIterationCap {
		world, err := wm.Snapshot(ctx, driver)
		if err != nil {
			var zero S
			return zero, err
		}
		engineWorld := engine.World{Windows: world.Windows, Spaces: world.Spaces, Displays: world.Displays}
		action, next, done := step(engineWorld, state)
		if done {
			return next, nil
		}
		if err := ctx.Err(); err != nil {
			var zero S
			return zero, err
		}
		if err := applyOp(ctx, driver, action); err != nil {
			var zero S
			return zero, fmt.Errorf("applying converge action: %w", err)
		}
		state = next
	}
	var zero S
	return zero, fmt.Errorf("run converge exceeded %d iterations without reaching a fixed point", convergeIterationCap)
}
