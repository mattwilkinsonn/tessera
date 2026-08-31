// Layer 3 — the effectful executor around the pure engine.
//
// The ONE place a plan op becomes a driver call, and the ONE place the
// query→step→execute converge loop turns. Everything below the engine's pure
// planners lives here: the planners return values (a flat `PlanOp[]` from
// desk/snap, or one `ConvergeAction` per turn from the laptop converger), and
// this module realizes them against a `WmDriver`. It is driver-agnostic — it
// imports the `WmDriver` interface and the plan/converge vocabulary only, never
// a concrete driver — so its tests drive the committed `FakeDriver`.

import type { WmDriver } from "./driver/types.ts";
import type { ConvergeAction } from "./engine/laptop.ts";
import type { PlanOp } from "./engine/plan.ts";
import type { WorldSnapshot } from "./engine/world.ts";

/**
 * Hard cap on converge iterations. The laptop converger is designed to reach a
 * fixed point in a bounded number of world-driven turns; a converger bug that
 * never returns `{ done }` must fail LOUD (throw), not hang.
 */
const CONVERGE_ITERATION_CAP = 200;

/**
 * Realize one plan op as one logical operation — usually a single driver call,
 * but a few ops are a small fixed sequence (createSpace = create + label;
 * rehomeAndDestroy = N moveWindowToSpace + destroy). `PlanOp` is a superset of
 * `ConvergeAction`, so this same mapping backs both {@link runPlan} and
 * {@link runConverge}. The `switch` is exhaustive with a `never` default:
 * a new op kind forces a compile error here.
 */
async function applyOp(driver: WmDriver, op: PlanOp): Promise<void> {
	switch (op.op) {
		case "relabelHome":
			await driver.labelSpace(op.homeSpace, op.label);
			return;
		case "createSpace": {
			// Create then label — the design's create+label pair. yabai's `--create`
			// returns the fresh SpaceId (or null if the addSpace pointer is dead);
			// only label a space that actually came into being.
			const id = await driver.createSpace(op.displayIdx);
			if (id != null) {
				await driver.labelSpace(id, op.label);
			}
			return;
		}
		case "moveWindow":
			await driver.moveWindowToSpace(op.windowId, op.toSpace);
			return;
		case "rehomeAndDestroy": {
			// Re-home residual windows to the chosen home space, THEN destroy — the
			// converger's teardown that must not lose windows to yabai's auto-relocate.
			const residual = await driver.queryWindowsOnSpace(op.staleSpace);
			for (const w of residual) {
				await driver.moveWindowToSpace(w.id, op.homeSpace);
			}
			await driver.destroySpace(op.staleSpace);
			return;
		}
		case "moveSpace":
			await driver.moveSpaceToIndex(op.space, op.toIndex);
			return;
		case "setLayout":
			await driver.setSpaceLayout(op.space, op.layout);
			return;
		case "destroySpace":
			await driver.destroySpace(op.space);
			return;
		case "realizeLayout":
			await driver.realizeSpaceLayout(op.space, op.target);
			return;
		case "balanceSpace":
			await driver.balanceSpace(op.space);
			return;
		default: {
			const _exhaustive: never = op;
			throw new Error(`unhandled plan op: ${JSON.stringify(_exhaustive)}`);
		}
	}
}

/**
 * Execute a flat `PlanOp[]` (from `desk.ts` / `snap.ts`) in order, mapping each
 * op to exactly one driver effect. The ops are order-independent by construction
 * (every space addressed by its stable `SpaceId`), so a linear walk suffices.
 *
 * `onOp` is an optional per-op hook run BEFORE each op: `tess apply` uses it to
 * re-stamp the signal guard between ops so a long desk converge (per-anchor
 * settle sleeps) cannot outrun GUARD_TTL_SECS mid-run — the coordination
 * equivalent of the bash's phase-boundary `suppress_signals`. It never
 * affects which ops run or their order.
 */
export async function runPlan(
	driver: WmDriver,
	plan: readonly PlanOp[],
	onOp?: (op: PlanOp) => void,
): Promise<void> {
	for (const op of plan) {
		onOp?.(op);
		await applyOp(driver, op);
	}
}

/** The result of one converge step: a corrective action, or done — both carry the threaded state. */
export type ConvergeStepResult<S> =
	| { readonly done: true; readonly state: S }
	| { readonly action: ConvergeAction; readonly state: S };

/** A converger step function: pure over `(world, state)`, returning the next step. */
export type ConvergeStepFn<S> = (
	world: WorldSnapshot,
	state: S,
) => ConvergeStepResult<S>;

/** Capture the current world from the driver (the fresh snapshot every converge turn reads). */
async function snapshot(driver: WmDriver): Promise<WorldSnapshot> {
	const [windows, spaces, displays] = await Promise.all([
		driver.queryWindows(),
		driver.querySpaces(),
		driver.queryDisplays(),
	]);
	return { windows, spaces, displays };
}

/**
 * The laptop converger's query→step→execute loop: build a fresh world, ask the
 * step function for the single next corrective action, realize it via the same
 * op→driver mapping as {@link runPlan}, thread the returned state, and re-query.
 * Returns the final threaded state (the caller persists `state.toPersist`; the
 * flex-order file effect is a T6 module, NOT this loop's concern).
 *
 * A converger that never returns `{ done }` throws once the hard iteration cap
 * is exceeded — a converger bug fails loud rather than hanging.
 */
export async function runConverge<S>(
	driver: WmDriver,
	stepFn: ConvergeStepFn<S>,
	initialState: S,
): Promise<S> {
	let state = initialState;
	for (let i = 0; i < CONVERGE_ITERATION_CAP; i++) {
		const world = await snapshot(driver);
		const step = stepFn(world, state);
		if ("done" in step) {
			return step.state;
		}
		await applyOp(driver, step.action);
		state = step.state;
	}
	throw new Error(
		`runConverge exceeded ${CONVERGE_ITERATION_CAP} iterations without reaching a fixed point — non-terminating converger`,
	);
}
