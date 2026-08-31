// Layer 2 — ENGINE: the laptop-mode converger. A faithful port of the
// four-phase laptop converger as a PURE, resumable
// step-planner: no driver / filesystem / clock, no mutation of its inputs, a
// NEW `ConvergeState` returned every call.
//
// ── The executor loop (the seam this planner plugs into) ─────────────────
// The executor (T5, outside this module) drives one corrective step at a time:
//
//   let s = initialConvergeState(homeSpace, persistedFlexOrder);
//   for (;;) {
//     const world = queryWorld();                 // fresh snapshot every turn
//     const r = laptopConvergeStep(profile, world, s);
//     if ("done" in r) break;
//     applyAction(r.action);                       // realize the single step
//     s = r.state;                                 // thread the returned state
//   }
//   persist(s.toPersist);                          // save the flex order file
//
// The planner emits exactly ONE `ConvergeAction` per call (or `{ done: true }`);
// the executor realizes it, RE-QUERIES the world, and calls again with the
// returned state. No `requery` op (re-query is unconditional, every turn) and
// no `settle` op (the laptop converger has no in-phase sleeps in the bash).
//
// ── ConvergeState shape + resume protocol ────────────────────────────────
// State is a plain immutable record threaded across calls (soft-frozen at T3):
//   - `phase`            which of pre → A → B → C → D → layout → done we're in.
//   - `cursor`           phase-local progress index: phase A → index into
//                        `profile.laptopPinned`; phase B → index into the
//                        reconciled flex `order`; phase D → index into
//                        `desiredOrder`. Reset to 0 at each phase boundary.
//   - `placed`           phase D only: count of PRESENT labels already given a
//                        slot, so a genuinely-absent label is skipped without
// consuming a position.
//   - `claimedIds`       the accumulating claimed-window id set. Phase A claims
//                        distinct windows via `ClaimSet` (whose own claimed set
//                        is private); the converger tracks the ids ITSELF here,
//                        then hands the set to `laptopFlexWindows` in phase B so
//                        the flex tail excludes the pinned core.
//   - `targetLabels`     the set of `lap-*` labels this run wants to exist
//                        (pinned ∪ flex); phase C reconciles away any `lap-*`
//                        space NOT in it.
//   - `desiredOrder`     those same labels in intended display order (pinned
//                        core in `laptopPinned` order, then the flex tail in
//                        stable-append order); phase D positions to match.
//   - `createFailed`     set when a space we asked to create is still absent on
//                        the next look — the scripting-addition addSpace pointer
//                        is dead; abort before the destructive reconcile.
//   - `pendingCreateLabel` the label of the space whose `createSpace` we emitted
//                        last turn, awaiting confirmation in the fresh world.
//   - `homeSpace`        the stable home SpaceId (spaces[0] of the laptop).
//   - `laptopIdx`        the resolved laptop display index (−1 until the `pre`
//                        step resolves it via `resolveDisplay`).
//   - `persistedFlexOrder` the flex-order file contents at converge start.
//   - `toPersist`        the flex order to write back (recomputed in phase B).
//
// Resume is world-driven, not blind replay. A `createSpace` and the following
// `moveWindow` are two turns: the planner emits `createSpace`, and on the NEXT
// call looks for the label in the fresh `world.spaces` — present → emit the
// `moveWindow` (and record the target); still absent → `createFailed`. Phase C
// is fully world-driven (no cursor): each call finds the first stale `lap-*`
// space and emits one `rehomeAndDestroy`, so the executor's destroy + re-query
// surfaces the next one. Under D1 SpaceIds are stable — no action re-derives a
// space by live index — so a re-query never invalidates a threaded id.

import type { Profile } from "../config/types.ts";
import type { SpaceId, WmWindow } from "../driver/types.ts";
import { ClaimSet } from "./claim.ts";
import { resolveDisplay } from "./display.ts";
import { laptopFlexWindows, reconcileFlexOrder } from "./flex.ts";
import type { WorldSnapshot } from "./world.ts";

/**
 * The single backend-neutral corrective step the executor realizes, then
 * re-queries the world and calls {@link laptopConvergeStep} again (D1
 * SpaceId-typed).
 */
export type ConvergeAction =
	| {
			readonly op: "relabelHome";
			readonly homeSpace: SpaceId;
			readonly label: string;
	  }
	| {
			readonly op: "createSpace";
			readonly displayIdx: number;
			readonly label: string;
	  }
	| {
			readonly op: "moveWindow";
			readonly windowId: number;
			readonly toSpace: SpaceId;
	  }
	| {
			readonly op: "rehomeAndDestroy";
			readonly staleSpace: SpaceId;
			readonly homeSpace: SpaceId;
	  }
	| {
			readonly op: "moveSpace";
			readonly space: SpaceId;
			readonly toIndex: number;
	  }
	| {
			readonly op: "setLayout";
			readonly space: SpaceId;
			readonly layout: "bsp" | "stack" | "float";
	  };

/** The converge phase (see the module header for the per-phase cursor meaning). */
export type ConvergePhase = "pre" | "A" | "B" | "C" | "D" | "layout" | "done";

/** The immutable planner state threaded across steps (soft-frozen at T3). */
export interface ConvergeState {
	readonly phase: ConvergePhase;
	readonly cursor: number;
	readonly placed: number;
	readonly claimedIds: ReadonlySet<number>;
	readonly targetLabels: ReadonlySet<string>;
	readonly desiredOrder: readonly string[];
	readonly createFailed: boolean;
	readonly pendingCreateLabel: string | null;
	readonly homeSpace: SpaceId;
	readonly laptopIdx: number;
	readonly persistedFlexOrder: readonly string[];
	readonly toPersist: readonly string[];
}

/** The result of one planner step: a corrective action + next state, or done. */
export type ConvergeStep =
	| { action: ConvergeAction; state: ConvergeState }
	| { done: true };

/**
 * Seed a converge (the executor / tests start here). `laptopIdx` is left
 * unresolved (−1) — the `pre` step resolves it from the world via
 * `resolveDisplay`, per the caller's "laptop is the sole display" guard.
 */
export function initialConvergeState(
	homeSpace: SpaceId,
	persistedFlexOrder: readonly string[],
): ConvergeState {
	return {
		phase: "pre",
		cursor: 0,
		placed: 0,
		claimedIds: new Set<number>(),
		targetLabels: new Set<string>(),
		desiredOrder: [],
		createFailed: false,
		pendingCreateLabel: null,
		homeSpace,
		laptopIdx: -1,
		persistedFlexOrder: [...persistedFlexOrder],
		toPersist: [...persistedFlexOrder],
	};
}

/**
 * Compute the occurrence-suffixed labels for the pinned core. A name's 1st
 * textual occurrence is `lap-<name>`,
 * its k-th is `lap-<name>-<k>`. The counter follows TEXTUAL occurrence, not
 * successful claims — a skipped (no-window) occurrence still advances it — so a
 * label is purely a function of the pinned list and index.
 */
function pinnedLabels(profile: Profile): string[] {
	const seen = new Map<string, number>();
	const labels: string[] = [];
	for (const name of profile.laptopPinned) {
		const n = (seen.get(name) ?? 0) + 1;
		seen.set(name, n);
		labels.push(n === 1 ? `lap-${name}` : `lap-${name}-${n}`);
	}
	return labels;
}

/**
 * Replay the phase-A claims against the CURRENT world (`
 * 144-146`). Deterministic and pure: `ClaimSet` hands out distinct windows in
 * pinned order preferring the laptop display; window-set membership is stable
 * across a converge (moves don't change app/title/min/float), so replaying
 * every step yields the same per-index id and the same accumulated claimed set
 * the converger passes to phase B.
 */
function planPhaseAClaims(
	profile: Profile,
	world: WorldSnapshot,
	laptopIdx: number,
): { ids: (number | null)[]; claimed: Set<number> } {
	const cs = new ClaimSet(profile);
	const claimed = new Set<number>();
	const windows: WmWindow[] = [...world.windows];
	const ids: (number | null)[] = [];
	for (const name of profile.laptopPinned) {
		const id = cs.claim(windows, name, laptopIdx);
		ids.push(id);
		if (id != null) {
			claimed.add(id);
		}
	}
	return { ids, claimed };
}

/**
 * The 1-based global position of a space in the display-ordered flatten of all
 * spaces (the `space --move <index>` domain).
 * Returns −1 when absent (the home space is always present under the caller's
 * guard). With the laptop as sole display this is the space's position in the
 * laptop display's ordered `spaceIds`.
 */
function globalIndex(id: SpaceId, world: WorldSnapshot): number {
	let idx = 0;
	for (const d of world.displays) {
		for (const sid of d.spaceIds) {
			idx++;
			if (sid === id) {
				return idx;
			}
		}
	}
	return -1;
}

/**
 * Plan one corrective step of the laptop converge, PURE
 * over its inputs. See the module header for the executor loop, the state
 * shape, and the resume protocol.
 */
export function laptopConvergeStep(
	profile: Profile,
	world: WorldSnapshot,
	state: ConvergeState,
): ConvergeStep {
	let s = state;
	// Phase transitions consume no executor turn — loop until we emit an action
	// or reach `{ done: true }`.
	for (;;) {
		// Dead scripting-addition abort or terminal:
		// leave the grid intact.
		if (s.createFailed || s.phase === "done") {
			return { done: true };
		}

		// ── Pre (once): re-assert the home label ──
		if (s.phase === "pre") {
			const laptopIdx = resolveDisplay(profile, "laptop", world.displays);
			if (laptopIdx == null) {
				return { done: true };
			}
			const next: ConvergeState = { ...s, phase: "A", cursor: 0, laptopIdx };
			return {
				action: { op: "relabelHome", homeSpace: s.homeSpace, label: "laptop" },
				state: next,
			};
		}

		// ── Phase A — pinned core (D7) ──────────
		if (s.phase === "A") {
			const labels = pinnedLabels(profile);
			const { ids, claimed } = planPhaseAClaims(profile, world, s.laptopIdx);
			let cursor = s.cursor;
			let deadCreate = false;
			while (cursor < profile.laptopPinned.length) {
				const label = labels[cursor];
				const id = ids[cursor];
				// D7: a pinned entry with no live window is skipped entirely — no
				// placeholder space.
				if (label == null || id == null) {
					cursor++;
					continue;
				}
				const existing = world.spaces.find((sp) => sp.label === label);
				if (existing != null) {
					const next: ConvergeState = {
						...s,
						cursor: cursor + 1,
						claimedIds: claimed,
						targetLabels: new Set([...s.targetLabels, label]),
						desiredOrder: [...s.desiredOrder, label],
						pendingCreateLabel: null,
					};
					return {
						action: { op: "moveWindow", windowId: id, toSpace: existing.id },
						state: next,
					};
				}
				// Space absent. If we already emitted its createSpace last turn and
				// it is STILL absent, the addSpace pointer is dead → CREATE_FAILED.
				if (s.pendingCreateLabel === label) {
					deadCreate = true;
					break;
				}
				const next: ConvergeState = {
					...s,
					cursor,
					claimedIds: claimed,
					pendingCreateLabel: label,
				};
				return {
					action: { op: "createSpace", displayIdx: s.laptopIdx, label },
					state: next,
				};
			}
			if (deadCreate) {
				s = { ...s, createFailed: true };
				continue;
			}
			s = {
				...s,
				phase: "B",
				cursor: 0,
				claimedIds: claimed,
				pendingCreateLabel: null,
			};
			continue;
		}

		// ── Phase B — flex tail (D9/D10) ────────
		if (s.phase === "B") {
			// Compose the stable-append render order the bash's laptop_flex_order
			// gave; `toPersist` is threaded out for the executor to save.
			const rows = laptopFlexWindows(profile, world.windows, s.claimedIds);
			const bareInIdOrder = rows.map((r) => r.label);
			const { order, toPersist } = reconcileFlexOrder(
				[...s.persistedFlexOrder],
				bareInIdOrder,
			);
			// Map each bare slug to its window id (rows are the id-order source).
			const idOf = new Map<string, number>();
			for (const r of rows) {
				idOf.set(r.label, r.id);
			}
			let cursor = s.cursor;
			let deadCreate = false;
			while (cursor < order.length) {
				const bare = order[cursor];
				if (bare == null) {
					cursor++;
					continue;
				}
				const id = idOf.get(bare);
				const label = `lap-${bare}`;
				// A slug retained in the reconciled order but not present this run
				// (id-less) contributes no space — skip it.
				if (id == null) {
					cursor++;
					continue;
				}
				const existing = world.spaces.find((sp) => sp.label === label);
				if (existing != null) {
					const next: ConvergeState = {
						...s,
						cursor: cursor + 1,
						toPersist,
						targetLabels: new Set([...s.targetLabels, label]),
						desiredOrder: [...s.desiredOrder, label],
						pendingCreateLabel: null,
					};
					return {
						action: { op: "moveWindow", windowId: id, toSpace: existing.id },
						state: next,
					};
				}
				if (s.pendingCreateLabel === label) {
					deadCreate = true;
					break;
				}
				const next: ConvergeState = {
					...s,
					cursor,
					toPersist,
					pendingCreateLabel: label,
				};
				return {
					action: { op: "createSpace", displayIdx: s.laptopIdx, label },
					state: next,
				};
			}
			if (deadCreate) {
				s = { ...s, createFailed: true, toPersist };
				continue;
			}
			s = {
				...s,
				phase: "C",
				cursor: 0,
				toPersist,
				pendingCreateLabel: null,
			};
			continue;
		}

		// ── CREATE_FAILED gate ─────────────────
		// (Handled inline above by setting `createFailed`, caught at loop top:
		// phases C and D never run.)

		// ── Phase C — reconcile (H5) ────────────
		if (s.phase === "C") {
			// World-driven: find the first lap-* space no longer targeted and emit
			// one rehomeAndDestroy; the executor re-homes its residual windows
			// (unfiltered) then destroys it, and the re-query surfaces the next.
			const stale = world.spaces.find(
				(sp) => sp.label.startsWith("lap-") && !s.targetLabels.has(sp.label),
			);
			if (stale != null) {
				return {
					action: {
						op: "rehomeAndDestroy",
						staleSpace: stale.id,
						homeSpace: s.homeSpace,
					},
					state: s,
				};
			}
			s = { ...s, phase: "D", cursor: 0, placed: 0 };
			continue;
		}

		// ── Phase D — order ────────────────────
		if (s.phase === "D") {
			// Single left-to-right insertion pass: desiredOrder[i] goes to the
			// slot home_pos + 1 + placed. A genuinely-absent label is skipped
			// WITHOUT consuming a slot.
			const homePos = globalIndex(s.homeSpace, world);
			let cursor = s.cursor;
			let placed = s.placed;
			while (cursor < s.desiredOrder.length) {
				const label = s.desiredOrder[cursor];
				if (label == null) {
					cursor++;
					continue;
				}
				const sp = world.spaces.find((x) => x.label === label);
				if (sp == null) {
					cursor++;
					continue;
				}
				const target = homePos + 1 + placed;
				const cur = globalIndex(sp.id, world);
				placed++;
				cursor++;
				// Already in place → no move.
				if (cur === target) {
					continue;
				}
				const next: ConvergeState = { ...s, cursor, placed };
				return {
					action: { op: "moveSpace", space: sp.id, toIndex: target },
					state: next,
				};
			}
			s = { ...s, phase: "layout", cursor, placed };
			continue;
		}

		// ── Home layout then done ──────────────────
		if (s.phase === "layout") {
			const next: ConvergeState = { ...s, phase: "done" };
			return {
				action: { op: "setLayout", space: s.homeSpace, layout: "stack" },
				state: next,
			};
		}

		return { done: true };
	}
}
