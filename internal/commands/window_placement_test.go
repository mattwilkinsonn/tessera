package commands

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/engine"

	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
)

func candidate() wm.Window {
	return wm.Window{ID: 90, App: "Code", Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, Floating: true, SpaceID: "1"}
}

func TestHandleWindowCreatedWaits(t *testing.T) {
	for _, name := range []string{"manual lock release without event", "manual apply already tiled", "crashed guard expires", "bounded lock wait"} {
		t.Run(name, func(t *testing.T) { synctest.Test(t, func(t *testing.T) { testPlacementWait(t, name) }) })
	}
}

func testPlacementWait(t *testing.T, name string) {
	t.Helper()
	env := commandEnv(t)
	driver := newPlacementDriver(deskWorld(), candidate())
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
		driver := newPlacementDriver(deskWorld(), candidate())
		lock, acquired, err := effects.AcquireLock(env.Paths.ApplyLock)
		if err != nil || !acquired {
			t.Fatalf("lock: %t, %v", acquired, err)
		}
		var ready atomic.Bool
		ready.Store(true)
		env.ArrivalReady = ready.Load
		done := make(chan error, 1)
		go func() { done <- HandleWindowCreated(t.Context(), testProfile(t), driver, env, 90) }()
		synctest.Wait()
		ready.Store(false)
		if err := lock.Unlock(); err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, ErrDisplayPending) {
			t.Fatalf("readiness result = %v", err)
		}
		if driver.queries+driver.moves+driver.focuses+driver.toggles != 0 {
			t.Fatal("placed while new display work was pending")
		}
	})
}

func TestRunTopologyCommandsSerializePlacement(t *testing.T) {
	for _, command := range []Command{RulesCommand{}, DisplaySetupCommand{}} {
		t.Run(fmt.Sprintf("%T", command), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				env := commandEnv(t)
				driver := &blockedTopologyDriver{Driver: deskWorld(), entered: make(chan struct{}), release: make(chan struct{})}
				done := make(chan error, 1)
				go func() { _, err := Run(t.Context(), testProfile(t), command, driver, env); done <- err }()
				<-driver.entered
				// Independent drivers model separate CLI and daemon processes; only paths are shared.
				arrival := newPlacementDriver(deskWorld(), candidate())
				placed := make(chan error, 1)
				go func() { placed <- HandleWindowCreated(t.Context(), testProfile(t), arrival, env, 90) }()
				synctest.Wait()
				arrival.mu.Lock()
				queries := arrival.queries
				arrival.mu.Unlock()
				if queries != 0 {
					t.Fatal("arrival queried during standalone topology command")
				}
				close(driver.release)
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if err := <-placed; err != nil {
					t.Fatal(err)
				}
				if arrival.window.Floating {
					t.Fatal("arrival was not placed after command completion")
				}
			})
		})
	}
}

type blockedTopologyDriver struct {
	*fake.Driver
	entered chan struct{}
	release chan struct{}
}

func (d *blockedTopologyDriver) List(context.Context) ([]wm.Rule, error) { return nil, nil }
func (d *blockedTopologyDriver) Remove(context.Context, string) error    { return nil }
func (d *blockedTopologyDriver) Add(context.Context, wm.Rule) error      { return nil }
func (d *blockedTopologyDriver) Apply(context.Context) error {
	return errors.New("unexpected bulk rule apply")
}

func (d *blockedTopologyDriver) QueryDisplays(ctx context.Context) ([]wm.Display, error) {
	close(d.entered)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-d.release:
		return d.Driver.QueryDisplays(ctx)
	}
}

type transitionDriver struct {
	*fake.Driver
	rules       ruleRecorder
	toggled     bool
	converged   bool
	t           *testing.T
	failRules   bool
	failHandoff bool
}

func (d *transitionDriver) List(context.Context) ([]wm.Rule, error) { return nil, nil }
func (d *transitionDriver) Remove(context.Context, string) error    { return nil }
func (d *transitionDriver) Add(_ context.Context, rule wm.Rule) error {
	if d.failRules {
		d.failRules = false
		return errors.New("rules failed once")
	}
	d.rules.added = append(d.rules.added, rule)
	return nil
}
func (d *transitionDriver) Apply(context.Context) error { d.t.Fatal("bulk rule apply"); return nil }
func (d *transitionDriver) ToggleFloat(ctx context.Context, id int) error {
	if d.failHandoff {
		d.failHandoff = false
		return errors.New("handoff failed once")
	}
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

func TestRunDisplayCascadeRetriesHandoff(t *testing.T) {
	for _, stage := range []string{"rules", "handoff"} {
		t.Run(stage, func(t *testing.T) {
			driver := &transitionDriver{Driver: fake.New(fake.Seed{
				Displays: []fake.SeedDisplay{{Idx: 3, Frame: displayFrame(1728, 1117)}},
				Spaces:   []fake.SeedSpace{{DisplayIdx: 3, Label: "laptop"}, {DisplayIdx: 3}},
				Windows:  []fake.SeedWindow{{ID: 93, App: "Arc", SpaceIndex: 2, Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, Floating: true}},
			}), t: t, failRules: stage == "rules", failHandoff: stage == "handoff"}
			env := commandEnv(t)
			env.Topology = &TopologyState{}
			env.Topology.RecordDisplays(3)
			if _, err := RunDisplayCascade(t.Context(), testProfile(t), driver, env); err == nil {
				t.Fatal("first cascade did not fail")
			}
			if count, known := env.Topology.Displays(); !known || count != 3 {
				t.Fatalf("failed handoff consumed transition: %d, %t", count, known)
			}
			if _, err := RunDisplayCascade(t.Context(), testProfile(t), driver, env); err != nil {
				t.Fatal(err)
			}
			window, found, err := driver.QueryWindow(t.Context(), 93)
			if err != nil || !found || window.Floating || !driver.converged {
				t.Fatalf("retry did not reclaim arrival: %+v, %t, %v", window, found, err)
			}
		})
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
	env.ReclaimedDeskWindows = make(map[int]struct{})
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

func TestRecoverDeskWindowsIgnoresHistoricalClaims(t *testing.T) {
	profile := testProfile(t)
	for name, spec := range profile.Windows {
		spec.Spawn = nil
		profile.Windows[name] = spec
	}
	driver := &recoveryDriver{Driver: fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}, {Idx: 3, Frame: displayFrame(1728, 1117)}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 3}},
		Windows:  []fake.SeedWindow{{ID: 1, App: "Code", SpaceIndex: 1, Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, Floating: true}},
	})}
	env := commandEnv(t)
	env.ReclaimedDeskWindows = make(map[int]struct{})
	displays, err := driver.QueryDisplays(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	key := engine.SlotKeys("desk", config.DisplayG9, []string{"vscode"})[0]
	if err := effects.WriteSlots(env.Paths.WindowSlots, engine.SlotMemory{Seq: 7, Slots: map[engine.SlotKey]engine.WindowRef{
		key: {ID: 1, App: "Code", Displays: engine.DisplaySet(profile, displays), Seq: 7},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := RunDisplayCascade(t.Context(), profile, driver, env); err != nil {
		t.Fatal(err)
	}
	if err := RecoverDeskWindows(t.Context(), profile, driver, env); err != nil {
		t.Fatal(err)
	}
	window, _, err := driver.QueryWindow(t.Context(), 1)
	if err != nil || window.SpaceID != "2" || window.Floating || !slices.Equal(driver.toggled, []int{1}) {
		t.Fatalf("historical owner not recovered: %+v, toggled %v, %v", window, driver.toggled, err)
	}
}

func TestAdoptSpawnedWindowsClaimPolicy(t *testing.T) {
	for _, test := range []struct {
		name     string
		titles   []string
		baseline bool
		dialog   bool
		want     []int
	}{
		{name: "blank title fallback", titles: []string{""}, want: []int{1}},
		{name: "other spec rejected", titles: []string{"mbp"}},
		{name: "exact match precedes fallback", titles: []string{"", "pc"}, want: []int{2}},
		{name: "baseline excluded", titles: []string{"pc"}, baseline: true},
		{name: "dialog excluded", titles: []string{"pc"}, dialog: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile := testProfile(t)
			spec := profile.Windows["ghostty-wave"]
			spec.Spawn = []string{"open", "-a", "Ghostty"}
			profile.Windows["ghostty-wave"] = spec
			seed := fake.Seed{
				Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}, {Idx: 3, Frame: displayFrame(1728, 1117)}},
				Spaces:   []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 3}},
			}
			for i, title := range test.titles {
				subrole := "AXStandardWindow"
				if test.dialog {
					subrole = "AXDialog"
				}
				seed.Windows = append(seed.Windows, fake.SeedWindow{
					ID: i + 1, App: "Ghostty", Title: title, SpaceIndex: 1,
					Root: true, Role: "AXWindow", Subrole: subrole,
					CanMove: true, CanResize: true, Floating: true,
				})
			}
			driver := &recoveryDriver{Driver: fake.New(seed)}
			before := []wm.Window{}
			if test.baseline {
				var err error
				before, err = driver.QueryWindows(t.Context())
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := runPlacementCommand(t.Context(), commandEnv(t), func(ctx context.Context) error {
				return adoptSpawnedWindows(ctx, profile, driver, []string{"ghostty-wave"}, before)
			}); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(driver.toggled, test.want) {
				t.Fatalf("adopted %v; want %v", driver.toggled, test.want)
			}
		})
	}
}
