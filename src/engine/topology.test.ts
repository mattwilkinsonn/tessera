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
		{ display: "g9", label: "main", kind: "3col", columns: [["arc"]] },
		{ display: "aw", label: "plan", kind: "2col", columns: [["arc"]] },
		{ display: "laptop", label: "laptop", kind: "stack", columns: [["arc"]] },
	],
	ratios: { col3Root: 0.3, col3Inner: 0.5714 },
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
			{ display: "aw", label: "solo", kind: "3col", columns: [["arc"]] },
		] as const;
		const profile = {
			...base,
			topologies: [
				{ name: "aw-laptop", displays: ["aw", "laptop"], desk: awOnly },
			],
		} satisfies Profile;

		// AW + laptop present, G9 absent → the named topology.
		expect(resolveDesk(profile, [display(2, AW), display(3, LAPTOP)])).toBe(
			awOnly,
		);
	});

	test("a superset of a topology's displays does NOT match it", () => {
		// Exact-set, not subset: with G9 also present this is a different rig, and
		// silently reusing the two-display layout would drop G9 entirely.
		const profile = {
			...base,
			topologies: [
				{
					name: "aw-laptop",
					displays: ["aw", "laptop"],
					desk: [
						{ display: "aw", label: "solo", kind: "3col", columns: [["arc"]] },
					],
				},
			],
		} satisfies Profile;

		const desk = resolveDesk(profile, [
			display(1, G9),
			display(2, AW),
			display(3, LAPTOP),
		]);
		expect(desk).toBe(profile.desk);
	});

	test("a subset of a topology's displays does NOT match it", () => {
		const profile = {
			...base,
			topologies: [
				{
					name: "aw-laptop",
					displays: ["aw", "laptop"],
					desk: [
						{ display: "aw", label: "solo", kind: "3col", columns: [["arc"]] },
					],
				},
			],
		} satisfies Profile;

		expect(resolveDesk(profile, [display(2, AW)])).toBe(profile.desk);
	});

	test("first matching topology wins — declaration order is the precedence", () => {
		const first = [
			{ display: "aw", label: "first", kind: "3col", columns: [["arc"]] },
		] as const;
		const second = [
			{ display: "aw", label: "second", kind: "3col", columns: [["arc"]] },
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

	test("declared display order is irrelevant — it is a set, not a sequence", () => {
		const desk = [
			{ display: "aw", label: "solo", kind: "3col", columns: [["arc"]] },
		] as const;
		const profile = {
			...base,
			topologies: [{ name: "x", displays: ["laptop", "aw"], desk }],
		} satisfies Profile;

		expect(resolveDesk(profile, [display(2, AW), display(3, LAPTOP)])).toBe(
			desk,
		);
	});

	test("an unrecognized display width does not match any topology", () => {
		// A display whose width is in no profile slot is not a known display; the
		// present set cannot equal a declared set, so the default desk stands.
		const profile = {
			...base,
			topologies: [
				{
					name: "aw-laptop",
					displays: ["aw", "laptop"],
					desk: [
						{ display: "aw", label: "solo", kind: "3col", columns: [["arc"]] },
					],
				},
			],
		} satisfies Profile;

		const desk = resolveDesk(profile, [
			display(2, AW),
			display(3, LAPTOP),
			display(9, 2560),
		]);
		expect(desk).toBe(profile.desk);
	});

	test("two displays of one width do not match any topology", () => {
		// Both panels resolve to the aw slot, but only the first is laid out; a
		// topology claiming {aw, laptop} would leave the second unmanaged.
		const profile = {
			...base,
			topologies: [
				{
					name: "aw-laptop",
					displays: ["aw", "laptop"],
					desk: [
						{ display: "aw", label: "solo", kind: "3col", columns: [["arc"]] },
					],
				},
			],
		} satisfies Profile;

		const desk = resolveDesk(profile, [
			display(2, AW),
			display(4, AW),
			display(3, LAPTOP),
		]);
		expect(desk).toBe(profile.desk);
	});
});
