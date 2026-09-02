import { afterEach, describe, expect, test } from "bun:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { FakeDriver } from "./driver/fake.ts";
import type { WmDriver, WmEvent } from "./driver/types.ts";
import { type Command, parseArgs, run } from "./index.ts";

// ─── parseArgs — the pure arg grammar ────────────────────────────────────────

describe("parseArgs — no-arg subcommands", () => {
	test.each([
		"apply",
		"laptop",
		"display-event",
		"flex-event",
		"rules",
		"display-setup",
		"reset-splits",
		"columns",
		"toggle-float",
		"balance",
	])("%s parses with no argument", (sub) => {
		const r = parseArgs([sub]);
		expect(r.ok).toBe(true);
		expect(r.ok && r.command.kind).toBe(sub);
	});
});

describe("parseArgs — unknown / empty", () => {
	test("empty argv → not ok, no message (bare usage)", () => {
		const r = parseArgs([]);
		expect(r.ok).toBe(false);
		expect(r.ok === false && r.msg).toBeUndefined();
	});

	test("unknown subcommand → not ok, no message", () => {
		const r = parseArgs(["frobnicate"]);
		expect(r.ok).toBe(false);
		expect(r.ok === false && r.msg).toBeUndefined();
	});
});

describe("parseArgs — enum-arg subcommands", () => {
	test("snap accepts each mode", () => {
		expect(parseArgs(["snap", "3col"])).toEqual({
			ok: true,
			command: { kind: "snap", mode: "3col" },
		});
		expect(parseArgs(["snap", "50-50"])).toEqual({
			ok: true,
			command: { kind: "snap", mode: "50-50" },
		});
		expect(parseArgs(["snap", "columns"])).toEqual({
			ok: true,
			command: { kind: "snap", mode: "columns" },
		});
	});

	test("snap with a bad mode → not ok, names the choices", () => {
		const r = parseArgs(["snap", "quad"]);
		expect(r.ok).toBe(false);
		expect(r.ok === false && r.msg).toContain("3col|50-50|columns");
	});

	test("snap with no mode → not ok", () => {
		expect(parseArgs(["snap"]).ok).toBe(false);
	});

	test("stack-cycle / cycle-display take next|prev", () => {
		expect(parseArgs(["stack-cycle", "next"])).toEqual({
			ok: true,
			command: { kind: "stack-cycle", dir: "next" },
		});
		expect(parseArgs(["cycle-display", "prev"])).toEqual({
			ok: true,
			command: { kind: "cycle-display", dir: "prev" },
		});
		expect(parseArgs(["stack-cycle", "sideways"]).ok).toBe(false);
	});

	test("resize takes grow|shrink", () => {
		expect(parseArgs(["resize", "grow"])).toEqual({
			ok: true,
			command: { kind: "resize", dir: "grow" },
		});
		expect(parseArgs(["resize", "bigger"]).ok).toBe(false);
	});

	test("move-display takes a display name", () => {
		expect(parseArgs(["move-display", "g9"])).toEqual({
			ok: true,
			command: { kind: "move-display", name: "g9" },
		});
		expect(parseArgs(["move-display", "tv"]).ok).toBe(false);
	});

	test("focus / swap / warp take a direction", () => {
		expect(parseArgs(["focus", "west"])).toEqual({
			ok: true,
			command: { kind: "focus", dir: "west" },
		});
		expect(parseArgs(["swap", "east"])).toEqual({
			ok: true,
			command: { kind: "swap", dir: "east" },
		});
		expect(parseArgs(["warp", "north"])).toEqual({
			ok: true,
			command: { kind: "warp", dir: "north" },
		});
		expect(parseArgs(["focus", "up"]).ok).toBe(false);
	});

	test("insert takes a direction incl. stack", () => {
		expect(parseArgs(["insert", "stack"])).toEqual({
			ok: true,
			command: { kind: "insert", dir: "stack" },
		});
		expect(parseArgs(["insert", "east"])).toEqual({
			ok: true,
			command: { kind: "insert", dir: "east" },
		});
		expect(parseArgs(["insert", "sideways"]).ok).toBe(false);
	});

	test("space takes bsp|stack", () => {
		expect(parseArgs(["space", "bsp"])).toEqual({
			ok: true,
			command: { kind: "space", layout: "bsp" },
		});
		expect(parseArgs(["space", "float"]).ok).toBe(false);
	});
});

describe("parseArgs — init --self", () => {
	test("init --self <path> carries the path", () => {
		expect(parseArgs(["init", "--self", "/etc/profiles/x/bin/tess"])).toEqual({
			ok: true,
			command: { kind: "init", self: "/etc/profiles/x/bin/tess" },
		});
	});

	test("init without --self → not ok, names the flag", () => {
		const r = parseArgs(["init"]);
		expect(r.ok).toBe(false);
		expect(r.ok === false && r.msg).toContain("--self");
	});

	test("init --self with no path → not ok", () => {
		const r = parseArgs(["init", "--self"]);
		expect(r.ok).toBe(false);
		expect(r.ok === false && r.msg).toContain("--self");
	});

	test("init --self with an empty path → not ok", () => {
		const r = parseArgs(["init", "--self", ""]);
		expect(r.ok).toBe(false);
		expect(r.ok === false && r.msg).toContain("--self");
	});

	test("init --self with a flag-like value → not ok (dropped-arg guard)", () => {
		const r = parseArgs(["init", "--self", "--apply"]);
		expect(r.ok).toBe(false);
		expect(r.ok === false && r.msg).toContain("--self");
	});
});

describe("parseArgs — focus-slot number", () => {
	test("a slot integer parses", () => {
		expect(parseArgs(["focus-slot", "5"])).toEqual({
			ok: true,
			command: { kind: "focus-slot", n: 5 },
		});
	});

	test("no slot → not ok", () => {
		expect(parseArgs(["focus-slot"]).ok).toBe(false);
	});

	test("a non-integer slot → not ok", () => {
		const r = parseArgs(["focus-slot", "two"]);
		expect(r.ok).toBe(false);
		expect(r.ok === false && r.msg).toContain("not an integer");
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
		const code = await run({ kind: "focus-slot", n: 1 }, driver);
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
		const code = await run({ kind: "toggle-float" }, driver);
		expect(code).toBe(0);
		expect((await driver.queryFocusedWindow())?.floating).toBe(true);
	});

	test("space sets the focused space's layout", async () => {
		const driver = new FakeDriver({
			spaces: [{ displayIdx: 1, layout: "bsp" }],
			windows: [{ id: 9, app: "Arc", spaceIndex: 1 }],
		});
		await driver.focusWindow(9);
		const code = await run({ kind: "space", layout: "stack" }, driver);
		expect(code).toBe(0);
		expect((await driver.queryFocusedSpace())?.layout).toBe("stack");
	});

	test("a raw-passthrough (focus) is a no-op on the world and exits 0", async () => {
		const driver = new FakeDriver({
			spaces: [{ displayIdx: 1 }],
			windows: [{ id: 1, app: "Arc", spaceIndex: 1 }],
		});
		const before = await driver.querySpaces();
		const code = await run({ kind: "focus", dir: "west" }, driver);
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
		const code = await run({ kind: "laptop" }, driver, {
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
		const code = await run({ kind: "laptop" }, driver, {
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
				const code = await run(cmd, driver, {
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
