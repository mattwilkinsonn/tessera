// snap reshapes the focused space's tiled leaves in present left-to-right order.
// The focused display supplies defaults for the mode's nominal track count.

import type { Profile } from "../config/types.ts";
import type { SpaceId } from "../driver/types.ts";
import type { PlanOp } from "./plan.ts";
import { displayOfSpace, weightsFor } from "./split.ts";
import type { WorldSnapshot } from "./world.ts";

export type SnapMode = "3col" | "50-50" | "columns";

export function snapPlan(
	profile: Profile,
	world: WorldSnapshot,
	focusedSpace: SpaceId,
	mode: SnapMode,
): PlanOp[] {
	const ids = world.windows
		.filter((w) => w.spaceId === focusedSpace && !w.floating && !w.minimized)
		.sort((a, b) => a.frame.x - b.frame.x)
		.map((w) => w.id);
	if (ids.length === 0) {
		return [];
	}
	if (mode !== "3col" && mode !== "50-50") {
		return [{ op: "balanceSpace", space: focusedSpace }];
	}

	const display = displayOfSpace(profile, world, focusedSpace);
	const count = mode === "3col" ? 3 : 2;
	const configuredWeights = weightsFor(profile, "columns", count, display);
	let tracks: number[][];
	if (mode === "3col") {
		tracks = ids.slice(0, 3).map((id) => [id]);
		if (ids.length > 3) {
			const lastTrack = tracks[tracks.length - 1];
			if (lastTrack != null) {
				lastTrack.push(...ids.slice(3));
			}
		}
	} else {
		const half = Math.floor((ids.length + 1) / 2);
		tracks = [ids.slice(0, half), ids.slice(half)].filter(
			(track) => track.length > 0,
		);
	}

	const weights = tracks.flatMap((_, index) => {
		const weight = configuredWeights[index];
		return weight == null ? [] : [weight];
	});
	return [
		{
			op: "realizeLayout",
			space: focusedSpace,
			target: { kind: "columns", tracks, weights },
		},
	];
}
