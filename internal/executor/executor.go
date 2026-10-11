// Package executor realizes engine plans against a window-manager driver.
package executor

import (
	"context"
	"fmt"

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
		return driver.MoveWindowToSpace(ctx, op.WindowID, op.ToSpace)
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
	default:
		return fmt.Errorf("unhandled plan operation %T", op)
	}
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
		action, next, done := step(engine.World{
			Windows:  world.Windows,
			Spaces:   world.Spaces,
			Displays: world.Displays,
		}, state)
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
