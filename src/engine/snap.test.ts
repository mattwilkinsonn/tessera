// snap (T4): snapPlan port of. Builds worlds with SpaceId casts
// and a small window helper; each leaf gets a distinct frame.x to fix sort
// order, and every leaf's spaceId is the focused space.

import { describe, expect, test } from "bun:test";
import { profile } from "../config/profile.fixture.ts";
import type { Profile } from "../config/types.ts";
import type { SpaceId, WmWindow } from "../driver/types.ts";
import { snapPlan } from "./snap.ts";
import type { WorldSnapshot } from "./world.ts";

const FOCUS = "sf" as SpaceId;

function win(
	id: number,
	x: number,
	opts: { spaceId?: SpaceId; floating?: boolean; minimized?: boolean } = {},
): WmWindow {
	return {
		id,
		app: "X",
		title: "",
		displayIdx: 0,
		spaceId: opts.spaceId ?? FOCUS,
		minimized: opts.minimized ?? false,
		floating: opts.floating ?? false,
		sticky: false,
		visible: true,
		splitType: "none",
		frame: { x, y: 0, w: 100, h: 100 },
	};
}

function world(windows: WmWindow[]): WorldSnapshot {
	return { windows, spaces: [], displays: [] };
}

function worldOnG9(windows: WmWindow[]): WorldSnapshot {
	return {
		windows,
		spaces: [
			{ id: FOCUS, label: "", displayIdx: 4, windowIds: [], layout: "bsp" },
		],
		displays: [
			{
				idx: 4,
				frame: { x: 0, y: 0, w: profile.displays.g9.width, h: 1000 },
				spaceIds: [FOCUS],
			},
		],
	};
}

describe("snapPlan", () => {
	test("both split modes use the focused display defaults", () => {
		const configured = {
			...profile,
			displays: {
				...profile.displays,
				g9: {
					...profile.displays.g9,
					ratios: { col3: { col3Root: 0.42, col3Inner: 0.61 }, col2: 0.68 },
				},
			},
		} satisfies Profile;
		const focused = worldOnG9([win(1, 0), win(2, 100), win(3, 200)]);
		expect(snapPlan(configured, focused, FOCUS, "3col")).toContainEqual({
			op: "realizeLayout",
			space: FOCUS,
			target: {
				kind: "3col",
				columns: [[1], [2], [3]],
				ratios: { root: 0.42, inner: 0.61 },
			},
		});
		expect(snapPlan(configured, focused, FOCUS, "50-50")).toContainEqual({
			op: "realizeLayout",
			space: FOCUS,
			target: { kind: "2col", columns: [[1, 2], [3]], split: 0.68 },
		});
	});
	test("x-sort: out-of-order windows yield left→right visual order", () => {
		const w = world([win(30, 300), win(10, 100), win(20, 200)]);
		const plan = snapPlan(profile, w, FOCUS, "3col");
		expect(plan).toEqual([
			{
				op: "realizeLayout",
				space: FOCUS,
				target: {
					kind: "3col",
					columns: [[10], [20], [30]],
					ratios: { root: 0.3, inner: 0.5714 },
				},
			},
		]);
	});

	test("3col with 5 leaves → [[0],[1],[2,3,4]] with ratios", () => {
		const w = world([
			win(0, 0),
			win(1, 100),
			win(2, 200),
			win(3, 300),
			win(4, 400),
		]);
		const plan = snapPlan(profile, w, FOCUS, "3col");
		expect(plan).toEqual([
			{
				op: "realizeLayout",
				space: FOCUS,
				target: {
					kind: "3col",
					columns: [[0], [1], [2, 3, 4]],
					ratios: { root: 0.3, inner: 0.5714 },
				},
			},
		]);
	});

	test("3col with 2 leaves → [[0],[1]] (empty col3 dropped)", () => {
		const w = world([win(0, 0), win(1, 100)]);
		const plan = snapPlan(profile, w, FOCUS, "3col");
		expect(plan).toEqual([
			{
				op: "realizeLayout",
				space: FOCUS,
				target: {
					kind: "3col",
					columns: [[0], [1]],
					ratios: { root: 0.3, inner: 0.5714 },
				},
			},
		]);
	});

	test("50-50 with 4 leaves → [[0,1],[2,3]], 2col, no ratios", () => {
		const w = world([win(0, 0), win(1, 100), win(2, 200), win(3, 300)]);
		const plan = snapPlan(profile, w, FOCUS, "50-50");
		expect(plan).toEqual([
			{
				op: "realizeLayout",
				space: FOCUS,
				target: {
					kind: "2col",
					columns: [
						[0, 1],
						[2, 3],
					],
					split: 0.5,
				},
			},
		]);
	});

	test("50-50 with 5 leaves → half=3 → [[0,1,2],[3,4]]", () => {
		const w = world([
			win(0, 0),
			win(1, 100),
			win(2, 200),
			win(3, 300),
			win(4, 400),
		]);
		const plan = snapPlan(profile, w, FOCUS, "50-50");
		expect(plan).toEqual([
			{
				op: "realizeLayout",
				space: FOCUS,
				target: {
					kind: "2col",
					columns: [
						[0, 1, 2],
						[3, 4],
					],
					split: 0.5,
				},
			},
		]);
	});

	test("columns mode → single balanceSpace op", () => {
		const w = world([win(0, 0), win(1, 100)]);
		const plan = snapPlan(profile, w, FOCUS, "columns");
		expect(plan).toEqual([{ op: "balanceSpace", space: FOCUS }]);
	});

	test("empty focused space (no tiled leaves) → []", () => {
		const w = world([win(0, 0, { spaceId: "other" as SpaceId })]);
		expect(snapPlan(profile, w, FOCUS, "3col")).toEqual([]);
		expect(snapPlan(profile, w, FOCUS, "columns")).toEqual([]);
	});

	test("floating/minimized on focus are not counted as leaves", () => {
		const w = world([
			win(0, 0),
			win(1, 100, { floating: true }),
			win(2, 200, { minimized: true }),
		]);
		const plan = snapPlan(profile, w, FOCUS, "3col");
		expect(plan).toEqual([
			{
				op: "realizeLayout",
				space: FOCUS,
				target: {
					kind: "3col",
					columns: [[0]],
					ratios: { root: 0.3, inner: 0.5714 },
				},
			},
		]);
	});
});
