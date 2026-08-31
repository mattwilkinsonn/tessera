// Layer 2 — ENGINE: the space-reaping candidate rules.
//
// Pure over one `WorldSnapshot`: no driver / filesystem / clock. Ports the two
// destroy-prelude rules desk mode runs before a rebuild:
// - `teardownLabels` ← `teardown_laptop_grid`
// - `straySpaces` ← `reap_stray_spaces`
//
// Both return the opaque stable `SpaceId`s to destroy (D1), NOT live indexes:
// the bash re-queried after every destroy because yabai renumbers space
// indexes, the exact renumber hazard `SpaceId` exists to erase. An
// executor destroys the whole returned set from one snapshot — the ids stay
// valid across the destroys.

import type { SpaceId, WmSpace, WmWindow } from "../driver/types.ts";
import type { WorldSnapshot } from "./world.ts";

/**
 * Every space whose label starts with `prefix` (`teardown_laptop_grid` with
 * `"lap-"`). yabai relocates any windows on a destroyed space to an
 * adjacent space on the same display — nothing is closed. A home space is
 * never prefixed, so it is never returned. Returned in display then space
 * order for determinism.
 */
export function teardownLabels(
	world: WorldSnapshot,
	prefix: string,
): SpaceId[] {
	const byId = spaceById(world);
	const out: SpaceId[] = [];
	for (const d of world.displays) {
		for (const sid of d.spaceIds) {
			const sp = byId.get(sid);
			if (sp?.label.startsWith(prefix)) {
				out.push(sid);
			}
		}
	}
	return out;
}

/**
 * Every stray space macOS spawned on a topology change (`reap_stray_spaces`).
 * A stray is:
 * - UNLABELLED — every desk (`main`/`plan`/`laptop`) and `lap-*` space is
 * labelled, so a labelled space is never a candidate.
 * - Holds no NON-STICKY window — a sticky floater (e.g. Akiflow) is reported
 * on EVERY space, so emptiness must count only non-sticky windows; a space
 * whose sole occupants are sticky floaters is genuinely empty.
 * - NOT the last space on its display — a display is never left space-less.
 *
 * The last-on-display guard is enforced per display against the snapshot: when
 * EVERY space on a display is a stray, one is kept (the last in display order,
 * matching the bash's destroy-lowest-index-first drain);
 * otherwise all strays are returned (a non-stray space survives).
 */
export function straySpaces(world: WorldSnapshot): SpaceId[] {
	const spaces = spaceById(world);
	const windows = windowById(world);
	const out: SpaceId[] = [];
	for (const d of world.displays) {
		// `total` is the pre-teardown space count from this one snapshot, so it
		// can include `lap-*` spaces that teardownLabels() reaps in the same plan
		// (the bash re-queries after each destroy). The last-on-display
		// guard below stays correct only because spaceIds[0] (home) is always
		// desk-labelled — never `lap-*`, never empty — so a
		// non-doomed home always survives and the count can't drop a display below it.
		const total = d.spaceIds.length;
		if (total <= 1) {
			continue;
		}
		const candidates = d.spaceIds.filter((sid) => {
			const sp = spaces.get(sid);
			return sp != null && isStray(sp, windows);
		});
		// All-stray display → keep the last in order (never leave it space-less).
		const reapable =
			candidates.length === total ? candidates.slice(0, -1) : candidates;
		for (const sid of reapable) {
			out.push(sid);
		}
	}
	return out;
}

/** A space is empty of non-sticky windows. */
function isStray(sp: WmSpace, windows: Map<number, WmWindow>): boolean {
	if (sp.label !== "") {
		return false;
	}
	for (const wid of sp.windowIds) {
		const w = windows.get(wid);
		if (w != null && !w.sticky) {
			return false;
		}
	}
	return true;
}

function spaceById(world: WorldSnapshot): Map<SpaceId, WmSpace> {
	const m = new Map<SpaceId, WmSpace>();
	for (const sp of world.spaces) {
		m.set(sp.id, sp);
	}
	return m;
}

function windowById(world: WorldSnapshot): Map<number, WmWindow> {
	const m = new Map<number, WmWindow>();
	for (const w of world.windows) {
		m.set(w.id, w);
	}
	return m;
}
