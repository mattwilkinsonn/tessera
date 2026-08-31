// snap (T4): a pure port — reshape the FOCUSED space's current
// tiled leaves into a target column layout, in place, by present left→right
// (frame.x) visual order rather than profile membership.
//
// DEVIATION FROM DESIGN SKETCH: the sketch wrote `snapPlan(mode, world)`. A pure
// function additionally needs (a) `profile` for the 3col ratios
// (COL3_ROOT_RATIO/COL3_INNER_RATIO) and (b) an explicit `focusedSpace`
// because `WorldSnapshot` carries no focus marker and is a frozen T3 contract we
// must not widen. Signature is `snapPlan(profile, world, focusedSpace, mode)`.

import type { Profile } from "../config/types.ts";
import type { SpaceId } from "../driver/types.ts";
import type { PlanOp } from "./plan.ts";
import type { WorldSnapshot } from "./world.ts";

export type SnapMode = "3col" | "50-50" | "columns";

export function snapPlan(
	profile: Profile,
	world: WorldSnapshot,
	focusedSpace: SpaceId,
	mode: SnapMode,
): PlanOp[] {
	// Ordered ids of the current tiled leaves, by x = visual order.
	const ids = world.windows
		.filter((w) => w.spaceId === focusedSpace && !w.floating && !w.minimized)
		.sort((a, b) => a.frame.x - b.frame.x)
		.map((w) => w.id);

	// No tiled leaves → nothing to reshape.
	if (ids.length === 0) {
		return [];
	}

	// "columns" mode — and any mode that is not 3col/50-50 — is the balance
	// catch-all.
	if (mode !== "3col" && mode !== "50-50") {
		return [{ op: "balanceSpace", space: focusedSpace }];
	}

	if (mode === "3col") {
		// First three leaves become the three columns; any beyond stack on col 3.
		const columns: number[][] = [];
		const col1 = ids[0];
		if (col1 !== undefined) {
			columns.push([col1]);
		}
		const col2 = ids[1];
		if (col2 !== undefined) {
			columns.push([col2]);
		}
		const col3 = ids.slice(2);
		if (col3.length > 0) {
			columns.push(col3);
		}
		return [
			{
				op: "realizeLayout",
				space: focusedSpace,
				target: {
					kind: "3col",
					columns,
					ratios: {
						root: profile.ratios.col3Root,
						inner: profile.ratios.col3Inner,
					},
				},
			},
		];
	}

	// 50-50: split leaves into two halves; each half is one stacked column.
	const half = Math.floor((ids.length + 1) / 2);
	const left = ids.slice(0, half);
	const right = ids.slice(half);
	const columns: number[][] = [];
	if (left.length > 0) {
		columns.push(left);
	}
	if (right.length > 0) {
		columns.push(right);
	}
	return [
		{
			op: "realizeLayout",
			space: focusedSpace,
			target: { kind: "2col", columns },
		},
	];
}
