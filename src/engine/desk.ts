// Layer 2 — ENGINE: the desk-mode plan builder. A faithful, PURE port of the
// engine-side logic — arrange every PRESENT display to its standard desk
// layout — as a flat, backend-neutral `PlanOp[]` computed from ONE
// `WorldSnapshot`. No driver / filesystem / clock; inputs are never mutated.
//
// The engine emits a declarative tracks-and-weights target. When multiple desk
// displays are present, it evacuates tiled windows from rebuild spaces to one
// stable park before realization; this prevents insert operations from no-oping
// when a target window is already on its destination space.
//
// Order of ops: all destroy preludes first
// (teardown_laptop_grid `:42`, then reap_stray_spaces `:49`), then per present
// display in `profile.desk` order (relabelHome, then realizeLayout).

import type { Profile } from "../config/types.ts";
import type { SpaceId, SpaceLayoutTarget } from "../driver/types.ts";
import { ClaimSet } from "./claim.ts";
import { resolveDisplay } from "./display.ts";
import type { PlanOp } from "./plan.ts";
import { straySpaces, teardownLabels } from "./reap.ts";
import { weightsFor } from "./split.ts";
import { resolveDesk } from "./topology.ts";
import type { WorldSnapshot } from "./world.ts";

/**
 * Build the flat desk plan: the ops that arrange every present display to its
 * standard layout. Pure over one `WorldSnapshot`.
 *
 * Missing windows are skipped: an unclaimed name
 * contributes no id, so a partial app set still lays out cleanly — a column that
 * resolves empty is dropped, and a display with zero claimed windows still gets
 * its `relabelHome` but no `realizeLayout`.
 *
 * Evacuation: with ≥2 desk displays present, EVERY tiled window on a
 * REBUILD display's home (targets AND foreign) is moved up front onto ONE stable
 * park — the LAST present desk display's home (`profile.desk` order is g9, aw,
 * laptop, so the park is the laptop stack when present, else aw; mirrors the bash
 * `PARK` choice). Targets are evacuated too because
 * the driver's column recipe arms an insert and re-adds each target with a
 * cross-space move, and yabai only consumes an armed insert on a REAL move — a
 * target left on-space would no-op the move and strand the armed insert (the red
 * overlay) unstacked. The park
 * display is never evacuated: it is the catch-all that absorbs the refugees (they
 * join its stack when it is the laptop desk). A single display → no park, rebuild
 * in place.
 */
export function deskPlan(profile: Profile, world: WorldSnapshot): PlanOp[] {
	const ops: PlanOp[] = [];

	// ── Destroy preludes ────────────────────────
	// teardown_laptop_grid (lap-*) THEN reap_stray_spaces, in that order. `doomed`
	// is every SpaceId these ops remove — a reaped stray at spaceIds[0] must not
	// become a display's relabel target (the bash re-queries first_space AFTER
	// the reap).
	const doomed = new Set<SpaceId>();
	for (const space of teardownLabels(world, "lap-")) {
		doomed.add(space);
		ops.push({ op: "destroySpace", space });
	}
	for (const space of straySpaces(world)) {
		doomed.add(space);
		ops.push({ op: "destroySpace", space });
	}

	// ── Up-front global claim across all displays ─
	// ONE ClaimSet: a logical name (`arc`) that repeats across displays claims a
	// DISTINCT window per display via the shared claimed set. `profile.desk` is
	// already in the bash claim order (g9, then aw, then mbp).
	const claims = new ClaimSet(profile);
	// ClaimSet wants a mutable array; the snapshot's is readonly. One copy, reused
	// across every claim so the query-order dedup is preserved.
	const windows = [...world.windows];

	// ── Pass 1: resolve every present desk display's home + tracks ────────────
	// Resolve first so the stable park is chosen before any build op.
	interface Build {
		readonly homeSpace: SpaceId;
		readonly label: string;
		readonly kind: SpaceLayoutTarget["kind"];
		readonly tracks: number[][];
		readonly weights?: number[];
	}
	const builds: Build[] = [];
	for (const layout of resolveDesk(profile, world.displays)) {
		const idx = resolveDisplay(profile, layout.display, world.displays);
		if (idx == null) {
			// Absent display — not selected (topology-portable, D2-corollary).
			continue;
		}
		const display = world.displays.find((d) => d.idx === idx);
		if (display == null) {
			continue;
		}
		// Home space: the first spaceId that SURVIVED the reap (mirrors first_space
		// after the destroy preludes). All-doomed or empty → no target, skip.
		let homeSpace: SpaceId | undefined;
		for (const space of display.spaceIds) {
			if (!doomed.has(space)) {
				homeSpace = space;
				break;
			}
		}
		if (homeSpace == null) {
			continue;
		}

		const claimedTracks = layout.tracks.map((track) =>
			claims.claimMany(windows, [...track], idx),
		);
		const tracks: number[][] = [];
		const configuredWeights =
			layout.kind === "stack"
				? undefined
				: weightsFor(
						profile,
						layout.kind,
						layout.tracks.length,
						layout.display,
						layout.weights,
					);
		const weights: number[] = [];
		for (const [index, track] of claimedTracks.entries()) {
			if (track.length === 0) {
				continue;
			}
			tracks.push(track);
			if (configuredWeights != null) {
				const weight = configuredWeights[index];
				if (weight != null) {
					weights.push(weight);
				}
			}
		}
		builds.push({
			homeSpace,
			label: layout.label,
			kind: layout.kind,
			tracks,
			...(configuredWeights == null ? {} : { weights }),
		});
	}

	// ── Stable park ───────────────────────────
	// The home of the LAST present desk display (profile.desk order g9, aw,
	// laptop → laptop when present, else aw; never g9 alone). Chosen ONCE, up
	// front, so it cannot ping-pong. Only when ≥2 desk displays are present is
	// there a display to rebuild AND a distinct catch-all to park onto; a single
	// display rebuilds in place.
	const lastBuild = builds[builds.length - 1];
	const park =
		builds.length >= 2 && lastBuild != null ? lastBuild.homeSpace : undefined;

	// ── Pass 2a: up-front evacuation ──────────────────────────────
	// Clear every REBUILD display's home space ENTIRELY — targets AND foreign
	// windows — onto the single park before ANY build, so each rebuilt space
	// starts from a genuinely empty tree. This is load-bearing, not just tidiness:
	// the driver's column recipe arms `--insert east`/`--insert stack` and then
	// re-adds each target with a cross-space move, and yabai only CONSUMES an
	// armed insert on a REAL move — a move to the space a window already sits on
	// is a no-op that leaves the insert armed (the red overlay) and the window
	// unstacked. So the targets must be off-space too. The bash does exactly this:
	// `evacuate_to "$PARK" "$space"` is called with NO keep ids (everything goes),
	// then the targets are re-parked as well; the driver's build then brings the targets
	// back on with real, insert-consuming moves. The park display is NEVER
	// evacuated: it is the catch-all that absorbs the refugees (they join its
	// stack when it is the laptop desk).
	if (park != null) {
		for (const b of builds) {
			if (b.homeSpace === park) {
				continue;
			}
			for (const w of world.windows) {
				if (w.spaceId !== b.homeSpace || w.floating || w.minimized) {
					continue;
				}
				ops.push({ op: "moveWindow", windowId: w.id, toSpace: park });
			}
		}
	}

	// ── Pass 2b: relabel + realize, per present display in profile.desk order ──
	for (const b of builds) {
		// relabelHome ALWAYS for a present display with a surviving home
		// (label the space unconditionally).
		ops.push({ op: "relabelHome", homeSpace: b.homeSpace, label: b.label });

		if (b.tracks.length > 0) {
			const target: SpaceLayoutTarget = {
				kind: b.kind,
				tracks: b.tracks,
				...(b.weights == null ? {} : { weights: b.weights }),
			};
			ops.push({ op: "realizeLayout", space: b.homeSpace, target });
		}
	}

	return ops;
}
