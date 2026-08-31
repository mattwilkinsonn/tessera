// Layer 4 — the `tess` CLI entrypoint.
//
// The single binary skhd and the yabai signals invoke: `tess <subcommand>` maps
// 1:1 onto the old shell scripts. This module is a THIN router — argv → a
// command function in `commands.ts` (or a one-line driver passthrough for the
// raw-yabai keybinds, Q6). All planning lives in `engine/`, all effects in
// `effects/`, all yabai contact in `driver/`; nothing here reaches past those seams.
//
// `parseArgs` is pure (the house pattern: a discriminated `ok` result, no I/O,
// no process exit) so the arg grammar is
// unit-tested without spawning. `run` wires the real `YabaiDriver` + `profile`
// and maps the parsed command to its effect. Exit codes: `tess laptop` /
// `tess flex-event` exit nonzero ONLY on live-lock contention — the bash callers
// re-loop on that; everything
// else exits 0.

import {
	apply,
	type CycleDir,
	columns,
	cycleDisplay,
	displaySetup,
	focusSlot,
	type InsertDir,
	init,
	insert,
	laptop,
	moveDisplay,
	type ResizeDir,
	resetSplits,
	resize,
	rules,
	runDisplayCascade,
	runFlexConverge,
	type SpaceLayout,
	snap,
	spaceLayout,
	stackCycle,
	toggleFloat,
} from "./commands.ts";
import { profile } from "./config/profile.ts";
import type { DisplayName } from "./config/types.ts";
import type { DirSel, WmDriver } from "./driver/types.ts";
import { YabaiDriver } from "./driver/yabai.ts";
import { DISPLAY_STAMP, FLEX_STAMP } from "./effects/constants.ts";
import {
	recordEvent,
	runDisplayWaiter,
	runFlexWaiter,
	type WaiterDeps,
} from "./effects/debounce.ts";
import type { SnapMode } from "./engine/snap.ts";

// ── Arg grammar ─────────────────────────────────────────────────────────────

/** The parsed command — a closed union the router switches over exhaustively. */
export type Command =
	| { kind: "apply" }
	| { kind: "laptop" }
	| { kind: "display-event" }
	| { kind: "flex-event" }
	| { kind: "rules" }
	| { kind: "display-setup" }
	| { kind: "init" }
	| { kind: "focus-slot"; n: number }
	| { kind: "snap"; mode: SnapMode }
	| { kind: "stack-cycle"; dir: CycleDir }
	| { kind: "resize"; dir: ResizeDir }
	| { kind: "move-display"; name: DisplayName }
	| { kind: "cycle-display"; dir: CycleDir }
	| { kind: "reset-splits" }
	| { kind: "columns" }
	// Raw-yabai keybind one-liners (skhdrc Focus/Move/Stack blocks, Q6).
	| { kind: "focus"; dir: DirSel }
	| { kind: "swap"; dir: DirSel }
	| { kind: "warp"; dir: DirSel }
	| { kind: "insert"; dir: InsertDir }
	| { kind: "toggle-float" }
	| { kind: "balance" }
	| { kind: "space"; layout: SpaceLayout };

export type ParseResult =
	| { ok: true; command: Command }
	| { ok: false; msg?: string };

const CYCLE_DIRS: Readonly<Record<string, CycleDir>> = {
	next: "next",
	prev: "prev",
};
const RESIZE_DIRS: Readonly<Record<string, ResizeDir>> = {
	grow: "grow",
	shrink: "shrink",
};
const SNAP_MODES: Readonly<Record<string, SnapMode>> = {
	"3col": "3col",
	"50-50": "50-50",
	columns: "columns",
};
const DISPLAY_NAMES: Readonly<Record<string, DisplayName>> = {
	g9: "g9",
	aw: "aw",
	laptop: "laptop",
};
const DIR_SELS: Readonly<Record<string, DirSel>> = {
	west: "west",
	south: "south",
	north: "north",
	east: "east",
};
const INSERT_DIRS: Readonly<Record<string, InsertDir>> = {
	east: "east",
	west: "west",
	north: "north",
	south: "south",
	stack: "stack",
};
const SPACE_LAYOUTS: Readonly<Record<string, SpaceLayout>> = {
	bsp: "bsp",
	stack: "stack",
};

/**
 * Pure arg parse: `argv` is `process.argv.slice(2)`. Returns the closed
 * {@link Command} union or a not-ok result with an optional usage message. No
 * I/O, no exit — the entry guard maps the result to a process exit.
 */
export function parseArgs(argv: string[]): ParseResult {
	const sub = argv[0] ?? "";
	const arg = argv[1];
	switch (sub) {
		case "apply":
			return { ok: true, command: { kind: "apply" } };
		case "laptop":
			return { ok: true, command: { kind: "laptop" } };
		case "display-event":
			return { ok: true, command: { kind: "display-event" } };
		case "flex-event":
			return { ok: true, command: { kind: "flex-event" } };
		case "rules":
			return { ok: true, command: { kind: "rules" } };
		case "display-setup":
			return { ok: true, command: { kind: "display-setup" } };
		case "init":
			return { ok: true, command: { kind: "init" } };
		case "reset-splits":
			return { ok: true, command: { kind: "reset-splits" } };
		case "columns":
			return { ok: true, command: { kind: "columns" } };
		case "toggle-float":
			return { ok: true, command: { kind: "toggle-float" } };
		case "balance":
			return { ok: true, command: { kind: "balance" } };
		case "focus-slot": {
			if (arg == null) {
				return { ok: false, msg: "focus-slot needs a slot number (1-9)" };
			}
			const n = Number(arg);
			if (!Number.isInteger(n)) {
				return { ok: false, msg: `focus-slot: not an integer: ${arg}` };
			}
			return { ok: true, command: { kind: "focus-slot", n } };
		}
		case "snap": {
			const mode = arg == null ? undefined : SNAP_MODES[arg];
			if (mode == null) {
				return { ok: false, msg: "snap needs a mode (3col|50-50|columns)" };
			}
			return { ok: true, command: { kind: "snap", mode } };
		}
		case "stack-cycle": {
			const dir = arg == null ? undefined : CYCLE_DIRS[arg];
			if (dir == null) {
				return { ok: false, msg: "stack-cycle needs a direction (next|prev)" };
			}
			return { ok: true, command: { kind: "stack-cycle", dir } };
		}
		case "resize": {
			const dir = arg == null ? undefined : RESIZE_DIRS[arg];
			if (dir == null) {
				return { ok: false, msg: "resize needs a direction (grow|shrink)" };
			}
			return { ok: true, command: { kind: "resize", dir } };
		}
		case "move-display": {
			const name = arg == null ? undefined : DISPLAY_NAMES[arg];
			if (name == null) {
				return {
					ok: false,
					msg: "move-display needs a display (g9|aw|laptop)",
				};
			}
			return { ok: true, command: { kind: "move-display", name } };
		}
		case "cycle-display": {
			const dir = arg == null ? undefined : CYCLE_DIRS[arg];
			if (dir == null) {
				return {
					ok: false,
					msg: "cycle-display needs a direction (next|prev)",
				};
			}
			return { ok: true, command: { kind: "cycle-display", dir } };
		}
		case "focus": {
			const dir = arg == null ? undefined : DIR_SELS[arg];
			if (dir == null) {
				return {
					ok: false,
					msg: "focus needs a direction (west|south|north|east)",
				};
			}
			return { ok: true, command: { kind: "focus", dir } };
		}
		case "swap": {
			const dir = arg == null ? undefined : DIR_SELS[arg];
			if (dir == null) {
				return {
					ok: false,
					msg: "swap needs a direction (west|south|north|east)",
				};
			}
			return { ok: true, command: { kind: "swap", dir } };
		}
		case "warp": {
			const dir = arg == null ? undefined : DIR_SELS[arg];
			if (dir == null) {
				return {
					ok: false,
					msg: "warp needs a direction (west|south|north|east)",
				};
			}
			return { ok: true, command: { kind: "warp", dir } };
		}
		case "insert": {
			const dir = arg == null ? undefined : INSERT_DIRS[arg];
			if (dir == null) {
				return {
					ok: false,
					msg: "insert needs a direction (east|west|north|south|stack)",
				};
			}
			return { ok: true, command: { kind: "insert", dir } };
		}
		case "space": {
			const layout = arg == null ? undefined : SPACE_LAYOUTS[arg];
			if (layout == null) {
				return { ok: false, msg: "space needs a layout (bsp|stack)" };
			}
			return { ok: true, command: { kind: "space", layout } };
		}
		default:
			return { ok: false };
	}
}

const USAGE =
	"usage: tess <apply|laptop|display-event|flex-event|rules|display-setup|init|" +
	"focus-slot N|snap MODE|stack-cycle DIR|resize DIR|move-display NAME|" +
	"cycle-display DIR|reset-splits|columns|focus DIR|swap DIR|warp DIR|" +
	"insert DIR|toggle-float|balance|space LAYOUT>";

/**
 * The absolute path this binary was invoked as — the `command` string `tess init`
 * registers with yabai for each signal (``). `process.argv[1]` is
 * the script/compiled-binary path bun ran.
 */
function selfPath(): string {
	return process.argv[1] ?? "tess";
}

/**
 * Injectable effect surfaces for the commands that touch locks / stamps /
 * flex-order / sketchybar. Every field defaults to the real `/tmp` + cache
 * constant (the production wiring), so the entry guard calls `run` with no opts;
 * tests point them at temp dirs + spies and never touch the live machine — the
 * same DI seam the command functions expose.
 */
export interface RunOpts {
	/** `tess` binary path `init` registers with each yabai signal (default: argv[1]). */
	wmPath?: string;
	applyLock?: string;
	laptopLock?: string;
	guardPath?: string;
	flexPath?: string;
	/** SketchyBar nudge for the layout commands (apply/laptop/init/cascade); a
	 * spy in tests so the unit run never spawns the real bar. */
	nudge?: (event: string) => Promise<void>;
	/** Debounce injection (clock/sleep/stamp + waiter locks + stamps + nudge). */
	displayStamp?: string;
	flexStamp?: string;
	displayWaiter?: DisplayWaiterInject;
	flexWaiter?: FlexWaiterInject;
}

/** Debounce injection forwarded to {@link runDisplayWaiter} (tests only). */
type DisplayWaiterInject = {
	waiterLock?: string;
	nudge?: (event: string) => Promise<void>;
	deps?: WaiterDeps;
};
/** Debounce injection forwarded to {@link runFlexWaiter} (tests only). */
type FlexWaiterInject = {
	displayStampPath?: string;
	waiterLock?: string;
	deps?: WaiterDeps;
};

/**
 * Execute a parsed {@link Command} against a driver. Returns the process exit
 * code: nonzero only when a debounced converge lost its live lock (the bash
 * re-loop contract). `tess display-event` / `tess flex-event` stamp the event first,
 * then run the debounce waiter with the router-supplied driver-backed callback.
 * `opts` defaults every effect path to the real constants (production wiring).
 */
export async function run(
	command: Command,
	driver: WmDriver,
	opts: RunOpts = {},
): Promise<number> {
	const displayStamp = opts.displayStamp ?? DISPLAY_STAMP;
	const flexStamp = opts.flexStamp ?? FLEX_STAMP;
	switch (command.kind) {
		case "apply":
			await apply(driver, profile, opts.applyLock, opts.guardPath, opts.nudge);
			return 0;
		case "laptop":
			return (await laptop(
				driver,
				profile,
				opts.flexPath,
				opts.laptopLock,
				opts.guardPath,
				opts.nudge,
			)) === "contended"
				? 1
				: 0;
		case "rules":
			await rules(driver, profile);
			return 0;
		case "display-setup":
			await displaySetup(driver, profile);
			return 0;
		case "init":
			await init(
				driver,
				profile,
				opts.wmPath ?? selfPath(),
				opts.applyLock,
				opts.guardPath,
				opts.nudge,
				opts.laptopLock,
				opts.flexPath,
			);
			return 0;
		case "display-event":
			recordEvent(displayStamp);
			await runDisplayWaiter({
				cascade: async () =>
					(await runDisplayCascade(driver, profile, opts.nudge)) === "contended"
						? "restamp"
						: "settled",
				stampPath: displayStamp,
				...opts.displayWaiter,
			});
			return 0;
		case "flex-event":
			recordEvent(flexStamp);
			await runFlexWaiter({
				displayCount: async () => (await driver.queryDisplays()).length,
				converge: async () =>
					(await runFlexConverge(driver, profile, opts.nudge)) !== "contended",
				stampPath: flexStamp,
				guardPath: opts.guardPath,
				...opts.flexWaiter,
			});
			return 0;
		case "focus-slot":
			await focusSlot(driver, profile, command.n);
			return 0;
		case "snap":
			await snap(driver, profile, command.mode);
			return 0;
		case "stack-cycle":
			await stackCycle(driver, command.dir);
			return 0;
		case "resize":
			await resize(driver, command.dir);
			return 0;
		case "move-display":
			await moveDisplay(driver, profile, command.name);
			return 0;
		case "cycle-display":
			await cycleDisplay(driver, command.dir);
			return 0;
		case "reset-splits":
			await resetSplits(driver);
			return 0;
		case "columns":
			await columns(driver);
			return 0;
		case "insert":
			await insert(driver, command.dir);
			return 0;
		case "toggle-float":
			await toggleFloat(driver);
			return 0;
		case "space":
			await spaceLayout(driver, command.layout);
			return 0;
		// Pure passthroughs: one unguarded driver call, inlined per
		// rule://ts-no-tiny-functions.
		case "focus":
			await driver.focusWindowDir(command.dir);
			return 0;
		case "swap":
			await driver.swapWindows(command.dir);
			return 0;
		case "warp":
			await driver.warpWindow(command.dir);
			return 0;
		case "balance":
			await driver.balanceSpace();
			return 0;
	}
}

if (import.meta.main) {
	const parsed = parseArgs(process.argv.slice(2));
	if (!parsed.ok) {
		process.stderr.write(`${parsed.msg ?? USAGE}\n`);
		process.exit(2);
	}
	process.exit(await run(parsed.command, new YabaiDriver()));
}
