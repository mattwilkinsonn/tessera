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

func DeskPlan(profile *config.Profile, world World) []Op {
	if profile == nil {
		return []Op{}
	}
	doomed, ops := teardownSpaces(world)
	builds := buildDesks(profile, world, doomed)
	park := parkingSpace(builds)
	if park != "" {
		ops = append(ops, evacuateWindows(world, builds, park)...)
	}
	return append(ops, realizeDesks(builds)...)
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

func buildDesks(profile *config.Profile, world World, doomed map[wm.SpaceID]struct{}) []deskBuild {
	claims := NewClaimSet(profile)
	builds := make([]deskBuild, 0)
	for _, layout := range ResolveDesk(profile, world.Displays) {
		index, present := ResolveDisplay(profile, layout.Display, world.Displays)
		if !present {
			continue
		}
		home := homeSpace(world.Displays, index, doomed)
		if home == "" {
			continue
		}
		tracks, weights := deskTracks(profile, world, layout, claims, index)
		builds = append(builds, deskBuild{homeSpace: home, label: layout.Label, kind: layout.Kind, tracks: tracks, weights: weights})
	}
	return builds
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

func deskTracks(profile *config.Profile, world World, layout config.DeskLayout, claims *ClaimSet, index int) ([][]int, []float64) {
	tracks := make([][]int, 0)
	weights := make([]float64, 0)
	configured := WeightsFor(profile, layout.Kind, len(layout.Tracks), layout.Display, layout.Weights)
	for trackIndex, names := range layout.Tracks {
		claimed := claims.ClaimMany(world.Windows, names, mo.Some(index))
		if len(claimed) == 0 {
			continue
		}
		tracks = append(tracks, claimed)
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
