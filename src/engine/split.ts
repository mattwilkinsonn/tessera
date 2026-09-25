// Column-split resolution shared by desk and snap: the layout's override, then
// the display's default, then the profile-wide fallback.

import type { DeskLayout, DisplayName, Profile } from "../config/types.ts";
import type { SpaceLayoutTarget } from "../driver/types.ts";
import type { WorldSnapshot } from "./world.ts";

/** The split fields of a realize target for `kind`: 3col ratios or 2col share. */
export function splitFor(
	profile: Profile,
	kind: DeskLayout["kind"],
	display: DisplayName | undefined,
	override?: DeskLayout["ratios"],
): Pick<SpaceLayoutTarget, "ratios" | "split"> {
	const defaults =
		display == null ? undefined : profile.displays[display].ratios;
	if (kind === "3col") {
		const r = override?.col3 ?? defaults?.col3 ?? profile.ratios;
		return { ratios: { root: r.col3Root, inner: r.col3Inner } };
	}
	if (kind === "2col") {
		return { split: override?.col2 ?? defaults?.col2 ?? 0.5 };
	}
	return {};
}

/** The profile display holding `space`, matched by width; unknown → undefined. */
export function displayOfSpace(
	profile: Profile,
	world: WorldSnapshot,
	space: string,
): DisplayName | undefined {
	const idx = world.spaces.find((s) => s.id === space)?.displayIdx;
	const width = world.displays.find((d) => d.idx === idx)?.frame.w;
	for (const [name, spec] of Object.entries(profile.displays)) {
		if (spec.width === width) {
			return name as DisplayName;
		}
	}
	return undefined;
}
