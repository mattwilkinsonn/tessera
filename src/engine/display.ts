// Layer 2 — ENGINE: display resolution.
//
// Pure: takes the `queryDisplays()` snapshot, touches no driver. Ports
// `display_idx` — logical `DisplayName` → live macOS display
// index, matched by frame width because "display UUIDs/indexes are not [stable]
// (macOS reorders them on connect)". Returns `null` for an
// absent display: the topology-portability primitive the D2-corollary rests on
// — a no-laptop rig resolves `laptop` to `null` and its layout is simply not
// selected.

import type { DisplayName, Profile } from "../config/types.ts";
import type { WmDisplay } from "../driver/types.ts";

/**
 * Resolve a logical `DisplayName` to its live macOS display index by matching
 * the profile's stable width against the queried display frames. Returns the
 * first match's index, or `null` when the display is not connected (mirrors
 * `display_idx`'s `.[0].index // empty`).
 */
export function resolveDisplay(
	profile: Profile,
	name: DisplayName,
	displays: ReadonlyArray<WmDisplay>,
): number | null {
	const width = profile.displays[name]?.width;
	if (width == null) {
		return null;
	}
	const match = displays.find((d) => d.frame.w === width);
	return match?.idx ?? null;
}
