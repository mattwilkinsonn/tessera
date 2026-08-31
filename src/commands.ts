// Layer 4 — the `tess` subcommand surface.
//
// One exported async function per `tess` subcommand. Every command takes its
// `WmDriver` (and any other effect) as a parameter so the router injects the
// live `YabaiDriver` while tests drive the in-memory `FakeDriver`. The pure
// planning lives in `engine/`; the effect wiring (locks, guard, flex-order
// file, sketchybar nudge) lives in `effects/`; this module is the thin
// composition seam between them, a faithful port of the yabai shell scripts
// under `personal/matt/nix/dotfiles/yabai/`.
//
// Injectable paths: the composite commands (`apply`, `laptop`, `init`) take
// optional trailing path params defaulting to the real /tmp + cache constants —
// the same pattern the `effects/` modules use — so the router calls them plainly
// while tests point them at temp dirs and never touch the live machine's state.

import type { DisplayName, Profile } from "./config/types.ts";
import type {
	DisplaySel,
	SpaceId,
	WmDisplay,
	WmDriver,
} from "./driver/types.ts";
import {
	APPLY_LOCK,
	flexOrderPath,
	LAPTOP_LOCK,
	SIGNAL_GUARD,
	SKETCHYBAR_PATH,
} from "./effects/constants.ts";
import { releaseSignals, suppressSignals } from "./effects/guard.ts";
import { acquireLock, acquireLockOrSkip } from "./effects/locks.ts";
import { nudgeSketchybar } from "./effects/notify.ts";
import { readFlexOrder, writeFlexOrder } from "./effects/state.ts";
import { deskPlan } from "./engine/desk.ts";
import { resolveDisplay } from "./engine/display.ts";
import { resolveSlot } from "./engine/focus.ts";
import { initialConvergeState, laptopConvergeStep } from "./engine/laptop.ts";
import { type SnapMode, snapPlan } from "./engine/snap.ts";
import type { WorldSnapshot } from "./engine/world.ts";
import { runConverge, runPlan } from "./exec.ts";

/** Direction for stack-cycle / cycle-display. */
export type CycleDir = "next" | "prev";
/** Resize step direction. */
export type ResizeDir = "grow" | "shrink";

/**
 * Outcome of a laptop converge. `ran` — converged; `contended` — a LIVE lock
 * holder owned the converge lock (nonzero-exit contract;
 * callers re-loop); `skipped` — not laptop-only, or no laptop display (no-op).
 */
export type LaptopResult = "ran" | "contended" | "skipped";

/** Capture the current world from the driver (the snapshot the engine reasons over). */
async function worldSnapshot(driver: WmDriver): Promise<WorldSnapshot> {
	const [windows, spaces, displays] = await Promise.all([
		driver.queryWindows(),
		driver.querySpaces(),
		driver.queryDisplays(),
	]);
	return { windows, spaces, displays };
}

/**
 * 1-based mission-control index of a space in the display-ordered flatten — the
 * value yabai's `.spaces[]` array holds and `rule --add space=` accepts.
 * Null when the space is absent.
 */
function spaceGlobalIndex(
	id: SpaceId,
	displays: ReadonlyArray<WmDisplay>,
): number | null {
	let idx = 0;
	for (const d of displays) {
		for (const sid of d.spaceIds) {
			idx++;
			if (sid === id) {
				return idx;
			}
		}
	}
	return null;
}

// ── Simple commands — direct driver calls with the bash fallback chains ──────

/**
 * Focus the window at 1-based numpad slot `n`. `resolveSlot`
 * resolves the slot to a live window id over the world; null → silent no-op.
 */
export async function focusSlot(
	driver: WmDriver,
	profile: Profile,
	n: number,
): Promise<void> {
	const world = await worldSnapshot(driver);
	const id = resolveSlot(profile, world, n);
	if (id == null) {
		return;
	}
	await driver.focusWindow(id);
}

/**
 * Reshape the FOCUSED space's tiled leaves into a column layout.
 * No focused space → no-op.
 */
export async function snap(
	driver: WmDriver,
	profile: Profile,
	mode: SnapMode,
): Promise<void> {
	const focused = await driver.queryFocusedSpace();
	if (focused == null) {
		return;
	}
	const world = await worldSnapshot(driver);
	await runPlan(driver, snapPlan(profile, world, focused.id, mode));
}

/**
 * Cycle focus within the focused window's stack. Falls back
 * to first/last at the stack ends when the primary directional focus fails.
 */
export async function stackCycle(
	driver: WmDriver,
	dir: CycleDir,
): Promise<void> {
	if (dir === "next") {
		if (!(await driver.focusWindowDir("stack.next"))) {
			await driver.focusWindowDir("stack.first");
		}
		return;
	}
	if (!(await driver.focusWindowDir("stack.prev"))) {
		await driver.focusWindowDir("stack.last");
	}
}

/**
 * Grow/shrink the focused window's column. STEP=100: push
 * the right edge out first, else push the left edge in.
 */
export async function resize(driver: WmDriver, dir: ResizeDir): Promise<void> {
	const d = dir === "grow" ? 100 : -100;
	if (!(await driver.resizeWindow("right", d, 0))) {
		await driver.resizeWindow("left", -d, 0);
	}
}

/**
 * Move the focused window to a named display, focus following.
 * The display is resolved by its profile width; absent → silent no-op.
 * No focused window → no-op (nothing to move).
 */
export async function moveDisplay(
	driver: WmDriver,
	profile: Profile,
	name: DisplayName,
): Promise<void> {
	const displays = await driver.queryDisplays();
	const idx = resolveDisplay(profile, name, displays);
	if (idx == null) {
		return;
	}
	const focused = await driver.queryFocusedWindow();
	if (focused == null) {
		return;
	}
	await driver.moveWindowToDisplay(focused.id, idx);
	await driver.focusDisplay(idx);
}

/**
 * Cycle the focused window + focus across displays, wrapping at the ends:
 * next → first, prev → last on the wrap. No focused window → no-op.
 */
export async function cycleDisplay(
	driver: WmDriver,
	dir: CycleDir,
): Promise<void> {
	const focused = await driver.queryFocusedWindow();
	if (focused == null) {
		return;
	}
	const fallback: DisplaySel = dir === "next" ? "first" : "last";
	if (!(await driver.moveWindowToDisplay(focused.id, dir))) {
		await driver.moveWindowToDisplay(focused.id, fallback);
	}
	if (!(await driver.focusDisplay(dir))) {
		await driver.focusDisplay(fallback);
	}
}

/**
 * Reset every BSP split ratio on the focused space to 0.5 —
 * only the tiled (non-floating), non-minimized, visible windows.
 * No focused space → no-op.
 */
export async function resetSplits(driver: WmDriver): Promise<void> {
	const focused = await driver.queryFocusedSpace();
	if (focused == null) {
		return;
	}
	const windows = await driver.queryWindowsOnSpace(focused.id);
	for (const w of windows) {
		if (!w.floating && !w.minimized && w.visible) {
			await driver.setSplitRatio(w.id, 0.5);
		}
	}
}

// Safety cap on the columns flatten loop — a horizontal split flips to vertical
// per toggle, so the loop is monotone, but a driver that never converges must
// not spin forever (mirrors exec.ts CONVERGE_ITERATION_CAP).
const COLUMNS_ITERATION_CAP = 200;

/**
 * Flatten every horizontal split on the focused space to vertical, then balance
 * into equal-width columns. Each toggle flips one parent to
 * vertical; re-query and repeat until none remain. No
 * focused space → no-op.
 */
export async function columns(driver: WmDriver): Promise<void> {
	const focused = await driver.queryFocusedSpace();
	if (focused == null) {
		return;
	}
	for (let i = 0; i < COLUMNS_ITERATION_CAP; i++) {
		const windows = await driver.queryWindowsOnSpace(focused.id);
		const next = windows.find(
			(w) =>
				!w.floating &&
				!w.minimized &&
				w.visible &&
				w.splitType === "horizontal",
		);
		if (next == null) {
			break;
		}
		await driver.toggleSplit(next.id);
	}
	await driver.balanceSpace(focused.id);
}

/** Insert direction for the next-window arm (`tess insert`). */
export type InsertDir = "east" | "west" | "north" | "south" | "stack";
/** Focused-space layout toggle (`tess space`). */
export type SpaceLayout = "bsp" | "stack";

// ── Keybind one-liners with a focus query (`skhdrc` raw-yabai block, Q6) ─────
// The pure passthroughs (`focus`/`swap`/`warp`/`balance`) are one unguarded
// driver call each and stay inline in the router; these three first resolve the
// focused window/space and no-op when there is none, so they carry real logic.

/**
 * Arm the next-window insertion point off the focused window (`tess insert <dir>`,
 *`insert stack` /`insert east`). No focused window → no-op.
 */
export async function insert(driver: WmDriver, dir: InsertDir): Promise<void> {
	const focused = await driver.queryFocusedWindow();
	if (focused == null) {
		return;
	}
	await driver.armInsert(focused.id, dir);
}

/**
 * Toggle float on the focused window in place (`tess toggle-float`).
 * No focused window → no-op.
 */
export async function toggleFloat(driver: WmDriver): Promise<void> {
	const focused = await driver.queryFocusedWindow();
	if (focused == null) {
		return;
	}
	await driver.toggleFloat(focused.id);
}

/**
 * Set the focused space's layout (`tess space bsp|stack`). No
 * focused space → no-op.
 */
export async function spaceLayout(
	driver: WmDriver,
	layout: SpaceLayout,
): Promise<void> {
	const focused = await driver.queryFocusedSpace();
	if (focused == null) {
		return;
	}
	await driver.setSpaceLayout(focused.id, layout);
}

// ── Composite commands — engine + exec + effects ────────────────────────────

/**
 * Lay out every present display to its standard desk layout.
 * Surrender-on-contention lock (a second apply just exits 0);
 * the signal guard is held for the whole apply and
 * re-stamped at each display-phase boundary so a long converge can't outrun
 * GUARD_TTL_SECS; the sketchybar nudge is the one
 * intentional new trigger — a lap-* teardown mutates the space list the bar
 * shows. Lock/guard paths and the nudge are injectable so
 * tests never touch the live machine's /tmp state or real sketchybar.
 */
export async function apply(
	driver: WmDriver,
	profile: Profile,
	lockDir: string = APPLY_LOCK,
	guardPath: string = SIGNAL_GUARD,
	nudge: (event: string) => Promise<void> = nudgeSketchybar,
): Promise<void> {
	const lock = acquireLockOrSkip(lockDir);
	if (lock == null) {
		return;
	}
	try {
		suppressSignals(guardPath);
		const world = await worldSnapshot(driver);
		// Re-stamp the guard before each plan op: a full desk converge carries
		// per-anchor settle sleeps that can outrun GUARD_TTL_SECS, and a lapsed
		// guard would let an auto handler fire on top of this run. The bash
		// re-stamps at each display-phase boundary
		// rather than raise the TTL; re-stamping per op is the same guarantee (each
		// op is far under the TTL) and needs no phase bookkeeping — it's a cheap
		// stamp write.
		await runPlan(driver, deskPlan(profile, world), () =>
			suppressSignals(guardPath),
		);
		await nudge("yabai_spaces_changed");
	} finally {
		releaseSignals(guardPath);
		lock.release();
	}
}

/**
 * Converge the laptop-only workspace onto the pinned-core + flex-tail grid.
 * RECLAIM-variant lock: a LIVE holder → `contended`
 * (nonzero-exit contract callers re-loop on). Runs
 * only when the laptop is the SOLE display. Reads +
 * persists the flex order; sets the home space to `stack`; nudges sketchybar.
 * The signal guard is re-stamped each converge
 * turn so a long grid rebuild can't outrun GUARD_TTL_SECS and let the flex
 * waiter fire on top of it (re-stamps per phase).
 * Flex-order / lock / guard paths and the nudge are injectable so tests never
 * touch the live machine's state or real sketchybar.
 */
export async function laptop(
	driver: WmDriver,
	profile: Profile,
	flexPath: string = flexOrderPath(),
	lockDir: string = LAPTOP_LOCK,
	guardPath: string = SIGNAL_GUARD,
	nudge: (event: string) => Promise<void> = nudgeSketchybar,
): Promise<LaptopResult> {
	const lock = acquireLock(lockDir);
	if (lock == null) {
		return "contended";
	}
	try {
		suppressSignals(guardPath);
		// Guard: only act when the laptop is the sole display.
		const displays = await driver.queryDisplays();
		if (displays.length !== 1) {
			return "skipped";
		}
		const laptopIdx = resolveDisplay(profile, "laptop", displays);
		if (laptopIdx == null) {
			return "skipped";
		}
		const lapDisplay = displays.find((d) => d.idx === laptopIdx);
		const homeSpace = lapDisplay?.spaceIds[0];
		if (homeSpace == null) {
			return "skipped";
		}
		const persisted = readFlexOrder(flexPath);
		const finalState = await runConverge(
			driver,
			(world, state) => {
				// Re-stamp the guard each turn: a many-window grid rebuild is dozens
				// of driver calls with settle sleeps and can outrun GUARD_TTL_SECS; a
				// lapsed guard would let a flex-event converge on top of this run
				// (re-stamps per phase rather than raise the
				// TTL). Each turn is far under the TTL.
				suppressSignals(guardPath);
				const step = laptopConvergeStep(profile, world, state);
				// laptopConvergeStep's `{ done: true }` carries no state; thread the
				// incoming state through so runConverge returns it (and its toPersist).
				return "done" in step ? { done: true, state } : step;
			},
			initialConvergeState(homeSpace, persisted),
		);
		writeFlexOrder(finalState.toPersist, flexPath);
		// The home space keeps its stack layout for the catch-all windows
		// (the converger asserts it too — idempotent).
		await driver.setSpaceLayout(homeSpace, "stack");
		await nudge("yabai_spaces_changed");
		return "ran";
	} finally {
		releaseSignals(guardPath);
		lock.release();
	}
}

/**
 * Re-apply the per-app arrival rules. Removes every `auto:`-prefixed
 * rule, re-adds the ported set (catch-all default-laptop-stack FIRST so the
 * specific rules override it), then applies to open windows.
 * Display/space indexes are resolved live by profile width. No-op when the
 * backend has no rules capability.
 */
export async function rules(driver: WmDriver, profile: Profile): Promise<void> {
	const ops = driver.rules;
	if (ops == null) {
		return;
	}
	// 1) Remove any rules we previously added.
	const existing = await ops.list();
	for (const r of existing) {
		if (r.label.startsWith("auto:")) {
			await ops.remove(r.label);
		}
	}

	const displays = await driver.queryDisplays();
	const g9 = resolveDisplay(profile, "g9", displays);
	const aw = resolveDisplay(profile, "aw", displays);
	const laptopIdx = resolveDisplay(profile, "laptop", displays);

	// Laptop stack space index — the catch-all target.
	let laptopStackSpaceIdx: number | null = null;
	if (laptopIdx != null) {
		const lapDisplay = displays.find((d) => d.idx === laptopIdx);
		const home = lapDisplay?.spaceIds[0];
		if (home != null) {
			laptopStackSpaceIdx = spaceGlobalIndex(home, displays);
		}
	}

	// Catch-all FIRST.
	if (laptopStackSpaceIdx != null) {
		await ops.add({
			label: "auto:default-laptop-stack",
			app: "^.*$",
			spaceIdx: laptopStackSpaceIdx,
		});
	}
	// G9.
	if (g9 != null) {
		await ops.add({ label: "auto:code", app: "^Code$", displayIdx: g9 });
	}
	// Alienware.
	if (aw != null) {
		await ops.add({ label: "auto:akiflow", app: "^Akiflow$", displayIdx: aw });
		await ops.add({ label: "auto:linear", app: "^Linear$", displayIdx: aw });
	}
	// Always float.
	await ops.add({ label: "auto:finder", app: "^Finder$", manage: false });
	await ops.add({
		label: "auto:akiflow-dialog",
		app: "^Akiflow$",
		subrole: "AXDialog",
		manage: false,
	});

	// Re-apply to currently-open windows.
	await ops.apply();
}

/**
 * Apply per-display layout defaults: the laptop's first
 * space → stack layout, other displays left at bsp. No-op if no laptop display.
 */
export async function displaySetup(
	driver: WmDriver,
	profile: Profile,
): Promise<void> {
	const displays = await driver.queryDisplays();
	const laptopIdx = resolveDisplay(profile, "laptop", displays);
	if (laptopIdx == null) {
		return;
	}
	const lapDisplay = displays.find((d) => d.idx === laptopIdx);
	const home = lapDisplay?.spaceIds[0];
	if (home == null) {
		return;
	}
	await driver.setSpaceLayout(home, "stack");
}

/**
 * The full display-change cascade: refresh per-display
 * defaults + arrival rules, then reclaim the layout for the current display set —
 * laptop-only → the per-space grid (`laptop`), else the desk layout (`apply`).
 * Surfaces `laptop`'s contention so the debounce waiter can re-loop (H2). Kept
 * here so the debounce slice can wire `tess display-event = runWaiter(this)`.
 */
export async function runDisplayCascade(
	driver: WmDriver,
	profile: Profile,
	nudge: (event: string) => Promise<void> = nudgeSketchybar,
): Promise<LaptopResult | "applied"> {
	await displaySetup(driver, profile);
	await rules(driver, profile);
	const displays = await driver.queryDisplays();
	if (displays.length <= 1) {
		return laptop(
			driver,
			profile,
			flexOrderPath(),
			LAPTOP_LOCK,
			SIGNAL_GUARD,
			nudge,
		);
	}
	await apply(driver, profile, APPLY_LOCK, SIGNAL_GUARD, nudge);
	return "applied";
}

/**
 * The live flex-space converge callback: reconverge the
 * laptop grid. Identical to `laptop` — factored as its own export so the debounce
 * slice can wire `tess flex-event = runWaiter(this)` without importing `laptop`
 * under a second name.
 */
export async function runFlexConverge(
	driver: WmDriver,
	profile: Profile,
	nudge: (event: string) => Promise<void> = nudgeSketchybar,
): Promise<LaptopResult> {
	return laptop(
		driver,
		profile,
		flexOrderPath(),
		LAPTOP_LOCK,
		SIGNAL_GUARD,
		nudge,
	);
}

/**
 * The yabai startup body (``): register the signal wirings,
 * then run the startup cascade displaySetup → rules → reclaim. `wmPath` is the
 * absolute `tess` binary path the router supplies; signal `command` args are
 * `argv` string arrays. dock_did_restart is registered by the yabairc shim, NOT
 * here (it loads the scripting addition, ``). No-op registration when
 * the backend has no events capability. Apply lock/guard paths and the sketchybar
 * nudge are injectable so tests never touch the live machine's state or real bar.
 *
 * The reclaim step branches on display count exactly as `runDisplayCascade`
 * (and the pre-port) does: laptop-only (`displays <= 1`)
 * → the per-space grid (`laptop`), else the desk layout (`apply`). A
 * bare `apply` at startup ran the desk teardown (`teardownLabels` + `straySpaces`)
 * on a laptop-only machine, destroying the `lap-*` grid and collapsing every
 * window onto the home space with nothing to rebuild it — the desk path has no
 * laptop grid to lay out, and no display/flex event necessarily follows a
 * startup to trigger the converge.
 */
export async function init(
	driver: WmDriver,
	profile: Profile,
	wmPath: string,
	applyLock: string = APPLY_LOCK,
	guardPath: string = SIGNAL_GUARD,
	nudge: (event: string) => Promise<void> = nudgeSketchybar,
	laptopLock: string = LAPTOP_LOCK,
	flexPath: string = flexOrderPath(),
): Promise<void> {
	const events = driver.events;
	if (events != null) {
		// Display trio → the debounced display cascade.
		for (const event of [
			"display_added",
			"display_removed",
			"display_moved",
		] as const) {
			await events.register(event, [wmPath, "display-event"]);
		}
		// Flex signals → the debounced laptop converge.
		for (const event of [
			"application_launched",
			"application_terminated",
			"window_created",
			"window_destroyed",
		] as const) {
			await events.register(event, [wmPath, "flex-event"]);
		}
		// SketchyBar space/display re-read triggers.
		for (const event of ["space_changed", "display_changed"] as const) {
			await events.register(event, [
				SKETCHYBAR_PATH,
				"--trigger",
				"yabai_spaces_changed",
			]);
		}
	}

	// Startup cascade: per-display defaults + arrival rules, then
	// reclaim the layout for the current display set.
	await displaySetup(driver, profile);
	await rules(driver, profile);
	const displays = await driver.queryDisplays();
	if (displays.length <= 1) {
		await laptop(driver, profile, flexPath, laptopLock, guardPath, nudge);
		return;
	}
	await apply(driver, profile, applyLock, guardPath, nudge);
}
