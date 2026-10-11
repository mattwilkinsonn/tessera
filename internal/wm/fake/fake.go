// Package fake provides an in-memory implementation of wm.Driver.
package fake

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/wm"
)

type SeedDisplay struct {
	Idx   int
	Frame *wm.Frame
}

type SeedSpace struct {
	Label      string
	DisplayIdx int
	Layout     wm.Layout
}

type SeedWindow struct {
	ID         int
	App        string
	Title      string
	SpaceIndex int
	Minimized  bool
	Floating   bool
	Sticky     bool
	Visible    *bool
	SplitType  wm.SplitType
	Frame      *wm.Frame
}

type Seed struct {
	Displays      []SeedDisplay
	Spaces        []SeedSpace
	Windows       []SeedWindow
	SpawnApp      string
	SpawnFails    int
	SpawnNoWindow bool
	Settle        time.Duration
}

type fakeSpace struct {
	id         wm.SpaceID
	label      string
	displayIdx int
	layout     wm.Layout
}

type fakeDisplay struct {
	idx   int
	frame wm.Frame
}

type Driver struct {
	spaces        []fakeSpace
	windows       []wm.Window
	displays      []fakeDisplay
	nextSpaceID   int
	focusedWindow *int
	spawnFails    int
	spawnNoWindow bool
	spawnApp      string
	settleUnit    time.Duration
	SpawnCalls    [][]string
}

const (
	defaultDisplayWidth  = 1728
	defaultDisplayHeight = 1117
	defaultWindowSize    = 100
)

// New creates a fake driver initialized from seed.
func New(seed Seed) *Driver {
	d := &Driver{
		spawnFails:    seed.SpawnFails,
		spawnNoWindow: seed.SpawnNoWindow,
		spawnApp:      seed.SpawnApp,
		settleUnit:    seed.Settle,
	}
	if d.spawnApp == "" {
		d.spawnApp = "Ghostty"
	}
	if len(seed.Displays) == 0 {
		seed.Displays = []SeedDisplay{{Idx: 1}}
	}
	for _, display := range seed.Displays {
		frame := wm.Frame{W: defaultDisplayWidth, H: defaultDisplayHeight}
		if display.Frame != nil {
			frame = *display.Frame
		}
		d.displays = append(d.displays, fakeDisplay{idx: display.Idx, frame: frame})
	}
	for _, space := range seed.Spaces {
		d.nextSpaceID++
		d.spaces = append(d.spaces, fakeSpace{
			id:         wm.SpaceID(strconv.Itoa(d.nextSpaceID)),
			label:      space.Label,
			displayIdx: space.DisplayIdx,
			layout:     defaultLayout(space.Layout),
		})
	}
	for _, window := range seed.Windows {
		index := window.SpaceIndex - 1
		if index < 0 || index >= len(d.spaces) {
			continue
		}
		space := d.spaces[index]
		visible := true
		if window.Visible != nil {
			visible = *window.Visible
		}
		frame := wm.Frame{W: defaultWindowSize, H: defaultWindowSize}
		if window.Frame != nil {
			frame = *window.Frame
		}
		split := window.SplitType
		if split == "" {
			split = wm.SplitNone
		}
		d.windows = append(d.windows, wm.Window{
			ID:         window.ID,
			App:        window.App,
			Title:      window.Title,
			DisplayIdx: space.displayIdx,
			SpaceID:    space.id,
			Minimized:  window.Minimized,
			Floating:   window.Floating,
			Sticky:     window.Sticky,
			Visible:    visible,
			SplitType:  split,
			Frame:      frame,
		})
	}
	return d
}

func defaultLayout(layout wm.Layout) wm.Layout {
	if layout == "" {
		return wm.LayoutBSP
	}
	return layout
}

// Restart models yabai restarting and losing all space labels.
func (d *Driver) Restart() {
	for i := range d.spaces {
		d.spaces[i].label = ""
	}
}

func (d *Driver) SpawnWindow(_ context.Context, argv []string) error {
	d.SpawnCalls = append(d.SpawnCalls, slices.Clone(argv))
	if d.spawnFails > 0 {
		d.spawnFails--
		return errors.New("fake spawn failure")
	}
	if d.spawnNoWindow {
		return nil
	}
	var space *fakeSpace
	if d.focusedWindow != nil {
		if focused := d.windowByID(*d.focusedWindow); focused != nil {
			space = d.spaceByID(focused.SpaceID)
		}
	}
	if space == nil && len(d.spaces) > 0 {
		space = &d.spaces[0]
	}
	if space == nil {
		return nil
	}
	id := 0
	for _, window := range d.windows {
		if window.ID > id {
			id = window.ID
		}
	}
	d.windows = append(d.windows, wm.Window{
		ID: id + 1, App: d.spawnApp, DisplayIdx: space.displayIdx,
		SpaceID: space.id, Visible: true, SplitType: wm.SplitNone,
		Frame: wm.Frame{W: defaultWindowSize, H: defaultWindowSize},
	})
	return nil
}

func (d *Driver) QueryWindows(ctx context.Context) ([]wm.Window, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return slices.Clone(d.windows), nil
}

func (d *Driver) QueryWindowsOnSpace(ctx context.Context, id wm.SpaceID) ([]wm.Window, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	windows := make([]wm.Window, 0)
	for _, window := range d.windows {
		if window.SpaceID == id {
			windows = append(windows, window)
		}
	}
	return windows, nil
}

func (d *Driver) QuerySpaces(ctx context.Context) ([]wm.Space, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	spaces := make([]wm.Space, len(d.spaces))
	for i := range d.spaces {
		spaces[i] = d.toSpace(&d.spaces[i])
	}
	return spaces, nil
}

func (d *Driver) QueryDisplays(ctx context.Context) ([]wm.Display, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	displays := make([]wm.Display, len(d.displays))
	for i, display := range d.displays {
		spaceIDs := make([]wm.SpaceID, 0)
		for _, space := range d.spaces {
			if space.displayIdx == display.idx {
				spaceIDs = append(spaceIDs, space.id)
			}
		}
		displays[i] = wm.Display{Idx: display.idx, Frame: display.frame, SpaceIDs: spaceIDs}
	}
	return displays, nil
}

func (d *Driver) QueryFocusedSpace(ctx context.Context) (wm.Space, bool, error) {
	if err := ctx.Err(); err != nil {
		return wm.Space{}, false, err
	}
	if d.focusedWindow == nil {
		return wm.Space{}, false, nil
	}
	window := d.windowByID(*d.focusedWindow)
	if window == nil {
		return wm.Space{}, false, nil
	}
	space := d.spaceByID(window.SpaceID)
	if space == nil {
		return wm.Space{}, false, nil
	}
	return d.toSpace(space), true, nil
}

func (d *Driver) QueryFocusedWindow(ctx context.Context) (wm.Window, bool, error) {
	if err := ctx.Err(); err != nil {
		return wm.Window{}, false, err
	}
	if d.focusedWindow == nil {
		return wm.Window{}, false, nil
	}
	window := d.windowByID(*d.focusedWindow)
	if window == nil {
		return wm.Window{}, false, nil
	}
	return *window, true, nil
}

func (d *Driver) CreateSpace(ctx context.Context, displayIdx int) (wm.SpaceID, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	d.nextSpaceID++
	space := fakeSpace{id: wm.SpaceID(strconv.Itoa(d.nextSpaceID)), displayIdx: displayIdx, layout: wm.LayoutBSP}
	insertAt := len(d.spaces)
	for index := range slices.Backward(d.spaces) {
		if d.spaces[index].displayIdx == displayIdx {
			insertAt = index + 1
			break
		}
	}
	d.spaces = slices.Insert(d.spaces, insertAt, space)
	return space.id, true, nil
}

func (d *Driver) DestroySpace(ctx context.Context, id wm.SpaceID) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	space := d.spaceByID(id)
	if space == nil {
		return false, nil
	}
	onDisplay := 0
	var target *fakeSpace
	for i := range d.spaces {
		if d.spaces[i].displayIdx == space.displayIdx {
			onDisplay++
			if &d.spaces[i] != space && target == nil {
				target = &d.spaces[i]
			}
		}
	}
	if onDisplay <= 1 || target == nil {
		return false, nil
	}
	for i := range d.windows {
		if d.windows[i].SpaceID == id {
			d.windows[i].SpaceID = target.id
			d.windows[i].DisplayIdx = target.displayIdx
		}
	}
	index := slices.IndexFunc(d.spaces, func(candidate fakeSpace) bool { return candidate.id == id })
	d.spaces = slices.Delete(d.spaces, index, index+1)
	return true, nil
}

func (d *Driver) LabelSpace(ctx context.Context, id wm.SpaceID, label string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if space := d.spaceByID(id); space != nil {
		space.label = label
	}
	return nil
}

func (d *Driver) SetSpaceLayout(ctx context.Context, id wm.SpaceID, layout wm.Layout) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if space := d.spaceByID(id); space != nil {
		space.layout = layout
	}
	return nil
}

func (d *Driver) MoveSpaceToIndex(ctx context.Context, id wm.SpaceID, toIdx int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	index := slices.IndexFunc(d.spaces, func(space fakeSpace) bool { return space.id == id })
	if index < 0 {
		return nil
	}
	space := d.spaces[index]
	d.spaces = slices.Delete(d.spaces, index, index+1)
	position := max(1, min(toIdx, len(d.spaces)+1))
	d.spaces = slices.Insert(d.spaces, position-1, space)
	return nil
}

func (d *Driver) BalanceSpace(ctx context.Context, _ *wm.SpaceID) error { return ctx.Err() }

func (d *Driver) RealizeSpaceLayout(ctx context.Context, id wm.SpaceID, target wm.SpaceLayoutTarget) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	space := d.spaceByID(id)
	if space == nil {
		return nil
	}
	for _, track := range target.Tracks {
		for _, windowID := range track {
			if window := d.windowByID(windowID); window != nil {
				window.SpaceID = space.id
				window.DisplayIdx = space.displayIdx
				window.Floating = false
			}
		}
	}
	if target.Kind == wm.TargetStack {
		space.layout = wm.LayoutStack
		return nil
	}
	space.layout = wm.LayoutBSP
	var display *fakeDisplay
	for i := range d.displays {
		if d.displays[i].idx == space.displayIdx {
			display = &d.displays[i]
			break
		}
	}
	if display == nil {
		return nil
	}
	tracks := make([][]int, 0, len(target.Tracks))
	for _, track := range target.Tracks {
		if len(track) > 0 {
			tracks = append(tracks, track)
		}
	}
	weights := target.Weights
	if weights == nil {
		weights = make([]float64, len(tracks))
		for i := range weights {
			weights[i] = 1
		}
	}
	if len(weights) != len(tracks) {
		return fmt.Errorf("realize space layout: %d weights for %d non-empty tracks", len(weights), len(tracks))
	}
	total := 0.0
	for _, weight := range weights {
		total += weight
	}
	before := 0.0
	for i, track := range tracks {
		weight := weights[i]
		frame := display.frame
		if target.Kind == wm.LayoutColumns {
			frame.X += frame.W * before / total
			frame.W = display.frame.W * weight / total
		} else {
			frame.Y += frame.H * before / total
			frame.H = display.frame.H * weight / total
		}
		before += weight
		for _, windowID := range track {
			if window := d.windowByID(windowID); window != nil {
				window.Frame = frame
			}
		}
	}
	return nil
}

func (d *Driver) MoveWindowToSpace(ctx context.Context, windowID int, id wm.SpaceID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	window := d.windowByID(windowID)
	space := d.spaceByID(id)
	if window != nil && space != nil {
		window.SpaceID = space.id
		window.DisplayIdx = space.displayIdx
	}
	return nil
}

func (d *Driver) MoveWindowToDisplay(ctx context.Context, windowID int, selector wm.DisplaySelector) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	window := d.windowByID(windowID)
	if window == nil {
		return false, nil
	}
	indices := make([]int, len(d.displays))
	for i, display := range d.displays {
		indices[i] = display.idx
	}
	slices.Sort(indices)
	var target int
	if selector.Position == "" {
		if !slices.Contains(indices, selector.Index) {
			return false, nil
		}
		target = selector.Index
	} else {
		if len(indices) == 0 {
			return false, nil
		}
		position := slices.Index(indices, window.DisplayIdx)
		switch selector.Position {
		case wm.DisplayFirst:
			target = indices[0]
		case wm.DisplayLast:
			target = indices[len(indices)-1]
		case wm.DisplayNext:
			target = indices[(position+1)%len(indices)]
		case wm.DisplayPrev:
			target = indices[(position-1+len(indices))%len(indices)]
		}
	}
	for _, space := range d.spaces {
		if space.displayIdx == target {
			window.DisplayIdx = target
			window.SpaceID = space.id
			return true, nil
		}
	}
	return false, nil
}

func (d *Driver) SetSplitRatio(ctx context.Context, _ int, _ float64) error { return ctx.Err() }

func (d *Driver) ToggleSplit(ctx context.Context, windowID int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if window := d.windowByID(windowID); window != nil {
		if window.SplitType == wm.SplitVertical {
			window.SplitType = wm.SplitHorizontal
		} else {
			window.SplitType = wm.SplitVertical
		}
	}
	return nil
}

func (d *Driver) ToggleFloat(ctx context.Context, windowID int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if window := d.windowByID(windowID); window != nil {
		window.Floating = !window.Floating
	}
	return nil
}

func (d *Driver) ArmInsert(ctx context.Context, _ int, _ wm.Direction) error { return ctx.Err() }

func (d *Driver) StackOnto(ctx context.Context, targetID int, windowID int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	target := d.windowByID(targetID)
	window := d.windowByID(windowID)
	if target != nil && window != nil {
		window.SpaceID = target.SpaceID
		window.DisplayIdx = target.DisplayIdx
	}
	return nil
}

func (d *Driver) SwapWindows(ctx context.Context, _ wm.Direction) (bool, error) {
	return true, ctx.Err()
}
func (d *Driver) WarpWindow(ctx context.Context, _ wm.Direction) (bool, error) {
	return true, ctx.Err()
}
func (d *Driver) ResizeWindow(ctx context.Context, _ wm.ResizeEdge, _, _ int) (bool, error) {
	return true, ctx.Err()
}

func (d *Driver) FocusWindow(ctx context.Context, windowID int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if d.windowByID(windowID) == nil {
		return false, nil
	}
	d.focusedWindow = &windowID
	return true, nil
}

func (d *Driver) FocusWindowDir(ctx context.Context, _ wm.FocusSelector) (bool, error) {
	return true, ctx.Err()
}
func (d *Driver) FocusDisplay(ctx context.Context, _ wm.DisplaySelector) (bool, error) {
	return true, ctx.Err()
}
func (d *Driver) SettleUnit() time.Duration { return d.settleUnit }

func (d *Driver) spaceByID(id wm.SpaceID) *fakeSpace {
	for i := range d.spaces {
		if d.spaces[i].id == id {
			return &d.spaces[i]
		}
	}
	return nil
}

func (d *Driver) windowByID(id int) *wm.Window {
	for i := range d.windows {
		if d.windows[i].ID == id {
			return &d.windows[i]
		}
	}
	return nil
}

func (d *Driver) toSpace(space *fakeSpace) wm.Space {
	windowIDs := make([]int, 0)
	for _, window := range d.windows {
		if window.SpaceID == space.id {
			windowIDs = append(windowIDs, window.ID)
		}
	}
	return wm.Space{ID: space.id, Label: space.label, DisplayIdx: space.displayIdx, WindowIDs: windowIDs, Layout: space.layout}
}
