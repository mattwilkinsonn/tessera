package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

type focusRestoreKey struct{}

// PreserveFocus restores the original surviving window after run, including on
// failure. Nested rebuilds share the outer caller's snapshot and restoration.
func PreserveFocus(ctx context.Context, driver wm.Driver, nudge effects.Nudger, run func(context.Context) error) (err error) {
	if ctx.Value(focusRestoreKey{}) != nil {
		return run(ctx)
	}
	if nudge == nil {
		nudge = effects.NudgeSketchybar
	}
	defer signalBusy(context.WithoutCancel(ctx), nudge, "off")
	signalBusy(ctx, nudge, "on")
	focused, found, err := driver.QueryFocusedWindow(ctx)
	if err != nil {
		return fmt.Errorf("saving focus before rebuild: %w", err)
	}
	ctx = context.WithValue(ctx, focusRestoreKey{}, true)
	if found {
		defer func() {
			// Cleanup must still run if realization canceled its context.
			restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			err = errors.Join(err, restoreFocus(restoreCtx, driver, focused.ID))
		}()
	}
	return run(ctx)
}

func signalBusy(ctx context.Context, nudge effects.Nudger, state string) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := nudge(ctx, "tess_busy", "STATE="+state); err != nil {
		slog.DebugContext(ctx, "signaling rebuild busy state", "state", state, "error", err)
	}
}

func restoreFocus(ctx context.Context, driver wm.Driver, id int) error {
	if _, found, err := driver.QueryWindow(ctx, id); err != nil {
		return fmt.Errorf("checking window %d before restoring focus: %w", id, err)
	} else if !found {
		return nil
	}
	focused, err := driver.FocusWindow(ctx, id)
	if err != nil {
		return fmt.Errorf("restoring focus to window %d: %w", id, err)
	}
	if focused {
		return nil
	}
	// The window can close between the existence check and the focus command.
	if _, found, err := driver.QueryWindow(ctx, id); err != nil {
		return fmt.Errorf("checking window %d after restoring focus: %w", id, err)
	} else if !found {
		return nil
	}
	return fmt.Errorf("restoring focus to window %d: focus rejected", id)
}
