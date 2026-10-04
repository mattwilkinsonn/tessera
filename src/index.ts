// Layer 4 — the `tess` CLI entrypoint.
//
// The single binary skhd and the yabai signals invoke: `tess <subcommand>` maps
// 1:1 onto the old shell scripts. This module is a THIN router — argv → a
// command function in `commands.ts` (or a one-line driver passthrough for the
// raw-yabai keybinds, Q6). All planning lives in `engine/`, all effects in
// `effects/`, all yabai contact in `driver/`; nothing here reaches past those seams.
//
// The Effect CLI owns argv parsing and generated help; run dispatches typed commands.

import { homedir } from "node:os";
import { formatDriverError, liveDriver } from "./cli/driver.ts";
import { commandFor, isIntToken, SUBCOMMANDS } from "./cli/grammar.ts";
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
import { profile as defaultProfile } from "./config/profile.ts";
import type { DisplayName, Profile } from "./config/types.ts";
import { validateProfile } from "./config/validate.ts";
import type { DirSel, WmDriver } from "./driver/types.ts";
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
	| { kind: "init"; self: string }
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

/** Settle a keybind argv without the Effect graph; null defers to the CLI. Pure. */
export function fastMatch(argv: ReadonlyArray<string>): Command | null {
	if (argv.length === 0 || argv.some((token) => token.startsWith("-")))
		return null;
	const [name, value] = argv;
	if (name === undefined || !Object.hasOwn(SUBCOMMANDS, name)) return null;
	const spec = SUBCOMMANDS[name as keyof typeof SUBCOMMANDS];
	if (!spec.fastPath) return null;
	const hasArgument = spec.choices !== null || spec.int;
	if (argv.length !== 1 + Number(hasArgument)) return null;
	if (
		spec.choices !== null &&
		!(spec.choices as ReadonlyArray<string>).includes(value ?? "")
	)
		return null;
	if (spec.int && (value === undefined || !isIntToken(value))) return null;
	if (name === "focus-slot") return commandFor(name, Number(value));
	if (name === "snap") return commandFor(name, value ?? "");
	if (name === "stack-cycle") return commandFor(name, value ?? "");
	if (name === "resize") return commandFor(name, value ?? "");
	if (name === "move-display") return commandFor(name, value ?? "");
	if (name === "cycle-display") return commandFor(name, value ?? "");
	if (name === "reset-splits") return commandFor(name);
	if (name === "columns") return commandFor(name);
	if (name === "focus") return commandFor(name, value ?? "");
	if (name === "swap") return commandFor(name, value ?? "");
	if (name === "warp") return commandFor(name, value ?? "");
	if (name === "insert") return commandFor(name, value ?? "");
	if (name === "toggle-float") return commandFor(name);
	if (name === "balance") return commandFor(name);
	if (name === "space") return commandFor(name, value ?? "");
	return null;
}

/**
 * Injectable effect surfaces for the commands that touch locks / stamps /
 * flex-order / sketchybar. Every field defaults to the real `/tmp` + cache
 * constant (the production wiring), so the entry guard calls `run` with no opts;
 * tests point them at temp dirs + spies and never touch the live machine — the
 * same DI seam the command functions expose.
 */
export interface RunOpts {
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
	profile: Profile,
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
				command.self,
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

/**
 * Resolve the runtime profile: `$TESSERA_PROFILE` (explicit override) →
 * `${XDG_CONFIG_HOME:-~/.config}/tessera/profile.ts` (the well-known path) →
 * the statically-imported bundled default. An explicit override, or a
 * well-known file that exists but is unreadable/broken, surfaces its error; a
 * MISSING well-known file falls through to the bundled default.
 */
export async function loadProfile(): Promise<Profile> {
	// Resolve the profile in precedence order; the actual runtime-selected
	// dynamic import lives in importProfile (see its comment).
	const override = process.env.TESSERA_PROFILE;
	if (override) {
		return importProfile(override);
	}
	const configHome = process.env.XDG_CONFIG_HOME ?? `${homedir()}/.config`;
	const wellKnown = `${configHome}/tessera/profile.ts`;
	if (await Bun.file(wellKnown).exists()) {
		return importProfile(wellKnown);
	}
	return defaultProfile;
}

// Import a profile module and require its `profile` export, so a module that is
// valid TS but exports nothing named `profile` fails with a clear message
// rather than passing `undefined` into run() to crash opaquely downstream.
//
// Dynamic import (ts-no-dynamic-import exception): the module specifier is
// genuinely runtime-selected — a user's $TESSERA_PROFILE override or the
// well-known config path — so a static import cannot name it at author time.
async function importProfile(path: string): Promise<Profile> {
	const mod = (await import(path)) as { profile?: Profile };
	if (!mod.profile) {
		throw new Error(`profile module ${path} must export a \`profile\``);
	}
	try {
		validateProfile(mod.profile);
	} catch (error) {
		throw new Error(`profile module ${path}: ${(error as Error).message}`);
	}
	return mod.profile;
}

if (import.meta.main) {
	const argv = process.argv.slice(2);
	const fast = fastMatch(argv);
	if (fast) {
		try {
			const profile = await loadProfile();
			process.exit(await run(profile, fast, liveDriver()));
		} catch (cause) {
			process.stderr.write(`${formatDriverError(cause)}\n`);
			process.exit(1);
		}
	}
	// Effect CLI owns help, version, malformed input and non-fast commands (effect4-cli.md § T3).
	// Dynamic import keeps the Effect graph off keybind startup.
	const { main } = await import("./cli/main.ts");
	main();
}
