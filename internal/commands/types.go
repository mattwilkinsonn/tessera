// Package commands composes the engine, executor, effects, and window-manager driver.
package commands

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

// Env contains the injectable filesystem, output, and daemon topology state used by commands.
type Env struct {
	Paths        effects.Paths
	Nudge        effects.Nudger
	Stderr       io.Writer
	Topology     *TopologyState
	ArrivalReady func() bool
	// ReclaimedDeskWindows contains only claims completed by this wake's desk plan.
	ReclaimedDeskWindows map[int]struct{}
}

// ErrDisplayPending asks the daemon to retain an arrival until display work settles.
var ErrDisplayPending = errors.New("display work pending")

// TopologyState serializes arrivals with full wake and display cascades.
type TopologyState struct {
	mu               sync.Mutex
	displayMu        sync.Mutex
	previousDisplays int
	previousKnown    bool
	laptopWindows    map[int]struct{}
}

// SerializeCascade runs a full cascade in the same domain as arrivals.
func (state *TopologyState) SerializeCascade(ctx context.Context, run func(context.Context) error) error {
	if state == nil {
		return run(ctx)
	}
	if err := state.lock(ctx); err != nil {
		return err
	}
	defer state.mu.Unlock()
	return run(ctx)
}

// SerializeArrival runs fn only if the caller's readiness check still passes under the lock.
func (state *TopologyState) SerializeArrival(ctx context.Context, ready func() bool, fn func(context.Context) error) (bool, error) {
	if state == nil {
		if !ready() {
			return false, nil
		}
		return true, fn(ctx)
	}
	if err := state.lock(ctx); err != nil {
		return false, err
	}
	defer state.mu.Unlock()
	if !ready() {
		return false, nil
	}
	return true, fn(ctx)
}

// Displays returns the last topology whose rules and handoff completed.
func (state *TopologyState) Displays() (count int, known bool) {
	if state == nil {
		return 0, false
	}
	state.displayMu.Lock()
	defer state.displayMu.Unlock()
	return state.previousDisplays, state.previousKnown
}

// RecordDisplays reports whether a topology was previously known and updates it.
func (state *TopologyState) RecordDisplays(count int) (previous int, known bool) {
	if state == nil {
		return 0, false
	}
	state.displayMu.Lock()
	defer state.displayMu.Unlock()
	previous, known = state.previousDisplays, state.previousKnown
	state.previousDisplays = count
	state.previousKnown = true
	return previous, known
}

// RecordLaptopHomeWindows replaces the IDs last seen in the laptop home space.
func (state *TopologyState) RecordLaptopHomeWindows(ids []int) {
	if state == nil {
		return
	}
	windows := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		windows[id] = struct{}{}
	}
	state.displayMu.Lock()
	state.laptopWindows = windows
	state.displayMu.Unlock()
}

// RecordLaptopHomeWindow adds a confirmed placement without replacing prior observations.
func (state *TopologyState) RecordLaptopHomeWindow(id int) {
	if state == nil {
		return
	}
	state.displayMu.Lock()
	defer state.displayMu.Unlock()
	if state.laptopWindows == nil {
		state.laptopWindows = make(map[int]struct{})
	}
	state.laptopWindows[id] = struct{}{}
}

// LaptopHomeWindows returns the latest snapshot of laptop home-space IDs.
func (state *TopologyState) LaptopHomeWindows() map[int]struct{} {
	windows := make(map[int]struct{})
	if state == nil {
		return windows
	}
	state.displayMu.Lock()
	defer state.displayMu.Unlock()
	for id := range state.laptopWindows {
		windows[id] = struct{}{}
	}
	return windows
}

// ConsumeLaptopHomeWindows removes only resolved lid-close candidates.
func (state *TopologyState) ConsumeLaptopHomeWindows(ids map[int]struct{}) {
	if state == nil {
		return
	}
	state.displayMu.Lock()
	for id := range ids {
		delete(state.laptopWindows, id)
	}
	state.displayMu.Unlock()
}

func (state *TopologyState) lock(ctx context.Context) error {
	for !state.mu.TryLock() {
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

// Command is the sealed set of supported commands.
//
//sumtype:decl Command
type Command interface {
	command()
}

type ApplyCommand struct{}
type LaptopCommand struct{}
type RulesCommand struct{}
type DisplaySetupCommand struct{}
type FocusSlotCommand struct{ Number int }
type SnapCommand struct{ Mode engine.SnapMode }
type StackCycleCommand struct{ Direction CycleDir }
type ResizeCommand struct{ Direction ResizeDir }
type MoveDisplayCommand struct{ Display config.DisplayName }
type CycleDisplayCommand struct{ Direction CycleDir }
type ResetSplitsCommand struct{}
type ColumnsCommand struct{}
type FocusCommand struct{ Direction wm.Direction }
type SwapCommand struct{ Direction wm.Direction }
type WarpCommand struct{ Direction wm.Direction }
type InsertCommand struct{ Direction InsertDir }
type ToggleFloatCommand struct{}
type BalanceCommand struct{}
type SpaceCommand struct{ Layout SpaceLayoutMode }
type WakeCommand struct{}
type DaemonCommand struct{ NoBootstrap bool }

func (ApplyCommand) command()        {}
func (LaptopCommand) command()       {}
func (RulesCommand) command()        {}
func (DisplaySetupCommand) command() {}
func (FocusSlotCommand) command()    {}
func (SnapCommand) command()         {}
func (StackCycleCommand) command()   {}
func (ResizeCommand) command()       {}
func (MoveDisplayCommand) command()  {}
func (CycleDisplayCommand) command() {}
func (ResetSplitsCommand) command()  {}
func (ColumnsCommand) command()      {}
func (FocusCommand) command()        {}
func (SwapCommand) command()         {}
func (WarpCommand) command()         {}
func (InsertCommand) command()       {}
func (ToggleFloatCommand) command()  {}
func (BalanceCommand) command()      {}
func (SpaceCommand) command()        {}
func (WakeCommand) command()         {}
func (DaemonCommand) command()       {}

// CycleDir selects the next or previous focus target.
type CycleDir string

const (
	CycleNext CycleDir = "next"
	CyclePrev CycleDir = "prev"
)

// ResizeDir selects whether the focused column grows or shrinks.
type ResizeDir string

const (
	ResizeGrow   ResizeDir = "grow"
	ResizeShrink ResizeDir = "shrink"
)

// InsertDir selects where the next window is inserted.
type InsertDir string

const (
	InsertEast  InsertDir = "east"
	InsertWest  InsertDir = "west"
	InsertNorth InsertDir = "north"
	InsertSouth InsertDir = "south"
	InsertStack InsertDir = "stack"
)

// SpaceLayoutMode selects the focused space layout.
type SpaceLayoutMode string

const (
	SpaceBSP   SpaceLayoutMode = "bsp"
	SpaceStack SpaceLayoutMode = "stack"
)

// LaptopResult reports the outcome of laptop convergence and display cascades.
type LaptopResult string

const (
	LaptopRan       LaptopResult = "ran"
	LaptopContended LaptopResult = "contended"
	LaptopSkipped   LaptopResult = "skipped"
	LaptopApplied   LaptopResult = "applied"
)
