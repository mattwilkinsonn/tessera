// Layer 2 — ENGINE: topology selection.
//
// Pure: takes the `queryDisplays()` snapshot, touches no driver. Picks WHICH
// desk layout set applies to the display set currently present.
//
// `resolveDisplay` already makes an ABSENT display degrade gracefully — its
// layout is skipped. That is not enough on its own: a display that survives
// keeps its one hard-coded layout, so "the AW is my only external today" cannot
// lay the AW out differently from "the AW sits beside the G9". A named topology
// supplies a whole replacement `desk` for one exact present-set.
//
// Matching is EXACT-SET, not subset. A subset match would let a two-display
// topology claim a three-display rig and silently drop the third display's
// layout; an operator adding a monitor would see one vanish with nothing to
// point at. Exact means an undeclared combination falls back to `desk`, which
// still degrades per-display as before.

import type { DisplayName, Profile } from "../config/types.ts";
import type { WmDisplay } from "../driver/types.ts";

/**
 * The logical display slots currently present (duplicate widths collapse to one
 * slot). `null` when a connected display matches NO profile slot: the present
 * set is then unknowable, so no exact-set topology may claim this arrangement.
 */
function presentDisplays(
	profile: Profile,
	displays: ReadonlyArray<WmDisplay>,
): Set<DisplayName> | null {
	const byWidth = new Map<number, DisplayName>();
	for (const [name, spec] of Object.entries(profile.displays)) {
		byWidth.set(spec.width, name as DisplayName);
	}
	const present = new Set<DisplayName>();
	for (const d of displays) {
		const name = byWidth.get(d.frame.w);
		if (name == null) {
			return null;
		}
		present.add(name);
	}
	return present;
}

/**
 * Resolve the desk layout set for the present displays: the first declared
 * topology whose display set is exactly the present set, else `profile.desk`.
 *
 * Declaration order is the precedence, so overlapping topologies resolve
 * predictably rather than by object-key order.
 */
export function resolveDesk(
	profile: Profile,
	displays: ReadonlyArray<WmDisplay>,
): Profile["desk"] {
	if (profile.topologies == null) {
		return profile.desk;
	}
	const present = presentDisplays(profile, displays);
	if (present == null) {
		return profile.desk;
	}
	for (const topology of profile.topologies) {
		const declared = new Set(topology.displays);
		if (declared.size !== present.size) {
			continue;
		}
		let match = true;
		for (const name of declared) {
			if (!present.has(name)) {
				match = false;
				break;
			}
		}
		if (match) {
			return topology.desk;
		}
	}
	return profile.desk;
}
