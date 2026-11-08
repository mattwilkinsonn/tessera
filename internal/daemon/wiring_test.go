package daemon

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"gotest.tools/v3/assert"

	"github.com/mattwilkinsonn/tessera/internal/commands"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
)

type eventStreamFake struct {
	events <-chan wm.Event
	err    error
	calls  int
	ctx    context.Context
}

func (f *eventStreamFake) Events(ctx context.Context) (<-chan wm.Event, error) {
	f.calls++
	f.ctx = ctx
	return f.events, f.err
}

type processStart struct {
	started bool
	exited  <-chan error
	err     error
}

type processSupervisorFake struct {
	mu         sync.Mutex
	starts     []processStart
	startCalls chan struct{}
	startCount int
	stopCount  int
}

func (f *processSupervisorFake) Start(context.Context) (bool, <-chan error, error) {
	f.mu.Lock()
	f.startCount++
	start := f.starts[0]
	f.starts = f.starts[1:]
	f.mu.Unlock()
	select {
	case f.startCalls <- struct{}{}:
	default:
	}
	return start.started, start.exited, start.err
}

func (f *processSupervisorFake) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCount++
	return nil
}

func (f *processSupervisorFake) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.startCount, f.stopCount
}

func TestRunForwardsEvents(t *testing.T) {
	events := append(append([]wm.Event{}, DisplayEvents...), FlexEvents...)
	for _, event := range events {
		t.Run(string(event), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				pushedEvents := make(chan wm.Event, 1)
				pushedEvents <- event
				close(pushedEvents)
				stream := &eventStreamFake{events: pushedEvents}
				var pushedDisplay, pushedFlex recorder
				pushedListener := newPipeListener()
				pushedCtx, cancelPushed := context.WithCancel(t.Context())
				defer cancelPushed()
				pushedDone := make(chan error, 1)
				go func() {
					pushedDone <- run(pushedCtx, Options{
						Events:  stream,
						Display: pushedDisplay.work,
						Flex:    pushedFlex.work,
						Wake:    noWake,
					}, pushedListener, pushedListener.Close)
				}()
				synctest.Wait()
				pushedStart := time.Now()
				<-time.After(3 * time.Second)
				synctest.Wait()

				isDisplay := slices.Contains(DisplayEvents, event)
				wantDisplay, wantFlex := 0, 0
				if isDisplay {
					wantDisplay = 1
				} else {
					wantFlex = 1
				}
				assert.Equal(t, len(pushedDisplay.elapsed(pushedStart)), wantDisplay)
				assert.Equal(t, len(pushedFlex.elapsed(pushedStart)), wantFlex)
				select {
				case err := <-pushedDone:
					t.Fatalf("Run() ended after event channel closed: %v", err)
				default:
				}
				cancelPushed()
				assert.NilError(t, <-pushedDone)
				assert.Equal(t, stream.calls, 1)

				var socketDisplay, socketFlex recorder
				socketListener := newPipeListener()
				socketCtx, cancelSocket := context.WithCancel(t.Context())
				defer cancelSocket()
				socketDone := make(chan error, 1)
				go func() {
					socketDone <- run(socketCtx, Options{
						Display: socketDisplay.work,
						Flex:    socketFlex.work,
						Wake:    noWake,
					}, socketListener, socketListener.Close)
				}()
				socketStart := time.Now()
				socketListener.send(t, string(event))
				<-time.After(3 * time.Second)
				synctest.Wait()
				assert.DeepEqual(t, pushedDisplay.elapsed(pushedStart), socketDisplay.elapsed(socketStart))
				assert.DeepEqual(t, pushedFlex.elapsed(pushedStart), socketFlex.elapsed(socketStart))
				cancelSocket()
				assert.NilError(t, <-socketDone)
			})
		})
	}
}

func TestRunEventsError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sentinel := errors.New("subscribe failed")
		stream := &eventStreamFake{err: sentinel}
		err := run(t.Context(), Options{Events: stream}, newPipeListener(), func() error { return nil })
		assert.Assert(t, errors.Is(err, sentinel), "Run() error = %v", err)
		assert.Equal(t, stream.calls, 1)
	})
}

func TestRunProcessSupervision(t *testing.T) {
	t.Run("restarts an owned process after an error exit and stops its replacement", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			firstExit := make(chan error, 1)
			supervisor := &processSupervisorFake{
				starts: []processStart{
					{started: true, exited: firstExit},
					{started: true, exited: make(chan error)},
				},
				startCalls: make(chan struct{}, 2),
			}
			done := make(chan error, 1)
			ln := newPipeListener()
			go func() {
				done <- run(ctx, Options{Bootstrap: true, Supervisor: supervisor, Wake: noWake}, ln, ln.Close)
			}()
			<-supervisor.startCalls
			firstExit <- errors.New("process crashed")
			<-supervisor.startCalls
			synctest.Wait()
			cancel()
			assert.NilError(t, <-done)
			starts, stops := supervisor.counts()
			assert.Equal(t, starts, 2)
			assert.Equal(t, stops, 1)
		})
	})

	t.Run("does not restart or stop after a clean exit", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			exited := make(chan error, 1)
			supervisor := &processSupervisorFake{
				starts:     []processStart{{started: true, exited: exited}},
				startCalls: make(chan struct{}, 1),
			}
			done := make(chan error, 1)
			ln := newPipeListener()
			go func() {
				done <- run(ctx, Options{Bootstrap: true, Supervisor: supervisor, Wake: noWake}, ln, ln.Close)
			}()
			<-supervisor.startCalls
			exited <- nil
			synctest.Wait()
			starts, stops := supervisor.counts()
			assert.Equal(t, starts, 1)
			assert.Equal(t, stops, 0)
			cancel()
			assert.NilError(t, <-done)
		})
	})

	t.Run("does not own or stop an existing process", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			exited := make(chan error, 1)
			supervisor := &processSupervisorFake{
				starts:     []processStart{{started: false, exited: exited}},
				startCalls: make(chan struct{}, 1),
			}
			done := make(chan error, 1)
			ln := newPipeListener()
			go func() {
				done <- run(ctx, Options{Bootstrap: true, Supervisor: supervisor, Wake: noWake}, ln, ln.Close)
			}()
			<-supervisor.startCalls
			exited <- errors.New("existing process ended")
			synctest.Wait()
			starts, stops := supervisor.counts()
			assert.Equal(t, starts, 1)
			assert.Equal(t, stops, 0)
			cancel()
			assert.NilError(t, <-done)
		})
	})
}

func TestRunProcessSupervisionOwnership(t *testing.T) {
	t.Run("does not stop after a restart that found an existing process", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			firstExit := make(chan error, 1)
			supervisor := &processSupervisorFake{
				starts: []processStart{
					{started: true, exited: firstExit},
					{started: false, exited: make(chan error)},
				},
				startCalls: make(chan struct{}, 2),
			}
			done := make(chan error, 1)
			ln := newPipeListener()
			go func() {
				done <- run(ctx, Options{Bootstrap: true, Supervisor: supervisor, Wake: noWake}, ln, ln.Close)
			}()
			<-supervisor.startCalls
			firstExit <- errors.New("process crashed")
			<-supervisor.startCalls
			synctest.Wait()
			cancel()
			assert.NilError(t, <-done)
			starts, stops := supervisor.counts()
			assert.Equal(t, starts, 2)
			assert.Equal(t, stops, 0)
		})
	})

	t.Run("never starts or stops without bootstrap", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			supervisor := &processSupervisorFake{startCalls: make(chan struct{}, 1)}
			done := make(chan error, 1)
			ln := newPipeListener()
			go func() {
				done <- run(ctx, Options{Supervisor: supervisor, Wake: noWake}, ln, ln.Close)
			}()
			synctest.Wait()
			cancel()
			assert.NilError(t, <-done)
			starts, stops := supervisor.counts()
			assert.Equal(t, starts, 0)
			assert.Equal(t, stops, 0)
		})
	})
}

func TestRunEndsSubscriptionWhenServeReturnsEarly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stream := &eventStreamFake{events: make(chan wm.Event)}
		ln := newPipeListener()
		done := make(chan error, 1)
		go func() { done <- run(t.Context(), Options{Events: stream, Wake: noWake}, ln, ln.Close) }()
		synctest.Wait()
		assert.NilError(t, ln.Close())
		<-done
		assert.Assert(t, stream.ctx != nil)
		assert.Assert(t, stream.ctx.Err() != nil, "Events context still live after Run returned")
	})
}

func TestRunCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		ln := newPipeListener()
		done := make(chan error, 1)
		go func() { done <- run(ctx, Options{Wake: noWake}, ln, ln.Close) }()
		synctest.Wait()
		cancel()
		assert.NilError(t, <-done)
	})
}

type pausedRulesDriver struct {
	*fake.Driver
	entered chan struct{}
	release chan struct{}
	queried chan int
	paused  bool
}

func (d *pausedRulesDriver) List(context.Context) ([]wm.Rule, error) { return nil, nil }
func (d *pausedRulesDriver) Remove(context.Context, string) error    { return nil }
func (d *pausedRulesDriver) Add(context.Context, wm.Rule) error {
	if !d.paused {
		d.paused = true
		close(d.entered)
		<-d.release
	}
	return nil
}
func (d *pausedRulesDriver) Apply(context.Context) error {
	return errors.New("unexpected bulk rule apply")
}
func (d *pausedRulesDriver) QueryWindow(ctx context.Context, id int) (wm.Window, bool, error) {
	d.queried <- id
	return d.Driver.QueryWindow(ctx, id)
}

func TestRunArrivalWaitsInsideWakeRules(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		profile := testProfile(t)
		profile.Desk = nil
		driver := &pausedRulesDriver{Driver: fake.New(fake.Seed{
			Displays: []fake.SeedDisplay{{Idx: 1, Frame: &wm.Frame{W: 5120}}, {Idx: 3, Frame: &wm.Frame{W: 1728}}},
			Spaces:   []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 3}},
		}), entered: make(chan struct{}), release: make(chan struct{}), queried: make(chan int, 1)}
		env := daemonEnv(t)
		env.Topology = &commands.TopologyState{}
		ln := newPipeListener()
		done := make(chan error, 1)
		go func() {
			done <- run(ctx, Options{
				Topology: env.Topology,
				Display:  DisplayWork(profile, driver, env), Flex: idle,
				Wake: WakeRoutine(profile, driver, env, "/tmp/tess-placement-test.sock"),
				WindowCreated: func(ctx context.Context, id int, ready func() bool) error {
					arrivalEnv := env
					arrivalEnv.ArrivalReady = ready
					return commands.HandleWindowCreated(ctx, profile, driver, arrivalEnv, id)
				},
			}, ln, ln.Close)
		}()
		<-driver.entered
		ln.send(t, "window_created 90")
		synctest.Wait()
		select {
		case <-driver.queried:
			t.Fatal("arrival queried while wake was inside Rules")
		default:
		}
		close(driver.release)
		assert.Equal(t, <-driver.queried, 90)
		cancel()
		assert.NilError(t, <-done)
	})
}

type ruleAwareSpawnDriver struct {
	*fake.Driver
	deskRule        bool
	spawnedFloating bool
}

func (d *ruleAwareSpawnDriver) List(context.Context) ([]wm.Rule, error) { return nil, nil }
func (d *ruleAwareSpawnDriver) Remove(context.Context, string) error    { return nil }
func (d *ruleAwareSpawnDriver) Add(_ context.Context, rule wm.Rule) error {
	if rule.Label == "auto:desk-manage-off" {
		d.deskRule = true
	}
	return nil
}
func (d *ruleAwareSpawnDriver) Apply(context.Context) error {
	return errors.New("unexpected bulk rule apply")
}
func (d *ruleAwareSpawnDriver) SpawnWindow(ctx context.Context, argv []string) error {
	if err := d.Driver.SpawnWindow(ctx, argv); err != nil {
		return err
	}
	windows, err := d.Driver.QueryWindows(ctx)
	if err != nil {
		return err
	}
	if d.deskRule && len(windows) > 0 {
		d.spawnedFloating = true
		return d.Driver.ToggleFloat(ctx, windows[len(windows)-1].ID)
	}
	return nil
}

func TestWakeRoutineReclaimsRuleFloatedSpawns(t *testing.T) {
	for _, topology := range []spawnTopology{
		{name: "full desk", displays: []fake.SeedDisplay{{Idx: 1, Frame: &wm.Frame{W: 5120}}, {Idx: 2, Frame: &wm.Frame{W: 3440}}, {Idx: 3, Frame: &wm.Frame{W: 1728}}}},
		{name: "AW only", displays: []fake.SeedDisplay{{Idx: 2, Frame: &wm.Frame{W: 3440}}}},
		{name: "G9 only", displays: []fake.SeedDisplay{{Idx: 1, Frame: &wm.Frame{W: 5120}}}},
	} {
		t.Run(topology.name, func(t *testing.T) { testRuleFloatedSpawns(t, topology) })
	}
}

type spawnTopology struct {
	name     string
	displays []fake.SeedDisplay
}

func testRuleFloatedSpawns(t *testing.T, topology spawnTopology) {
	t.Helper()
	for _, test := range []struct {
		name    string
		app     string
		display int
	}{
		{name: "vscode", app: "Code", display: 1},
		{name: "linear", app: "Linear", display: 2},
		{name: "ghostty-wave", app: "Ghostty", display: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile := testProfile(t)
			spaces := make([]fake.SeedSpace, 0, len(topology.displays))
			for _, display := range topology.displays {
				spaces = append(spaces, fake.SeedSpace{DisplayIdx: display.Idx})
			}
			// Only configure a spawn when this topology has the corresponding slot.
			names := []string{}
			for _, layout := range engine.ResolveDesk(profile, seedDisplays(topology.displays)) {
				if _, present := engine.ResolveDisplay(profile, layout.Display, seedDisplays(topology.displays)); !present {
					continue
				}
				for _, track := range layout.Tracks {
					names = append(names, track...)
				}
			}
			if !slices.Contains(names, test.name) {
				t.Skip("configured app has no slot on this topology")
			}
			spec := profile.Windows[test.name]
			spec.Spawn = []string{"open", "-a", test.app}
			profile.Windows[test.name] = spec
			driver := &ruleAwareSpawnDriver{Driver: fake.New(fake.Seed{Displays: topology.displays, Spaces: spaces, SpawnApp: test.app})}
			env := daemonEnv(t)
			env.Nudge = func(context.Context, string, ...string) error { return nil }
			if err := WakeRoutine(profile, driver, env, "/tmp/tess-rule-spawn.sock")(t.Context()); err != nil {
				t.Fatal(err)
			}
			wantDisplay := test.display
			if len(topology.displays) == 1 {
				wantDisplay = topology.displays[0].Idx
			}
			windows, err := driver.QueryWindows(t.Context())
			if err != nil || len(windows) != 1 || !driver.spawnedFloating || windows[0].Floating || windows[0].DisplayIdx != wantDisplay {
				t.Fatalf("spawn not reclaimed to configured desk slot: %+v, floated %t, %v", windows, driver.spawnedFloating, err)
			}
		})
	}
}

func seedDisplays(displays []fake.SeedDisplay) []wm.Display {
	result := make([]wm.Display, 0, len(displays))
	for _, display := range displays {
		result = append(result, wm.Display{Idx: display.Idx, Frame: *display.Frame})
	}
	return result
}

type wakeRecoveryDriver struct {
	*fake.Driver
	stacked         []int
	failPlacementID int
	failFinalQuery  bool
	recovered       bool
}

func (d *wakeRecoveryDriver) StackOnto(ctx context.Context, target, id int) error {
	d.stacked = append(d.stacked, id)
	return d.Driver.StackOnto(ctx, target, id)
}

func (d *wakeRecoveryDriver) ToggleFloat(ctx context.Context, id int) error {
	if id == d.failPlacementID {
		return errors.New("startup placement rejected")
	}
	if err := d.Driver.ToggleFloat(ctx, id); err != nil {
		return err
	}
	d.recovered = true
	return nil
}

func (d *wakeRecoveryDriver) QueryWindows(ctx context.Context) ([]wm.Window, error) {
	if d.recovered && d.failFinalQuery {
		d.failFinalQuery = false
		return nil, errors.New("startup observation failed")
	}
	return d.Driver.QueryWindows(ctx)
}

func TestWakeRoutineObservesRecoveredLaptopArrival(t *testing.T) {
	for _, stage := range []string{"success", "later placement fails", "final observation fails"} {
		t.Run(stage, func(t *testing.T) { testWakeRecoveryObservation(t, stage) })
	}
}

func testWakeRecoveryObservation(t *testing.T, stage string) {
	t.Helper()
	profile := testProfile(t)
	env := daemonEnv(t)
	env.Topology = &commands.TopologyState{}
	env.Nudge = func(context.Context, string, ...string) error { return nil }
	driver := &wakeRecoveryDriver{Driver: fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1, Frame: &wm.Frame{W: 1728}}, {Idx: 2, Frame: &wm.Frame{W: 3440}}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 1, Label: "laptop"}, {DisplayIdx: 2}},
		Windows:  []fake.SeedWindow{{ID: 90, App: "Extra", SpaceIndex: 2, Floating: true, Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true}},
	})}
	if stage == "later placement fails" {
		if err := driver.SpawnWindow(t.Context(), []string{"open", "-a", "Extra"}); err != nil {
			t.Fatal(err)
		}
		if err := driver.Driver.ToggleFloat(t.Context(), 91); err != nil {
			t.Fatal(err)
		}
		if err := driver.MoveWindowToSpace(t.Context(), 91, "2"); err != nil {
			t.Fatal(err)
		}
		driver.failPlacementID = 91
	}
	driver.failFinalQuery = stage == "final observation fails"
	wakeErr := WakeRoutine(profile, driver, env, "/tmp/tess-recovery.sock")(t.Context())
	if (wakeErr != nil) != (stage != "success") {
		t.Fatalf("wake error = %v for stage %q", wakeErr, stage)
	}
	window, found, err := driver.QueryWindow(t.Context(), 90)
	if err != nil || !found || window.Floating || window.DisplayIdx != 1 {
		t.Fatalf("startup arrival not placed on laptop: %+v, %v", window, err)
	}
	driver.Driver = fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 2, Frame: &wm.Frame{W: 3440}}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 2}},
		Windows: []fake.SeedWindow{
			{ID: 6, App: "Linear", SpaceIndex: 1, Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true},
			{ID: 90, App: "Extra", SpaceIndex: 1, Root: true, Role: "AXWindow", Subrole: "AXStandardWindow", CanMove: true, CanResize: true},
		},
	})
	if _, err := DisplayWork(profile, driver, env)(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(driver.stacked, []int{90}) {
		t.Fatalf("startup arrival not recovered after lid close: %v", driver.stacked)
	}
}

func TestWakeRoutineBusyScope(t *testing.T) {
	driver := fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 1}},
	})
	env := daemonEnv(t)
	states := []string{}
	env.Nudge = func(_ context.Context, event string, variables ...string) error {
		if event == "tess_busy" {
			states = append(states, variables...)
		}
		return nil
	}
	if err := WakeRoutine(testProfile(t), driver, env, "/tmp/tess-busy-test.sock")(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(states, []string{"STATE=on", "STATE=off"}) {
		t.Fatalf("nested wake busy states = %v", states)
	}
}
