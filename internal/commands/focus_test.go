package commands

import (
	"context"
	"errors"
	"slices"
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

type focusOrderDriver struct {
	*fake.Driver
	events *[]string
}

func (d *focusOrderDriver) FocusWindow(ctx context.Context, id int) (bool, error) {
	*d.events = append(*d.events, "restore")
	return d.Driver.FocusWindow(ctx, id)
}

func TestPreserveFocusBusyRestoreOrder(t *testing.T) {
	for _, panics := range []bool{false, true} {
		name := "normal"
		if panics {
			name = "panic"
		}
		t.Run(name, func(t *testing.T) {
			events := []string{}
			driver := &focusOrderDriver{
				Driver: fake.New(fake.Seed{
					Spaces:  []fake.SeedSpace{{DisplayIdx: 1}},
					Windows: []fake.SeedWindow{{ID: 1, SpaceIndex: 1}},
				}),
				events: &events,
			}
			if ok, err := driver.Driver.FocusWindow(t.Context(), 1); err != nil || !ok {
				t.Fatalf("initial focus = %t, %v", ok, err)
			}
			nudge := func(_ context.Context, _ string, variables ...string) error {
				events = append(events, variables[0])
				return errors.New("sketchybar unavailable")
			}
			panicValue := &struct{ message string }{message: "rebuild panic"}
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				err := PreserveFocus(t.Context(), driver, nudge, func(context.Context) error {
					events = append(events, "rebuild")
					if panics {
						panic(panicValue)
					}
					return nil
				})
				if err != nil {
					t.Errorf("rebuild = %v", err)
				}
			}()
			if panics && recovered != panicValue {
				t.Fatalf("recovered = %v, want original panic value", recovered)
			}
			if !panics && recovered != nil {
				t.Fatalf("unexpected panic: %v", recovered)
			}
			if !slices.Equal(events, []string{"STATE=on", "rebuild", "restore", "STATE=off"}) {
				t.Fatalf("rebuild sequence = %v", events)
			}
		})
	}
}

func TestPreserveFocusBusyTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		states := []string{}
		nudge := func(ctx context.Context, _ string, variables ...string) error {
			states = append(states, variables[0])
			<-ctx.Done()
			return ctx.Err()
		}
		ran := false
		rebuildErr := errors.New("rebuild failed")
		start := time.Now()
		err := PreserveFocus(t.Context(), fake.New(fake.Seed{}), nudge, func(context.Context) error {
			ran = true
			return rebuildErr
		})
		if !ran || err != rebuildErr {
			t.Fatalf("ran = %t, result = %v", ran, err)
		}
		if !slices.Equal(states, []string{"STATE=on", "STATE=off"}) {
			t.Fatalf("busy states = %v", states)
		}
		if elapsed := time.Since(start); elapsed > 4*time.Second {
			t.Fatalf("signals exceeded bounded timeout: %v", elapsed)
		}
	})
}
