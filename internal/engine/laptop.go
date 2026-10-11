package engine

import (
	"slices"
	"strconv"
	"strings"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/samber/mo"
)

// LaptopConvergeStep plans one corrective action and returns its next state.
func LaptopConvergeStep(profile *config.Profile, world World, state ConvergeState) (ConvergeAction, ConvergeState, bool) {
	s := state
	for {
		if s.Phase == PhaseDone {
			return nil, s, false
		}
		switch s.Phase {
		case PhasePre:
			laptopIdx, found := ResolveDisplay(profile, config.DisplayLaptop, world.Displays)
			if !found {
				return nil, s, false
			}
			s.Phase, s.Cursor, s.LaptopIdx = PhaseA, 0, laptopIdx
			return RelabelHomeAction{HomeSpace: s.HomeSpace, Label: "laptop"}, s, true
		case PhaseA:
			action, next, emitted := stepPinned(profile, world, s)
			if emitted {
				return action, next, true
			}
			s = next
			continue
		case PhaseB:
			action, next, emitted := stepFlex(profile, world, s)
			if emitted {
				return action, next, true
			}
			s = next
			continue
		case PhaseC:
			action, next, emitted := stepReconcile(world, s)
			if emitted {
				return action, next, true
			}
			s = next
			continue
		case PhaseD:
			action, next, emitted := stepOrder(world, s)
			if emitted {
				return action, next, true
			}
			s = next
			continue
		case PhaseLayout:
			s.Phase = PhaseDone
			return SetLayoutAction{Space: s.HomeSpace, Layout: wm.LayoutStack}, s, true
		default:
			return nil, s, false
		}
	}
}

func stepPinned(profile *config.Profile, world World, state ConvergeState) (ConvergeAction, ConvergeState, bool) {
	s := state
	names, labels := profile.LaptopPinned, pinnedLabels(profile.LaptopPinned)
	claims := NewClaimSet(profile)
	ids := make([]int, len(names))
	hasID := make([]bool, len(names))
	claimedIDs := make(map[int]struct{})
	windows := slices.Clone(world.Windows)
	slices.SortFunc(windows, func(a, b wm.Window) int { return a.ID - b.ID })
	for index, name := range names {
		if id, ok := claims.Claim(windows, name, mo.Some(s.LaptopIdx)); ok {
			ids[index], hasID[index] = id, true
			claimedIDs[id] = struct{}{}
		}
	}
	deadCreate := false
	for s.Cursor < len(names) {
		cursor := s.Cursor
		label := labels[cursor]
		if !hasID[cursor] {
			s.Cursor++
			continue
		}
		if existing, found := spaceWithLabel(world.Spaces, label); found {
			s.Cursor++
			s.ClaimedIDs = claimedIDs
			addString(&s.TargetLabels, label)
			s.DesiredOrder = append(s.DesiredOrder, label)
			s.PendingCreateLabel, s.HasPendingCreate = "", false
			if window, found := findWindow(world.Windows, ids[cursor]); found && window.SpaceID == existing.ID {
				continue
			}
			return MoveWindowAction{WindowID: ids[cursor], ToSpace: existing.ID}, s, true
		}
		if pendingCreateFailed(s, label) {
			deadCreate = true
			s.Cursor++
			continue
		}
		s.ClaimedIDs = claimedIDs
		s.PendingCreateLabel, s.HasPendingCreate = label, true
		return CreateSpaceAction{DisplayIdx: s.LaptopIdx, Label: label}, s, true
	}
	s.Phase, s.Cursor, s.ClaimedIDs = PhaseB, 0, claimedIDs
	s.CreateFailed = s.CreateFailed || deadCreate
	s.PendingCreateLabel, s.HasPendingCreate = "", false
	return nil, s, false
}

func stepFlex(profile *config.Profile, world World, state ConvergeState) (ConvergeAction, ConvergeState, bool) {
	s := state
	rows := LaptopFlexWindows(profile, world.Windows, s.ClaimedIDs)
	bare := make([]string, len(rows))
	idOf := make(map[string]int, len(rows))
	for i, row := range rows {
		bare[i], idOf[row.Label] = row.Label, row.ID
	}
	order := ReconcileFlexOrder(s.PersistedFlexOrder, bare)
	s.ToPersist = order.ToPersist
	deadCreate := false
	for s.Cursor < len(order.Order) {
		bareLabel := order.Order[s.Cursor]
		id, found := idOf[bareLabel]
		if !found {
			s.Cursor++
			continue
		}
		label := "lap-" + bareLabel
		if existing, exists := spaceWithLabel(world.Spaces, label); exists {
			s.Cursor++
			addString(&s.TargetLabels, label)
			s.DesiredOrder = append(s.DesiredOrder, label)
			s.PendingCreateLabel, s.HasPendingCreate = "", false
			if window, found := findWindow(world.Windows, id); found && window.SpaceID == existing.ID {
				continue
			}
			return MoveWindowAction{WindowID: id, ToSpace: existing.ID}, s, true
		}
		if pendingCreateFailed(s, label) {
			deadCreate = true
			s.Cursor++
			continue
		}
		s.PendingCreateLabel, s.HasPendingCreate = label, true
		return CreateSpaceAction{DisplayIdx: s.LaptopIdx, Label: label}, s, true
	}
	s.Phase, s.Cursor = PhaseC, 0
	s.CreateFailed = s.CreateFailed || deadCreate
	s.PendingCreateLabel, s.HasPendingCreate = "", false
	return nil, s, false
}

func pendingCreateFailed(state ConvergeState, label string) bool {
	return state.HasPendingCreate && state.PendingCreateLabel == label
}

func stepReconcile(world World, state ConvergeState) (ConvergeAction, ConvergeState, bool) {
	s := state
	if s.CreateFailed {
		s.Phase = PhaseLayout
		return nil, s, false
	}
	for _, space := range world.Spaces {
		stale := strings.HasPrefix(space.Label, "lap-") && !hasString(s.TargetLabels, space.Label)
		alreadyTried := hasSpace(s.DestroyTried, space.ID)
		if stale && !alreadyTried {
			addSpace(&s.DestroyTried, space.ID)
			return RehomeAndDestroyAction{StaleSpace: space.ID, HomeSpace: s.HomeSpace}, s, true
		}
	}
	if s.ReapStrays {
		for _, id := range laptopStrays(world, s) {
			if hasSpace(s.DestroyTried, id) {
				continue
			}
			addSpace(&s.DestroyTried, id)
			return DestroySpaceOp{Space: id}, s, true
		}
	}
	s.Phase, s.Cursor, s.Placed = PhaseD, 0, 0
	return nil, s, false
}

func stepOrder(world World, state ConvergeState) (ConvergeAction, ConvergeState, bool) {
	s := state
	homePos := globalIndex(s.HomeSpace, world)
	for s.Cursor < len(s.DesiredOrder) {
		label := s.DesiredOrder[s.Cursor]
		s.Cursor++
		space, found := spaceWithLabel(world.Spaces, label)
		if !found {
			continue
		}
		target := homePos + 1 + s.Placed
		current := globalIndex(space.ID, world)
		s.Placed++
		if current == target {
			continue
		}
		return MoveSpaceAction{Space: space.ID, ToIndex: target}, s, true
	}
	s.Phase = PhaseLayout
	return nil, s, false
}

func InitialConvergeState(homeSpace wm.SpaceID, persisted []string, reapStrays bool) ConvergeState {
	return ConvergeState{
		Phase: PhasePre, ClaimedIDs: make(map[int]struct{}), TargetLabels: make(map[string]struct{}),
		HomeSpace: homeSpace, LaptopIdx: -1, PersistedFlexOrder: slices.Clone(persisted),
		ToPersist: slices.Clone(persisted), ReapStrays: reapStrays, DestroyTried: make(map[wm.SpaceID]struct{}),
	}
}

func pinnedLabels(names []string) []string {
	seen := make(map[string]int)
	labels := make([]string, len(names))
	for i, name := range names {
		seen[name]++
		labels[i] = "lap-" + name
		if seen[name] > 1 {
			labels[i] += "-" + strconv.Itoa(seen[name])
		}
	}
	return labels
}

func spaceWithLabel(spaces []wm.Space, label string) (wm.Space, bool) {
	for _, space := range spaces {
		if space.Label == label {
			return space, true
		}
	}
	return wm.Space{}, false
}

func findWindow(windows []wm.Window, id int) (wm.Window, bool) {
	for _, window := range windows {
		if window.ID == id {
			return window, true
		}
	}
	return wm.Window{}, false
}

func globalIndex(id wm.SpaceID, world World) int {
	index := 0
	for _, display := range world.Displays {
		for _, spaceID := range display.SpaceIDs {
			index++
			if spaceID == id {
				return index
			}
		}
	}
	return -1
}

func laptopStrays(world World, state ConvergeState) []wm.SpaceID {
	strays := make(map[wm.SpaceID]struct{})
	for _, id := range StraySpaces(world) {
		strays[id] = struct{}{}
	}
	ids := make([]wm.SpaceID, 0)
	for _, space := range world.Spaces {
		if space.DisplayIdx == state.LaptopIdx && space.ID != state.HomeSpace {
			if _, ok := strays[space.ID]; ok {
				ids = append(ids, space.ID)
			}
		}
	}
	return ids
}

func addString(set *map[string]struct{}, value string) {
	if *set == nil {
		*set = make(map[string]struct{})
	}
	(*set)[value] = struct{}{}
}

func addSpace(set *map[wm.SpaceID]struct{}, value wm.SpaceID) {
	if *set == nil {
		*set = make(map[wm.SpaceID]struct{})
	}
	(*set)[value] = struct{}{}
}

func hasString(set map[string]struct{}, value string) bool {
	_, ok := set[value]
	return ok
}

func hasSpace(set map[wm.SpaceID]struct{}, value wm.SpaceID) bool {
	_, ok := set[value]
	return ok
}
