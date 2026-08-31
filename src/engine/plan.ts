// Layer 2 — ENGINE: the backend-neutral plan vocabulary.
//
// A `PlanOp` is one declarative step an executor (T5/T6) maps to a single
// `WmDriver` call. The desk/snap plan builders (`desk.ts`, `snap.ts`) return a
// flat `PlanOp[]` computed from ONE `WorldSnapshot`; the ops are
// order-independent because every space is addressed by its opaque stable
// `SpaceId` (D1), never a live index that renumbers on destroy.
//
// `PlanOp` is a SUPERSET of the laptop converger's `ConvergeAction`: it reuses
// those six ops verbatim (a desk build relabels a display's home space exactly
// as the converger does) and adds the three desk/snap ops below.

import type { SpaceId, SpaceLayoutTarget } from "../driver/types.ts";
import type { ConvergeAction } from "./laptop.ts";

/**
 * Plain space destroy — the teardown + reap preludes. yabai relocates any
 * windows on the space to an adjacent space on the same display, so nothing
 * is closed. Distinct from `ConvergeAction`'s `rehomeAndDestroy`, which
 * first re-homes windows to a chosen home space: teardown/reap pick no home
 * target (yabai's auto-relocate is correct here). The driver refuses to destroy
 * a display's last space, so a redundant destroy is a safe
 * no-op.
 */
export interface DestroySpaceOp {
	readonly op: "destroySpace";
	readonly space: SpaceId;
}

/**
 * Realize a declarative layout target (D2) — desk builds and snap
 * `3col`/`50-50`. The engine names only
 * the target (kind + resolved column ids + ratios); the driver owns the
 * build recipe and its empirical settle cadence, a
 * yabai-Tahoe workaround a Hyprland driver omits. The driver does NOT clear the
 * space first: the desk plan evacuates foreign windows up front via `moveWindow`
 * ops, so realize builds from the space AS-IS.
 */
export interface RealizeLayoutOp {
	readonly op: "realizeLayout";
	readonly space: SpaceId;
	readonly target: SpaceLayoutTarget;
}

/**
 * Balance the space into equal columns — snap `columns` mode: flatten
 * every nested horizontal split into a flat
 * column row, then equalize. The driver owns the flatten-then-balance recipe;
 * the engine only names the intent.
 */
export interface BalanceSpaceOp {
	readonly op: "balanceSpace";
	readonly space: SpaceId;
}

/**
 * The full backend-neutral plan vocabulary (design): a superset of the
 * laptop converger's `ConvergeAction` plus the desk/snap ops. An executor maps
 * each op to one `WmDriver` call.
 */
export type PlanOp =
	| ConvergeAction
	| DestroySpaceOp
	| RealizeLayoutOp
	| BalanceSpaceOp;
