// Package wm defines the window-manager contract shared by planners and drivers.
package wm

import (
	"context"
	"time"
)

type SpaceID string

type Event string

const (
	EventDisplayAdded          Event = "display_added"
	EventDisplayRemoved        Event = "display_removed"
	EventDisplayMoved          Event = "display_moved"
	EventApplicationLaunched   Event = "application_launched"
	EventApplicationTerminated Event = "application_terminated"
	EventWindowCreated         Event = "window_created"
	EventWindowDestroyed       Event = "window_destroyed"
	EventSpaceChanged          Event = "space_changed"
	EventDisplayChanged        Event = "display_changed"
	EventDockDidRestart        Event = "dock_did_restart"
)

type Layout string

const (
	LayoutBSP   Layout = "bsp"
	LayoutStack Layout = "stack"
	LayoutFloat Layout = "float"
)

type SplitType string

const (
	SplitVertical   SplitType = "vertical"
	SplitHorizontal SplitType = "horizontal"
	SplitNone       SplitType = "none"
)

type Direction string

const (
	DirectionWest  Direction = "west"
	DirectionSouth Direction = "south"
	DirectionNorth Direction = "north"
	DirectionEast  Direction = "east"
)

type StackSelector string

const (
	StackNext  StackSelector = "stack.next"
	StackPrev  StackSelector = "stack.prev"
	StackFirst StackSelector = "stack.first"
	StackLast  StackSelector = "stack.last"
)

type DisplayPosition string

const (
	DisplayNext  DisplayPosition = "next"
	DisplayPrev  DisplayPosition = "prev"
	DisplayFirst DisplayPosition = "first"
	DisplayLast  DisplayPosition = "last"
)

type Frame struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type Window struct {
	ID         int
	App        string
	Title      string
	DisplayIdx int
	SpaceID    SpaceID
	Minimized  bool
	Floating   bool
	Sticky     bool
	Visible    bool
	SplitType  SplitType
	Frame      Frame
}

type Space struct {
	ID         SpaceID
	Label      string
	DisplayIdx int
	WindowIDs  []int
	Layout     Layout
}

type Display struct {
	Idx      int
	Frame    Frame
	SpaceIDs []SpaceID
}

type LayoutKind string

const (
	LayoutColumns LayoutKind = "columns"
	LayoutRows    LayoutKind = "rows"
	TargetStack   LayoutKind = "stack"
)

type SpaceLayoutTarget struct {
	Kind    LayoutKind
	Tracks  [][]int
	Weights []float64
}

type DisplaySelector struct {
	Index    int
	Position DisplayPosition
}

type Rule struct {
	Label   string
	App     string
	Subrole string
	Display *int
	Space   *int
	Manage  *bool
}

type ResizeEdge string

const (
	EdgeLeft   ResizeEdge = "left"
	EdgeRight  ResizeEdge = "right"
	EdgeTop    ResizeEdge = "top"
	EdgeBottom ResizeEdge = "bottom"
)

type FocusSelector string

const (
	FocusWest       FocusSelector = "west"
	FocusSouth      FocusSelector = "south"
	FocusNorth      FocusSelector = "north"
	FocusEast       FocusSelector = "east"
	FocusStackNext  FocusSelector = "stack.next"
	FocusStackPrev  FocusSelector = "stack.prev"
	FocusStackFirst FocusSelector = "stack.first"
	FocusStackLast  FocusSelector = "stack.last"
)

type QueryOps interface {
	QueryWindows(ctx context.Context) ([]Window, error)
	QueryWindowsOnSpace(ctx context.Context, id SpaceID) ([]Window, error)
	QuerySpaces(ctx context.Context) ([]Space, error)
	QueryDisplays(ctx context.Context) ([]Display, error)
	QueryFocusedSpace(ctx context.Context) (Space, bool, error)
	QueryFocusedWindow(ctx context.Context) (Window, bool, error)
}

type SpaceOps interface {
	CreateSpace(ctx context.Context, displayIdx int) (SpaceID, bool, error)
	DestroySpace(ctx context.Context, id SpaceID) (bool, error)
	LabelSpace(ctx context.Context, id SpaceID, label string) error
	SetSpaceLayout(ctx context.Context, id SpaceID, layout Layout) error
	MoveSpaceToIndex(ctx context.Context, id SpaceID, toIdx int) error
	BalanceSpace(ctx context.Context, id *SpaceID) error
}

type LayoutOps interface {
	RealizeSpaceLayout(ctx context.Context, id SpaceID, target SpaceLayoutTarget) error
}

type WindowOps interface {
	SpawnWindow(ctx context.Context, argv []string) error
	MoveWindowToSpace(ctx context.Context, winID int, id SpaceID) error
	MoveWindowToDisplay(ctx context.Context, winID int, sel DisplaySelector) (bool, error)
	SetSplitRatio(ctx context.Context, winID int, absRatio float64) error
	ToggleSplit(ctx context.Context, winID int) error
	ToggleFloat(ctx context.Context, winID int) error
	ArmInsert(ctx context.Context, winID int, dir Direction) error
	StackOnto(ctx context.Context, targetWinID, winID int) error
	ResizeWindow(ctx context.Context, edge ResizeEdge, dx, dy int) (bool, error)
}

type WindowNavigationOps interface {
	SwapWindows(ctx context.Context, sel Direction) (bool, error)
	WarpWindow(ctx context.Context, sel Direction) (bool, error)
}

type FocusOps interface {
	FocusWindow(ctx context.Context, winID int) (bool, error)
	FocusWindowDir(ctx context.Context, sel FocusSelector) (bool, error)
	FocusDisplay(ctx context.Context, sel DisplaySelector) (bool, error)
}

type SettleOps interface {
	SettleUnit() time.Duration
}

// Driver is the combined window-manager contract used by engine consumers.
type Driver interface {
	QueryOps
	SpaceOps
	LayoutOps
	WindowOps
	WindowNavigationOps
	FocusOps
	SettleOps
}

type RuleOps interface {
	List(ctx context.Context) ([]Rule, error)
	Remove(ctx context.Context, label string) error
	Add(ctx context.Context, rule Rule) error
	Apply(ctx context.Context) error
}

type EventSource interface {
	AddSignal(ctx context.Context, ev Event, label, action string) error
}

type World struct {
	Windows  []Window
	Spaces   []Space
	Displays []Display
}
