import { afterEach, describe, expect, test } from "bun:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
	isFlexQuiet,
	isQuiet,
	runDisplayWaiter,
	runFlexWaiter,
	runWaiter,
	type WaiterDeps,
} from "./debounce.ts";
import { releaseSignals, suppressSignals } from "./guard.ts";

let root = "";
afterEach(() => {
	if (root !== "") {
		rmSync(root, { recursive: true, force: true });
	}
	root = "";
});

/** A deterministic clock+sleep+stamp: `now` advances by 1s per `sleep`, and
 * `stamp` writes the CURRENT fake second — so the waiter's re-stamp and quiet
 * arithmetic stay in the same injected timeline, no real time involved. */
function fakeClock(start: number): {
	deps: WaiterDeps;
	get(): number;
} {
	let current = start;
	const deps: WaiterDeps = {
		now: () => current,
		sleep: () => {
			current += 1;
			return Promise.resolve();
		},
		stamp: (path) => {
			writeFileSync(path, `${current}\n`);
		},
	};
	return { deps, get: () => current };
}

function writeStampValue(path: string, secs: number): void {
	writeFileSync(path, `${secs}\n`);
}

describe("isQuiet (/ `-ge`)", () => {
	test("age exactly equal to the window counts as quiet", () => {
		expect(isQuiet(103, 100, 3)).toBe(true);
	});

	test("age one second under the window is not quiet", () => {
		expect(isQuiet(102, 100, 3)).toBe(false);
	});
});

describe("isFlexQuiet — H7 combined gate", () => {
	test("own-quiet AND display-quiet → quiet", () => {
		// own age 2 >= FLEX 2, display age 3 >= DISPLAY 3.
		expect(isFlexQuiet(200, 198, 197)).toBe(true);
	});

	test("own-quiet but display NOT quiet → keep waiting (H7)", () => {
		// own age 2 >= FLEX 2, but display age 2 < DISPLAY 3 → mid display-settle.
		expect(isFlexQuiet(200, 198, 198)).toBe(false);
	});

	test("own NOT quiet (even with display quiet) → keep waiting", () => {
		// own age 1 < FLEX 2, display age 10 >= DISPLAY 3.
		expect(isFlexQuiet(200, 199, 190)).toBe(false);
	});
});

describe("runWaiter — lock + settle loop", () => {
	test("returns immediately when a LIVE waiter already holds the lock", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-debounce-"));
		const lockDir = join(root, "waiter.lock");
		const stampPath = join(root, "stamp");
		writeStampValue(stampPath, 100);
		// A live holder: our own pid is alive, so acquireLock surrenders.
		mkdirSync(lockDir);
		writeFileSync(join(lockDir, "pid"), `${process.pid}\n`);

		let workCalls = 0;
		const ran = await runWaiter(
			{
				waiterLock: lockDir,
				stampPath,
				quiet: () => true,
				work: () => {
					workCalls += 1;
					return "settled";
				},
			},
			fakeClock(1000).deps,
		);

		expect(ran).toBe(false);
		expect(workCalls).toBe(0);
	});

	// The named acted_on regression test. An event lands at the instant the quiet
	// condition passes; the value handed to `work` MUST be the one captured
	// INSIDE the poll (S0), not a re-read after the poll broke (which would pick
	// up the just-landed, still-settling S1). A capture-after-loop refactor would
	// hand work S1 on the first call and this test would fail.
	test("captures acted_on inside the poll, then re-loops on the injected event", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-debounce-"));
		const lockDir = join(root, "waiter.lock");
		const stampPath = join(root, "stamp");
		const s0 = 100;
		const s1 = 105;
		writeStampValue(stampPath, s0);

		let injected = false;
		const workActedOn: number[] = [];
		const ran = await runWaiter(
			{
				waiterLock: lockDir,
				stampPath,
				quiet: () => {
					// Fire a fresh event exactly when quiet is first satisfied —
					// between quiet-satisfaction and the work call.
					if (!injected) {
						injected = true;
						writeStampValue(stampPath, s1);
					}
					return true;
				},
				work: (actedOn) => {
					workActedOn.push(actedOn);
					return "settled";
				},
			},
			fakeClock(1000).deps,
		);

		expect(ran).toBe(true);
		// First run used the in-loop capture S0 (not the injected S1); the
		// mid-work re-check then saw S1 != S0 and re-looped, acting on S1.
		expect(workActedOn).toEqual([s0, s1]);
	});
});

describe("runDisplayWaiter", () => {
	test("runs the cascade once when quiet, then nudges sketchybar once", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-debounce-"));
		const stampPath = join(root, "display.stamp");
		const lockDir = join(root, "display.lock");
		writeStampValue(stampPath, 100);

		let cascades = 0;
		const nudges: string[] = [];
		await runDisplayWaiter({
			cascade: () => {
				cascades += 1;
				return "settled";
			},
			stampPath,
			waiterLock: lockDir,
			nudge: async (event) => {
				nudges.push(event);
			},
			deps: fakeClock(1000).deps,
		});

		expect(cascades).toBe(1);
		expect(nudges).toEqual(["display_relatch"]);
	});

	test("re-loops on a mid-cascade event but nudges only once, after settling", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-debounce-"));
		const stampPath = join(root, "display.stamp");
		const lockDir = join(root, "display.lock");
		writeStampValue(stampPath, 100);

		let cascades = 0;
		const nudges: string[] = [];
		await runDisplayWaiter({
			cascade: () => {
				cascades += 1;
				// A fresh event lands during the first cascade (still quiet vs the
				// far-future fake clock, so the re-loop settles at once).
				if (cascades === 1) {
					writeStampValue(stampPath, 500);
				}
				return "settled";
			},
			stampPath,
			waiterLock: lockDir,
			nudge: async (event) => {
				nudges.push(event);
			},
			deps: fakeClock(1000).deps,
		});

		expect(cascades).toBe(2);
		expect(nudges).toEqual(["display_relatch"]);
	});

	test("a live waiter holds the lock → no cascade, no nudge", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-debounce-"));
		const stampPath = join(root, "display.stamp");
		const lockDir = join(root, "display.lock");
		writeStampValue(stampPath, 100);
		mkdirSync(lockDir);
		writeFileSync(join(lockDir, "pid"), `${process.pid}\n`);

		let cascades = 0;
		const nudges: string[] = [];
		await runDisplayWaiter({
			cascade: () => {
				cascades += 1;
				return "settled";
			},
			stampPath,
			waiterLock: lockDir,
			nudge: async (event) => {
				nudges.push(event);
			},
			deps: fakeClock(1000).deps,
		});

		expect(cascades).toBe(0);
		expect(nudges).toEqual([]);
	});
});

describe("runFlexWaiter", () => {
	test("H2: a held signal guard re-stamps and re-waits — converge only runs once the guard clears", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-debounce-"));
		const flexStamp = join(root, "flex.stamp");
		const displayStamp = join(root, "display.stamp");
		const lockDir = join(root, "flex.lock");
		const guardPath = join(root, "guard");
		writeStampValue(flexStamp, 100);
		writeStampValue(displayStamp, 100);
		suppressSignals(guardPath); // guard held

		let displayChecks = 0;
		let convergeCalls = 0;
		await runFlexWaiter({
			displayCount: () => {
				displayChecks += 1;
				// Clear the guard on the third pass, so the first two must re-wait.
				if (displayChecks === 3) {
					releaseSignals(guardPath);
				}
				return 1;
			},
			converge: () => {
				convergeCalls += 1;
				return true;
			},
			stampPath: flexStamp,
			displayStampPath: displayStamp,
			waiterLock: lockDir,
			guardPath,
			deps: fakeClock(1000).deps,
		});

		// Converge never ran while the guard was held; it ran exactly once after
		// it cleared on the third pass.
		expect(displayChecks).toBe(3);
		expect(convergeCalls).toBe(1);
	});

	test("H2: a contended converge (returns false) re-stamps and re-loops until it succeeds", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-debounce-"));
		const flexStamp = join(root, "flex.stamp");
		const displayStamp = join(root, "display.stamp");
		const lockDir = join(root, "flex.lock");
		const guardPath = join(root, "guard"); // absent → never suppressed
		writeStampValue(flexStamp, 100);
		writeStampValue(displayStamp, 100);

		let convergeCalls = 0;
		await runFlexWaiter({
			displayCount: () => 1,
			converge: () => {
				convergeCalls += 1;
				return convergeCalls >= 2; // contended first, succeeds second
			},
			stampPath: flexStamp,
			displayStampPath: displayStamp,
			waiterLock: lockDir,
			guardPath,
			deps: fakeClock(1000).deps,
		});

		expect(convergeCalls).toBe(2);
	});

	test("stops without converging when a monitor is attached (not laptop-only)", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-debounce-"));
		const flexStamp = join(root, "flex.stamp");
		const displayStamp = join(root, "display.stamp");
		const lockDir = join(root, "flex.lock");
		const guardPath = join(root, "guard");
		writeStampValue(flexStamp, 100);
		writeStampValue(displayStamp, 100);

		let convergeCalls = 0;
		await runFlexWaiter({
			displayCount: () => 2, // external display present → desk mode owns it
			converge: () => {
				convergeCalls += 1;
				return true;
			},
			stampPath: flexStamp,
			displayStampPath: displayStamp,
			waiterLock: lockDir,
			guardPath,
			deps: fakeClock(1000).deps,
		});

		expect(convergeCalls).toBe(0);
	});
});
