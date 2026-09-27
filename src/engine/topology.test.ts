// topology: selecting the desk layout set for the CURRENT display set. Asserts
// exact-set matching, declaration-order precedence, and the `desk` fallback.

import { describe, expect, test } from "bun:test";
import type { Profile } from "../config/types.ts";
import type { WmDisplay } from "../driver/types.ts";
import { resolveDesk } from "./topology.ts";

const G9 = 5120;
const AW = 3440;
const LAPTOP = 1728;

function display(idx: number, width: number): WmDisplay {
	return { idx, frame: { x: 0, y: 0, w: width, h: 1080 }, spaceIds: [] };
}

const base = {
	displays: { g9: { width: G9 }, aw: { width: AW }, laptop: { width: LAPTOP } },
	windows: { arc: { app: /Arc/ } },
	desk: [
		{ display: "g9", label: "main", kind: "columns", tracks: [["arc"]] },
		{ display: "aw", label: "plan", kind: "columns", tracks: [["arc"]] },
		{ display: "laptop", label: "laptop", kind: "stack", tracks: [["arc"]] },
	],
	deskSlots: [],
	laptopPinned: [],
	laptopStackApps: {},
} satisfies Profile;

describe("resolveDesk", () => {
	test("no topologies declared — always the default desk", () => {
		const desk = resolveDesk(base, [display(1, G9), display(2, LAPTOP)]);
		expect(desk).toBe(base.desk);
	});

	test("exact present-set match wins over the default desk", () => {
		const awOnly = [
			{ display: "aw", label: "solo", kind: "columns", tracks: [["arc"]] },
		] as const;
		const profile = {
			...base,
			topologies: [
				{ name: "aw-laptop", displays: ["aw", "laptop"], desk: awOnly },
			],
		} satisfies Profile;
		expect(resolveDesk(profile, [display(2, AW), display(3, LAPTOP)])).toBe(
			awOnly,
		);
	});

	test("a superset or subset of a topology display set does not match", () => {
		const profile = {
			...base,
			topologies: [
				{
					name: "aw-laptop",
					displays: ["aw", "laptop"],
					desk: [
						{
							display: "aw",
							label: "solo",
							kind: "columns",
							tracks: [["arc"]],
						},
					],
				},
			],
		} satisfies Profile;
		expect(
			resolveDesk(profile, [
				display(1, G9),
				display(2, AW),
				display(3, LAPTOP),
			]),
		).toBe(profile.desk);
		expect(resolveDesk(profile, [display(2, AW)])).toBe(profile.desk);
	});

	test("first matching topology wins and display declaration order is irrelevant", () => {
		const first = [
			{ display: "aw", label: "first", kind: "columns", tracks: [["arc"]] },
		] as const;
		const second = [
			{ display: "aw", label: "second", kind: "columns", tracks: [["arc"]] },
		] as const;
		const profile = {
			...base,
			topologies: [
				{ name: "a", displays: ["aw", "laptop"], desk: first },
				{ name: "b", displays: ["laptop", "aw"], desk: second },
			],
		} satisfies Profile;
		expect(resolveDesk(profile, [display(2, AW), display(3, LAPTOP)])).toBe(
			first,
		);
	});

	test("unrecognized and duplicate display widths fall back to default", () => {
		const profile = {
			...base,
			topologies: [
				{
					name: "aw-laptop",
					displays: ["aw", "laptop"],
					desk: [
						{
							display: "aw",
							label: "solo",
							kind: "columns",
							tracks: [["arc"]],
						},
					],
				},
			],
		} satisfies Profile;
		expect(
			resolveDesk(profile, [
				display(2, AW),
				display(3, LAPTOP),
				display(9, 2560),
			]),
		).toBe(profile.desk);
		expect(
			resolveDesk(profile, [
				display(2, AW),
				display(4, AW),
				display(3, LAPTOP),
			]),
		).toBe(profile.desk);
	});
});
