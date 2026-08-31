// Layer 2 — ENGINE: the distinct-window claim engine.
//
// Pure over its inputs: no driver / filesystem / clock. Ports `win_id_claim` /
// `win_ids_claim`. A logical name like `arc` matches MANY
// windows; the desk layout wants a DIFFERENT Arc per display, so each claim
// hands out the first matching UNCLAIMED window, preferring one already on the
// target display (windows you've placed tend to stay put). The bash `_CLAIMED`
// global (a subshell-fragile string) becomes instance state here — the whole
// point of the port.

import type { Profile, WindowName } from "../config/types.ts";
import type { WmWindow } from "../driver/types.ts";
import { matchesSpec } from "./matcher.ts";

export class ClaimSet {
	private readonly profile: Profile;
	private readonly claimed = new Set<number>();

	constructor(profile: Profile) {
		this.profile = profile;
	}

	/**
	 * Claim one distinct window for `name` (`win_id_claim`).
	 *
	 * Absent spec → `null` (`[[ -z $spec ]] && return 0`). Candidates
	 * are the non-minimized, non-floating windows matching the spec
	 * (`matchesSpec`), kept in the given array order (query order). Pass 1 (only
	 * when `preferDisplay` is set): the first unclaimed candidate on that
	 * display. Pass 2: the first unclaimed candidate.
	 * On a hit, record the id and return it; otherwise `null`.
	 */
	claim(
		windows: WmWindow[],
		name: WindowName,
		preferDisplay?: number,
	): number | null {
		const spec = this.profile.windows[name];
		if (spec == null) {
			return null;
		}
		const cands = windows.filter(
			(w) => !w.minimized && !w.floating && matchesSpec(spec, w.app, w.title),
		);
		// Pass 1: an unclaimed candidate already on the preferred display.
		if (preferDisplay != null) {
			for (const w of cands) {
				if (w.displayIdx === preferDisplay && !this.claimed.has(w.id)) {
					this.claimed.add(w.id);
					return w.id;
				}
			}
		}
		// Pass 2: any unclaimed candidate.
		for (const w of cands) {
			if (!this.claimed.has(w.id)) {
				this.claimed.add(w.id);
				return w.id;
			}
		}
		return null;
	}

	/**
	 * Claim a list of names in order (`win_ids_claim`),
	 * collecting the distinct claimed ids and skipping any name with no free
	 * window.
	 */
	claimMany(
		windows: WmWindow[],
		names: WindowName[],
		preferDisplay?: number,
	): number[] {
		const out: number[] = [];
		for (const name of names) {
			const id = this.claim(windows, name, preferDisplay);
			if (id != null) {
				out.push(id);
			}
		}
		return out;
	}

	/** Clear the claimed set (`claim_reset`). */
	reset(): void {
		this.claimed.clear();
	}
}
