package commands

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
)

func TestPreserveFocusCleanup(t *testing.T) {
	for _, scenario := range []string{
		"canceled rebuild restores with live deadline",
		"blocked restore stops at cleanup deadline",
		"cleanup context is canceled after return",
	} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testPreserveFocusCleanup(t, scenario)
			})
		})
	}
}

func testPreserveFocusCleanup(t *testing.T, scenario string) {
	t.Helper()
	rebuildErr := errors.New("rebuild failed")
	cleanupErr := errors.New("cleanup failed")
	driver := &focusCleanupDriver{
		Driver: fake.New(fake.Seed{
			Spaces:  []fake.SeedSpace{{DisplayIdx: 1}},
			Windows: []fake.SeedWindow{{ID: 1, SpaceIndex: 1}},
		}),
		t: t, block: scenario == "blocked restore stops at cleanup deadline", cleanupErr: cleanupErr,
	}
	if focused, err := driver.Driver.FocusWindow(t.Context(), 1); err != nil || !focused {
		t.Fatalf("initial focus = %t, %v", focused, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := PreserveFocus(ctx, driver, commandEnv(t).Nudge, func(ctx context.Context) error {
		cancel()
		return errors.Join(rebuildErr, ctx.Err())
	})
	if !errors.Is(err, rebuildErr) || !errors.Is(err, context.Canceled) {
		t.Errorf("PreserveFocus = %v, want original rebuild and cancellation errors", err)
	}
	if driver.cleanupCtx == nil {
		t.Fatal("restore was not attempted")
	}
	select {
	case <-driver.cleanupCtx.Done():
	default:
		t.Error("cleanup context still live after PreserveFocus returned")
	}
	if driver.block {
		if !errors.Is(err, cleanupErr) || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("PreserveFocus = %v, want cleanup and deadline errors", err)
		}
		return
	}
	if !driver.restored || !errors.Is(driver.cleanupCtx.Err(), context.Canceled) {
		t.Errorf("restored = %t, cleanup context error = %v", driver.restored, driver.cleanupCtx.Err())
	}
}

type focusCleanupDriver struct {
	*fake.Driver
	t          *testing.T
	block      bool
	cleanupErr error
	cleanupCtx context.Context
	restored   bool
}

func (d *focusCleanupDriver) FocusWindow(ctx context.Context, id int) (bool, error) {
	d.t.Helper()
	d.cleanupCtx = ctx
	if err := ctx.Err(); err != nil {
		d.t.Errorf("restore received canceled context: %v", err)
		return false, err
	}
	deadline, bounded := ctx.Deadline()
	if !bounded || !deadline.After(time.Now()) {
		d.t.Error("restore context must have a future finite deadline")
		return false, d.cleanupErr
	}
	if d.block {
		<-ctx.Done()
		return false, errors.Join(d.cleanupErr, ctx.Err())
	}
	var err error
	d.restored, err = d.Driver.FocusWindow(ctx, id)
	return d.restored, err
}

func TestPreserveFocusBusyCleanup(t *testing.T) {
	for _, scenario := range []string{"success", "error", "cancel", "nested"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			driver := fake.New(fake.Seed{})
			states := []string{}
			rebuildErr := errors.New("rebuild failed")
			nudge := func(ctx context.Context, event string, variables ...string) error {
				if event != "tess_busy" || len(variables) != 1 {
					t.Fatalf("unexpected signal: %s %v", event, variables)
				}
				if ctx.Err() != nil {
					t.Errorf("signal context canceled: %v", ctx.Err())
				}
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 2*time.Second {
					t.Error("signal context has no short deadline")
				}
				states = append(states, variables[0])
				return errors.New("sketchybar unavailable")
			}
			err := PreserveFocus(ctx, driver, nudge, func(ctx context.Context) error {
				switch scenario {
				case "error":
					return rebuildErr
				case "cancel":
					cancel()
					return ctx.Err()
				case "nested":
					return PreserveFocus(ctx, driver, nudge, func(context.Context) error { return nil })
				}
				return nil
			})
			if len(states) != 2 || states[0] != "STATE=on" || states[1] != "STATE=off" {
				t.Fatalf("busy states = %v", states)
			}
			switch scenario {
			case "error":
				if !errors.Is(err, rebuildErr) {
					t.Fatalf("rebuild error = %v", err)
				}
			case "cancel":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation error = %v", err)
				}
			default:
				if err != nil {
					t.Fatalf("nudger changed rebuild result: %v", err)
				}
			}
		})
	}
}
