// Package engine contains pure window-world planners.
package engine

import (
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

// Op is one executable plan operation.
//
//sumtype:decl Op
type Op interface {
	op()
}

// ConvergeAction is one corrective operation emitted by the laptop converger.
//
//sumtype:decl ConvergeAction
type ConvergeAction interface {
	Op
	convergeAction()
}

type DestroySpaceOp struct {
	Space wm.SpaceID
}

func (DestroySpaceOp) op()             {}
func (DestroySpaceOp) convergeAction() {}

type RealizeLayoutOp struct {
	Space  wm.SpaceID
	Target wm.SpaceLayoutTarget
}

func (RealizeLayoutOp) op() {}

type BalanceSpaceOp struct {
	Space wm.SpaceID
}

func (BalanceSpaceOp) op() {}

type RelabelHomeAction struct {
	HomeSpace wm.SpaceID
	Label     string
}

func (RelabelHomeAction) op()             {}
func (RelabelHomeAction) convergeAction() {}

type CreateSpaceAction struct {
	DisplayIdx int
	Label      string
}

func (CreateSpaceAction) op()             {}
func (CreateSpaceAction) convergeAction() {}

type MoveWindowAction struct {
	WindowID int
	ToSpace  wm.SpaceID
}

func (MoveWindowAction) op()             {}
func (MoveWindowAction) convergeAction() {}

type RehomeAndDestroyAction struct {
	StaleSpace wm.SpaceID
	HomeSpace  wm.SpaceID
}

func (RehomeAndDestroyAction) op()             {}
func (RehomeAndDestroyAction) convergeAction() {}

type MoveSpaceAction struct {
	Space   wm.SpaceID
	ToIndex int
}

func (MoveSpaceAction) op()             {}
func (MoveSpaceAction) convergeAction() {}

type SetLayoutAction struct {
	Space  wm.SpaceID
	Layout wm.Layout
}

func (SetLayoutAction) op()             {}
func (SetLayoutAction) convergeAction() {}

type ConvergePhase string

// ConvergePhase identifies the next laptop convergence phase.

const (
	PhasePre    ConvergePhase = "pre"
	PhaseA      ConvergePhase = "A"
	PhaseB      ConvergePhase = "B"
	PhaseC      ConvergePhase = "C"
	PhaseD      ConvergePhase = "D"
	PhaseLayout ConvergePhase = "layout"
	PhaseDone   ConvergePhase = "done"
)

type ConvergeState struct {
	Phase              ConvergePhase
	Cursor             int
	Placed             int
	ClaimedIDs         map[int]struct{}
	TargetLabels       map[string]struct{}
	DesiredOrder       []string
	CreateFailed       bool
	PendingCreateLabel string
	HasPendingCreate   bool
	HomeSpace          wm.SpaceID
	LaptopIdx          int
	PersistedFlexOrder []string
	ToPersist          []string
	ReapStrays         bool
	DestroyTried       map[wm.SpaceID]struct{}
}

type ClaimSet struct {
	profile *config.Profile
	claimed map[int]struct{}
}
