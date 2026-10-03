import { afterEach, describe, expect, test } from "bun:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { SUBCOMMANDS } from "./cli/grammar.ts";
import { profile } from "./config/profile.fixture.ts";
import { FakeDriver } from "./driver/fake.ts";
import type { WmDriver, WmEvent } from "./driver/types.ts";
import { type Command, fastMatch, run } from "./index.ts";

const fastPathCases = Object.entries(SUBCOMMANDS).flatMap(([name, spec]) => {
	if (!spec.fastPath) return [];
	const values = spec.choices ?? [spec.int ? "1" : undefined];
	return values.map((value) => ({
		argv: value === undefined ? [name] : [name, value],
		expected: expectedFastCommand(name, value),
	}));
});

function expectedFastCommand(name: string, value: string | undefined): Command {
	switch (name) {
		case "focus-slot":
			return { kind: name, n: Number(value) };
		case "snap":
			return {
				kind: name,
				mode: value as Extract<Command, { kind: "snap" }>["mode"],
			};
		case "stack-cycle":
		case "cycle-display":
			return { kind: name, dir: value as "next" | "prev" };
		case "resize":
			return { kind: name, dir: value as "grow" | "shrink" };
		case "move-display":
			return { kind: name, name: value as "g9" | "aw" | "laptop" };
		case "reset-splits":
		case "columns":
		case "toggle-float":
		case "balance":
			return { kind: name };
		case "focus":
		case "swap":
		case "warp":
			return { kind: name, dir: value as "west" | "south" | "north" | "east" };
		case "insert":
			return {
				kind: name,
				dir: value as "east" | "west" | "north" | "south" | "stack",
			};
		case "space":
			return { kind: name, layout: value as "bsp" | "stack" };
		default:
			throw new Error(`Unexpected fastPath subcommand: ${name}`);
	}
}

let fastPathRoot = "";
afterEach(() => {
	if (fastPathRoot !== "") {
		rmSync(fastPathRoot, { recursive: true, force: true });
		fastPathRoot = "";
	}
});

describe("fastMatch", () => {
	const deferredArgv: ReadonlyArray<ReadonlyArray<string>> = [
		[],
		["--help"],
		["snap", "--help"],
		["snap", "bogus"],
		["snap"],
		["snap", "3col", "extra"],
		["apply"],
		["laptop"],
		["init", "--self", "x"],
		["focus-slot", "x"],
		["focus-slot", "-1"],
		["focus-slot", "0x10"],
	];

	test("matches every fastPath command choice", () => {
		for (const { argv, expected } of fastPathCases)
			expect(fastMatch(argv)).toEqual(expected);
	});

	test("dispatches every fastPath command through the FakeDriver", async () => {
		fastPathRoot = mkdtempSync(join(tmpdir(), "tess-fast-dispatch-"));
		const driver = new FakeDriver({
			displays: [{ idx: 1, frame: { x: 0, y: 0, w: 5120, h: 1440 } }],
			spaces: [{ displayIdx: 1 }],
			windows: [
				{ id: 42, app: "Ghostty", title: "pc", spaceIndex: 1 },
				{ id: 7, app: "Arc", title: "x", spaceIndex: 1 },
			],
		});
		await driver.focusWindow(7);
		const tmpOpts = {
			applyLock: join(fastPathRoot, "apply.lock"),
			laptopLock: join(fastPathRoot, "laptop.lock"),
			guardPath: join(fastPathRoot, "guard"),
			flexPath: join(fastPathRoot, "flex"),
			displayStamp: join(fastPathRoot, "display.stamp"),
			flexStamp: join(fastPathRoot, "flex.stamp"),
			displayWaiter: { waiterLock: join(fastPathRoot, "display.waiter") },
			flexWaiter: { waiterLock: join(fastPathRoot, "flex.waiter") },
		};
		for (const { argv } of fastPathCases) {
			const command = fastMatch(argv);
			expect(command).not.toBeNull();
			if (command === null) throw new Error(`Expected fast match for ${argv}`);
			const code = await run(profile, command, driver, tmpOpts);
			expect(code).toBe(0);
			if (command.kind === "focus-slot")
				expect((await driver.queryFocusedWindow())?.id).toBe(42);
			if (command.kind === "toggle-float")
				expect((await driver.queryFocusedWindow())?.floating).toBe(true);
			if (command.kind === "space")
				expect((await driver.queryFocusedSpace())?.layout).toBe(command.layout);
		}
	});

	test("falls back to the Effect CLI for subcommand help", () => {
		const root = mkdtempSync(join(tmpdir(), "tess-fast-help-"));
		try {
			const shim = join(root, "yabai");
			writeFileSync(shim, "#!/bin/sh\nexit 1\n", { mode: 0o755 });
			const result = Bun.spawnSync(["bun", "src/index.ts", "snap", "--help"], {
				env: { PATH: process.env.PATH, HOME: root, TESS_YABAI: shim },
			});
			expect(result.exitCode).toBe(0);
			expect(result.stdout.toString()).toContain("tess snap");
		} finally {
			rmSync(root, { recursive: true, force: true });
		}
	});

	test("defers malformed and non-fast argv to the Effect CLI", () => {
		for (const argv of deferredArgv) expect(fastMatch(argv)).toBeNull();
	});
});

// ─── run — dispatch against the FakeDriver ───────────────────────────────────

describe("run — simple dispatch (observable driver effects)", () => {
	test("focus-slot focuses the resolved slot window", async () => {
		// Slot 1 → ghostty-wave = {app:/Ghostty/, title:/pc/}; seed a matching win.
		const driver = new FakeDriver({
			displays: [{ idx: 1, frame: { x: 0, y: 0, w: 5120, h: 1440 } }],
			spaces: [{ displayIdx: 1 }],
			windows: [
				{ id: 42, app: "Ghostty", title: "pc", spaceIndex: 1 },
				{ id: 43, app: "Arc", title: "x", spaceIndex: 1 },
			],
		});
		const code = await run(profile, { kind: "focus-slot", n: 1 }, driver);
		expect(code).toBe(0);
		expect((await driver.queryFocusedWindow())?.id).toBe(42);
	});

	test("toggle-float flips the focused window's float", async () => {
		const driver = new FakeDriver({
			spaces: [{ displayIdx: 1 }],
			windows: [{ id: 7, app: "Arc", spaceIndex: 1 }],
		});
		await driver.focusWindow(7);
		expect((await driver.queryFocusedWindow())?.floating).toBe(false);
		const code = await run(profile, { kind: "toggle-float" }, driver);
		expect(code).toBe(0);
		expect((await driver.queryFocusedWindow())?.floating).toBe(true);
	});

	test("space sets the focused space's layout", async () => {
		const driver = new FakeDriver({
			spaces: [{ displayIdx: 1, layout: "bsp" }],
			windows: [{ id: 9, app: "Arc", spaceIndex: 1 }],
		});
		await driver.focusWindow(9);
		const code = await run(profile, { kind: "space", layout: "stack" }, driver);
		expect(code).toBe(0);
		expect((await driver.queryFocusedSpace())?.layout).toBe("stack");
	});

	test("a raw-passthrough (focus) is a no-op on the world and exits 0", async () => {
		const driver = new FakeDriver({
			spaces: [{ displayIdx: 1 }],
			windows: [{ id: 1, app: "Arc", spaceIndex: 1 }],
		});
		const before = await driver.querySpaces();
		const code = await run(profile, { kind: "focus", dir: "west" }, driver);
		expect(code).toBe(0);
		expect(await driver.querySpaces()).toEqual(before);
	});
});

describe("run — laptop contended-exit contract", () => {
	let root = "";
	afterEach(() => {
		if (root !== "") {
			rmSync(root, { recursive: true, force: true });
			root = "";
		}
	});

	test("a LIVE lock holder → run returns exit code 1", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-router-"));
		const laptopLock = join(root, "laptop.lock");
		// Pre-hold the lock with our own (live) pid so acquireLock surrenders.
		mkdirSync(laptopLock);
		writeFileSync(join(laptopLock, "pid"), `${process.pid}\n`);

		const driver = new FakeDriver({
			displays: [{ idx: 1, frame: { x: 0, y: 0, w: 1728, h: 1117 } }],
			spaces: [{ displayIdx: 1 }],
		});
		const code = await run(profile, { kind: "laptop" }, driver, {
			laptopLock,
			guardPath: join(root, "guard"),
		});
		expect(code).toBe(1);
	});

	test("laptop on a multi-display world → skipped, exit 0", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-router-"));
		const driver = new FakeDriver({
			displays: [
				{ idx: 1, frame: { x: 0, y: 0, w: 5120, h: 1440 } },
				{ idx: 2, frame: { x: 0, y: 0, w: 1728, h: 1117 } },
			],
			spaces: [{ displayIdx: 1 }, { displayIdx: 2 }],
		});
		const code = await run(profile, { kind: "laptop" }, driver, {
			laptopLock: join(root, "laptop.lock"),
			guardPath: join(root, "guard"),
			flexPath: join(root, "flex"),
		});
		expect(code).toBe(0);
	});
});

describe("run — exhaustiveness", () => {
	// A representative of every Command kind — a compile-checked map keyed by
	// the discriminant. If a new kind is added to Command without a run() arm,
	// TypeScript's exhaustive switch fails to compile; this keeps the value-level
	// coverage honest alongside it.
	test("every command kind dispatches without throwing on a benign world", async () => {
		const samples: Record<Command["kind"], Command> = {
			apply: { kind: "apply" },
			laptop: { kind: "laptop" },
			"display-event": { kind: "display-event" },
			"flex-event": { kind: "flex-event" },
			rules: { kind: "rules" },
			"display-setup": { kind: "display-setup" },
			init: { kind: "init", self: "/usr/local/bin/tess" },
			"focus-slot": { kind: "focus-slot", n: 1 },
			snap: { kind: "snap", mode: "3col" },
			"stack-cycle": { kind: "stack-cycle", dir: "next" },
			resize: { kind: "resize", dir: "grow" },
			"move-display": { kind: "move-display", name: "g9" },
			"cycle-display": { kind: "cycle-display", dir: "next" },
			"reset-splits": { kind: "reset-splits" },
			columns: { kind: "columns" },
			focus: { kind: "focus", dir: "west" },
			swap: { kind: "swap", dir: "west" },
			warp: { kind: "warp", dir: "west" },
			insert: { kind: "insert", dir: "east" },
			"toggle-float": { kind: "toggle-float" },
			balance: { kind: "balance" },
			space: { kind: "space", layout: "bsp" },
		};
		// The five effect-bearing arms (apply/laptop/init/display-event/flex-event)
		// are exercised for DISPATCH only: we pre-hold every lock with this live
		// pid so each short-circuits (apply/laptop surrender or report contention,
		// the waiters see a live holder and never run the cascade/converge). That
		// keeps the test off the live machine's real locks, sketchybar, and yabai
		// while still proving every kind routes to a handler — the deep behavior of
		// each command is covered in commands.test.ts / debounce.test.ts. The
		// compile-checked `Record<Command["kind"], …>` is the structural guard: a
		// new kind without a run() arm fails the exhaustive switch at compile time.
		const root = mkdtempSync(join(tmpdir(), "tess-router-exh-"));
		const applyLock = join(root, "apply.lock");
		const laptopLock = join(root, "laptop.lock");
		const displayWaiterLock = join(root, "dwait.lock");
		const flexWaiterLock = join(root, "fwait.lock");
		for (const lock of [
			applyLock,
			laptopLock,
			displayWaiterLock,
			flexWaiterLock,
		]) {
			mkdirSync(lock);
			writeFileSync(join(lock, "pid"), `${process.pid}\n`);
		}
		try {
			for (const cmd of Object.values(samples)) {
				const driver = new FakeDriver({
					displays: [{ idx: 1, frame: { x: 0, y: 0, w: 5120, h: 1440 } }],
					spaces: [{ displayIdx: 1 }],
				});
				const code = await run(profile, cmd, driver, {
					applyLock,
					laptopLock,
					guardPath: join(root, "guard"),
					flexPath: join(root, "flex"),
					displayStamp: join(root, "dstamp"),
					flexStamp: join(root, "fstamp"),
					displayWaiter: {
						waiterLock: displayWaiterLock,
						nudge: async () => {},
					},
					flexWaiter: { waiterLock: flexWaiterLock },
				});
				expect(typeof code).toBe("number");
			}
		} finally {
			rmSync(root, { recursive: true, force: true });
		}
	});
});

// ─── run → init: the --self path reaches the registered signal action ────────
// The crux of the /$bunfs bugfix: whatever path the caller passes as
// `--self` is what init() registers with each yabai signal, so events
// re-invoke tess by a real, usable path. parseArgs carrying the token into
// command.self is covered above; this closes the OTHER half — that run's init
// arm threads command.self through to the registered action (both are
// `string`, so tsc alone cannot catch a regression that drops it).

/** Records signal registrations so the router's init wiring is observable. */
class EventRecorder implements NonNullable<WmDriver["events"]> {
	readonly registered: Array<{ event: WmEvent; command: string[] }> = [];
	async register(event: WmEvent, command: string[]): Promise<void> {
		this.registered.push({ event, command });
	}
}

describe("run — init registers the --self path", () => {
	test("command.self is the binary path in every registered signal action", async () => {
		const recorder = new EventRecorder();
		const driver = new FakeDriver({
			displays: [{ idx: 1, frame: { x: 0, y: 0, w: 5120, h: 1440 } }],
			spaces: [{ displayIdx: 1 }],
		}) as FakeDriver & { events: EventRecorder };
		Object.defineProperty(driver, "events", { value: recorder });

		const root = mkdtempSync(join(tmpdir(), "tess-init-self-"));
		try {
			const code = await run(
				profile,
				{ kind: "init", self: "/opt/tess/bin/tess" },
				driver,
				{
					applyLock: join(root, "apply.lock"),
					laptopLock: join(root, "laptop.lock"),
					guardPath: join(root, "guard"),
					flexPath: join(root, "flex"),
					nudge: async () => {},
				},
			);
			expect(typeof code).toBe("number");

			const byEvent = new Map(
				recorder.registered.map((r) => [r.event, r.command]),
			);
			// Display trio → `<self> display-event`; flex signals → `<self> flex-event`.
			expect(byEvent.get("display_added")).toEqual([
				"/opt/tess/bin/tess",
				"display-event",
			]);
			expect(byEvent.get("window_created")).toEqual([
				"/opt/tess/bin/tess",
				"flex-event",
			]);
		} finally {
			rmSync(root, { recursive: true, force: true });
		}
	});
});
