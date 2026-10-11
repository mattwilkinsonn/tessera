package engine

import "github.com/mattwilkinsonn/tessera/internal/wm"

type laptopTestWorld struct {
	windows       []wm.Window
	spaces        []wm.Space
	order         []wm.SpaceID
	next          int
	createEnabled bool
	unreported    map[int]struct{}
	undestroyable map[wm.SpaceID]struct{}
}

func newLaptopWorld(windows []wm.Window, staleLabels []string, createEnabled bool) *laptopTestWorld {
	world := &laptopTestWorld{
		windows: windows, order: []wm.SpaceID{"home"}, next: 1, createEnabled: createEnabled,
		unreported: make(map[int]struct{}), undestroyable: make(map[wm.SpaceID]struct{}),
		spaces: []wm.Space{{ID: "home", DisplayIdx: 3, Layout: wm.LayoutBSP}},
	}
	for _, label := range staleLabels {
		id := wm.SpaceID("s-" + itoa(world.next))
		world.next++
		world.order = append(world.order, id)
		world.spaces = append(world.spaces, wm.Space{ID: id, Label: label, DisplayIdx: 3, Layout: wm.LayoutBSP})
	}
	world.recompute()
	return world
}

func (world *laptopTestWorld) recompute() {
	for index := range world.spaces {
		world.spaces[index].WindowIDs = []int{}
	}
	for _, window := range world.windows {
		for index := range world.spaces {
			if world.spaces[index].ID == window.SpaceID {
				world.spaces[index].WindowIDs = append(world.spaces[index].WindowIDs, window.ID)
			}
		}
	}
}

func laptopSnapshot(world *laptopTestWorld) World {
	spaces := make([]wm.Space, 0, len(world.order))
	for _, id := range world.order {
		for _, space := range world.spaces {
			if space.ID == id {
				spaces = append(spaces, space)
				break
			}
		}
	}
	windows := make([]wm.Window, 0, len(world.windows))
	for _, window := range world.windows {
		if _, hidden := world.unreported[window.ID]; !hidden {
			windows = append(windows, window)
		}
	}
	return World{Windows: windows, Spaces: spaces, Displays: []wm.Display{{Idx: 3, Frame: wm.Frame{W: deskLaptop, H: 1117}, SpaceIDs: append([]wm.SpaceID{}, world.order...)}}}
}

func applyLaptopAction(world *laptopTestWorld, action ConvergeAction) {
	switch value := action.(type) {
	case RelabelHomeAction:
		for index := range world.spaces {
			if world.spaces[index].ID == value.HomeSpace {
				world.spaces[index].Label = value.Label
			}
		}
	case CreateSpaceAction:
		if world.createEnabled {
			id := wm.SpaceID("s-" + itoa(world.next))
			world.next++
			world.spaces = append(world.spaces, wm.Space{ID: id, Label: value.Label, DisplayIdx: value.DisplayIdx, Layout: wm.LayoutBSP, WindowIDs: []int{}})
			world.order = append(world.order, id)
		}
	case MoveWindowAction:
		for index := range world.windows {
			if world.windows[index].ID == value.WindowID {
				world.windows[index].SpaceID = value.ToSpace
			}
		}
		world.recompute()
	case RehomeAndDestroyAction:
		for index := range world.windows {
			if world.windows[index].SpaceID == value.StaleSpace {
				world.windows[index].SpaceID = value.HomeSpace
			}
		}
		world.destroy(value.StaleSpace)
		world.recompute()
	case MoveSpaceAction:
		from := -1
		for index, id := range world.order {
			if id == value.Space {
				from = index
				break
			}
		}
		if from >= 0 {
			world.order = append(world.order[:from], world.order[from+1:]...)
			to := min(max(value.ToIndex-1, 0), len(world.order))
			world.order = append(world.order, "")
			copy(world.order[to+1:], world.order[to:])
			world.order[to] = value.Space
		}
	case DestroySpaceOp:
		world.destroy(value.Space)
	case SetLayoutAction:
		for index := range world.spaces {
			if world.spaces[index].ID == value.Space {
				world.spaces[index].Layout = value.Layout
			}
		}
	}
}
