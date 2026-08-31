// snap (T4): snapPlan port of. Builds worlds with SpaceId casts
// and a small window helper; each leaf gets a distinct frame.x to fix sort
// order, and every leaf's spaceId is the focused space.

import { describe, expect, test } from "bun:test";
import { profile } from "../config/profile.ts";
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

describe("snapPlan", () => {
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
