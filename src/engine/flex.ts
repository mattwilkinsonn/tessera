// Layer 2 — ENGINE: the laptop-flex window planner.
//
// Pure over its inputs: no driver / filesystem / clock. Ports two pure bash
// helpers: `laptop_flex_order` as `reconcileFlexOrder`, and
// the seed + filter + id-order occurrence-labeling core of
// `laptop_flex_windows` as `laptopFlexWindows`. The bash
// `laptop_flex_order` read/wrote a cache file and the converger (T4) does that
// I/O now, so the reconcile is a pure list operation here. The bash
// `laptop_flex_windows` also composed `laptop_flex_order` and `lap-` prefixed
// its output — that composition is the converger's job, so
// this port stops at the bare occurrence-labeled id-order rows.

import type { Profile } from "../config/types.ts";
import type { WmWindow } from "../driver/types.ts";
import { slugForWindow } from "./matcher.ts";

/**
 * Reconcile the persisted flex-order against the current slugs
 * (`laptop_flex_order`). Pure list reconciliation; the caller
 * owns the cache-file I/O the bash did.
 *
 * `toPersist` is `persisted` with every slug in `current` NOT already present
 * appended in `current`'s order (each new slug once, mirroring `grep -qxF`
 * against the growing file). Every original `persisted` line
 * is retained even when absent from `current`, so a slug's slot is stable
 * across relaunch. `order` is `toPersist` filtered to the
 * slugs present in `current`, in file order. Empty-string
 * slugs are skipped.
 */
export function reconcileFlexOrder(
	persisted: string[],
	current: string[],
): { order: string[]; toPersist: string[] } {
	const toPersist = [...persisted];
	for (const slug of current) {
		if (!toPersist.includes(slug)) {
			toPersist.push(slug);
		}
	}
	const currentSet = new Set(current);
	const order = toPersist.filter((slug) => slug !== "" && currentSet.has(slug));
	return { order, toPersist };
}

/**
 * Plan the flex windows (seed + filter + label core). Returns one row per
 * non-minimized, non-floating window
 * whose app is NOT a `laptopStackApps` key and whose id is NOT already claimed,
 * in ASCENDING ID ORDER, each carrying its BARE occurrence slug (the converger
 * adds the `lap-` prefix and reconciles order).
 *
 * The occurrence map is seeded from `profile.laptopPinned` — each pinned name's
 * count is how many times it appears. Pinned names share the
 * slug namespace with `slugForWindow`, so a pinned slug's flex overflow
 * continues its sequence (a 5th Arc after 4 pinned arcs → `arc-5`, D10).
 * A slug's first occurrence is the bare base; the n-th is
 * `${base}-${n}`.
 *
 * Claimed handling (divergence from bash): the bash mutates the `_CLAIMED`
 * global as it goes so a duplicate id isn't emitted twice.
 * Here `claimed` is a readonly INPUT and the window snapshot is de-duplicated
 * (each id visited once), so we only skip ids already claimed on entry —
 * nothing is added back.
 */
export function laptopFlexWindows(
	profile: Profile,
	windows: readonly WmWindow[],
	claimed: ReadonlySet<number>,
): Array<{ id: number; label: string }> {
	const occ = new Map<string, number>();
	for (const name of profile.laptopPinned) {
		occ.set(name, (occ.get(name) ?? 0) + 1);
	}
	const survivors = windows
		.filter(
			(w) =>
				!w.minimized &&
				!w.floating &&
				profile.laptopStackApps[w.app] !== true &&
				!claimed.has(w.id),
		)
		.sort((a, b) => a.id - b.id);
	const out: Array<{ id: number; label: string }> = [];
	for (const w of survivors) {
		const base = slugForWindow(profile, w.app, w.title);
		const n = (occ.get(base) ?? 0) + 1;
		occ.set(base, n);
		const label = n === 1 ? base : `${base}-${n}`;
		out.push({ id: w.id, label });
	}
	return out;
}
