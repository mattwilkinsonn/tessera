package engine

import (
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/samber/mo"
)

type deskBuild struct {
	homeSpace wm.SpaceID
	label     string
	kind      config.LayoutKind
	tracks    [][]int
	weights   []float64
}

func DeskPlan(profile *config.Profile, world World) ([]Op, map[SlotKey]int) {
	filled := make(map[SlotKey]int)
	if profile == nil {
		return []Op{}, filled
	}
	doomed, ops := teardownSpaces(world)
	builds, filled := buildDesks(profile, world, doomed)
	park := parkingSpace(builds)
	if park != "" {
		ops = append(ops, evacuateWindows(world, builds, park)...)
	}
	return append(ops, realizeDesks(builds)...), filled
}

func teardownSpaces(world World) (map[wm.SpaceID]struct{}, []Op) {
	teardown := TeardownLabels(world, "lap-")
	stray := StraySpaces(world)
	doomed := make(map[wm.SpaceID]struct{})
	ops := make([]Op, 0, len(teardown)+len(stray))
	for _, id := range teardown {
		doomed[id] = struct{}{}
		ops = append(ops, DestroySpaceOp{Space: id})
	}
	for _, id := range stray {
		doomed[id] = struct{}{}
		ops = append(ops, DestroySpaceOp{Space: id})
	}
	return doomed, ops
}

func buildDesks(profile *config.Profile, world World, doomed map[wm.SpaceID]struct{}) ([]deskBuild, map[SlotKey]int) {
	layouts := ResolveDesk(profile, world.Displays)
	namesByDisplay := make(map[config.DisplayName][]string)
	for _, layout := range layouts {
		if _, present := ResolveDisplay(profile, layout.Display, world.Displays); !present {
			continue
		}
		for _, track := range layout.Tracks {
			namesByDisplay[layout.Display] = append(namesByDisplay[layout.Display], track...)
		}
	}
	keysByDisplay := make(map[config.DisplayName][]SlotKey, len(namesByDisplay))
	allSlotKeys := make([]SlotKey, 0)
	for _, layout := range layouts {
		if _, present := ResolveDisplay(profile, layout.Display, world.Displays); !present {
			continue
		}
		if _, added := keysByDisplay[layout.Display]; added {
			continue
		}
		keys := SlotKeys("desk", layout.Display, namesByDisplay[layout.Display])
		keysByDisplay[layout.Display] = keys
		allSlotKeys = append(allSlotKeys, keys...)
	}
	claims := NewClaimSet(profile, world.Slots, Reserve(profile, world.Slots, allSlotKeys, DisplaySet(profile, world.Displays), world.Windows))
	filled := make(map[SlotKey]int)
	builds := make([]deskBuild, 0)
	keyOffsets := make(map[config.DisplayName]int)
	for _, layout := range layouts {
		index, present := ResolveDisplay(profile, layout.Display, world.Displays)
		if !present {
			continue
		}
		home := homeSpace(world.Displays, index, doomed)
		keyOffset := keyOffsets[layout.Display]
		if home == "" {
			keyOffsets[layout.Display] = keyOffset + slotCount(layout.Tracks)
			continue
		}
		tracks, weights := deskTracks(profile, world, layout, claims, index, keysByDisplay[layout.Display], &keyOffset, filled)
		keyOffsets[layout.Display] = keyOffset
		builds = append(builds, deskBuild{homeSpace: home, label: layout.Label, kind: layout.Kind, tracks: tracks, weights: weights})
	}
	return builds, filled
}

func homeSpace(displays []wm.Display, index int, doomed map[wm.SpaceID]struct{}) wm.SpaceID {
	for _, display := range displays {
		if display.Idx != index {
			continue
		}
		for _, id := range display.SpaceIDs {
			if _, removed := doomed[id]; !removed {
				return id
			}
		}
		return ""
	}
	return ""
}

func slotCount(tracks [][]string) int {
	count := 0
	for _, track := range tracks {
		count += len(track)
	}
	return count
}

func deskTracks(profile *config.Profile, world World, layout config.DeskLayout, claims *ClaimSet, index int, keys []SlotKey, keyOffset *int, filled map[SlotKey]int) ([][]int, []float64) {
	tracks := make([][]int, 0)
	weights := make([]float64, 0)
	configured := WeightsFor(profile, layout.Kind, len(layout.Tracks), layout.Display, layout.Weights)
	for trackIndex, names := range layout.Tracks {
		track := make([]int, 0, len(names))
		for _, name := range names {
			// Keys come from these same tracks, so a missing key means a builder bug; claim without memory.
			var key SlotKey
			if *keyOffset < len(keys) {
				key = keys[*keyOffset]
			}
			*keyOffset++
			if id, ok := claims.ClaimSlot(world.Windows, key, name, mo.Some(index)); ok {
				track = append(track, id)
				if key != "" {
					filled[key] = id
				}
			}
		}
		if len(track) == 0 {
			continue
		}
		tracks = append(tracks, track)
		if layout.Kind != config.LayoutStack && trackIndex < len(configured) {
			weights = append(weights, configured[trackIndex])
		}
	}
	return tracks, weights
}

func parkingSpace(builds []deskBuild) wm.SpaceID {
	if len(builds) < 2 {
		return ""
	}
	return builds[len(builds)-1].homeSpace
}

func evacuateWindows(world World, builds []deskBuild, park wm.SpaceID) []Op {
	ops := make([]Op, 0)
	for _, build := range builds {
		if build.homeSpace == park {
			continue
		}
		for _, window := range world.Windows {
			if window.SpaceID == build.homeSpace && !window.Floating && !window.Minimized {
				ops = append(ops, MoveWindowAction{WindowID: window.ID, ToSpace: park})
			}
		}
	}
	return ops
}

func realizeDesks(builds []deskBuild) []Op {
	ops := make([]Op, 0, len(builds)*2)
	for _, build := range builds {
		ops = append(ops, RelabelHomeAction{HomeSpace: build.homeSpace, Label: build.label})
		if len(build.tracks) == 0 {
			continue
		}
		kind := wm.LayoutKind(build.kind)
		if build.kind == config.LayoutStack {
			kind = wm.TargetStack
		}
		ops = append(ops, RealizeLayoutOp{Space: build.homeSpace, Target: wm.SpaceLayoutTarget{Kind: kind, Tracks: build.tracks, Weights: build.weights}})
	}
	return ops
}
