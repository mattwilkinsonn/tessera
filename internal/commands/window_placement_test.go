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

func deskSlotWindow(id int, app string, spaceIndex int, x float64) fake.SeedWindow {
	return fake.SeedWindow{
		ID: id, App: app, SpaceIndex: spaceIndex, Root: true,
		Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true,
		Frame: &wm.Frame{X: x, W: 1000, H: 1000},
	}
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

func TestHandleWindowCreatedRechecksReadinessAfterAnchorQuery(t *testing.T) {
	profile := testProfile(t)
	driver := &readinessAfterWindowsDriver{Driver: fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}, {Idx: 2, Frame: displayFrame(3440, 1440)}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 2}},
		Windows: []fake.SeedWindow{
			deskSlotWindow(100, "Arc", 2, 0),
			{ID: 90, App: "Code", SpaceIndex: 1, Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, Floating: true},
		},
	})}
	var ready atomic.Bool
	ready.Store(true)
	env := commandEnv(t)
	env.ArrivalReady = ready.Load
	driver.onQueryWindows = func() { ready.Store(false) }
	if err := HandleWindowCreated(t.Context(), profile, driver, env, 90); !errors.Is(err, ErrDisplayPending) {
		t.Fatalf("arrival error = %v; want display pending", err)
	}
	if driver.StackCalls != 0 || driver.ToggleCalls != 0 || driver.MoveCalls != 0 {
		t.Fatalf("placement mutated before readiness: stack=%d toggle=%d move=%d", driver.StackCalls, driver.ToggleCalls, driver.MoveCalls)
	}
}

type readinessAfterWindowsDriver struct {
	*fake.Driver
	onQueryWindows func()
	StackCalls     int
	ToggleCalls    int
	MoveCalls      int
}

func (d *readinessAfterWindowsDriver) QueryWindows(ctx context.Context) ([]wm.Window, error) {
	windows, err := d.Driver.QueryWindows(ctx)
	if err != nil {
		return nil, err
	}
	if d.onQueryWindows != nil {
		d.onQueryWindows()
	}
	return windows, nil
}

func (d *readinessAfterWindowsDriver) StackOnto(ctx context.Context, targetID, windowID int) error {
	d.StackCalls++
	return d.Driver.StackOnto(ctx, targetID, windowID)
}

func (d *readinessAfterWindowsDriver) MoveWindowToSpace(ctx context.Context, windowID int, space wm.SpaceID) error {
	d.MoveCalls++
	return d.Driver.MoveWindowToSpace(ctx, windowID, space)
}

func (d *readinessAfterWindowsDriver) ToggleFloat(ctx context.Context, windowID int) error {
	d.ToggleCalls++
	return d.Driver.ToggleFloat(ctx, windowID)
}

func TestArrivalPlacementStacksAfterFocusAndActiveSpaceChecks(t *testing.T) {
	for _, test := range []struct {
		name        string
		focusFailed bool
		focusErr    error
		activeWrong bool
	}{
		{name: "focus rejected", focusFailed: true},
		{name: "focus errored", focusFailed: true, focusErr: errors.New("focus failed")},
		{name: "active space changed", activeWrong: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			driver := &stackFailureDriver{
				Driver: fake.New(fake.Seed{
					Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}, {Idx: 2, Frame: displayFrame(3440, 1440)}},
					Spaces:   []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 2}},
					Windows: []fake.SeedWindow{
						deskSlotWindow(100, "Arc", 2, 0),
						{ID: 90, App: "Code", SpaceIndex: 1, Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, Floating: true},
						{ID: 91, App: "Code", SpaceIndex: 1, Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, Floating: true},
					},
				}),
				focusFailed: test.focusFailed,
				focusErr:    test.focusErr,
				activeWrong: test.activeWrong,
			}
			err := HandleWindowCreated(t.Context(), testProfile(t), driver, commandEnv(t), 90)
			if !errors.Is(err, test.focusErr) {
				t.Fatalf("placement error = %v; want %v", err, test.focusErr)
			}
			if driver.stackCalls != 0 || driver.toggleCalls != 0 || driver.moves != 1 {
				t.Fatalf("aborted placement side effects: stack=%d toggle=%d moves=%d", driver.stackCalls, driver.toggleCalls, driver.moves)
			}
			driver.focusFailed, driver.focusErr, driver.activeWrong = false, nil, false
			if err := HandleWindowCreated(t.Context(), testProfile(t), driver, commandEnv(t), 91); err != nil {
				t.Fatal(err)
			}
			if driver.stackCalls != 1 || driver.toggleCalls != 0 || driver.stacked != 1 {
				t.Fatalf("second arrival did not stack cleanly: stack=%d toggle=%d stacked=%d", driver.stackCalls, driver.toggleCalls, driver.stacked)
			}
		})
	}
}

type stackFailureDriver struct {
	*fake.Driver
	focusFailed bool
	focusErr    error
	activeWrong bool
	stackCalls  int
	toggleCalls int
	moves       int
	stacked     int
}

func (d *stackFailureDriver) FocusWindow(ctx context.Context, id int) (bool, error) {
	if d.focusFailed {
		return false, d.focusErr
	}
	return d.Driver.FocusWindow(ctx, id)
}

func (d *stackFailureDriver) QueryFocusedSpace(ctx context.Context) (wm.Space, bool, error) {
	if d.activeWrong {
		return wm.Space{}, false, nil
	}
	return d.Driver.QueryFocusedSpace(ctx)
}

func (d *stackFailureDriver) MoveWindowToSpace(ctx context.Context, windowID int, space wm.SpaceID) error {
	d.moves++
	return d.Driver.MoveWindowToSpace(ctx, windowID, space)
}

func (d *stackFailureDriver) StackOnto(ctx context.Context, targetID, windowID int) error {
	d.stackCalls++
	if err := d.Driver.StackOnto(ctx, targetID, windowID); err != nil {
		return err
	}
	d.stacked++
	return nil
}

func (d *stackFailureDriver) ToggleFloat(ctx context.Context, windowID int) error {
	d.toggleCalls++
	return d.Driver.ToggleFloat(ctx, windowID)
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
			subrole := "AXStandardWindow"
			if test.dialog {
				subrole = "AXDialog"
			}
			for i, title := range test.titles {
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
func TestHandleWindowCreatedArrivalTargets(t *testing.T) {
	tests := []struct {
		name                    string
		seed                    fake.Seed
		wantSpaceLabel          string
		wantFloating            bool
		wantStackTargets        []int
		checkSingleDisplayRules bool
	}{
		{
			name:           "laptop and both desks use laptop stack",
			seed:           fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}, {Idx: 2, Frame: displayFrame(3440, 1440)}, {Idx: 3, Frame: displayFrame(1728, 1117)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1, Label: "g9-home"}, {DisplayIdx: 2, Label: "aw-home"}, {DisplayIdx: 3, Label: "laptop-home"}}, Windows: []fake.SeedWindow{{ID: 90, App: "Code", Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, SpaceIndex: 1, Floating: true}}},
			wantSpaceLabel: "laptop-home",
		},
		{
			name:             "g9 and aw use the left aw column",
			seed:             fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}, {Idx: 2, Frame: displayFrame(3440, 1440)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1, Label: "g9-home"}, {DisplayIdx: 2, Label: "aw-home"}}, Windows: []fake.SeedWindow{deskSlotWindow(100, "Arc", 2, 0), {ID: 90, App: "Code", Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, SpaceIndex: 1, Floating: true}}},
			wantSpaceLabel:   "aw-home",
			wantStackTargets: []int{100},
		},
		{
			name:             "g9 without aw falls back to left g9 column",
			seed:             fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}, {Idx: 2, Frame: displayFrame(2560, 1440)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1, Label: "g9-home"}, {DisplayIdx: 2, Label: "external-home"}}, Windows: []fake.SeedWindow{deskSlotWindow(100, "Arc", 1, 50), {ID: 90, App: "Code", Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, SpaceIndex: 2, Floating: true}}},
			wantSpaceLabel:   "g9-home",
			wantStackTargets: []int{100},
		},
		{
			name:             "G9-only topology targets left slot",
			seed:             fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1, Label: "g9-home"}}, Windows: []fake.SeedWindow{deskSlotWindow(100, "Arc", 1, 40), {ID: 90, App: "Code", Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, SpaceIndex: 1, Floating: true}}},
			wantSpaceLabel:   "g9-home",
			wantStackTargets: []int{100},
		},
		{
			name:                    "laptop-only keeps single-display rules and placement",
			seed:                    fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(1728, 1117)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1, Label: "laptop-home"}}, Windows: []fake.SeedWindow{{ID: 90, App: "Code", Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, SpaceIndex: 1, Floating: true}}},
			wantSpaceLabel:          "laptop-home",
			wantFloating:            true,
			checkSingleDisplayRules: true,
		},
		{
			name:           "unmatched displays skip placement",
			seed:           fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(2560, 1440)}, {Idx: 2, Frame: displayFrame(1920, 1080)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1, Label: "external-one"}, {DisplayIdx: 2, Label: "external-two"}}, Windows: []fake.SeedWindow{{ID: 90, App: "Code", Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, SpaceIndex: 1, Floating: true}}},
			wantSpaceLabel: "external-one",
			wantFloating:   true,
		},
		{
			name:             "swapped windows stack into current leftmost slot",
			seed:             fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(3440, 1440)}, {Idx: 2, Frame: displayFrame(5120, 2160)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1, Label: "aw-home"}, {DisplayIdx: 2, Label: "g9-home"}}, Windows: []fake.SeedWindow{deskSlotWindow(100, "Akiflow", 1, 0), deskSlotWindow(101, "Arc", 1, 1200), {ID: 90, App: "Code", Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, SpaceIndex: 2, Floating: true}}},
			wantSpaceLabel:   "aw-home",
			wantStackTargets: []int{100},
		},
		{
			name:             "unmatched leftmost app still anchors arrival",
			seed:             fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(3440, 1440)}, {Idx: 2, Frame: displayFrame(5120, 2160)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1, Label: "aw-home"}, {DisplayIdx: 2, Label: "g9-home"}}, Windows: []fake.SeedWindow{deskSlotWindow(100, "Unconfigured", 1, 0), deskSlotWindow(101, "Arc", 1, 1200), {ID: 90, App: "Code", Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, SpaceIndex: 2, Floating: true}}},
			wantSpaceLabel:   "aw-home",
			wantStackTargets: []int{100},
		},
		{
			name:             "duplicate app anchor chooses leftmost position",
			seed:             fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(3440, 1440)}, {Idx: 2, Frame: displayFrame(5120, 2160)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1, Label: "aw-home"}, {DisplayIdx: 2, Label: "g9-home"}}, Windows: []fake.SeedWindow{deskSlotWindow(100, "Arc", 1, 0), deskSlotWindow(101, "Arc", 1, 1200), {ID: 90, App: "Code", Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, SpaceIndex: 2, Floating: true}}},
			wantSpaceLabel:   "aw-home",
			wantStackTargets: []int{100},
		},
		{
			name:             "missing configured first-track apps still stack into left slot",
			seed:             fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(3440, 1440)}, {Idx: 2, Frame: displayFrame(5120, 2160)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1, Label: "aw-home"}, {DisplayIdx: 2, Label: "g9-home"}}, Windows: []fake.SeedWindow{deskSlotWindow(100, "Spotify", 1, 0), deskSlotWindow(101, "Discord", 1, 1200), {ID: 90, App: "Code", Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, SpaceIndex: 2, Floating: true}}},
			wantSpaceLabel:   "aw-home",
			wantStackTargets: []int{100},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { testArrivalTarget(t, test) })
	}
}

func testArrivalTarget(t *testing.T, test struct {
	name                    string
	seed                    fake.Seed
	wantSpaceLabel          string
	wantFloating            bool
	wantStackTargets        []int
	checkSingleDisplayRules bool
}) {
	t.Helper()
	base := fake.New(test.seed)
	driver := &arrivalTargetDriver{Driver: base}
	profile := testProfile(t)
	if test.checkSingleDisplayRules {
		recorder := &ruleRecorder{}
		if err := Rules(t.Context(), profile, &ruleDriver{Driver: base, rules: recorder}); err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc(recorder.added, func(rule wm.Rule) bool { return rule.Label == "auto:desk-manage-off" }) {
			t.Fatal("single laptop display unexpectedly installs desk manage-off rule")
		}
	}
	if err := HandleWindowCreated(t.Context(), profile, driver, commandEnv(t), 90); err != nil {
		t.Fatal(err)
	}
	window, found, err := driver.QueryWindow(t.Context(), 90)
	if err != nil || !found {
		t.Fatalf("arrival window = %t, %v", found, err)
	}
	spaces, err := driver.QuerySpaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	gotSpaceLabel := ""
	for _, space := range spaces {
		if space.ID == window.SpaceID {
			gotSpaceLabel = space.Label
			break
		}
	}
	if gotSpaceLabel != test.wantSpaceLabel || window.Floating != test.wantFloating {
		t.Fatalf("arrival = %+v in %q; want floating=%t in %q", window, gotSpaceLabel, test.wantFloating, test.wantSpaceLabel)
	}
	if !slices.Equal(driver.stackTargets, test.wantStackTargets) {
		t.Fatalf("stack targets = %v; want %v", driver.stackTargets, test.wantStackTargets)
	}
}
func TestHandoffDeskWindowsUsesLeftAWColumn(t *testing.T) {
	driver := newLidClosedArrivalDriver()
	if err := HandoffDeskWindows(t.Context(), testProfile(t), driver); err != nil {
		t.Fatal(err)
	}
	assertLeftAWArrival(t, driver)
}

func TestRecoverDeskWindowsUsesLeftAWColumn(t *testing.T) {
	driver := newLidClosedArrivalDriver()
	env := commandEnv(t)
	env.ReclaimedDeskWindows = make(map[int]struct{})
	if err := RecoverDeskWindows(t.Context(), testProfile(t), driver, env); err != nil {
		t.Fatal(err)
	}
	assertLeftAWArrival(t, driver)
}

type arrivalTargetDriver struct {
	*fake.Driver
	stackTargets []int
}

func (d *arrivalTargetDriver) StackOnto(ctx context.Context, targetID, windowID int) error {
	d.stackTargets = append(d.stackTargets, targetID)
	return d.Driver.StackOnto(ctx, targetID, windowID)
}

func newLidClosedArrivalDriver() *arrivalTargetDriver {
	return &arrivalTargetDriver{Driver: fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{
			{Idx: 1, Frame: displayFrame(5120, 2160)},
			{Idx: 2, Frame: displayFrame(3440, 1440)},
		},
		Spaces: []fake.SeedSpace{
			{DisplayIdx: 1, Label: "g9-home"},
			{DisplayIdx: 2, Label: "aw-home"},
		},
		Windows: []fake.SeedWindow{
			deskSlotWindow(100, "Arc", 2, 0),
			{ID: 90, App: "Code", Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true, SpaceIndex: 1, Floating: true},
		},
	})}
}

func assertLeftAWArrival(t *testing.T, driver *arrivalTargetDriver) {
	t.Helper()
	window, found, err := driver.QueryWindow(t.Context(), 90)
	if err != nil || !found {
		t.Fatalf("arrival window = %t, %v", found, err)
	}
	spaces, err := driver.QuerySpaces(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	gotSpaceLabel := ""
	for _, space := range spaces {
		if space.ID == window.SpaceID {
			gotSpaceLabel = space.Label
			break
		}
	}
	if gotSpaceLabel != "aw-home" || window.Floating {
		t.Fatalf("arrival = %+v in %q; want tiled in aw-home", window, gotSpaceLabel)
	}
	if !slices.Equal(driver.stackTargets, []int{100}) {
		t.Fatalf("stack targets = %v; want [100]", driver.stackTargets)
	}
}
