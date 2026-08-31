// The two debounce waiters that gate the yabai event handlers, ported from
//and. macOS fires bursts of events —
// a monitor plug is a 10-30s display storm, an app launch is several window
// events — and running the full layout cascade on every one force-moves windows
// against a target that is still moving. Instead each event only STAMPS a "last
// seen" epoch second (`recordEvent`) and a single background waiter runs the
// real work ONCE, after events have been quiet for N seconds.
//
// THE load-bearing invariant:
// the `actedOn` stamp value is captured INSIDE the quiet poll, at the instant
// the quiet condition passes — never re-read after the poll breaks. Re-reading
// the stamp after the break reopens a race where an event landing between the
// break and the read silently re-anchors the waiter to a still-settling window
// set. `runWaiter` therefore captures `actedOn` in the poll and threads that
// exact value through the work callback and the mid-work re-check; the settle
// DECISION is factored pure (`isQuiet` / `isFlexQuiet`) and the clock is
// injected, so tests are deterministic WITHOUT reordering that capture.

import {
	DISPLAY_QUIET_SECS,
	DISPLAY_STAMP,
	DISPLAY_WAITER_LOCK,
	FLEX_QUIET_SECS,
	FLEX_STAMP,
	FLEX_WAITER_LOCK,
	SIGNAL_GUARD,
} from "./constants.ts";
import { signalsSuppressed } from "./guard.ts";
import { acquireLock } from "./locks.ts";
import { nudgeSketchybar } from "./notify.ts";
import { readStamp, writeStamp } from "./stamp.ts";

/** Poll cadence of the quiet loop (`sleep 1`). */
const POLL_MS = 1000;

/**
 * Pure quiet predicate: has it been at least `quietSecs` since `stamp`?
 * `now - stamp >= quietSecs` — the `-ge` in/
 *so age exactly equal to the window counts as quiet.
 * `stamp` is the epoch second of the last event (0 for a missing stamp, the
 * bash `cat … || echo 0` sentinel).
 */
export function isQuiet(
	now: number,
	stamp: number,
	quietSecs: number,
): boolean {
	return now - stamp >= quietSecs;
}

/**
 * The flex waiter's combined gate: the flex rebuild
 * fires only once BOTH its own stamp is quiet for `FLEX_QUIET_SECS` AND the
 * display stamp is quiet for `DISPLAY_QUIET_SECS` (H7) — a flex rebuild must
 * never fire mid display-settle. H7 gates on the DISPLAY window (3s), not the
 * shorter flex window (2s), or it would fire in the [+2s,+3s] gap while the
 * display cascade is still debouncing.
 */
export function isFlexQuiet(
	now: number,
	ownStamp: number,
	displayStamp: number,
): boolean {
	return (
		isQuiet(now, ownStamp, FLEX_QUIET_SECS) &&
		isQuiet(now, displayStamp, DISPLAY_QUIET_SECS)
	);
}

/** Record an event: stamp `stampPath` "now" (`date +%s >"$STAMP"`). The event
 * commands (`tess display-event` / `tess flex-event`) call this before launching the
 * waiter, so the waiter always observes the latest event. */
export function recordEvent(stampPath: string): void {
	writeStamp(stampPath);
}

/** What a work callback tells `runWaiter` to do next. */
export type WaiterStep =
	/** work ran cleanly — re-check the stamp, loop if a fresh event landed. */
	| "settled"
	/** H2: re-stamp own stamp and re-loop (guard held / converge contended). */
	| "restamp"
	/** stop the waiter immediately without the settle re-check. */
	| "stop";

/** Injected clock + sleep + stamp writer, so tests drive the waiter without real
 * time and every re-stamp respects the SAME injected clock. */
export interface WaiterDeps {
	/** Epoch seconds "now". */
	now(): number;
	/** Sleep `ms` between polls. */
	sleep(ms: number): Promise<void>;
	/** Record "now" to a stamp path (the H2 re-stamp forcing a re-loop). */
	stamp(path: string): void;
}

const defaultDeps: WaiterDeps = {
	now: () => Math.floor(Date.now() / 1000),
	sleep: (ms) => {
		const { promise, resolve } = Promise.withResolvers<void>();
		setTimeout(resolve, ms);
		return promise;
	},
	stamp: writeStamp,
};

/** The generic waiter loop shared by the display and flex waiters. */
export interface RunWaiterConfig {
	/** The waiter's mkdir-lock dir (stale-PID reclaim variant). */
	waiterLock: string;
	/** The event stamp this waiter debounces. */
	stampPath: string;
	/** Quiet predicate evaluated each poll: `(now, ownStamp) => quiet?`. The flex
	 * gate closes over the display stamp here (H7). */
	quiet(now: number, ownStamp: number): boolean;
	/** The real work, run once quiet; returns the next `WaiterStep`. Handed the
	 * `actedOn` captured at the quiet instant. */
	work(actedOn: number): Promise<WaiterStep> | WaiterStep;
}

/**
 * Acquire the waiter lock and, if we won it, debounce-loop until settled.
 * Returns `true` if we ran (won the lock), `false` if a LIVE waiter already
 * holds it — that waiter will observe the stamp our caller just recorded, so
 * this invocation is accounted for and exits.
 *
 * `actedOn` is captured in the inner poll at the instant `quiet` passes and is
 * NEVER re-read after the poll breaks — the invariant named in the file header.
 */
export async function runWaiter(
	config: RunWaiterConfig,
	deps: WaiterDeps = defaultDeps,
): Promise<boolean> {
	const lock = acquireLock(config.waiterLock);
	if (lock == null) {
		return false; // live waiter holds it; it will observe our stamp
	}
	try {
		for (;;) {
			// Inner quiet poll. Read the own stamp fresh each iteration and keep
			// the value that satisfies `quiet` as `actedOn` — capturing here,
			// before `work`, is the load-bearing invariant (never re-read after
			// the break/).
			let actedOn = readStamp(config.stampPath) ?? 0;
			while (!config.quiet(deps.now(), actedOn)) {
				await deps.sleep(POLL_MS);
				actedOn = readStamp(config.stampPath) ?? 0;
			}

			const step = await config.work(actedOn);
			if (step === "stop") {
				break;
			}
			if (step === "restamp") {
				deps.stamp(config.stampPath);
				continue;
			}

			// "settled": re-check for a fresh event that landed mid-work. If the
			// stamp still equals the value we acted on, nothing moved — settle.
			// Otherwise re-loop against the new event.
			const latest = readStamp(config.stampPath) ?? 0;
			if (latest === actedOn) {
				break;
			}
		}
	} finally {
		lock.release();
	}
	return true;
}

/** The display cascade's outcome. It runs display-setup → rules → apply-or-laptop
 * itself (router-supplied, it needs the driver); it only tells the waiter whether
 * the laptop path lost its lock (`"restamp"`) or settled. */
export type DisplayCascadeStep = "settled" | "restamp";

export interface DisplayWaiterOptions {
	/** The full display cascade; returns `"restamp"` when the laptop path was
	 * contended (its snapshot may be stale), else `"settled"`. */
	cascade(): Promise<DisplayCascadeStep> | DisplayCascadeStep;
	/** Overridable for tests (defaults to the shared /tmp paths). */
	stampPath?: string;
	waiterLock?: string;
	/** SketchyBar re-latch nudge (defaults to the real one); injected in tests. */
	nudge?: (event: string) => Promise<void>;
	deps?: WaiterDeps;
}

/**
 * The display-event waiter. Debounces on `DISPLAY_STAMP`
 * with a `DISPLAY_QUIET_SECS` window, runs the cascade, and — only if it was the
 * waiter that ran (won the lock) — nudges SketchyBar to re-latch ONCE after the
 * topology settled, never mid-loop.
 */
export async function runDisplayWaiter(
	options: DisplayWaiterOptions,
): Promise<void> {
	const stampPath = options.stampPath ?? DISPLAY_STAMP;
	const nudge = options.nudge ?? nudgeSketchybar;
	const ran = await runWaiter(
		{
			waiterLock: options.waiterLock ?? DISPLAY_WAITER_LOCK,
			stampPath,
			quiet: (now, ownStamp) => isQuiet(now, ownStamp, DISPLAY_QUIET_SECS),
			work: () => options.cascade(),
		},
		options.deps,
	);
	if (ran) {
		await nudge("display_relatch");
	}
}

export interface FlexWaiterOptions {
	/** Connected-display count — the laptop-only self-guard: with a monitor
	 * attached, desk mode owns the layout and this event is irrelevant. */
	displayCount(): Promise<number> | number;
	/** Converge the flex grid; `false` when it lost its lock to a concurrent run
	 * (contended, its snapshot may be stale — H2). */
	converge(): Promise<boolean> | boolean;
	/** Overridable for tests (default to the shared /tmp paths). */
	stampPath?: string;
	displayStampPath?: string;
	waiterLock?: string;
	guardPath?: string;
	deps?: WaiterDeps;
}

/**
 * The flex-event waiter. Debounces on `FLEX_STAMP` with
 * a `FLEX_QUIET_SECS` window AND the H7 display-quiet gate, then:
 * - stops if a monitor is attached (not laptop-only, `:99`);
 * - H2: if a manual apply/laptop holds the signal guard, re-stamps and
 * re-waits — NEVER converges on top of the in-flight rearrange (`:105-108`);
 * - H2: if the converge was contended, re-stamps and re-loops so the swallowed
 * rebuild is retried, not silently lost (`:114-117`).
 */
export async function runFlexWaiter(options: FlexWaiterOptions): Promise<void> {
	const stampPath = options.stampPath ?? FLEX_STAMP;
	const displayStampPath = options.displayStampPath ?? DISPLAY_STAMP;
	const guardPath = options.guardPath ?? SIGNAL_GUARD;
	const deps = options.deps ?? defaultDeps;
	await runWaiter(
		{
			waiterLock: options.waiterLock ?? FLEX_WAITER_LOCK,
			stampPath,
			// H7: gate on BOTH the own stamp and the live display stamp, read
			// fresh each poll.
			quiet: (now, ownStamp) =>
				isFlexQuiet(now, ownStamp, readStamp(displayStampPath) ?? 0),
			work: async () => {
				const ndisp = await options.displayCount();
				if (ndisp !== 1) {
					return "stop"; // monitor attached — desk mode owns it (:99)
				}
				if (signalsSuppressed(guardPath, deps.now())) {
					return "restamp"; // H2: never converge on a held guard (:105-108)
				}
				const ok = await options.converge();
				if (!ok) {
					return "restamp"; // H2: contended converge, retry (:114-117)
				}
				return "settled";
			},
		},
		deps,
	);
}
