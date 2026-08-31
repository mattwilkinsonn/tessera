// Layer 2 — ENGINE: numpad focus-slot resolver.
//
// Pure: resolves a 1-based numpad slot to a live window id over one
// WorldSnapshot, touching no driver. Ports— DESK_SLOTS map
// numpad 1..9 to stable logical positions spanning all displays, and a
// `name@display` slot focuses the window of that logical name currently on
// that display (falling back to the first match anywhere).

import type { Profile, WindowSpec } from "../config/types.ts";
import { resolveDisplay } from "./display.ts";
import { matchesSpec } from "./matcher.ts";
import type { WorldSnapshot } from "./world.ts";

/**
 * Resolve the window id for the 1-based numpad focus slot `n`, or `null` if
 * unresolvable.
 *
 * Candidate windows are the non-minimized windows matching the slot's spec, in
 * query (`world.windows`) order — floating windows are NOT excluded, they are
 * focusable (selects `is-minimized==false` only). For a
 * `name@display` slot, candidates on the resolved display come first, then the
 * rest (`[on display d] + all |.[0]`); this prefer-then-anywhere ordering
 * also subsumes the bash's final `win_id` fallback. A bare-name slot just
 * takes the first candidate.
 */
export function resolveSlot(
	profile: Profile,
	world: WorldSnapshot,
	n: number,
): number | null {
	//— 1-based; anything below the first slot is a no-op.
	if (n < 1) {
		return null;
	}

	//— the Nth slot, or a no-op past the list end.
	const slot = profile.deskSlots[n - 1];
	if (slot == null) {
		return null;
	}

	// No spec for the logical name → the bash `win_id` matcher yields empty.
	const spec: WindowSpec | undefined = profile.windows[slot.name];
	if (spec == null) {
		return null;
	}

	//— non-minimized matches, in query order.
	const candidates = world.windows.filter(
		(w) => !w.minimized && matchesSpec(spec, w.app, w.title),
	);
	if (candidates.length === 0) {
		return null;
	}

	//— `name@display` prefers the candidate on that
	// display, then falls through to the first anywhere.
	if (slot.onDisplay != null) {
		const prefer = resolveDisplay(profile, slot.onDisplay, world.displays);
		if (prefer != null) {
			const onDisplay = candidates.find((w) => w.displayIdx === prefer);
			if (onDisplay != null) {
				return onDisplay.id;
			}
		}
	}

	// First candidate anywhere: bare-name slot, absent display, or the
	// prefer-branch fall-through (final `win_id` fallback).
	const first = candidates[0];
	return first == null ? null : first.id;
}
