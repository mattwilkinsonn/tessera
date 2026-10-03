import type {
	CycleDir,
	InsertDir,
	ResizeDir,
	SpaceLayout,
} from "../commands.ts";
import type { DisplayName } from "../config/types.ts";
import type { DirSel } from "../driver/types.ts";
import type { SnapMode } from "../engine/snap.ts";
import type { Command } from "../index.ts";

/** One `tess` subcommand. `choices` null = no positional; a list = one literal; `int` = one integer. */
export interface SubcommandSpec {
	readonly description: string;
	readonly choices: ReadonlyArray<string> | null;
	readonly int: boolean;
	/** True = settled by `fastMatch` without the Effect graph. False = always the Effect CLI. */
	readonly fastPath: boolean;
}

export const SUBCOMMANDS = {
	apply: {
		description: "Converge the active space onto its configured layout.",
		choices: null,
		int: false,
		fastPath: false,
	},
	laptop: {
		description: "Converge the laptop-only fallback layout.",
		choices: null,
		int: false,
		fastPath: false,
	},
	"display-event": {
		description: "Rebuild after a display is added or removed.",
		choices: null,
		int: false,
		fastPath: false,
	},
	"flex-event": {
		description: "Reflow when a flex space changes.",
		choices: null,
		int: false,
		fastPath: false,
	},
	rules: {
		description: "Apply window rules from the profile.",
		choices: null,
		int: false,
		fastPath: false,
	},
	"display-setup": {
		description: "Set up displays for the current profile.",
		choices: null,
		int: false,
		fastPath: false,
	},
	"focus-slot": {
		description: "Focus a named slot.",
		choices: null,
		int: true,
		fastPath: true,
	},
	snap: {
		description: "Snap the focused window to a layout mode.",
		choices: ["3col", "50-50", "columns"],
		int: false,
		fastPath: true,
	},
	"stack-cycle": {
		description: "Cycle the focused stack.",
		choices: ["next", "prev"],
		int: false,
		fastPath: true,
	},
	resize: {
		description: "Resize the active column.",
		choices: ["grow", "shrink"],
		int: false,
		fastPath: true,
	},
	"move-display": {
		description: "Move the focused window to a display.",
		choices: ["g9", "aw", "laptop"],
		int: false,
		fastPath: true,
	},
	"cycle-display": {
		description: "Cycle the focused display.",
		choices: ["next", "prev"],
		int: false,
		fastPath: true,
	},
	"reset-splits": {
		description: "Reset splits in the active space.",
		choices: null,
		int: false,
		fastPath: true,
	},
	columns: {
		description: "Re-flow the active space into its columns.",
		choices: null,
		int: false,
		fastPath: true,
	},
	focus: {
		description: "Focus the neighboring window.",
		choices: ["west", "south", "north", "east"],
		int: false,
		fastPath: true,
	},
	swap: {
		description: "Swap the focused window with its neighbor.",
		choices: ["west", "south", "north", "east"],
		int: false,
		fastPath: true,
	},
	warp: {
		description: "Warp the focused window to its neighbor.",
		choices: ["west", "south", "north", "east"],
		int: false,
		fastPath: true,
	},
	insert: {
		description: "Set the insertion direction for the focused window.",
		choices: ["east", "west", "north", "south", "stack"],
		int: false,
		fastPath: true,
	},
	"toggle-float": {
		description: "Toggle floating for the focused window.",
		choices: null,
		int: false,
		fastPath: true,
	},
	balance: {
		description: "Balance the active space.",
		choices: null,
		int: false,
		fastPath: true,
	},
	space: {
		description: "Apply a named space layout.",
		choices: ["bsp", "stack"],
		int: false,
		fastPath: true,
	},
} as const satisfies Readonly<Record<string, SubcommandSpec>>;

export type SubcommandName = keyof typeof SUBCOMMANDS;

/** Matches Effect CLI's integer parser while excluding hex and whitespace forms. */
export function isIntToken(s: string): boolean {
	return (
		/^[+-]?\d+(?:\.0+)?(?:[eE][+-]?\d+)?$/.test(s) &&
		Number.isInteger(Number(s))
	);
}

export function commandFor(
	name:
		| "apply"
		| "laptop"
		| "display-event"
		| "flex-event"
		| "rules"
		| "display-setup"
		| "reset-splits"
		| "columns"
		| "toggle-float"
		| "balance",
): Command;
export function commandFor(
	name: "focus-slot",
	value: number,
): Extract<Command, { kind: "focus-slot" }>;
export function commandFor(
	name: "snap",
	value: string,
): Extract<Command, { kind: "snap" }>;
export function commandFor(
	name: "stack-cycle",
	value: string,
): Extract<Command, { kind: "stack-cycle" }>;
export function commandFor(
	name: "resize",
	value: string,
): Extract<Command, { kind: "resize" }>;
export function commandFor(
	name: "move-display",
	value: string,
): Extract<Command, { kind: "move-display" }>;
export function commandFor(
	name: "cycle-display",
	value: string,
): Extract<Command, { kind: "cycle-display" }>;
export function commandFor(
	name: "focus",
	value: string,
): Extract<Command, { kind: "focus" }>;
export function commandFor(
	name: "swap",
	value: string,
): Extract<Command, { kind: "swap" }>;
export function commandFor(
	name: "warp",
	value: string,
): Extract<Command, { kind: "warp" }>;
export function commandFor(
	name: "insert",
	value: string,
): Extract<Command, { kind: "insert" }>;
export function commandFor(
	name: "space",
	value: string,
): Extract<Command, { kind: "space" }>;
export function commandFor(
	name: "init",
	value: string,
): Extract<Command, { kind: "init" }>;
export function commandFor<
	Name extends SubcommandName | "init",
	Value extends string | number | undefined,
>(name: Name, value?: Value): Extract<Command, { kind: Name }>;
export function commandFor(
	name: SubcommandName | "init",
	value?: string | number,
): Command {
	switch (name) {
		case "apply":
			return { kind: "apply" };
		case "laptop":
			return { kind: "laptop" };
		case "display-event":
			return { kind: "display-event" };
		case "flex-event":
			return { kind: "flex-event" };
		case "rules":
			return { kind: "rules" };
		case "display-setup":
			return { kind: "display-setup" };
		case "init":
			return { kind: "init", self: typeof value === "string" ? value : "" };
		case "focus-slot":
			return { kind: name, n: typeof value === "number" ? value : 0 };
		case "snap":
			return { kind: name, mode: value as SnapMode };
		case "stack-cycle":
			return { kind: name, dir: value as CycleDir };
		case "resize":
			return { kind: name, dir: value as ResizeDir };
		case "move-display":
			return { kind: name, name: value as DisplayName };
		case "cycle-display":
			return { kind: name, dir: value as CycleDir };
		case "reset-splits":
			return { kind: name };
		case "columns":
			return { kind: name };
		case "focus":
			return { kind: name, dir: value as DirSel };
		case "swap":
			return { kind: name, dir: value as DirSel };
		case "warp":
			return { kind: name, dir: value as DirSel };
		case "insert":
			return { kind: name, dir: value as InsertDir };
		case "toggle-float":
			return { kind: name };
		case "balance":
			return { kind: name };
		case "space":
			return { kind: name, layout: value as SpaceLayout };
	}
}
