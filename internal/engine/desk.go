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
	ops := make([]Op, 0)
	doomed := make(map[wm.SpaceID]struct{})
	for _, id := range TeardownLabels(world, "lap-") {
		doomed[id] = struct{}{}
		ops = append(ops, DestroySpaceOp{Space: id})
	}
	for _, id := range StraySpaces(world) {
		doomed[id] = struct{}{}
		ops = append(ops, DestroySpaceOp{Space: id})
	}
	claims := NewClaimSet(profile)
	builds := make([]deskBuild, 0)
	for _, layout := range ResolveDesk(profile, world.Displays) {
		index, present := ResolveDisplay(profile, layout.Display, world.Displays)
		if !present {
			continue
		}
		var home wm.SpaceID
		for _, display := range world.Displays {
			if display.Idx != index {
				continue
			}
			for _, id := range display.SpaceIDs {
				if _, removed := doomed[id]; !removed {
					home = id
					break
				}
			}
			break
		}
		if home == "" {
			continue
		}
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
		builds = append(builds, deskBuild{homeSpace: home, label: layout.Label, kind: layout.Kind, tracks: tracks, weights: weights})
	}
	var park wm.SpaceID
	if len(builds) >= 2 {
		park = builds[len(builds)-1].homeSpace
	}
	if park != "" {
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
	}
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
