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
	err := PreserveFocus(ctx, driver, func(ctx context.Context) error {
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
