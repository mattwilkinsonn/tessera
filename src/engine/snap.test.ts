// snapPlan reshapes focused-space leaves in visual order and resolves nominal weights.

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
		app: "app",
		title: "",
		displayIdx: 1,
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
			{ id: FOCUS, label: "main", displayIdx: 1, windowIds: [], layout: "bsp" },
		],
		displays: [
			{ idx: 1, frame: { x: 0, y: 0, w: 5120, h: 1440 }, spaceIds: [FOCUS] },
		],
	};
}

describe("snapPlan", () => {
	test("snap modes use nominal count defaults on the focused display", () => {
		const configured = {
			...profile,
			displays: {
				...profile.displays,
				g9: {
					...profile.displays.g9,
					weights: { columns: { 2: [3, 2], 3: [4, 3, 3] } },
				},
			},
		} satisfies Profile;
		const focused = worldOnG9([win(1, 0), win(2, 100), win(3, 200)]);
		expect(snapPlan(configured, focused, FOCUS, "3col")).toContainEqual({
			op: "realizeLayout",
			space: FOCUS,
			target: { kind: "columns", tracks: [[1], [2], [3]], weights: [4, 3, 3] },
		});
		expect(snapPlan(configured, focused, FOCUS, "50-50")).toContainEqual({
			op: "realizeLayout",
			space: FOCUS,
			target: { kind: "columns", tracks: [[1, 2], [3]], weights: [3, 2] },
		});
	});

	test("x-sort preserves left-to-right track order", () => {
		expect(
			snapPlan(
				profile,
				world([win(30, 300), win(10, 100), win(20, 200)]),
				FOCUS,
				"3col",
			),
		).toEqual([
			{
				op: "realizeLayout",
				space: FOCUS,
				target: {
					kind: "columns",
					tracks: [[10], [20], [30]],
					weights: [3, 4, 3],
				},
			},
		]);
	});

	test("3col with 5 leaves stacks extras on the third track", () => {
		expect(
			snapPlan(
				profile,
				world([win(0, 0), win(1, 100), win(2, 200), win(3, 300), win(4, 400)]),
				FOCUS,
				"3col",
			),
		).toEqual([
			{
				op: "realizeLayout",
				space: FOCUS,
				target: {
					kind: "columns",
					tracks: [[0], [1], [2, 3, 4]],
					weights: [3, 4, 3],
				},
			},
		]);
	});

	test("3col with 2 leaves drops third track and weight", () => {
		expect(
			snapPlan(profile, world([win(0, 0), win(1, 100)]), FOCUS, "3col"),
		).toEqual([
			{
				op: "realizeLayout",
				space: FOCUS,
				target: { kind: "columns", tracks: [[0], [1]], weights: [3, 4] },
			},
		]);
	});

	test("50-50 puts leaves into two stacked halves", () => {
		expect(
			snapPlan(
				profile,
				world([win(0, 0), win(1, 100), win(2, 200), win(3, 300)]),
				FOCUS,
				"50-50",
			),
		).toEqual([
			{
				op: "realizeLayout",
				space: FOCUS,
				target: {
					kind: "columns",
					tracks: [
						[0, 1],
						[2, 3],
					],
					weights: [1, 1],
				},
			},
		]);
		expect(
			snapPlan(
				profile,
				world([win(0, 0), win(1, 100), win(2, 200), win(3, 300), win(4, 400)]),
				FOCUS,
				"50-50",
			),
		).toEqual([
			{
				op: "realizeLayout",
				space: FOCUS,
				target: {
					kind: "columns",
					tracks: [
						[0, 1, 2],
						[3, 4],
					],
					weights: [1, 1],
				},
			},
		]);
	});

	test("columns mode balances and empty space does nothing", () => {
		expect(
			snapPlan(profile, world([win(0, 0), win(1, 100)]), FOCUS, "columns"),
		).toEqual([{ op: "balanceSpace", space: FOCUS }]);
		expect(
			snapPlan(
				profile,
				world([win(0, 0, { spaceId: "other" as SpaceId })]),
				FOCUS,
				"3col",
			),
		).toEqual([]);
	});

	test("floating and minimized leaves do not change tracks", () => {
		expect(
			snapPlan(
				profile,
				world([
					win(0, 0),
					win(1, 100, { floating: true }),
					win(2, 200, { minimized: true }),
				]),
				FOCUS,
				"3col",
			),
		).toEqual([
			{
				op: "realizeLayout",
				space: FOCUS,
				target: { kind: "columns", tracks: [[0]], weights: [3] },
			},
		]);
	});

	test("3col drops an empty nominal middle track with its weight", () => {
		const configured = {
			...profile,
			displays: {
				...profile.displays,
				g9: { ...profile.displays.g9, weights: { columns: { 3: [3, 4, 3] } } },
			},
		};
		const target = snapPlan(
			configured,
			worldOnG9([win(0, 0), win(2, 200)]),
			FOCUS,
			"3col",
		).find((op) => op.op === "realizeLayout");
		expect(target).toEqual({
			op: "realizeLayout",
			space: FOCUS,
			target: { kind: "columns", tracks: [[0], [2]], weights: [3, 4] },
		});
	});
});
