import { afterEach, describe, expect, test } from "bun:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
	apply,
	columns,
	cycleDisplay,
	focusSlot,
	init,
	laptop,
	moveDisplay,
	resetSplits,
	resize,
	rules,
	snap,
	stackCycle,
} from "./commands.ts";
import { profile } from "./config/profile.ts";
import { FakeDriver } from "./driver/fake.ts";
import type { DirSel, StackSel, WmDriver, WmEvent } from "./driver/types.ts";
import { readFlexOrder } from "./effects/state.ts";

// Temp roots for the injectable lock/guard/flex paths — never the live /tmp or
// ~/.cache (the machine this runs on has yabai active).
let root = "";
afterEach(() => {
	if (root !== "") {
		rmSync(root, { recursive: true, force: true });
	}
	root = "";
});
function tempPaths(): { flex: string; lock: string; guard: string } {
	root = mkdtempSync(join(tmpdir(), "tess-cmd-"));
	return {
		flex: join(root, "flex-order"),
		lock: join(root, "lock"),
		guard: join(root, "guard"),
	};
}

// A no-op nudge spy: the composite commands (apply/laptop/init) fire a
// sketchybar nudge on success, and the real one spawns /opt/homebrew/bin/
// sketchybar. This machine has sketchybar installed, so every success-path test
// injects this instead of touching the live status bar.
const noNudge = async (): Promise<void> => {};

describe("focusSlot", () => {
	test("focuses the window the slot resolves to", async () => {
		// deskSlots[0] = ghostty-wave (Ghostty|pc). Seed one match.
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [{ displayIdx: 1, label: "" }],
			windows: [
				{ id: 10, app: "Ghostty", title: "pc wave", spaceIndex: 1 },
				{ id: 11, app: "Arc", spaceIndex: 1 },
			],
		});
		await focusSlot(driver, profile, 1);
		const focused = await driver.queryFocusedWindow();
		expect(focused?.id).toBe(10);
	});

	test("no-op when the slot resolves to nothing (no match)", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [{ displayIdx: 1, label: "" }],
			windows: [{ id: 11, app: "Arc", spaceIndex: 1 }],
		});
		await focusSlot(driver, profile, 1); // ghostty-wave — absent
		expect(await driver.queryFocusedWindow()).toBeNull();
	});
});

describe("snap", () => {
	test("50-50 splits the focused space's leaves into two stacked columns", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [{ displayIdx: 1, label: "focus" }],
			windows: [
				{ id: 1, app: "A", spaceIndex: 1, frame: { x: 0, y: 0, w: 10, h: 10 } },
				{
					id: 2,
					app: "B",
					spaceIndex: 1,
					frame: { x: 20, y: 0, w: 10, h: 10 },
				},
			],
		});
		await driver.focusWindow(1);
		await snap(driver, profile, "50-50");
		// realizeLayout for 2col keeps both windows on the space, unfloated, bsp.
		const space = (await driver.querySpaces())[0];
		expect(space?.layout).toBe("bsp");
		expect([...(space?.windowIds ?? [])].sort((a, b) => a - b)).toEqual([1, 2]);
	});

	test("no-op when no space is focused", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [{ displayIdx: 1, label: "focus" }],
			windows: [{ id: 1, app: "A", spaceIndex: 1 }],
		});
		// nothing focused → queryFocusedSpace null → no throw, no change.
		await snap(driver, profile, "columns");
		expect((await driver.querySpaces())[0]?.layout).toBe("bsp");
	});
});

/** A driver spy that reports a chosen boolean for the primary call and records selectors. */
class DirSpyDriver extends FakeDriver {
	readonly focusDirCalls: (DirSel | StackSel)[] = [];
	readonly resizeCalls: string[] = [];
	#primaryOk: boolean;
	constructor(primaryOk: boolean) {
		super();
		this.#primaryOk = primaryOk;
	}
	override async focusWindowDir(sel: DirSel | StackSel): Promise<boolean> {
		this.focusDirCalls.push(sel);
		// The primary is the first call; the fallback is the second.
		return this.focusDirCalls.length === 1 ? this.#primaryOk : true;
	}
	override async resizeWindow(
		edge?: "left" | "right" | "top" | "bottom",
		dx?: number,
		_dy?: number,
	): Promise<boolean> {
		this.resizeCalls.push(`${edge}:${dx}`);
		return this.resizeCalls.length === 1 ? this.#primaryOk : true;
	}
}

describe("stackCycle", () => {
	test("next: primary stack.next only when it succeeds", async () => {
		const d = new DirSpyDriver(true);
		await stackCycle(d, "next");
		expect(d.focusDirCalls).toEqual(["stack.next"]);
	});
	test("next: falls back to stack.first when stack.next fails", async () => {
		const d = new DirSpyDriver(false);
		await stackCycle(d, "next");
		expect(d.focusDirCalls).toEqual(["stack.next", "stack.first"]);
	});
	test("prev: falls back to stack.last when stack.prev fails", async () => {
		const d = new DirSpyDriver(false);
		await stackCycle(d, "prev");
		expect(d.focusDirCalls).toEqual(["stack.prev", "stack.last"]);
	});
});

describe("resize", () => {
	test("grow pushes the right edge out by +100", async () => {
		const d = new DirSpyDriver(true);
		await resize(d, "grow");
		expect(d.resizeCalls).toEqual(["right:100"]);
	});
	test("shrink falls back to the left edge when the right edge fails", async () => {
		const d = new DirSpyDriver(false);
		await resize(d, "shrink");
		// grow d=-100; primary right:-100, fallback left:-(-100)=left:100.
		expect(d.resizeCalls).toEqual(["right:-100", "left:100"]);
	});
});

describe("moveDisplay", () => {
	test("moves the focused window to the named display and focuses it", async () => {
		const driver = new FakeDriver({
			displays: [
				{ idx: 1, frame: { x: 0, y: 0, w: 1728, h: 1117 } }, // laptop
				{ idx: 2, frame: { x: 0, y: 0, w: 3440, h: 1440 } }, // aw
			],
			spaces: [
				{ displayIdx: 1, label: "home" },
				{ displayIdx: 2, label: "aw-home" },
			],
			windows: [{ id: 5, app: "Arc", spaceIndex: 1 }],
		});
		await driver.focusWindow(5);
		await moveDisplay(driver, profile, "aw");
		const w = (await driver.queryWindows()).find((x) => x.id === 5);
		expect(w?.displayIdx).toBe(2);
	});

	test("no-op when the named display is absent", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1, frame: { x: 0, y: 0, w: 1728, h: 1117 } }],
			spaces: [{ displayIdx: 1, label: "home" }],
			windows: [{ id: 5, app: "Arc", spaceIndex: 1 }],
		});
		await driver.focusWindow(5);
		await moveDisplay(driver, profile, "g9"); // not connected
		expect((await driver.queryWindows())[0]?.displayIdx).toBe(1);
	});
});

describe("cycleDisplay", () => {
	test("moves + focuses across displays", async () => {
		const driver = new FakeDriver({
			displays: [
				{ idx: 1, frame: { x: 0, y: 0, w: 1728, h: 1117 } },
				{ idx: 2, frame: { x: 0, y: 0, w: 3440, h: 1440 } },
			],
			spaces: [
				{ displayIdx: 1, label: "a" },
				{ displayIdx: 2, label: "b" },
			],
			windows: [{ id: 7, app: "Arc", spaceIndex: 1 }],
		});
		await driver.focusWindow(7);
		await cycleDisplay(driver, "next");
		expect((await driver.queryWindows())[0]?.displayIdx).toBe(2);
	});

	test("no-op when no window is focused", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [{ displayIdx: 1, label: "a" }],
			windows: [{ id: 7, app: "Arc", spaceIndex: 1 }],
		});
		// no focus set → early return, window unmoved.
		await cycleDisplay(driver, "next");
		expect((await driver.queryWindows())[0]?.displayIdx).toBe(1);
	});
});

describe("resetSplits", () => {
	test("sets abs ratio 0.5 on tiled visible windows only", async () => {
		const calls: Array<{ id: number; ratio: number }> = [];
		class RatioSpy extends FakeDriver {
			override async setSplitRatio(id: number, ratio: number): Promise<void> {
				calls.push({ id, ratio });
			}
		}
		const driver = new RatioSpy({
			displays: [{ idx: 1 }],
			spaces: [{ displayIdx: 1, label: "focus" }],
			windows: [
				{ id: 1, app: "A", spaceIndex: 1 },
				{ id: 2, app: "B", spaceIndex: 1, floating: true },
				{ id: 3, app: "C", spaceIndex: 1, minimized: true },
			],
		});
		await driver.focusWindow(1);
		await resetSplits(driver);
		expect(calls).toEqual([{ id: 1, ratio: 0.5 }]);
	});
});

describe("columns", () => {
	test("flips every horizontal split to vertical then balances", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [{ displayIdx: 1, label: "focus" }],
			windows: [
				{ id: 1, app: "A", spaceIndex: 1, splitType: "horizontal" },
				{ id: 2, app: "B", spaceIndex: 1, splitType: "horizontal" },
				{ id: 3, app: "C", spaceIndex: 1, splitType: "vertical" },
			],
		});
		await driver.focusWindow(1);
		await columns(driver);
		const wins = await driver.queryWindows();
		expect(wins.every((w) => w.splitType !== "horizontal")).toBe(true);
	});
});

// A multi-display desk world seeded to the profile's desk app set.
function deskWorld(): FakeDriver {
	return new FakeDriver({
		displays: [
			{ idx: 1, frame: { x: 0, y: 0, w: 5120, h: 2160 } }, // g9
			{ idx: 2, frame: { x: 0, y: 0, w: 3440, h: 1440 } }, // aw
			{ idx: 3, frame: { x: 0, y: 0, w: 1728, h: 1117 } }, // laptop
		],
		spaces: [
			{ displayIdx: 1, label: "" }, // 1 → g9 home
			{ displayIdx: 2, label: "" }, // 2 → aw home
			{ displayIdx: 3, label: "" }, // 3 → laptop home
		],
		windows: [
			// g9 3col: arc, obsidian, ghostty-wave, ghostty-mbp, vscode
			{ id: 1, app: "Arc", title: "one", spaceIndex: 1 },
			{ id: 2, app: "Obsidian", spaceIndex: 1 },
			{ id: 3, app: "Ghostty", title: "pc", spaceIndex: 1 },
			{ id: 4, app: "Ghostty", title: "mbp", spaceIndex: 1 },
			{ id: 5, app: "Code", spaceIndex: 1 },
			// aw 2col: linear, arc, akiflow
			{ id: 6, app: "Linear", spaceIndex: 2 },
			{ id: 7, app: "Arc", title: "two", spaceIndex: 2 },
			{ id: 8, app: "Akiflow", spaceIndex: 2 },
		],
	});
}

describe("apply", () => {
	test("labels each present display's home space, lays out the desk, nudges once", async () => {
		const driver = deskWorld();
		const p = tempPaths();
		const nudged: string[] = [];
		await apply(driver, profile, p.lock, p.guard, async (e) => {
			nudged.push(e);
		});
		const spaces = await driver.querySpaces();
		const labels = spaces.map((s) => s.label);
		expect(labels).toContain("main");
		expect(labels).toContain("plan");
		expect(labels).toContain("laptop");
		// The injected nudge fired exactly once (a re-hardcoded nudgeSketchybar
		// would leave this empty AND spawn the real bar).
		expect(nudged).toEqual(["yabai_spaces_changed"]);
	});

	test("no-op under lock contention (a live holder owns the lock)", async () => {
		const driver = deskWorld();
		const p = tempPaths();
		// Pre-acquire the injected lock dir with a live PID (this process).
		mkdirSync(p.lock);
		writeFileSync(join(p.lock, "pid"), `${process.pid}\n`);
		await apply(driver, profile, p.lock, p.guard);
		// The desk was never laid out — home spaces stay unlabelled.
		const labels = (await driver.querySpaces()).map((s) => s.label);
		expect(labels).toEqual(["", "", ""]);
	});

	test("foreign windows land on the laptop park; g9 keeps only its targets", async () => {
		// g9 + aw + laptop, each home carrying desk targets PLUS foreign windows.
		// After apply, the ping-pong is fixed: g9's home holds ONLY its 3col
		// targets, and every refugee ends up on the laptop park (which also stacks).
		const driver = new FakeDriver({
			displays: [
				{ idx: 1, frame: { x: 0, y: 0, w: 5120, h: 2160 } }, // g9
				{ idx: 2, frame: { x: 0, y: 0, w: 3440, h: 1440 } }, // aw
				{ idx: 3, frame: { x: 0, y: 0, w: 1728, h: 1117 } }, // laptop
			],
			spaces: [
				{ displayIdx: 1, label: "" }, // 1 → g9 home
				{ displayIdx: 2, label: "" }, // 2 → aw home
				{ displayIdx: 3, label: "" }, // 3 → laptop home (the park)
			],
			windows: [
				// g9 3col targets + two refugees.
				{ id: 1, app: "Arc", title: "one", spaceIndex: 1 },
				{ id: 2, app: "Obsidian", spaceIndex: 1 },
				{ id: 3, app: "Ghostty", title: "pc", spaceIndex: 1 },
				{ id: 4, app: "Ghostty", title: "mbp", spaceIndex: 1 },
				{ id: 5, app: "Code", spaceIndex: 1 },
				{ id: 100, app: "Messages", spaceIndex: 1 }, // refugee
				{ id: 101, app: "Slack", spaceIndex: 1 }, // refugee
				// aw 2col targets + one refugee.
				{ id: 6, app: "Linear", spaceIndex: 2 },
				{ id: 7, app: "Arc", title: "two", spaceIndex: 2 },
				{ id: 8, app: "Akiflow", spaceIndex: 2 },
				{ id: 102, app: "Finder", spaceIndex: 2 }, // refugee
				// laptop (park) targets — its own stack apps.
				{ id: 9, app: "Arc", title: "three", spaceIndex: 3 },
				{ id: 30, app: "Spotify", spaceIndex: 3 },
				{ id: 31, app: "Discord", spaceIndex: 3 },
			],
		});
		const p = tempPaths();
		await apply(driver, profile, p.lock, p.guard, noNudge);

		const spaces = await driver.querySpaces();
		const g9 = spaces.find((s) => s.label === "main");
		const laptop = spaces.find((s) => s.label === "laptop");
		// g9 holds ONLY its five 3col targets — refugees are gone (no ping-pong).
		expect(new Set(g9?.windowIds)).toEqual(new Set([1, 2, 3, 4, 5]));
		// Every refugee ended up on the laptop park.
		for (const refugee of [100, 101, 102]) {
			expect(laptop?.windowIds).toContain(refugee);
		}
		// The park is a stack (refugees + laptop targets pile into one stack).
		expect(laptop?.layout).toBe("stack");
	});
});

describe("laptop", () => {
	test("converges the grid on a laptop-only world and persists the flex order", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1, frame: { x: 0, y: 0, w: 1728, h: 1117 } }],
			spaces: [{ displayIdx: 1, label: "laptop" }], // home space
			windows: [
				{ id: 1, app: "Arc", title: "a", spaceIndex: 1 },
				{ id: 2, app: "Ghostty", title: "pc", spaceIndex: 1 },
				{ id: 3, app: "Spotify", spaceIndex: 1 }, // not pinned/flex-eligible? flexes
			],
		});
		const p = tempPaths();
		const result = await laptop(
			driver,
			profile,
			p.flex,
			p.lock,
			p.guard,
			noNudge,
		);
		expect(result).toBe("ran");
		// Pinned core created lap-* spaces beyond the single home space.
		const spaces = await driver.querySpaces();
		expect(spaces.some((s) => s.label.startsWith("lap-"))).toBe(true);
		// Home space is a stack.
		expect(spaces.find((s) => s.label === "laptop")?.layout).toBe("stack");
		// Flex order was written.
		expect(Array.isArray(readFlexOrder(p.flex))).toBe(true);
	});

	test("returns contended when a live holder owns the converge lock", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1, frame: { x: 0, y: 0, w: 1728, h: 1117 } }],
			spaces: [{ displayIdx: 1, label: "laptop" }],
			windows: [{ id: 1, app: "Arc", spaceIndex: 1 }],
		});
		const p = tempPaths();
		mkdirSync(p.lock);
		writeFileSync(join(p.lock, "pid"), `${process.pid}\n`);
		const result = await laptop(driver, profile, p.flex, p.lock, p.guard);
		expect(result).toBe("contended");
	});

	test("skipped when a monitor is attached (not laptop-only)", async () => {
		const driver = new FakeDriver({
			displays: [
				{ idx: 1, frame: { x: 0, y: 0, w: 1728, h: 1117 } },
				{ idx: 2, frame: { x: 0, y: 0, w: 3440, h: 1440 } },
			],
			spaces: [
				{ displayIdx: 1, label: "laptop" },
				{ displayIdx: 2, label: "aw" },
			],
			windows: [{ id: 1, app: "Arc", spaceIndex: 1 }],
		});
		const p = tempPaths();
		const result = await laptop(driver, profile, p.flex, p.lock, p.guard);
		expect(result).toBe("skipped");
	});
});

/** Records the rule ops so the port's add/remove/apply sequence is observable. */
class RuleRecorder implements NonNullable<WmDriver["rules"]> {
	readonly removed: string[] = [];
	readonly added: string[] = [];
	applied = 0;
	#existing: Array<{ label: string }>;
	constructor(existing: Array<{ label: string }>) {
		this.#existing = existing;
	}
	async list(): Promise<Array<{ label: string }>> {
		return this.#existing;
	}
	async remove(label: string): Promise<void> {
		this.removed.push(label);
	}
	async add(rule: { label: string }): Promise<void> {
		this.added.push(rule.label);
	}
	async apply(): Promise<void> {
		this.applied++;
	}
}

describe("rules", () => {
	test("removes only auto: rules, re-adds the ported set catch-all first, applies", async () => {
		const recorder = new RuleRecorder([
			{ label: "auto:old" },
			{ label: "manual-keep" },
		]);
		const driver = deskWorld() as FakeDriver & { rules: RuleRecorder };
		Object.defineProperty(driver, "rules", { value: recorder });
		await rules(driver, profile);
		expect(recorder.removed).toEqual(["auto:old"]);
		// Catch-all is added FIRST.
		expect(recorder.added[0]).toBe("auto:default-laptop-stack");
		expect(recorder.added).toContain("auto:code");
		expect(recorder.added).toContain("auto:akiflow");
		expect(recorder.added).toContain("auto:linear");
		expect(recorder.added).toContain("auto:finder");
		expect(recorder.added).toContain("auto:akiflow-dialog");
		expect(recorder.applied).toBe(1);
	});

	test("no-op when the backend has no rules capability", async () => {
		const driver: WmDriver = new FakeDriver();
		// FakeDriver has no rules capability.
		await rules(driver, profile);
		expect(driver.rules).toBeUndefined();
	});
});

/** Records signal registrations so init's wiring is observable. */
class EventRecorder implements NonNullable<WmDriver["events"]> {
	readonly registered: Array<{ event: WmEvent; command: string[] }> = [];
	async register(event: WmEvent, command: string[]): Promise<void> {
		this.registered.push({ event, command });
	}
}

describe("init", () => {
	test("registers the nine signal wirings and runs the startup cascade", async () => {
		const recorder = new EventRecorder();
		const driver = deskWorld() as FakeDriver & { events: EventRecorder };
		Object.defineProperty(driver, "events", { value: recorder });
		const p = tempPaths();
		await init(
			driver,
			profile,
			"/usr/local/bin/tess",
			p.lock,
			p.guard,
			noNudge,
		);

		const events = recorder.registered.map((r) => r.event);
		expect(events).toEqual([
			"display_added",
			"display_removed",
			"display_moved",
			"application_launched",
			"application_terminated",
			"window_created",
			"window_destroyed",
			"space_changed",
			"display_changed",
		]);
		// dock_did_restart is NOT registered here (yabairc shim owns it).
		expect(events).not.toContain("dock_did_restart");
		// Display trio → tess display-event; flex signals → tess flex-event.
		const byEvent = new Map(
			recorder.registered.map((r) => [r.event, r.command]),
		);
		expect(byEvent.get("display_added")).toEqual([
			"/usr/local/bin/tess",
			"display-event",
		]);
		expect(byEvent.get("window_created")).toEqual([
			"/usr/local/bin/tess",
			"flex-event",
		]);
		// Startup cascade ran: apply labelled the desk.
		const labels = (await driver.querySpaces()).map((s) => s.label);
		expect(labels).toContain("main");
	});

	// Startup on a laptop-only machine must reclaim the per-space grid
	// (laptop), not run the desk `apply` — whose teardown prelude destroys the
	// lap-* spaces and collapses every window onto the home space.
	test("converges the laptop grid at startup on a laptop-only world", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1, frame: { x: 0, y: 0, w: 1728, h: 1117 } }],
			spaces: [{ displayIdx: 1, label: "laptop" }], // home space
			windows: [
				{ id: 1, app: "Arc", title: "a", spaceIndex: 1 },
				{ id: 2, app: "Ghostty", title: "pc", spaceIndex: 1 },
				{ id: 3, app: "Spotify", spaceIndex: 1 },
			],
		});
		const p = tempPaths();
		await init(
			driver,
			profile,
			"/usr/local/bin/tess",
			p.lock,
			p.guard,
			noNudge,
			p.lock,
			p.flex,
		);
		// The laptop grid was reclaimed: pinned core created lap-* spaces and the
		// home space is a stack — not the desk `apply` teardown that would have
		// collapsed onto the single home space (no lap-*, no desk labels).
		const spaces = await driver.querySpaces();
		expect(spaces.some((s) => s.label.startsWith("lap-"))).toBe(true);
		expect(spaces.find((s) => s.label === "laptop")?.layout).toBe("stack");
		expect(spaces.map((s) => s.label)).not.toContain("main");
		expect(Array.isArray(readFlexOrder(p.flex))).toBe(true);
	});
});
