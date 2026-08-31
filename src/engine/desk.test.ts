// desk (T4): the desk-mode plan builder — a pure port of's
// engine side. Builds a WorldSnapshot from SpaceId casts + small win/space/
// display helpers (like reap.test.ts) and asserts the exact PlanOp[]: destroy
// preludes first, then per present display (relabelHome always, realizeLayout
// only when a column claimed a window), in profile.desk order.

import { describe, expect, test } from "bun:test";
import { profile } from "../config/profile.ts";
import type { SpaceId, WmDisplay, WmSpace, WmWindow } from "../driver/types.ts";
import { deskPlan } from "./desk.ts";
import type { WorldSnapshot } from "./world.ts";

function win(
	id: number,
	app: string,
	displayIdx: number,
	spaceId: string,
	title = "",
): WmWindow {
	return {
		id,
		app,
		title,
		displayIdx,
		spaceId: spaceId as SpaceId,
		minimized: false,
		floating: false,
		sticky: false,
		visible: true,
		splitType: "none",
		frame: { x: 0, y: 0, w: 100, h: 100 },
	};
}

function space(
	id: string,
	label: string,
	displayIdx: number,
	windowIds: number[] = [],
): WmSpace {
	return { id: id as SpaceId, label, displayIdx, windowIds, layout: "bsp" };
}

function display(idx: number, width: number, spaceIds: string[]): WmDisplay {
	return {
		idx,
		frame: { x: 0, y: 0, w: width, h: 1000 },
		spaceIds: spaceIds.map((s) => s as SpaceId),
	};
}

function world(
	displays: WmDisplay[],
	spaces: WmSpace[],
	windows: WmWindow[],
): WorldSnapshot {
	return { windows, spaces, displays };
}

// Display widths from profile.displays.
const G9 = 5120;
const AW = 3440;
const LAPTOP = 1728;

describe("deskPlan", () => {
	test("full app set golden — three displays, every desk app present", () => {
		// g9 idx 1 / home s1, aw idx 2 / home s2, laptop idx 3 / home s3.
		const displays = [
			display(1, G9, ["s1"]),
			display(2, AW, ["s2"]),
			display(3, LAPTOP, ["s3"]),
		];
		const spaces = [
			space("s1", "main", 1),
			space("s2", "plan", 2),
			space("s3", "laptop", 3),
		];
		// Enough Arc windows: g9 needs 1, aw needs 2, laptop needs 1 → 4 distinct.
		const windows = [
			// g9: arc, obsidian | ghostty-wave | ghostty-mbp, vscode
			win(10, "Arc", 1, "s1"),
			win(11, "Obsidian", 1, "s1"),
			win(12, "Ghostty", 1, "s1", "pc"),
			win(13, "Ghostty", 1, "s1", "mbp"),
			win(14, "Code", 1, "s1"),
			// aw: linear, arc | arc, akiflow
			win(20, "Linear", 2, "s2"),
			win(21, "Arc", 2, "s2"),
			win(22, "Arc", 2, "s2"),
			win(23, "Akiflow", 2, "s2"),
			// laptop: arc, spotify, discord, qalculate
			win(30, "Arc", 3, "s3"),
			win(31, "Spotify", 3, "s3"),
			win(32, "Discord", 3, "s3"),
			win(33, "Qalculate", 3, "s3"),
		];
		const plan = deskPlan(profile, world(displays, spaces, windows));

		// Every rebuild-display window (g9 + aw, targets included) evacuates to the
		// park (laptop s3) up front, so each column space rebuilds from an empty
		// tree and the driver's insert recipe lands on real cross-space moves. The
		// park (s3) is never evacuated.
		expect(plan).toEqual([
			{ op: "moveWindow", windowId: 10, toSpace: "s3" as SpaceId },
			{ op: "moveWindow", windowId: 11, toSpace: "s3" as SpaceId },
			{ op: "moveWindow", windowId: 12, toSpace: "s3" as SpaceId },
			{ op: "moveWindow", windowId: 13, toSpace: "s3" as SpaceId },
			{ op: "moveWindow", windowId: 14, toSpace: "s3" as SpaceId },
			{ op: "moveWindow", windowId: 20, toSpace: "s3" as SpaceId },
			{ op: "moveWindow", windowId: 21, toSpace: "s3" as SpaceId },
			{ op: "moveWindow", windowId: 22, toSpace: "s3" as SpaceId },
			{ op: "moveWindow", windowId: 23, toSpace: "s3" as SpaceId },
			{ op: "relabelHome", homeSpace: "s1" as SpaceId, label: "main" },
			{
				op: "realizeLayout",
				space: "s1" as SpaceId,
				target: {
					kind: "3col",
					columns: [[10, 11], [12], [13, 14]],
					ratios: { root: 0.3, inner: 0.5714 },
				},
			},
			{ op: "relabelHome", homeSpace: "s2" as SpaceId, label: "plan" },
			{
				op: "realizeLayout",
				space: "s2" as SpaceId,
				target: {
					kind: "2col",
					columns: [
						[20, 21],
						[22, 23],
					],
					ratios: undefined,
				},
			},
			{ op: "relabelHome", homeSpace: "s3" as SpaceId, label: "laptop" },
			{
				op: "realizeLayout",
				space: "s3" as SpaceId,
				target: {
					kind: "stack",
					columns: [[30, 31, 32, 33]],
					ratios: undefined,
				},
			},
		]);
	});

	test("missing windows skipped — columns shrink, empty columns dropped", () => {
		// g9 present but only arc + vscode exist: obsidian, both ghosttys absent.
		// col0 [arc,obsidian] → [arc]; col1 [ghostty-wave] → dropped; col2
		// [ghostty-mbp,vscode] → [vscode].
		const displays = [display(1, G9, ["s1"])];
		const spaces = [space("s1", "main", 1)];
		const windows = [win(10, "Arc", 1, "s1"), win(14, "Code", 1, "s1")];
		const plan = deskPlan(profile, world(displays, spaces, windows));

		expect(plan).toEqual([
			{ op: "relabelHome", homeSpace: "s1" as SpaceId, label: "main" },
			{
				op: "realizeLayout",
				space: "s1" as SpaceId,
				target: {
					kind: "3col",
					columns: [[10], [14]],
					ratios: { root: 0.3, inner: 0.5714 },
				},
			},
		]);
	});

	test("display with zero claimed windows gets relabelHome but no realizeLayout", () => {
		// g9 present, no desk apps at all present on it.
		const displays = [display(1, G9, ["s1"])];
		const spaces = [space("s1", "main", 1)];
		const windows = [win(99, "Finder", 1, "s1")]; // matches no desk WIN spec
		const plan = deskPlan(profile, world(displays, spaces, windows));

		expect(plan).toEqual([
			{ op: "relabelHome", homeSpace: "s1" as SpaceId, label: "main" },
		]);
	});

	test("absent display skipped — laptop-only world emits only the laptop stack", () => {
		const displays = [display(3, LAPTOP, ["s3"])];
		const spaces = [space("s3", "laptop", 3)];
		const windows = [
			win(30, "Arc", 3, "s3"),
			win(31, "Spotify", 3, "s3"),
			win(32, "Discord", 3, "s3"),
			win(33, "Qalculate", 3, "s3"),
		];
		const plan = deskPlan(profile, world(displays, spaces, windows));

		expect(plan).toEqual([
			{ op: "relabelHome", homeSpace: "s3" as SpaceId, label: "laptop" },
			{
				op: "realizeLayout",
				space: "s3" as SpaceId,
				target: {
					kind: "stack",
					columns: [[30, 31, 32, 33]],
					ratios: undefined,
				},
			},
		]);
	});

	test("global dedup — repeated arc claims distinct ids per display, no overlap", () => {
		// g9 + aw present. arc appears in g9 col0, aw col0, aw col1 → 3 distinct.
		const displays = [display(1, G9, ["s1"]), display(2, AW, ["s2"])];
		const spaces = [space("s1", "main", 1), space("s2", "plan", 2)];
		const windows = [
			win(10, "Arc", 1, "s1"),
			win(20, "Linear", 2, "s2"),
			win(21, "Arc", 2, "s2"),
			win(22, "Arc", 2, "s2"),
			win(23, "Akiflow", 2, "s2"),
		];
		const plan = deskPlan(profile, world(displays, spaces, windows));

		// Collect every id across every realizeLayout column; assert all distinct.
		const ids: number[] = [];
		for (const op of plan) {
			if (op.op === "realizeLayout") {
				for (const col of op.target.columns) {
					ids.push(...col);
				}
			}
		}
		expect(ids.length).toBe(new Set(ids).size);
		// The three Arc windows landed distinctly: g9 got one, aw got the other two.
		const g9 = plan.find(
			(o) => o.op === "realizeLayout" && o.space === ("s1" as SpaceId),
		);
		const aw = plan.find(
			(o) => o.op === "realizeLayout" && o.space === ("s2" as SpaceId),
		);
		expect(g9).toEqual({
			op: "realizeLayout",
			space: "s1" as SpaceId,
			target: {
				kind: "3col",
				columns: [[10]],
				ratios: { root: 0.3, inner: 0.5714 },
			},
		});
		// aw col0 [linear, arc] → [20, 21]; col1 [arc, akiflow] → [22, 23].
		expect(aw).toEqual({
			op: "realizeLayout",
			space: "s2" as SpaceId,
			target: {
				kind: "2col",
				columns: [
					[20, 21],
					[22, 23],
				],
				ratios: undefined,
			},
		});
	});

	test("destroy preludes lead the plan; a stray at spaceIds[0] is not the relabel target", () => {
		// g9 has a stray unlabelled empty space at index 0, its real home at index 1,
		// plus a leftover lap-* space. teardown (lap-) then reap (stray) → both
		// destroyed first; the surviving home (s1home) is the relabel target.
		const displays = [
			display(1, G9, ["s1stray", "s1home"]),
			display(9, 999, ["lapx"]),
		];
		const spaces = [
			space("s1stray", "", 1), // unlabelled + empty → reaped
			space("s1home", "main", 1),
			space("lapx", "lap-1", 9), // lap-* → torn down
		];
		const windows = [win(10, "Arc", 1, "s1home")];
		const plan = deskPlan(profile, world(displays, spaces, windows));

		// Leads with destroys: teardown (lap-*) before reap (stray).
		expect(plan[0]).toEqual({ op: "destroySpace", space: "lapx" as SpaceId });
		expect(plan[1]).toEqual({
			op: "destroySpace",
			space: "s1stray" as SpaceId,
		});
		// Relabel targets the SURVIVING home, never the reaped stray at index 0.
		expect(plan[2]).toEqual({
			op: "relabelHome",
			homeSpace: "s1home" as SpaceId,
			label: "main",
		});
		expect(plan[3]).toEqual({
			op: "realizeLayout",
			space: "s1home" as SpaceId,
			target: {
				kind: "3col",
				columns: [[10]],
				ratios: { root: 0.3, inner: 0.5714 },
			},
		});
		expect(plan.length).toBe(4);
	});

	test("every rebuild-display window evacuates ONCE to the stable park (last display), never the park itself", () => {
		// Three displays present. Each home space carries desk targets PLUS foreign
		// windows. With the fix, EVERY tiled window on a rebuild display (targets
		// included) evacuates up front onto the park — the targets must be off-space
		// so the driver's insert recipe consumes its armed insert on the real move
		// back. The park is the laptop home (s3, last in profile.desk order); it
		// absorbs g9's + aw's windows and is NEVER evacuated itself.
		const displays = [
			display(1, G9, ["s1"]),
			display(2, AW, ["s2"]),
			display(3, LAPTOP, ["s3"]),
		];
		const spaces = [
			space("s1", "main", 1),
			space("s2", "plan", 2),
			space("s3", "laptop", 3),
		];
		const windows = [
			// g9 targets + two refugees.
			win(10, "Arc", 1, "s1"),
			win(40, "Messages", 1, "s1"), // foreign
			win(12, "Ghostty", 1, "s1", "pc"),
			win(41, "Slack", 1, "s1"), // foreign
			win(14, "Code", 1, "s1"),
			// aw targets + one refugee.
			win(20, "Linear", 2, "s2"),
			win(21, "Arc", 2, "s2"),
			win(42, "Finder", 2, "s2"), // foreign
			win(23, "Akiflow", 2, "s2"),
			// laptop (the park) targets + one refugee that must STAY put.
			win(30, "Arc", 3, "s3"),
			win(43, "Notion", 3, "s3"), // foreign on park → never evacuated
			win(31, "Spotify", 3, "s3"),
		];
		const plan = deskPlan(profile, world(displays, spaces, windows));

		// Every rebuild-display window (g9's + aw's, in world order, targets
		// included) is a moveWindow to the park (s3), and they ALL precede the
		// first realizeLayout (evacuate once, up front — the ping-pong fix).
		const firstRealize = plan.findIndex((o) => o.op === "realizeLayout");
		const moves = plan.filter((o) => o.op === "moveWindow");
		expect(moves).toEqual([
			{ op: "moveWindow", windowId: 10, toSpace: "s3" as SpaceId },
			{ op: "moveWindow", windowId: 40, toSpace: "s3" as SpaceId },
			{ op: "moveWindow", windowId: 12, toSpace: "s3" as SpaceId },
			{ op: "moveWindow", windowId: 41, toSpace: "s3" as SpaceId },
			{ op: "moveWindow", windowId: 14, toSpace: "s3" as SpaceId },
			{ op: "moveWindow", windowId: 20, toSpace: "s3" as SpaceId },
			{ op: "moveWindow", windowId: 21, toSpace: "s3" as SpaceId },
			{ op: "moveWindow", windowId: 42, toSpace: "s3" as SpaceId },
			{ op: "moveWindow", windowId: 23, toSpace: "s3" as SpaceId },
		]);
		for (const [i, op] of plan.entries()) {
			if (op.op === "moveWindow") {
				expect(i).toBeLessThan(firstRealize);
			}
		}
		// The park's own windows (target 30, 31 and refugee 43) are NEVER evacuated.
		for (const stay of [30, 31, 43]) {
			expect(
				plan.some((o) => o.op === "moveWindow" && o.windowId === stay),
			).toBe(false);
		}
		// g9 still builds its 3col from its own targets only.
		const g9 = plan.find(
			(o) => o.op === "realizeLayout" && o.space === ("s1" as SpaceId),
		);
		expect(g9).toEqual({
			op: "realizeLayout",
			space: "s1" as SpaceId,
			target: {
				kind: "3col",
				columns: [[10], [12], [14]],
				ratios: { root: 0.3, inner: 0.5714 },
			},
		});
	});

	test("single display rebuilds in place — no park, no evacuation", () => {
		// One display → no stable catch-all exists, so foreign windows are NOT
		// evacuated (rebuild in place).
		const displays = [display(1, G9, ["s1"])];
		const spaces = [space("s1", "main", 1)];
		const windows = [
			win(10, "Arc", 1, "s1"),
			win(40, "Messages", 1, "s1"), // foreign, but nowhere to park
			win(14, "Code", 1, "s1"),
		];
		const plan = deskPlan(profile, world(displays, spaces, windows));
		expect(plan.some((o) => o.op === "moveWindow")).toBe(false);
	});

	test("two displays (g9+aw, no laptop) park onto aw — the last present desk display", () => {
		// profile.desk order is g9, aw, laptop; laptop absent → the LAST present
		// desk display is aw, so aw's home is the park. EVERY g9 window (targets
		// included) evacuates to aw, which builds around them; aw itself (the park)
		// is never evacuated.
		const displays = [display(1, G9, ["s1"]), display(2, AW, ["s2"])];
		const spaces = [space("s1", "main", 1), space("s2", "plan", 2)];
		const windows = [
			win(10, "Arc", 1, "s1"),
			win(40, "Messages", 1, "s1"), // g9 refugee → aw
			win(14, "Code", 1, "s1"),
			win(20, "Linear", 2, "s2"),
			win(21, "Arc", 2, "s2"),
			win(41, "Finder", 2, "s2"), // aw's OWN refugee → stays (aw is the park)
			win(23, "Akiflow", 2, "s2"),
		];
		const plan = deskPlan(profile, world(displays, spaces, windows));
		const moves = plan.filter((o) => o.op === "moveWindow");
		// All of g9's windows (targets + refugee, world order) evacuate to aw (s2).
		// aw's own windows never move.
		expect(moves).toEqual([
			{ op: "moveWindow", windowId: 10, toSpace: "s2" as SpaceId },
			{ op: "moveWindow", windowId: 40, toSpace: "s2" as SpaceId },
			{ op: "moveWindow", windowId: 14, toSpace: "s2" as SpaceId },
		]);
	});

	test("a window physically on g9 but claimed as aw's target evacuates to the park, not stranded", () => {
		// g9's home holds a second Linear that aw claims (aw's col0 is linear+arc).
		// g9 evacuates ALL its windows to the park (laptop s3), so this Linear lands
		// on the park; aw's realizeLayout then names it in aw's columns, so the
		// executor moves it park→aw. The engine plan proves the first leg: it is
		// evacuated, not left to strand g9's rebuild.
		const displays = [
			display(1, G9, ["s1"]),
			display(2, AW, ["s2"]),
			display(3, LAPTOP, ["s3"]),
		];
		const spaces = [
			space("s1", "main", 1),
			space("s2", "plan", 2),
			space("s3", "laptop", 3),
		];
		const windows = [
			win(10, "Arc", 1, "s1"),
			win(14, "Code", 1, "s1"),
			win(50, "Linear", 1, "s1"), // physically on g9, but aw claims it
			win(21, "Arc", 2, "s2"),
			win(30, "Arc", 3, "s3"),
			win(31, "Spotify", 3, "s3"),
		];
		const plan = deskPlan(profile, world(displays, spaces, windows));
		// g9 evacuates the aw-claimed Linear to the park (it is not g9's target).
		expect(
			plan.some(
				(o) =>
					o.op === "moveWindow" &&
					o.windowId === 50 &&
					o.toSpace === ("s3" as SpaceId),
			),
		).toBe(true);
		// aw's realizeLayout names window 50 as its col0 anchor (linear).
		const aw = plan.find(
			(o) => o.op === "realizeLayout" && o.space === ("s2" as SpaceId),
		);
		expect(aw?.op === "realizeLayout" && aw.target.columns[0]?.[0]).toBe(50);
	});
});
