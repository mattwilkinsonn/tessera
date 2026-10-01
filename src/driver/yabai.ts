// Layer 3 — the real yabai WmDriver.
//
// Construction vs execution (mirrors libs/deploy-utils/src/deploy.ts:1-9): the
// pure `yabaiArgs` builders below assemble typed CLI argv arrays — each a
// `string[]` WITHOUT the leading yabai path — and the pure normalizers parse
// yabai's kebab-case query JSON into the camelCase Wm* shapes. The one thin
// `Bun.$` runner (#run) executes them. So the whole driver is unit-testable by
// argv golden + normalization fixtures with no live yabai.
//
// The load-bearing detail (shared doc): `SpaceId` is the stable mission-control
// `.id` from `query --spaces`, NEVER yabai's live `.index`, which renumbers on
// create/destroy/move. `yabai -m space <N>` addresses spaces by that live
// index. So every SpaceId-typed method resolves id → live index by a FRESH
// `query --spaces` immediately before issuing the command — statelessly, caching
// nothing (snapshot caching is the executor's job).
//
// Failure convention (types.ts:106-109): mutators the bash scripts guard with
// `|| true` resolve (return `false` / void) on a nonzero exit rather than throw;
// queries throw only on driver-gone (yabai not running). The runner uses
// `.quiet().nothrow()` and inspects `.exitCode`.

import { rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { $ } from "bun";

import { chainRatios } from "../config/chain.ts";
import type {
	DirSel,
	DisplaySel,
	SpaceId,
	SpaceLayoutTarget,
	StackSel,
	WmDisplay,
	WmDriver,
	WmEvent,
	WmEventSource,
	WmRuleOps,
	WmSpace,
	WmWindow,
} from "./types.ts";

// ── Settle cadence (driver-owned empirical timings, D2) ──────────────────
// These are the literal durations from the column-build recipe, expressed
// directly as ms. D2 forbids absolute ms only in
// ENGINE plan data; the driver legitimately holds its own settle timings. The
// interface `settleMs` (150) is the base unit an executor `{op:"settle",units}`
// multiplies — the 150ms step here is exactly one such unit.
const ANCHOR_SETTLE_MS = 400; // 0.4s after bringing an anchor on
const STEP_SETTLE_MS = 150; // 0.15s after insert/ratio/unfloat (=1 settle unit)
const EXTRA_SETTLE_MS = 350; // 0.35s after stacking an extra

const DEFAULT_YABAI_PATH = "/opt/homebrew/bin/yabai"; //

export interface BspStep {
	args: string[];
	settleMs: number;
	unfloat?: number;
}

/** Clamps a yabai chain ratio and keeps its argv representation stable. */
export function ratioArg(r: number, warn?: (line: string) => void): string {
	const clamped = Math.max(0.1, Math.min(0.9, r));
	if (clamped !== r) {
		warn?.(`ratio ${r} clamped to ${clamped.toFixed(4)}`);
	}
	return `abs:${clamped.toFixed(4)}`;
}

/** Ordered argv recipe for columns/rows with unfloat after each moved window. */
export function bspSteps(
	spaceIdx: number,
	target: SpaceLayoutTarget,
	warn?: (line: string) => void,
): BspStep[] {
	const tracks = target.tracks.filter((track) => track.length > 0);
	const anchors = tracks.flatMap((track) =>
		track[0] == null ? [] : [track[0]],
	);
	if (anchors.length === 0) {
		return [];
	}
	const direction = target.kind === "rows" ? "south" : "east";
	const steps: BspStep[] = [
		{ args: yabaiArgs.setSpaceLayout(spaceIdx, "bsp"), settleMs: 0 },
	];
	for (let index = 0; index < anchors.length; index++) {
		const anchor = anchors[index];
		if (anchor == null) {
			continue;
		}
		steps.push({
			args: yabaiArgs.moveWindowToSpace(anchor, spaceIdx),
			settleMs: ANCHOR_SETTLE_MS,
			unfloat: anchor,
		});
		if (index < anchors.length - 1) {
			steps.push({
				args: yabaiArgs.armInsert(anchor, direction),
				settleMs: STEP_SETTLE_MS,
			});
		}
	}
	const weights = target.weights ?? anchors.map(() => 1);
	const ratios = chainRatios(weights);
	const ratioSteps = ratios.flatMap((ratio, index) => {
		const anchor = anchors[index];
		return anchor == null
			? []
			: [
					{
						args: yabaiArgs.setSplitRatioArg(
							anchor,
							ratioArg(ratio, (line) => warn?.(`space ${spaceIdx}: ${line}`)),
						),
						settleMs: STEP_SETTLE_MS,
					},
				];
	});
	steps.push(...ratioSteps);
	for (const track of tracks) {
		const anchor = track[0];
		if (anchor == null) {
			continue;
		}
		for (const extra of track.slice(1)) {
			if (extra === anchor) {
				continue;
			}
			steps.push(
				{
					args: yabaiArgs.armInsert(anchor, "stack"),
					settleMs: STEP_SETTLE_MS,
				},
				{
					args: yabaiArgs.moveWindowToSpace(extra, spaceIdx),
					settleMs: EXTRA_SETTLE_MS,
					unfloat: extra,
				},
			);
		}
	}
	return steps;
}
/** yabai `space.type` → WmSpace.layout. A static membership table (Record, not Set). */
const LAYOUT_BY_TYPE: Readonly<Record<string, "bsp" | "stack" | "float">> = {
	bsp: "bsp",
	stack: "stack",
	float: "float",
};

// ── Raw yabai query JSON (kebab-case, as the CLI emits) ──────────────────

/** One element of `yabai -m query --windows`. */
export interface RawYabaiWindow {
	id: number;
	app: string;
	title: string;
	display: number;
	/** The space's live INDEX (not its stable id) — resolved to a SpaceId here. */
	space: number;
	/** False for a surface yabai tracks but cannot address (move, focus, …). */
	"has-ax-reference": boolean;
	"is-minimized": boolean;
	"is-floating": boolean;
	"is-sticky": boolean;
	"is-visible": boolean;
	"split-type": "vertical" | "horizontal" | "none";
	frame: { x: number; y: number; w: number; h: number };
}

/** One element of `yabai -m query --spaces`. */
export interface RawYabaiSpace {
	/** The stable mission-control id — this becomes the SpaceId. */
	id: number;
	/** The live index — renumbers on create/destroy/move. */
	index: number;
	label: string;
	display: number;
	windows: number[];
	type: string;
}

/** One element of `yabai -m query --displays`. */
export interface RawYabaiDisplay {
	index: number;
	frame: { x: number; y: number; w: number; h: number };
	/** Live space INDEXes on this display, ordered; `[0]` is home. */
	spaces: number[];
}

// ── Pure normalizers (kebab JSON → camelCase Wm* shapes) ─────────────────

/** Build the space-index → stable SpaceId lookup a window normalize needs. */
function indexToSpaceId(
	rawSpaces: ReadonlyArray<RawYabaiSpace>,
): Map<number, SpaceId> {
	const map = new Map<number, SpaceId>();
	for (const s of rawSpaces) {
		map.set(s.index, String(s.id) as SpaceId);
	}
	return map;
}

/** Normalize one raw window; its `.space` index is resolved to a stable SpaceId. */
export function normalizeWindow(
	raw: RawYabaiWindow,
	spaceIdByIndex: ReadonlyMap<number, SpaceId>,
): WmWindow {
	const spaceId = spaceIdByIndex.get(raw.space);
	if (spaceId == null) {
		throw new Error(
			`window ${raw.id} references unknown space index ${raw.space}`,
		);
	}
	return {
		id: raw.id,
		app: raw.app,
		title: raw.title,
		displayIdx: raw.display,
		spaceId,
		minimized: raw["is-minimized"],
		floating: raw["is-floating"],
		sticky: raw["is-sticky"],
		visible: raw["is-visible"],
		splitType: raw["split-type"],
		frame: { ...raw.frame },
	};
}

/**
 * Normalize a raw window list, resolving `.space` via a concurrent spaces
 * snapshot. Surfaces yabai holds no AX reference for are dropped: it cannot
 * move them, so planning for one strands an empty space.
 */
export function normalizeWindows(
	rawWindows: ReadonlyArray<RawYabaiWindow>,
	rawSpaces: ReadonlyArray<RawYabaiSpace>,
): WmWindow[] {
	const map = indexToSpaceId(rawSpaces);
	return rawWindows
		.filter((w) => w["has-ax-reference"])
		.map((w) => normalizeWindow(w, map));
}

/** Normalize one raw space. `windowIds` is ALL its windows (`.windows`). */
export function normalizeSpace(raw: RawYabaiSpace): WmSpace {
	return {
		id: String(raw.id) as SpaceId,
		label: raw.label,
		displayIdx: raw.display,
		windowIds: [...raw.windows],
		layout: LAYOUT_BY_TYPE[raw.type] ?? "bsp",
	};
}

/**
 * Normalize a raw display; its space INDEXes resolve to stable SpaceIds in
 * order. Throws on an unresolved index — symmetric with `normalizeWindow`, so
 * a query race that drops a space fails loud rather than silently shrinking a
 * display's `spaceIds` (whose [0] is the load-bearing home space).
 */
export function normalizeDisplay(
	raw: RawYabaiDisplay,
	spaceIdByIndex: ReadonlyMap<number, SpaceId>,
): WmDisplay {
	return {
		idx: raw.index,
		frame: { ...raw.frame },
		spaceIds: raw.spaces.map((i) => {
			const id = spaceIdByIndex.get(i);
			if (id == null) {
				throw new Error(
					`display ${raw.index} references unknown space index ${i}`,
				);
			}
			return id;
		}),
	};
}

// ── Pure argv builders (each returns yabai CLI argv, sans the yabai path) ──
//
// Every form is grounded in the bash scripts (see the shared doc's line refs).
// A space is ALWAYS addressed by its LIVE INDEX here — SpaceId resolution
// happens in the class, never in these pure builders.

export interface YabaiRuleAdd {
	label: string;
	app?: string;
	subrole?: string;
	displayIdx?: number;
	spaceIdx?: number;
	manage?: boolean;
}

export const yabaiArgs = {
	// Queries.
	queryWindows(): string[] {
		return ["-m", "query", "--windows"];
	},
	queryWindowsOnSpace(spaceIdx: number): string[] {
		return ["-m", "query", "--windows", "--space", String(spaceIdx)];
	},
	queryWindow(winId: number): string[] {
		return ["-m", "query", "--windows", "--window", String(winId)];
	},
	queryFocusedWindow(): string[] {
		return ["-m", "query", "--windows", "--window"];
	},
	querySpaces(): string[] {
		return ["-m", "query", "--spaces"];
	},
	queryFocusedSpace(): string[] {
		// ≙ `query --spaces --space`.
		return ["-m", "query", "--spaces", "--space"];
	},
	queryDisplays(): string[] {
		return ["-m", "query", "--displays"];
	},

	// Space lifecycle.
	createSpace(): string[] {
		// Appends an unlabelled space; the id is diffed out.
		return ["-m", "space", "--create"];
	},
	destroySpace(spaceIdx: number): string[] {
		return ["-m", "space", String(spaceIdx), "--destroy"];
	},
	labelSpace(spaceIdx: number, label: string): string[] {
		return ["-m", "space", String(spaceIdx), "--label", label];
	},
	setSpaceLayout(
		spaceIdx: number,
		layout: "bsp" | "stack" | "float",
	): string[] {
		return ["-m", "space", String(spaceIdx), "--layout", layout];
	},
	moveSpaceToIndex(spaceIdx: number, toIdx: number): string[] {
		// ≙ `space <cur> --move <pos>`.
		return ["-m", "space", String(spaceIdx), "--move", String(toIdx)];
	},
	balanceSpace(spaceIdx?: number): string[] {
		// ≙ `space --balance`; `space <idx> --balance` when scoped.
		return spaceIdx == null
			? ["-m", "space", "--balance"]
			: ["-m", "space", String(spaceIdx), "--balance"];
	},

	// Window placement.
	moveWindowToSpace(winId: number, spaceIdx: number): string[] {
		return ["-m", "window", String(winId), "--space", String(spaceIdx)];
	},
	moveWindowToDisplay(winId: number, sel: DisplaySel): string[] {
		// ≙ `window --display <idx>`.
		return ["-m", "window", String(winId), "--display", String(sel)];
	},
	setSplitRatio(winId: number, absRatio: number): string[] {
		return ["-m", "window", String(winId), "--ratio", `abs:${absRatio}`];
	},
	setSplitRatioArg(winId: number, ratio: string): string[] {
		return ["-m", "window", String(winId), "--ratio", ratio];
	},
	toggleSplit(winId: number): string[] {
		// ≙ `--toggle split`.
		return ["-m", "window", String(winId), "--toggle", "split"];
	},
	toggleFloat(winId: number): string[] {
		// ≙ `--toggle float`.
		return ["-m", "window", String(winId), "--toggle", "float"];
	},
	armInsert(
		winId: number,
		dir: "east" | "west" | "north" | "south" | "stack",
	): string[] {
		// ≙ `--insert east` / `--insert stack`.
		return ["-m", "window", String(winId), "--insert", dir];
	},
	swapWindows(sel: DirSel): string[] {
		// ≙ `window --swap <dir>`.
		return ["-m", "window", "--swap", sel];
	},
	warpWindow(sel: DirSel): string[] {
		// ≙ `window --warp <dir>`.
		return ["-m", "window", "--warp", sel];
	},
	resizeWindow(
		edge: "left" | "right" | "top" | "bottom",
		dx: number,
		dy: number,
	): string[] {
		// ≙ `window --resize right:d:0` / `left:-d:0`.
		return ["-m", "window", "--resize", `${edge}:${dx}:${dy}`];
	},

	// Focus.
	focusWindow(winId: number): string[] {
		// ≙ `window --focus <id>`.
		return ["-m", "window", "--focus", String(winId)];
	},
	focusWindowDir(sel: DirSel | StackSel): string[] {
		// ≙ `window --focus <dir|stack.next|…>`.
		return ["-m", "window", "--focus", sel];
	},
	focusDisplay(sel: DisplaySel): string[] {
		// ≙ `display --focus <idx|next|…>`.
		return ["-m", "display", "--focus", String(sel)];
	},

	// Rules.
	ruleList(): string[] {
		return ["-m", "rule", "--list"];
	},
	ruleRemove(label: string): string[] {
		return ["-m", "rule", "--remove", label];
	},
	ruleAdd(rule: YabaiRuleAdd): string[] {
		const args = ["-m", "rule", "--add", `label=${rule.label}`];
		if (rule.app != null) {
			args.push(`app=${rule.app}`);
		}
		if (rule.subrole != null) {
			args.push(`subrole=${rule.subrole}`);
		}
		if (rule.displayIdx != null) {
			args.push(`display=${rule.displayIdx}`);
		}
		if (rule.spaceIdx != null) {
			args.push(`space=${rule.spaceIdx}`);
		}
		if (rule.manage != null) {
			args.push(`manage=${rule.manage ? "on" : "off"}`);
		}
		return args;
	},
	ruleApply(): string[] {
		return ["-m", "rule", "--apply"];
	},

	// Signals (yabairc).
	signalAdd(event: WmEvent, command: ReadonlyArray<string>): string[] {
		// ≙ `signal --add event=<e> action=<cmd>`.
		return [
			"-m",
			"signal",
			"--add",
			`event=${event}`,
			`action=${command.join(" ")}`,
		];
	},
};

/** The result of one yabai invocation. */
interface RunResult {
	stdout: string;
	exitCode: number;
}

/**
 * The real yabai driver. Owns no persisted state — every SpaceId is resolved to
 * a live index by a fresh `query --spaces` at call time.
 */
export class YabaiDriver implements WmDriver {
	readonly settleMs = 150;
	readonly rules: WmRuleOps;
	readonly events: WmEventSource;
	readonly #yabaiPath: string;
	readonly #spawnTimeoutMs: number;

	constructor(opts: { yabaiPath?: string; spawnTimeoutMs?: number } = {}) {
		this.#yabaiPath = opts.yabaiPath ?? DEFAULT_YABAI_PATH;
		this.#spawnTimeoutMs = opts.spawnTimeoutMs ?? 5000;
		this.rules = {
			list: async () => {
				const raw = await this.#json<Array<{ label?: string }>>(
					yabaiArgs.ruleList(),
				);
				return raw.map((r) => ({ label: r.label ?? "" }));
			},
			remove: async (label) => {
				await this.#run(yabaiArgs.ruleRemove(label));
			},
			add: async (rule) => {
				await this.#run(yabaiArgs.ruleAdd(rule));
			},
			apply: async () => {
				await this.#run(yabaiArgs.ruleApply());
			},
		};
		this.events = {
			register: async (event, command) => {
				await this.#run(yabaiArgs.signalAdd(event, command));
			},
		};
	}

	// ── The one thin runner + JSON helpers ──
	async #run(args: string[]): Promise<RunResult> {
		const res = await $`${this.#yabaiPath} ${args}`.quiet().nothrow();
		return { stdout: res.stdout.toString(), exitCode: res.exitCode };
	}

	/** A query that throws on nonzero exit (driver-gone). */
	async #json<T>(args: string[]): Promise<T> {
		const { stdout, exitCode } = await this.#run(args);
		if (exitCode !== 0) {
			throw new Error(
				`yabai query failed (exit ${exitCode}): ${args.join(" ")}`,
			);
		}
		return JSON.parse(stdout) as T;
	}

	/**
	 * A query that tolerates a nonzero exit / empty output (→ null, e.g. no
	 * focused window/space). This intentionally maps ANY nonzero exit to null,
	 * so — unlike the mainline `#json` queries, which throw on driver-gone —
	 * the two focus queries return null even if yabai is actually down: exit
	 * code alone can't distinguish "nothing focused" from "driver gone", and
	 * the contract's null-means-none convention wins for focus.
	 */
	async #jsonOrNull<T>(args: string[]): Promise<T | null> {
		const { stdout, exitCode } = await this.#run(args);
		if (exitCode !== 0) {
			return null;
		}
		const trimmed = stdout.trim();
		if (trimmed === "") {
			return null;
		}
		return JSON.parse(trimmed) as T;
	}

	/** Resolve a stable SpaceId to its current live index (fresh query, stateless). */
	async #resolveIndex(id: SpaceId): Promise<number | null> {
		const spaces = await this.#json<RawYabaiSpace[]>(yabaiArgs.querySpaces());
		const n = Number(id);
		const sp = spaces.find((s) => s.id === n);
		return sp == null ? null : sp.index;
	}

	async spawnWindow(argv: ReadonlyArray<string>): Promise<void> {
		// A file, not a pipe: a lingering launcher must not block or SIGPIPE on stderr.
		const errPath = join(
			tmpdir(),
			`tess-spawn-${process.pid}-${Date.now()}.err`,
		);
		const child = Bun.spawn([...argv], {
			stdout: "ignore",
			stderr: Bun.file(errPath),
		});
		let timer: Timer | undefined;
		const exitCode = await Promise.race([
			child.exited,
			new Promise<null>((resolve) => {
				timer = setTimeout(() => resolve(null), this.#spawnTimeoutMs);
			}),
		]);
		clearTimeout(timer);
		if (exitCode == null) {
			child.unref();
			return;
		}
		const stderr = (
			await Bun.file(errPath)
				.text()
				.catch(() => "")
		).trim();
		rmSync(errPath, { force: true });
		if (exitCode !== 0) {
			throw new Error(
				`spawnWindow failed (exit ${exitCode}): ${argv.join(" ")}${stderr === "" ? "" : `: ${stderr}`}`,
			);
		}
	}

	// ── Queries ──
	async queryWindows(): Promise<WmWindow[]> {
		const [rawWindows, rawSpaces] = await Promise.all([
			this.#json<RawYabaiWindow[]>(yabaiArgs.queryWindows()),
			this.#json<RawYabaiSpace[]>(yabaiArgs.querySpaces()),
		]);
		return normalizeWindows(rawWindows, rawSpaces);
	}

	async queryWindowsOnSpace(id: SpaceId): Promise<WmWindow[]> {
		const idx = await this.#resolveIndex(id);
		if (idx == null) {
			return [];
		}
		const [rawWindows, rawSpaces] = await Promise.all([
			this.#json<RawYabaiWindow[]>(yabaiArgs.queryWindowsOnSpace(idx)),
			this.#json<RawYabaiSpace[]>(yabaiArgs.querySpaces()),
		]);
		return normalizeWindows(rawWindows, rawSpaces);
	}

	async querySpaces(): Promise<WmSpace[]> {
		const raw = await this.#json<RawYabaiSpace[]>(yabaiArgs.querySpaces());
		return raw.map((s) => normalizeSpace(s));
	}

	async queryDisplays(): Promise<WmDisplay[]> {
		const [rawDisplays, rawSpaces] = await Promise.all([
			this.#json<RawYabaiDisplay[]>(yabaiArgs.queryDisplays()),
			this.#json<RawYabaiSpace[]>(yabaiArgs.querySpaces()),
		]);
		const map = indexToSpaceId(rawSpaces);
		return rawDisplays.map((d) => normalizeDisplay(d, map));
	}

	async queryFocusedSpace(): Promise<WmSpace | null> {
		const raw = await this.#jsonOrNull<RawYabaiSpace>(
			yabaiArgs.queryFocusedSpace(),
		);
		return raw == null ? null : normalizeSpace(raw);
	}

	async queryFocusedWindow(): Promise<WmWindow | null> {
		const raw = await this.#jsonOrNull<RawYabaiWindow>(
			yabaiArgs.queryFocusedWindow(),
		);
		if (raw == null) {
			return null;
		}
		const rawSpaces = await this.#json<RawYabaiSpace[]>(
			yabaiArgs.querySpaces(),
		);
		return normalizeWindow(raw, indexToSpaceId(rawSpaces));
	}

	// ── Space lifecycle ──
	async createSpace(_displayIdx: number): Promise<SpaceId | null> {
		// yabai `--create` appends an unlabelled space and returns nothing; on
		// Tahoe it always lands on the laptop display, so the
		// requested displayIdx cannot be honored at creation — the caller
		// repositions with moveSpaceToIndex. Identify the new space by the
		// set-difference idiom: snapshot the stable ids
		// before, create, and the one new id is the diff.
		const before = await this.#json<RawYabaiSpace[]>(yabaiArgs.querySpaces());
		const beforeIds = new Set(before.map((s) => s.id));
		await this.#run(yabaiArgs.createSpace());
		let created: RawYabaiSpace | undefined;
		for (let attempt = 0; attempt < 10; attempt++) {
			if (attempt > 0) {
				await Bun.sleep(50);
			}
			const after = await this.#json<RawYabaiSpace[]>(yabaiArgs.querySpaces());
			created = after.find((s) => !beforeIds.has(s.id));
			if (created != null) {
				break;
			}
		}
		return created == null ? null : (String(created.id) as SpaceId);
	}

	async destroySpace(id: SpaceId): Promise<boolean> {
		const idx = await this.#resolveIndex(id);
		if (idx == null) {
			return false;
		}
		const { exitCode } = await this.#run(yabaiArgs.destroySpace(idx));
		return exitCode === 0;
	}

	async labelSpace(id: SpaceId, label: string): Promise<void> {
		const idx = await this.#resolveIndex(id);
		if (idx == null) {
			return;
		}
		await this.#run(yabaiArgs.labelSpace(idx, label));
		await Bun.sleep(50);
	}

	async setSpaceLayout(
		id: SpaceId,
		layout: "bsp" | "stack" | "float",
	): Promise<void> {
		const idx = await this.#resolveIndex(id);
		if (idx == null) {
			return;
		}
		await this.#run(yabaiArgs.setSpaceLayout(idx, layout));
	}

	async moveSpaceToIndex(id: SpaceId, toIdx: number): Promise<void> {
		const idx = await this.#resolveIndex(id);
		if (idx == null) {
			return;
		}
		await this.#run(yabaiArgs.moveSpaceToIndex(idx, toIdx));
	}

	async balanceSpace(id?: SpaceId): Promise<void> {
		if (id == null) {
			await this.#run(yabaiArgs.balanceSpace());
			return;
		}
		const idx = await this.#resolveIndex(id);
		if (idx == null) {
			return;
		}
		await this.#run(yabaiArgs.balanceSpace(idx));
	}

	// Execute the pure bsp recipe so ratios and move settling can be golden-tested.
	async realizeSpaceLayout(
		id: SpaceId,
		target: SpaceLayoutTarget,
	): Promise<void> {
		const idx = await this.#resolveIndex(id);
		if (idx == null) {
			return;
		}
		if (target.kind === "stack") {
			for (const wid of target.tracks.flat()) {
				await this.#run(yabaiArgs.moveWindowToSpace(wid, idx));
				await Bun.sleep(EXTRA_SETTLE_MS);
				await this.#unfloatOne(wid);
			}
			await this.#run(yabaiArgs.setSpaceLayout(idx, "stack"));
			return;
		}
		for (const step of bspSteps(idx, target, (line) =>
			process.stderr.write(`${line}\n`),
		)) {
			await this.#run(step.args);
			if (step.settleMs > 0) {
				await Bun.sleep(step.settleMs);
			}
			if (step.unfloat != null) {
				await this.#unfloatOne(step.unfloat);
			}
		}
	}

	/** Re-tile a single window if it is floating (_unfloat_one). */
	async #unfloatOne(winId: number): Promise<void> {
		const raw = await this.#jsonOrNull<RawYabaiWindow>(
			yabaiArgs.queryWindow(winId),
		);
		if (raw?.["is-floating"]) {
			await this.#run(yabaiArgs.toggleFloat(winId));
			await Bun.sleep(STEP_SETTLE_MS);
		}
	}

	// ── Window placement ──
	async moveWindowToSpace(winId: number, id: SpaceId): Promise<void> {
		const idx = await this.#resolveIndex(id);
		if (idx == null) {
			return;
		}
		await this.#run(yabaiArgs.moveWindowToSpace(winId, idx));
		await Bun.sleep(100);
	}

	async moveWindowToDisplay(winId: number, sel: DisplaySel): Promise<boolean> {
		const { exitCode } = await this.#run(
			yabaiArgs.moveWindowToDisplay(winId, sel),
		);
		return exitCode === 0;
	}

	async setSplitRatio(winId: number, absRatio: number): Promise<void> {
		await this.#run(yabaiArgs.setSplitRatio(winId, absRatio));
	}

	async toggleSplit(winId: number): Promise<void> {
		await this.#run(yabaiArgs.toggleSplit(winId));
	}

	async toggleFloat(winId: number): Promise<void> {
		await this.#run(yabaiArgs.toggleFloat(winId));
	}

	async armInsert(
		winId: number,
		dir: "east" | "west" | "north" | "south" | "stack",
	): Promise<void> {
		await this.#run(yabaiArgs.armInsert(winId, dir));
	}

	async stackOnto(targetWinId: number, winId: number): Promise<void> {
		// yabai has no atomic stack-onto: arm insert-stack on the target, then move
		// the window onto the target's space so it joins the stack (types.ts:167-168,
		// theextra-stacking step generalized).
		const target = await this.#jsonOrNull<RawYabaiWindow>(
			yabaiArgs.queryWindow(targetWinId),
		);
		if (target == null) {
			return;
		}
		await this.#run(yabaiArgs.armInsert(targetWinId, "stack"));
		await this.#run(yabaiArgs.moveWindowToSpace(winId, target.space));
	}

	async swapWindows(sel: DirSel): Promise<boolean> {
		const { exitCode } = await this.#run(yabaiArgs.swapWindows(sel));
		return exitCode === 0;
	}

	async warpWindow(sel: DirSel): Promise<boolean> {
		const { exitCode } = await this.#run(yabaiArgs.warpWindow(sel));
		return exitCode === 0;
	}

	async resizeWindow(
		edge: "left" | "right" | "top" | "bottom",
		dx: number,
		dy: number,
	): Promise<boolean> {
		const { exitCode } = await this.#run(yabaiArgs.resizeWindow(edge, dx, dy));
		return exitCode === 0;
	}

	// ── Focus ──
	async focusWindow(winId: number): Promise<boolean> {
		const { exitCode } = await this.#run(yabaiArgs.focusWindow(winId));
		return exitCode === 0;
	}

	async focusWindowDir(sel: DirSel | StackSel): Promise<boolean> {
		const { exitCode } = await this.#run(yabaiArgs.focusWindowDir(sel));
		return exitCode === 0;
	}

	async focusDisplay(sel: DisplaySel): Promise<boolean> {
		const { exitCode } = await this.#run(yabaiArgs.focusDisplay(sel));
		return exitCode === 0;
	}
}
