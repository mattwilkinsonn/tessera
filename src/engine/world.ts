// Layer 2 — ENGINE: the immutable world snapshot.
//
// Every engine planner is a pure function over an immutable `WorldSnapshot`
// (the queried windows/spaces/displays) plus the `Profile` plus any persisted
// state, returning values — never touching the driver, filesystem, or clock
// (design). This is the one shared input shape those planners read.

import type { WmDisplay, WmSpace, WmWindow } from "../driver/types.ts";

/**
 * A point-in-time capture of the window world: the driver query results the
 * engine reasons over (design Interfaces, `{ windows, spaces, displays }`).
 * Read-only — a snapshot is never mutated; a converge step consumes the current
 * snapshot and the executor re-queries a fresh one between steps.
 */
export interface WorldSnapshot {
	readonly windows: readonly WmWindow[];
	readonly spaces: readonly WmSpace[];
	readonly displays: readonly WmDisplay[];
}
