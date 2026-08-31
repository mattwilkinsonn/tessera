// Layer 3 — the in-memory FakeDriver test double.
//
// FakeDriver implements WmDriver over a mutable in-memory world, modeling the
// yabai semantics the engine relies on — above all the D1 invariant it is the
// NAMED test for: a space's stable `SpaceId` survives while its live index
// renumbers on destroy/create/move. That mirrors the yabai ground truth
// (`query --spaces` exposes a stable `.id` AND a renumbering `.index`; a
// window's `.space` field is the index, resolved to the stable id here).
// exec.test.ts drives the executor/converger against this Fake to a fixed
// point with no live yabai.
//
// Scope: it faithfully models the space lifecycle + window placement + query
// surface the desk/snap plans and the laptop converger exercise. The
// interaction verbs (directional focus/swap/warp/resize, split/insert arming)
// are observable-minimal — the Fake tracks no pixel geometry, so a
// balance/resize/ratio has no id-level effect and a directional op resolves
// truthfully without a spatial model. This is an id/space-level test double,
// not a pixel-accurate yabai simulator.

import type {
	DirSel,
	DisplaySel,
	SpaceId,
	SpaceLayoutTarget,
	StackSel,
	WmDisplay,
	WmDriver,
	WmSpace,
	WmWindow,
} from "./types.ts";

type Frame = { x: number; y: number; w: number; h: number };
type Layout = "bsp" | "stack" | "float";
type SplitType = "vertical" | "horizontal" | "none";

interface FakeSpace {
	stableId: number;
	label: string;
	displayIdx: number;
	layout: Layout;
}

interface FakeWindow {
	id: number;
	app: string;
	title: string;
	displayIdx: number;
	spaceStableId: number;
	minimized: boolean;
	floating: boolean;
	sticky: boolean;
	visible: boolean;
	splitType: SplitType;
	frame: Frame;
}

export interface FakeSeedDisplay {
	idx: number;
	frame?: Frame;
}

export interface FakeSeedSpace {
	label?: string;
	displayIdx: number;
	layout?: Layout;
}

export interface FakeSeedWindow {
	id: number;
	app: string;
	title?: string;
	/** 1-based GLOBAL space index at seed time (the index yabai renumbers). */
	spaceIndex: number;
	minimized?: boolean;
	floating?: boolean;
	sticky?: boolean;
	visible?: boolean;
	splitType?: SplitType;
	frame?: Frame;
}

export interface FakeSeed {
	displays?: ReadonlyArray<FakeSeedDisplay>;
	spaces?: ReadonlyArray<FakeSeedSpace>;
	windows?: ReadonlyArray<FakeSeedWindow>;
	/** Base settle unit; 0 (default) so tests never sleep. */
	settleMs?: number;
}

const LAPTOP_FRAME: Frame = { x: 0, y: 0, w: 1728, h: 1117 };
const DEFAULT_WIN_FRAME: Frame = { x: 0, y: 0, w: 100, h: 100 };

/**
 * An in-memory `WmDriver`. Spaces live in a single array kept in GLOBAL INDEX
 * ORDER — a space's live index is `position + 1`, so a destroy/move shifts the
 * indexes of later spaces while their stable ids (the `SpaceId`s) are untouched.
 */
export class FakeDriver implements WmDriver {
	#spaces: FakeSpace[] = [];
	#windows: FakeWindow[] = [];
	#displays: Array<{ idx: number; frame: Frame }> = [];
	#nextSpaceId = 1;
	#focusedWindowId: number | null = null;
	readonly settleMs: number;

	constructor(seed: FakeSeed = {}) {
		this.settleMs = seed.settleMs ?? 0;
		this.#displays = (seed.displays ?? [{ idx: 1, frame: LAPTOP_FRAME }]).map(
			(d) => ({ idx: d.idx, frame: d.frame ?? LAPTOP_FRAME }),
		);
		for (const s of seed.spaces ?? []) {
			this.#spaces.push({
				stableId: this.#nextSpaceId++,
				label: s.label ?? "",
				displayIdx: s.displayIdx,
				layout: s.layout ?? "bsp",
			});
		}
		for (const w of seed.windows ?? []) {
			const sp = this.#spaces[w.spaceIndex - 1];
			if (sp == null) {
				throw new Error(
					`seed window ${w.id} references missing space index ${w.spaceIndex}`,
				);
			}
			this.#windows.push({
				id: w.id,
				app: w.app,
				title: w.title ?? "",
				displayIdx: sp.displayIdx,
				spaceStableId: sp.stableId,
				minimized: w.minimized ?? false,
				floating: w.floating ?? false,
				sticky: w.sticky ?? false,
				visible: w.visible ?? true,
				splitType: w.splitType ?? "none",
				frame: w.frame ?? { ...DEFAULT_WIN_FRAME },
			});
		}
	}

	/** Model a yabai restart: space labels do not survive it (design). */
	restart(): void {
		for (const s of this.#spaces) {
			s.label = "";
		}
	}

	#sid(stableId: number): SpaceId {
		return String(stableId) as SpaceId;
	}

	#byId(id: SpaceId): FakeSpace | undefined {
		const n = Number(id);
		return this.#spaces.find((s) => s.stableId === n);
	}

	#winById(winId: number): FakeWindow | undefined {
		return this.#windows.find((w) => w.id === winId);
	}

	#toWmWindow(w: FakeWindow): WmWindow {
		return {
			id: w.id,
			app: w.app,
			title: w.title,
			displayIdx: w.displayIdx,
			spaceId: this.#sid(w.spaceStableId),
			minimized: w.minimized,
			floating: w.floating,
			sticky: w.sticky,
			visible: w.visible,
			splitType: w.splitType,
			frame: { ...w.frame },
		};
	}

	#toWmSpace(s: FakeSpace): WmSpace {
		const windowIds = this.#windows
			.filter((w) => w.spaceStableId === s.stableId)
			.map((w) => w.id);
		return {
			id: this.#sid(s.stableId),
			label: s.label,
			displayIdx: s.displayIdx,
			windowIds,
			layout: s.layout,
		};
	}

	// ── Queries ──
	async queryWindows(): Promise<WmWindow[]> {
		return this.#windows.map((w) => this.#toWmWindow(w));
	}

	async queryWindowsOnSpace(id: SpaceId): Promise<WmWindow[]> {
		const n = Number(id);
		return this.#windows
			.filter((w) => w.spaceStableId === n)
			.map((w) => this.#toWmWindow(w));
	}

	async querySpaces(): Promise<WmSpace[]> {
		return this.#spaces.map((s) => this.#toWmSpace(s));
	}

	async queryDisplays(): Promise<WmDisplay[]> {
		return this.#displays.map((d) => ({
			idx: d.idx,
			frame: { ...d.frame },
			spaceIds: this.#spaces
				.filter((s) => s.displayIdx === d.idx)
				.map((s) => this.#sid(s.stableId)),
		}));
	}

	async queryFocusedSpace(): Promise<WmSpace | null> {
		// Match YabaiDriver: null when nothing is focused, no first-space
		// fallback — the two drivers must agree on this contract method so a
		// converger can switch between them without a semantic surprise.
		if (this.#focusedWindowId == null) {
			return null;
		}
		const fw = this.#winById(this.#focusedWindowId);
		if (fw == null) {
			return null;
		}
		const sp = this.#spaces.find((s) => s.stableId === fw.spaceStableId);
		return sp == null ? null : this.#toWmSpace(sp);
	}

	async queryFocusedWindow(): Promise<WmWindow | null> {
		if (this.#focusedWindowId == null) {
			return null;
		}
		const w = this.#winById(this.#focusedWindowId);
		return w == null ? null : this.#toWmWindow(w);
	}

	// ── Space lifecycle ──
	async createSpace(displayIdx: number): Promise<SpaceId | null> {
		const sp: FakeSpace = {
			stableId: this.#nextSpaceId++,
			label: "",
			displayIdx,
			layout: "bsp",
		};
		// Append within the display's group: insert after its last existing space
		// (else at the end), so the new space belongs to `displayIdx` in index order.
		let insertAt = this.#spaces.length;
		for (let i = this.#spaces.length - 1; i >= 0; i--) {
			const s = this.#spaces[i];
			if (s != null && s.displayIdx === displayIdx) {
				insertAt = i + 1;
				break;
			}
		}
		this.#spaces.splice(insertAt, 0, sp);
		return this.#sid(sp.stableId);
	}

	async destroySpace(id: SpaceId): Promise<boolean> {
		const sp = this.#byId(id);
		if (sp == null) {
			return false;
		}
		const onDisplay = this.#spaces.filter(
			(s) => s.displayIdx === sp.displayIdx,
		);
		// Never destroy a display's last space.
		if (onDisplay.length <= 1) {
			return false;
		}
		// yabai relocates residual windows to an adjacent same-display space
		// — nothing is closed.
		const target = onDisplay.find((s) => s !== sp);
		if (target != null) {
			for (const w of this.#windows) {
				if (w.spaceStableId === sp.stableId) {
					w.spaceStableId = target.stableId;
					w.displayIdx = target.displayIdx;
				}
			}
		}
		this.#spaces.splice(this.#spaces.indexOf(sp), 1);
		return true;
	}

	async labelSpace(id: SpaceId, label: string): Promise<void> {
		const sp = this.#byId(id);
		if (sp != null) {
			sp.label = label;
		}
	}

	async setSpaceLayout(id: SpaceId, layout: Layout): Promise<void> {
		const sp = this.#byId(id);
		if (sp != null) {
			sp.layout = layout;
		}
	}

	async moveSpaceToIndex(id: SpaceId, toIdx: number): Promise<void> {
		const sp = this.#byId(id);
		if (sp == null) {
			return;
		}
		this.#spaces.splice(this.#spaces.indexOf(sp), 1);
		const clamped = Math.max(1, Math.min(toIdx, this.#spaces.length + 1));
		this.#spaces.splice(clamped - 1, 0, sp);
	}

	async balanceSpace(_id?: SpaceId): Promise<void> {
		// id-level model tracks no pixel ratios — balance has no observable effect.
	}

	// ── Layout realization (D2) ──
	async realizeSpaceLayout(
		id: SpaceId,
		target: SpaceLayoutTarget,
	): Promise<void> {
		const sp = this.#byId(id);
		if (sp == null) {
			return;
		}
		// Move the resolved column windows onto the space and unfloat them; set the
		// observable layout. The park→insert-east→ratio→stack recipe and its settle
		// cadence are YabaiDriver detail with no id-level effect.
		for (const wid of target.columns.flat()) {
			const w = this.#winById(wid);
			if (w != null) {
				w.spaceStableId = sp.stableId;
				w.displayIdx = sp.displayIdx;
				w.floating = false;
			}
		}
		sp.layout = target.kind === "stack" ? "stack" : "bsp";
	}

	// ── Window placement ──
	async moveWindowToSpace(winId: number, id: SpaceId): Promise<void> {
		const w = this.#winById(winId);
		const sp = this.#byId(id);
		if (w != null && sp != null) {
			w.spaceStableId = sp.stableId;
			w.displayIdx = sp.displayIdx;
		}
	}

	async moveWindowToDisplay(winId: number, sel: DisplaySel): Promise<boolean> {
		const w = this.#winById(winId);
		if (w == null) {
			return false;
		}
		const target = this.#resolveDisplaySel(sel, w.displayIdx);
		if (target == null) {
			return false;
		}
		const home = this.#spaces.find((s) => s.displayIdx === target);
		if (home == null) {
			return false;
		}
		w.displayIdx = target;
		w.spaceStableId = home.stableId;
		return true;
	}

	async setSplitRatio(_winId: number, _absRatio: number): Promise<void> {
		// No pixel model — nothing to record.
	}

	async toggleSplit(winId: number): Promise<void> {
		const w = this.#winById(winId);
		if (w != null) {
			w.splitType = w.splitType === "vertical" ? "horizontal" : "vertical";
		}
	}

	async toggleFloat(winId: number): Promise<void> {
		const w = this.#winById(winId);
		if (w != null) {
			w.floating = !w.floating;
		}
	}

	async armInsert(): Promise<void> {
		// Transient yabai insert-feedback state — nothing to model at the id level.
	}

	async stackOnto(targetWinId: number, winId: number): Promise<void> {
		const t = this.#winById(targetWinId);
		const w = this.#winById(winId);
		if (t != null && w != null) {
			w.spaceStableId = t.spaceStableId;
			w.displayIdx = t.displayIdx;
		}
	}

	async swapWindows(_sel: DirSel): Promise<boolean> {
		return true;
	}

	async warpWindow(_sel: DirSel): Promise<boolean> {
		return true;
	}

	async resizeWindow(): Promise<boolean> {
		return true;
	}

	// ── Focus ──
	async focusWindow(winId: number): Promise<boolean> {
		const w = this.#winById(winId);
		if (w == null) {
			return false;
		}
		this.#focusedWindowId = winId;
		return true;
	}

	async focusWindowDir(_sel: DirSel | StackSel): Promise<boolean> {
		return true;
	}

	async focusDisplay(_sel: DisplaySel): Promise<boolean> {
		return true;
	}

	#resolveDisplaySel(sel: DisplaySel, from: number): number | null {
		const idxs = this.#displays.map((d) => d.idx).sort((a, b) => a - b);
		if (typeof sel === "number") {
			return idxs.includes(sel) ? sel : null;
		}
		if (idxs.length === 0) {
			return null;
		}
		const pos = idxs.indexOf(from);
		if (sel === "first") {
			return idxs[0] ?? null;
		}
		if (sel === "last") {
			return idxs[idxs.length - 1] ?? null;
		}
		if (sel === "next") {
			return idxs[(pos + 1) % idxs.length] ?? null;
		}
		// "prev"
		return idxs[(pos - 1 + idxs.length) % idxs.length] ?? null;
	}
}
