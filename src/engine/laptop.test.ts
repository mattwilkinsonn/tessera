// laptop (T3): the four-phase laptop converger as a pure, resumable
// step-planner. Driven here in a loop against a tiny
// in-memory fake world that applies each emitted ConvergeAction and feeds the
// fresh snapshot back, exactly as the real executor (T5) will.

import { describe, expect, test } from "bun:test";
import { profile } from "../config/profile.ts";
import type { SpaceId, WmDisplay, WmSpace, WmWindow } from "../driver/types.ts";
import {
	type ConvergeAction,
	initialConvergeState,
	laptopConvergeStep,
} from "./laptop.ts";
import type { WorldSnapshot } from "./world.ts";

const LAPTOP_IDX = 1;
const LAPTOP_W = profile.displays.laptop.width;
const HOME: SpaceId = "s-home" as SpaceId;

function win(
	id: number,
	app: string,
	opts: {
		title?: string;
		spaceId?: SpaceId;
		minimized?: boolean;
		floating?: boolean;
	} = {},
): WmWindow {
	return {
		id,
		app,
		title: opts.title ?? "",
		displayIdx: LAPTOP_IDX,
		spaceId: opts.spaceId ?? HOME,
		minimized: opts.minimized ?? false,
		floating: opts.floating ?? false,
		sticky: false,
		visible: true,
		splitType: "none",
		frame: { x: 0, y: 0, w: 100, h: 100 },
	};
}

/**
 * A minimal mutable in-memory window world: spaces keyed by stable SpaceId on a
 * single laptop display. Applies each ConvergeAction the planner emits, then
 * hands back a fresh immutable snapshot — the fake executor the planner drives.
 */
class FakeWorld {
	private windows: WmWindow[];
	private spaces: WmSpace[];
	private spaceOrder: SpaceId[];
	private nextSpace = 1;
	private readonly createSpaceEnabled: boolean;

	constructor(
		windows: WmWindow[],
		staleLabels: string[] = [],
		createSpaceEnabled = true,
	) {
		this.windows = windows;
		this.createSpaceEnabled = createSpaceEnabled;
		this.spaces = [
			{
				id: HOME,
				label: "",
				displayIdx: LAPTOP_IDX,
				windowIds: [],
				layout: "bsp",
			},
		];
		this.spaceOrder = [HOME];
		for (const label of staleLabels) {
			this.addSpace(label);
		}
		this.recomputeWindowIds();
	}

	private addSpace(label: string): SpaceId {
		const id = `s-${this.nextSpace++}` as SpaceId;
		this.spaces.push({
			id,
			label,
			displayIdx: LAPTOP_IDX,
			windowIds: [],
			layout: "bsp",
		});
		this.spaceOrder.push(id);
		return id;
	}

	private recomputeWindowIds(): void {
		const bySpace = new Map<SpaceId, number[]>();
		for (const sp of this.spaces) {
			bySpace.set(sp.id, []);
		}
		for (const w of this.windows) {
			bySpace.get(w.spaceId)?.push(w.id);
		}
		this.spaces = this.spaces.map((sp) => ({
			...sp,
			windowIds: bySpace.get(sp.id) ?? [],
		}));
	}

	snapshot(): WorldSnapshot {
		const orderedSpaces = this.spaceOrder.map(
			(id) => this.spaces.find((sp) => sp.id === id) as WmSpace,
		);
		const display: WmDisplay = {
			idx: LAPTOP_IDX,
			frame: { x: 0, y: 0, w: LAPTOP_W, h: 1117 },
			spaceIds: [...this.spaceOrder],
		};
		return {
			windows: this.windows.map((w) => ({ ...w })),
			spaces: orderedSpaces.map((sp) => ({ ...sp })),
			displays: [display],
		};
	}

	apply(a: ConvergeAction): void {
		switch (a.op) {
			case "relabelHome": {
				this.spaces = this.spaces.map((sp) =>
					sp.id === a.homeSpace ? { ...sp, label: a.label } : sp,
				);
				break;
			}
			case "createSpace": {
				if (this.createSpaceEnabled) {
					this.addSpace(a.label);
				}
				break;
			}
			case "moveWindow": {
				this.windows = this.windows.map((w) =>
					w.id === a.windowId ? { ...w, spaceId: a.toSpace } : w,
				);
				this.recomputeWindowIds();
				break;
			}
			case "rehomeAndDestroy": {
				this.windows = this.windows.map((w) =>
					w.spaceId === a.staleSpace ? { ...w, spaceId: a.homeSpace } : w,
				);
				this.spaces = this.spaces.filter((sp) => sp.id !== a.staleSpace);
				this.spaceOrder = this.spaceOrder.filter((id) => id !== a.staleSpace);
				this.recomputeWindowIds();
				break;
			}
			case "moveSpace": {
				const from = this.spaceOrder.indexOf(a.space);
				if (from >= 0) {
					this.spaceOrder.splice(from, 1);
					this.spaceOrder.splice(a.toIndex - 1, 0, a.space);
				}
				break;
			}
			case "setLayout": {
				this.spaces = this.spaces.map((sp) =>
					sp.id === a.space ? { ...sp, layout: a.layout } : sp,
				);
				break;
			}
		}
	}

	labelOrder(): string[] {
		return this.spaceOrder.map(
			(id) => (this.spaces.find((sp) => sp.id === id) as WmSpace).label,
		);
	}

	spaceIdOrder(): SpaceId[] {
		return [...this.spaceOrder];
	}
}

/** Drive the planner to completion, returning every emitted action. */
function runConverge(
	world: FakeWorld,
	persistedFlexOrder: readonly string[] = [],
): ConvergeAction[] {
	let state = initialConvergeState(HOME, persistedFlexOrder);
	const actions: ConvergeAction[] = [];
	for (let guard = 0; guard < 1000; guard++) {
		const r = laptopConvergeStep(profile, world.snapshot(), state);
		if ("done" in r) {
			return actions;
		}
		actions.push(r.action);
		world.apply(r.action);
		state = r.state;
	}
	throw new Error("converge did not terminate within 1000 steps");
}

describe("laptopConvergeStep (, four phases)", () => {
	test("5th Arc → lap-arc-5 (D10)", () => {
		// 4 pinned arcs claim ids 1-4; the 5th flexes, continuing the sequence.
		const world = new FakeWorld([
			win(1, "Arc"),
			win(2, "Arc"),
			win(3, "Arc"),
			win(4, "Arc"),
			win(5, "Arc"),
		]);
		runConverge(world);
		expect(world.labelOrder()).toContain("lap-arc-5");
	});

	test("absent pinned app → no space (D7)", () => {
		// Only Arc windows are live; linear/obsidian/etc. have no window.
		const world = new FakeWorld([win(1, "Arc"), win(2, "Arc")]);
		runConverge(world);
		const labels = world.labelOrder();
		expect(labels).not.toContain("lap-linear");
		expect(labels).not.toContain("lap-obsidian");
		expect(labels).not.toContain("lap-ghostty-wave");
	});

	test("CREATE_FAILED aborts before the destructive reconcile", () => {
		// createSpace is a NO-OP (dead addSpace pointer). Seed a stale lap-* space
		// that phase C WOULD destroy — the abort must leave it intact.
		const world = new FakeWorld([win(1, "Arc")], ["lap-oldapp"], false);
		const actions = runConverge(world);
		expect(actions.some((a) => a.op === "rehomeAndDestroy")).toBe(false);
		expect(actions.some((a) => a.op === "moveSpace")).toBe(false);
		// The grid is left intact: the stale space still exists.
		expect(world.labelOrder()).toContain("lap-oldapp");
	});

	test("reconcile destroys only the untargeted lap-* space", () => {
		const world = new FakeWorld([win(1, "Arc"), win(2, "Arc")], ["lap-oldapp"]);
		const staleId = world.spaceIdOrder()[1];
		const actions = runConverge(world);
		const destroys = actions.filter((a) => a.op === "rehomeAndDestroy");
		expect(destroys).toHaveLength(1);
		const only = destroys[0];
		if (only == null || only.op !== "rehomeAndDestroy") {
			throw new Error("expected a rehomeAndDestroy action");
		}
		expect(only.staleSpace).toBe(staleId as SpaceId);
		expect(only.homeSpace).toBe(HOME);
		// The stale space is gone; targeted lap-* and the home space survive.
		const labels = world.labelOrder();
		expect(labels).not.toContain("lap-oldapp");
		expect(labels).toContain("lap-arc");
		expect(labels).toContain("laptop");
	});

	test("final order is home, then desiredOrder (pinned core, then flex tail)", () => {
		// 4 pinned arcs (lap-arc..lap-arc-4), then a 5th arc + spotify flex tail
		// in ascending-id / stable-append order (lap-arc-5, lap-spotify).
		const world = new FakeWorld([
			win(1, "Arc"),
			win(2, "Arc"),
			win(3, "Arc"),
			win(4, "Arc"),
			win(5, "Arc"),
			win(6, "Spotify"),
		]);
		runConverge(world);
		expect(world.labelOrder()).toEqual([
			"laptop",
			"lap-arc",
			"lap-arc-2",
			"lap-arc-3",
			"lap-arc-4",
			"lap-arc-5",
			"lap-spotify",
		]);
	});

	test("SpaceIds are stable across the whole converge (D1)", () => {
		const world = new FakeWorld([
			win(1, "Arc"),
			win(2, "Arc"),
			win(3, "Spotify"),
		]);
		let state = initialConvergeState(HOME, []);
		const referenced = new Set<SpaceId>();
		for (let guard = 0; guard < 1000; guard++) {
			const r = laptopConvergeStep(profile, world.snapshot(), state);
			if ("done" in r) {
				break;
			}
			const a = r.action;
			if (a.op === "moveWindow") {
				referenced.add(a.toSpace);
			} else if (a.op === "moveSpace") {
				referenced.add(a.space);
			}
			world.apply(a);
			state = r.state;
		}
		// Every SpaceId the planner addressed still exists at the end — an
		// index-derived id would have drifted as spaces were created/moved.
		const live = new Set(world.spaceIdOrder());
		for (const id of referenced) {
			expect(live.has(id)).toBe(true);
		}
		// The home space kept its identity throughout.
		expect(live.has(HOME)).toBe(true);
	});

	test("phase D reorders pre-existing out-of-order lap-* spaces (moveSpace)", () => {
		// Seed three ALREADY-targeted lap-* spaces in REVERSE order. Phase A
		// reuses them (they exist → moveWindow, no createSpace), so phase D is
		// the only thing that can correct the order — it must emit moveSpace,
		// the one index-typed op. This is the corrective path the aligned-order
		// tests never reach: `cur === target` is always true when spaces are
		// created in desiredOrder, so the target arithmetic (homePos + 1 +
		// placed), the placed/cursor threading, and the splice model go
		// uncovered without this.
		const world = new FakeWorld(
			[win(1, "Arc"), win(2, "Arc"), win(3, "Arc")],
			["lap-arc-3", "lap-arc-2", "lap-arc"],
		);
		const actions = runConverge(world);
		const moves = actions.filter(
			(a): a is Extract<ConvergeAction, { op: "moveSpace" }> =>
				a.op === "moveSpace",
		);
		// Two corrective moves; the third space falls into place under the
		// left-shift. Each toIndex is homePos(1) + 1 + placementOrdinal → 2, 3.
		expect(moves.map((m) => m.toIndex)).toEqual([2, 3]);
		expect(world.labelOrder()).toEqual([
			"laptop",
			"lap-arc",
			"lap-arc-2",
			"lap-arc-3",
		]);
	});
});
