// reap (T4): the two destroy-prelude candidate rules — teardownLabels
// (teardown_laptop_grid) and straySpaces (reap_stray_spaces). Pure over one
// WorldSnapshot; the truth table below mirrors
// the bash safety comment.

import { describe, expect, test } from "bun:test";
import type { SpaceId, WmDisplay, WmSpace, WmWindow } from "../driver/types.ts";
import { straySpaces, teardownLabels } from "./reap.ts";
import type { WorldSnapshot } from "./world.ts";

function sp(
	id: string,
	label: string,
	displayIdx: number,
	windowIds: number[] = [],
): WmSpace {
	return { id: id as SpaceId, label, displayIdx, windowIds, layout: "bsp" };
}

function win(id: number, displayIdx: number, sticky = false): WmWindow {
	return {
		id,
		app: "X",
		title: "",
		displayIdx,
		spaceId: "s?" as SpaceId,
		minimized: false,
		floating: false,
		sticky,
		visible: true,
		splitType: "none",
		frame: { x: 0, y: 0, w: 100, h: 100 },
	};
}

/** Build a snapshot; each display's spaceIds come from the spaces on it, in order. */
function world(spaces: WmSpace[], windows: WmWindow[] = []): WorldSnapshot {
	const byDisplay = new Map<number, SpaceId[]>();
	for (const s of spaces) {
		const arr = byDisplay.get(s.displayIdx) ?? [];
		arr.push(s.id);
		byDisplay.set(s.displayIdx, arr);
	}
	const displays: WmDisplay[] = [...byDisplay.entries()].map(
		([idx, spaceIds]) => ({
			idx,
			frame: { x: 0, y: 0, w: 1000, h: 1000 },
			spaceIds,
		}),
	);
	return { windows, spaces, displays };
}

describe("teardownLabels (teardown_laptop_grid)", () => {
	test("returns every lap-* space, excludes home and desk-labelled", () => {
		const w = world([
			sp("s1", "laptop", 1),
			sp("s2", "lap-arc", 1),
			sp("s3", "lap-spotify", 1),
			sp("s4", "", 1),
			sp("s5", "main", 2),
		]);
		expect(teardownLabels(w, "lap-")).toEqual(["s2", "s3"] as SpaceId[]);
	});

	test("empty when no space carries the prefix", () => {
		const w = world([sp("s1", "main", 1), sp("s2", "", 1)]);
		expect(teardownLabels(w, "lap-")).toEqual([]);
	});
});

describe("straySpaces (reap_stray_spaces)", () => {
	test("reaps an unlabelled empty non-last space", () => {
		// home (labelled) keeps the display alive; the blank stray is reaped.
		const w = world([sp("s1", "main", 1), sp("s2", "", 1)]);
		expect(straySpaces(w)).toEqual(["s2"] as SpaceId[]);
	});

	test("never reaps a labelled space", () => {
		const w = world([sp("s1", "main", 1), sp("s2", "lap-arc", 1)]);
		expect(straySpaces(w)).toEqual([]);
	});

	test("does not reap an unlabelled space holding a non-sticky window", () => {
		const w = world([sp("s1", "main", 1), sp("s2", "", 1, [10])], [win(10, 1)]);
		expect(straySpaces(w)).toEqual([]);
	});

	test("reaps an unlabelled space whose only windows are sticky", () => {
		// A sticky floater is reported on every space; emptiness counts only
		// non-sticky windows, so a sticky-only space is genuinely empty.
		const w = world(
			[sp("s1", "main", 1, [10]), sp("s2", "", 1, [10])],
			[win(10, 1, true)],
		);
		expect(straySpaces(w)).toEqual(["s2"] as SpaceId[]);
	});

	test("never reaps a display's sole space, even blank", () => {
		const w = world([sp("s1", "", 1)]);
		expect(straySpaces(w)).toEqual([]);
	});

	test("all-stray display keeps the last space, reaps the rest", () => {
		// Draining lowest-index-first, the last in order survives.
		const w = world([sp("s1", "", 1), sp("s2", "", 1), sp("s3", "", 1)]);
		expect(straySpaces(w)).toEqual(["s1", "s2"] as SpaceId[]);
	});

	test("reaps strays across multiple displays independently", () => {
		const w = world([
			sp("s1", "main", 1),
			sp("s2", "", 1),
			sp("s3", "plan", 2),
			sp("s4", "", 2),
		]);
		expect(straySpaces(w)).toEqual(["s2", "s4"] as SpaceId[]);
	});
});
