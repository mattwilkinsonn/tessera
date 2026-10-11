// Package commands composes the engine, executor, effects, and window-manager driver.
package commands

import (
	"io"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

// Env contains the injectable filesystem and output effects used by commands.
type Env struct {
	Paths  effects.Paths
	Nudge  effects.Nudger
	Stderr io.Writer
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
