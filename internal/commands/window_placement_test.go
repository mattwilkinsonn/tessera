package commands

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
)

func candidate(id int) wm.Window {
	return wm.Window{ID: id, App: "Code", Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, Floating: true, SpaceID: "1"}
}

func TestHandleWindowCreatedWaits(t *testing.T) {
	for _, name := range []string{"manual lock release without event", "manual apply already tiled", "crashed guard expires", "bounded lock wait"} {
		t.Run(name, func(t *testing.T) { synctest.Test(t, func(t *testing.T) { testPlacementWait(t, name) }) })
	}
}

func testPlacementWait(t *testing.T, name string) {
	t.Helper()
	env := commandEnv(t)
	driver := newPlacementDriver(deskWorld(), candidate(90))
	var release func()
	if name == "crashed guard expires" {
		if err := effects.SuppressSignals(env.Paths.SignalGuard); err != nil {
			t.Fatal(err)
		}
		release = func() {}
	} else {
		lock, acquired, err := effects.AcquireLock(env.Paths.ApplyLock)
		if err != nil || !acquired {
			t.Fatalf("lock: %t, %v", acquired, err)
		}
		release = func() {
			if err := lock.Unlock(); err != nil {
				t.Error(err)
			}
		}
	}
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if name != "bounded lock wait" {
		ctx = t.Context()
	}
	go func() { done <- HandleWindowCreated(ctx, testProfile(t), driver, env, 90) }()
	synctest.Wait()
	driver.mu.Lock()
	if driver.moves != 0 {
		t.Fatal("mutated before lock/guard release")
	}
	if name == "manual apply already tiled" {
		driver.window.SpaceID = "2"
		driver.window.Floating = false
	}
	driver.mu.Unlock()
	if name == "bounded lock wait" {
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("wait error = %v", err)
		}
		release()
		return
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if name == "manual apply already tiled" {
		if driver.moves+driver.focuses+driver.toggles != 0 {
			t.Fatal("did not re-query after manual apply")
		}
	} else if driver.window.SpaceID != "3" || driver.window.Floating {
		t.Fatalf("arrival not placed after wait: %+v", driver.window)
	}
}

func TestHandleWindowCreatedRechecksReadiness(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		env := commandEnv(t)
		driver := newPlacementDriver(deskWorld(), candidate(90))
		if err := effects.SuppressSignals(env.Paths.SignalGuard); err != nil {
			t.Fatal(err)
		}
		checked := make(chan struct{})
		env.ArrivalReady = func() bool { close(checked); return false }
		done := make(chan error, 1)
		go func() { done <- HandleWindowCreated(t.Context(), testProfile(t), driver, env, 90) }()
		<-checked
		if err := <-done; !errors.Is(err, ErrDisplayPending) {
			t.Fatalf("readiness result = %v", err)
		}
		if driver.moves+driver.focuses+driver.toggles != 0 {
			t.Fatal("placed while new display work was pending")
		}
	})
}

type transitionDriver struct {
	*fake.Driver
	rules     ruleRecorder
	toggled   bool
	converged bool
	t         *testing.T
}

func (d *transitionDriver) List(context.Context) ([]wm.Rule, error) { return nil, nil }
func (d *transitionDriver) Remove(context.Context, string) error    { return nil }
func (d *transitionDriver) Add(_ context.Context, rule wm.Rule) error {
	d.rules.added = append(d.rules.added, rule)
	return nil
}
func (d *transitionDriver) Apply(context.Context) error { d.t.Fatal("bulk rule apply"); return nil }
func (d *transitionDriver) ToggleFloat(ctx context.Context, id int) error {
	if len(d.rules.added) == 0 || d.converged {
		d.t.Fatal("handoff must follow Rules and precede convergence")
	}
	d.toggled = true
	return d.Driver.ToggleFloat(ctx, id)
}
func (d *transitionDriver) LabelSpace(ctx context.Context, id wm.SpaceID, label string) error {
	if !d.toggled {
		d.t.Fatal("convergence ran before floating desk arrival was handed off")
	}
	d.converged = true
	return d.Driver.LabelSpace(ctx, id, label)
}

func TestRunDisplayCascadeHandoff(t *testing.T) {
	profile := testProfile(t)
	driver := &transitionDriver{Driver: fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 3, Frame: displayFrame(1728, 1117)}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 3, Label: "laptop"}, {DisplayIdx: 3}},
		Windows:  []fake.SeedWindow{{ID: 93, App: "Arc", SpaceIndex: 2, Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, Floating: true}},
	}), t: t}
	env := commandEnv(t)
	env.Topology = &TopologyState{}
	env.Topology.RecordDisplays(3)
	if _, err := RunDisplayCascade(t.Context(), profile, driver, env); err != nil {
		t.Fatal(err)
	}
	window, found, err := driver.QueryWindow(t.Context(), 93)
	if err != nil || !found || window.Floating || !driver.converged {
		t.Fatalf("handoff = %+v, %t, %v", window, found, err)
	}
	spaces, err := driver.QuerySpaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(spaces, func(space wm.Space) bool { return space.ID == window.SpaceID && space.Label == "lap-arc" }) {
		t.Fatalf("surviving arrival was not claimed by pinned convergence: %+v", window)
	}
}

func TestLaptopArrivalConvergence(t *testing.T) {
	driver := fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 3, Frame: displayFrame(1728, 1117)}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 3, Label: "laptop"}},
		Windows:  []fake.SeedWindow{{ID: 1, App: "Arc", SpaceIndex: 1}, {ID: 2, App: "Qalculate", SpaceIndex: 1}, {ID: 3, App: "Messages", SpaceIndex: 1}},
	})
	if _, err := RunFlexConverge(t.Context(), testProfile(t), driver, commandEnv(t)); err != nil {
		t.Fatal(err)
	}
	spaces, err := driver.QuerySpaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		id    int
		label string
	}{
		{name: "pinned", id: 1, label: "lap-arc"},
		{name: "flex tail", id: 2, label: "lap-qalculate"},
		{name: "stack app", id: 3, label: "laptop"},
	} {
		t.Run(test.name, func(t *testing.T) {
			window, found, err := driver.QueryWindow(t.Context(), test.id)
			if err != nil || !found {
				t.Fatalf("window = %t, %v", found, err)
			}
			if !slices.ContainsFunc(spaces, func(space wm.Space) bool { return space.ID == window.SpaceID && space.Label == test.label }) {
				t.Fatalf("window %+v not in %s: %+v", window, test.label, spaces)
			}
		})
	}
}

// recoveryDriver records recovery mutations independently of layout reclamation.
type recoveryDriver struct {
	*fake.Driver
	toggled []int
}

func (d *recoveryDriver) ToggleFloat(ctx context.Context, id int) error {
	d.toggled = append(d.toggled, id)
	return d.Driver.ToggleFloat(ctx, id)
}

func TestRecoverDeskWindows(t *testing.T) {
	profile := testProfile(t)
	driver := &recoveryDriver{Driver: fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}, {Idx: 3, Frame: displayFrame(1728, 1117)}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 3}},
		Windows: []fake.SeedWindow{
			{ID: 1, App: "Code", SpaceIndex: 1, Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true},
			{ID: 2, App: "Extra", SpaceIndex: 1, Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, Floating: true},
			{ID: 3, App: "Dialog", SpaceIndex: 1, Root: true, Role: "AXWindow", Subrole: "AXDialog", CanMove: true, CanResize: true, Floating: true},
		},
	})}
	// No spawn commands: this test concerns reclaim/recovery, not launcher polling.
	for name, spec := range profile.Windows {
		spec.Spawn = nil
		profile.Windows[name] = spec
	}
	env := commandEnv(t)
	if _, err := RunDisplayCascade(t.Context(), profile, driver, env); err != nil {
		t.Fatal(err)
	}
	// Make a reclaimed slot owner floating again: recovery must still respect ownership.
	if err := driver.Driver.ToggleFloat(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if err := RecoverDeskWindows(t.Context(), profile, driver, env); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(driver.toggled, []int{2}) {
		t.Fatalf("recovery toggled %v; want only unclaimed window 2", driver.toggled)
	}
	window, _, err := driver.QueryWindow(t.Context(), 2)
	if err != nil || window.SpaceID != "2" || window.Floating {
		t.Fatalf("recovery = %+v, %v", window, err)
	}
}
