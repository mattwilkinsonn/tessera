package daemon

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"gotest.tools/v3/assert"

	"github.com/mattwilkinsonn/tessera/internal/commands"
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/config/configtest"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
)

// recorder is a Work that records when it ran and returns scripted steps.
type recorder struct {
	mu    sync.Mutex
	runs  []time.Time
	steps []Step
}

func (r *recorder) work(_ context.Context) (Step, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs = append(r.runs, time.Now())
	if len(r.steps) == 0 {
		return Settled, nil
	}
	step := r.steps[0]
	r.steps = r.steps[1:]
	return step, nil
}

func (r *recorder) elapsed(start time.Time) []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]time.Duration, 0, len(r.runs))
	for _, run := range r.runs {
		out = append(out, run.Sub(start))
	}
	return out
}

// stopHub cancels the hub's context and waits for its waiters.
func stopHub(cancel context.CancelFunc, h *Hub) {
	cancel()
	h.waiters.Wait()
}

func idle(context.Context) (Step, error) { return Settled, nil }

func TestHubDisplayWaiter(t *testing.T) {
	t.Run("runs the cascade once when quiet, then nudges sketchybar once", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			var display recorder
			var nudges atomic.Int32
			h := NewHub(display.work, idle, OnDisplaySettled(func(context.Context) { nudges.Add(1) }))
			start := time.Now()
			for range 10 {
				h.Notify(ctx, string(wm.EventDisplayAdded))
				<-time.After(500 * time.Millisecond)
			}
			synctest.Wait()
			<-time.After(10 * time.Second)
			synctest.Wait()
			// The last event landed at 4.5s; one cascade runs at the 3s quiet deadline after it.
			assert.DeepEqual(t, display.elapsed(start), []time.Duration{7500 * time.Millisecond})
			assert.Equal(t, nudges.Load(), int32(1))
			stopHub(cancel, h)
		})
	})

	t.Run("re-loops on a mid-cascade event but nudges only once, after settling", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			entered := make(chan time.Time)
			release := make(chan struct{})
			var calls atomic.Int32
			work := func(context.Context) (Step, error) {
				if calls.Add(1) == 1 {
					entered <- time.Now()
					<-release
				}
				return Settled, nil
			}
			var nudges atomic.Int32
			h := NewHub(work, idle, OnDisplaySettled(func(context.Context) { nudges.Add(1) }))
			start := time.Now()
			h.Notify(ctx, string(wm.EventDisplayRemoved))
			assert.Equal(t, (<-entered).Sub(start), 3*time.Second)
			h.Notify(ctx, string(wm.EventDisplayMoved))
			close(release)
			synctest.Wait()
			assert.Equal(t, calls.Load(), int32(1))
			assert.Equal(t, nudges.Load(), int32(0))
			<-time.After(3 * time.Second)
			synctest.Wait()
			assert.Equal(t, calls.Load(), int32(2))
			assert.Equal(t, nudges.Load(), int32(1))
			stopHub(cancel, h)
		})
	})

	t.Run("an unknown event is dropped", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			var display, flex recorder
			h := NewHub(display.work, flex.work)
			h.Notify(ctx, "dock_did_restart")
			<-time.After(time.Minute)
			synctest.Wait()
			assert.Equal(t, len(display.elapsed(time.Now())), 0)
			assert.Equal(t, len(flex.elapsed(time.Now())), 0)
			stopHub(cancel, h)
		})
	})

	t.Run("cancel stops a pending waiter without running work", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			var display recorder
			h := NewHub(display.work, idle)
			h.Notify(ctx, string(wm.EventDisplayAdded))
			<-time.After(time.Second)
			stopHub(cancel, h)
			assert.Equal(t, len(display.elapsed(time.Now())), 0)
		})
	})
}

func TestHubFlexWaiter(t *testing.T) {
	t.Run("H7: flex waits for display-quiet", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			var display, flex recorder
			h := NewHub(display.work, flex.work)
			start := time.Now()
			h.Notify(ctx, string(wm.EventWindowCreated))
			<-time.After(500 * time.Millisecond)
			h.Notify(ctx, string(wm.EventDisplayAdded))
			<-time.After(10 * time.Second)
			synctest.Wait()
			// Flex alone would run at 2s; the display stamp at 0.5s holds it to 3.5s.
			assert.DeepEqual(t, flex.elapsed(start), []time.Duration{3500 * time.Millisecond})
			assert.DeepEqual(t, display.elapsed(start), []time.Duration{3500 * time.Millisecond})
			stopHub(cancel, h)
		})
	})

	t.Run("H2: a contended converge re-stamps and re-loops until it succeeds", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			flex := recorder{steps: []Step{Restamp}}
			h := NewHub(idle, flex.work)
			start := time.Now()
			h.Notify(ctx, string(wm.EventApplicationLaunched))
			<-time.After(time.Minute)
			synctest.Wait()
			assert.DeepEqual(t, flex.elapsed(start), []time.Duration{2 * time.Second, 4 * time.Second})
			stopHub(cancel, h)
		})
	})

	t.Run("H2: a held signal guard re-stamps and re-waits; converge runs once the guard clears", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			driver := laptopWorld()
			env := daemonEnv(t)
			assert.NilError(t, effects.SuppressSignals(env.Paths.SignalGuard))
			var steps []Step
			var runs []time.Duration
			start := time.Now()
			flexWork := FlexWork(testProfile(t), driver, env)
			h := NewHub(idle, func(ctx context.Context) (Step, error) {
				step, err := flexWork(ctx)
				steps = append(steps, step)
				runs = append(runs, time.Since(start))
				return step, err
			})
			h.Notify(ctx, string(wm.EventWindowDestroyed))
			<-time.After(time.Minute)
			synctest.Wait()
			stopHub(cancel, h)
			// The 8s guard holds the converge at 2, 4 and 6s; it runs at 8s, when the guard expires.
			assert.DeepEqual(t, steps, []Step{Restamp, Restamp, Restamp, Settled})
			assert.DeepEqual(t, runs, []time.Duration{2 * time.Second, 4 * time.Second, 6 * time.Second, 8 * time.Second})
			assert.Assert(t, hasLapSpace(t, driver))
		})
	})

	t.Run("stops without converging when a monitor is attached (not laptop-only)", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			driver := fake.New(fake.Seed{
				Displays: []fake.SeedDisplay{{Idx: 1}, {Idx: 2}},
				Spaces:   []fake.SeedSpace{{DisplayIdx: 1, Label: "laptop"}, {DisplayIdx: 2}},
				Windows:  []fake.SeedWindow{{ID: 1, App: "Arc", SpaceIndex: 1}},
			})
			var steps []Step
			flexWork := FlexWork(testProfile(t), driver, daemonEnv(t))
			h := NewHub(idle, func(ctx context.Context) (Step, error) {
				step, err := flexWork(ctx)
				steps = append(steps, step)
				return step, err
			})
			h.Notify(ctx, string(wm.EventApplicationTerminated))
			<-time.After(time.Minute)
			synctest.Wait()
			stopHub(cancel, h)
			assert.DeepEqual(t, steps, []Step{Settled})
			assert.Assert(t, !hasLapSpace(t, driver))
		})
	})
}

func testProfile(t *testing.T) *config.Profile {
	t.Helper()
	profile := configtest.Profile()
	assert.NilError(t, config.Validate(profile))
	return profile
}

func daemonEnv(t *testing.T) commands.Env {
	t.Helper()
	root := t.TempDir()
	return commands.Env{
		Paths: effects.Paths{
			ApplyLock:    filepath.Join(root, "apply.lock"),
			LaptopLock:   filepath.Join(root, "laptop.lock"),
			SignalGuard:  filepath.Join(root, "guard"),
			FlexOrder:    filepath.Join(root, "flex-order"),
			DaemonSocket: filepath.Join(root, "daemon.sock"),
		},
		Nudge: func(context.Context, string) error { return nil },
	}
}

func laptopWorld() *fake.Driver {
	return fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1, Frame: &wm.Frame{W: 1728, H: 1117}}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 1, Label: "laptop"}},
		Windows: []fake.SeedWindow{
			{ID: 1, App: "Arc", Title: "a", SpaceIndex: 1},
			{ID: 2, App: "Ghostty", Title: "pc", SpaceIndex: 1},
			{ID: 3, App: "Spotify", SpaceIndex: 1},
		},
	})
}

func hasLapSpace(t *testing.T, driver *fake.Driver) bool {
	t.Helper()
	spaces, err := driver.QuerySpaces(t.Context())
	assert.NilError(t, err)
	for _, space := range spaces {
		if len(space.Label) > 4 && space.Label[:4] == "lap-" {
			return true
		}
	}
	return false
}
