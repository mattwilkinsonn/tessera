// flex (T3): the laptop-flex planner — pure order reconciliation
// (`laptop_flex_order`) and the seed + filter + id-order
// occurrence-labeling core of `laptop_flex_windows`.

import { describe, expect, test } from "bun:test";
import { profile } from "../config/profile.ts";
import type { SpaceId, WmWindow } from "../driver/types.ts";
import { laptopFlexWindows, reconcileFlexOrder } from "./flex.ts";

const SPACE = "s1" as SpaceId;

function win(
	id: number,
	app: string,
	opts: {
		title?: string;
		minimized?: boolean;
		floating?: boolean;
	} = {},
): WmWindow {
	return {
		id,
		app,
		title: opts.title ?? "",
		displayIdx: 1,
		spaceId: SPACE,
		minimized: opts.minimized ?? false,
		floating: opts.floating ?? false,
		sticky: false,
		visible: true,
		splitType: "none",
		frame: { x: 0, y: 0, w: 100, h: 100 },
	};
}

describe("reconcileFlexOrder (laptop_flex_order)", () => {
	test("empty persisted seeds order + toPersist from current", () => {
		const { order, toPersist } = reconcileFlexOrder([], ["a", "b", "c"]);
		expect(order).toEqual(["a", "b", "c"]);
		expect(toPersist).toEqual(["a", "b", "c"]);
	});

	test("subset/reordered current follows FILE order; absent line retained", () => {
		const { order, toPersist } = reconcileFlexOrder(
			["a", "b", "c"],
			["c", "a"],
		);
		expect(order).toEqual(["a", "c"]);
		expect(toPersist).toEqual(["a", "b", "c"]);
	});

	test("new current slug appended", () => {
		const { order, toPersist } = reconcileFlexOrder(["a"], ["a", "b"]);
		expect(toPersist).toEqual(["a", "b"]);
		expect(order).toEqual(["a", "b"]);
	});

	test("duplicate in current appended once (grep -qxF vs growing file)", () => {
		const { order, toPersist } = reconcileFlexOrder([], ["a", "a"]);
		expect(toPersist).toEqual(["a"]);
		expect(order).toEqual(["a"]);
	});

	test("empty-string slug skipped from order", () => {
		const { order, toPersist } = reconcileFlexOrder(["", "a"], ["a", ""]);
		expect(order).toEqual(["a"]);
		expect(toPersist).toEqual(["", "a"]);
	});
});

describe("laptopFlexWindows (laptop_flex_windows core)", () => {
	// profile.laptopPinned has 4 "arc" entries, so occ["arc"] seeds to 4.
	const PINNED_ARCS = profile.laptopPinned.filter((n) => n === "arc").length;

	test("flex Arc continues the pinned occurrence sequence (D10)", () => {
		const rows = laptopFlexWindows(profile, [win(1, "Arc")], new Set());
		expect(PINNED_ARCS).toBe(4);
		expect(rows).toEqual([{ id: 1, label: `arc-${PINNED_ARCS + 1}` }]);
	});

	test("laptopStackApps app is omitted", () => {
		const rows = laptopFlexWindows(
			profile,
			[win(1, "Slack"), win(2, "Arc")],
			new Set(),
		);
		expect(rows.map((r) => r.id)).toEqual([2]);
	});

	test("claimed id is omitted", () => {
		const rows = laptopFlexWindows(
			profile,
			[win(1, "Arc"), win(2, "Arc")],
			new Set([1]),
		);
		expect(rows.map((r) => r.id)).toEqual([2]);
	});

	test("windows out of id order process ascending (sort_by(.id))", () => {
		const rows = laptopFlexWindows(
			profile,
			[win(3, "Arc"), win(1, "Arc"), win(2, "Arc")],
			new Set(),
		);
		expect(rows.map((r) => r.id)).toEqual([1, 2, 3]);
		expect(rows.map((r) => r.label)).toEqual([
			`arc-${PINNED_ARCS + 1}`,
			`arc-${PINNED_ARCS + 2}`,
			`arc-${PINNED_ARCS + 3}`,
		]);
	});

	test("minimized / floating windows filtered out", () => {
		const rows = laptopFlexWindows(
			profile,
			[
				win(1, "Arc", { minimized: true }),
				win(2, "Arc", { floating: true }),
				win(3, "Arc"),
			],
			new Set(),
		);
		expect(rows.map((r) => r.id)).toEqual([3]);
	});

	test("labels are BARE slugs, no lap- prefix (converger adds it)", () => {
		const rows = laptopFlexWindows(profile, [win(1, "Arc")], new Set());
		expect(rows[0]?.label.startsWith("lap-")).toBe(false);
	});
});
