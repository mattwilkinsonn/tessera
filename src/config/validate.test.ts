import { describe, expect, test } from "bun:test";
import { profile as bundled } from "./profile.ts";
import type { Profile } from "./types.ts";
import { validateProfile } from "./validate.ts";

const base = {
	displays: { g9: { width: 1 }, aw: { width: 2 }, laptop: { width: 3 } },
	windows: {},
	desk: [],
	deskSlots: [],
	laptopPinned: [],
	laptopStackApps: {},
} satisfies Profile;

const stackLayout = (display: "g9" | "aw" | "laptop") => ({
	display,
	label: display,
	kind: "stack" as const,
	tracks: [["app"]],
});

describe("validateProfile", () => {
	test("a topology with a layout for every declared display passes", () => {
		expect(() =>
			validateProfile({
				...base,
				topologies: [
					{
						name: "aw-laptop",
						displays: ["aw", "laptop"],
						desk: [stackLayout("aw"), stackLayout("laptop")],
					},
				],
			}),
		).not.toThrow();
	});

	test("topology display membership is validated", () => {
		expect(() =>
			validateProfile({
				...base,
				topologies: [
					{
						name: "aw-laptop",
						displays: ["aw", "laptop"],
						desk: [stackLayout("aw")],
					},
				],
			}),
		).toThrow('topology "aw-laptop": no layout for laptop');
		expect(() =>
			validateProfile({
				...base,
				topologies: [
					{ name: "aw-only", displays: ["aw"], desk: [stackLayout("laptop")] },
				],
			}),
		).toThrow('topology "aw-only": layout for undeclared display laptop');
	});

	test("duplicate topology layouts are rejected", () => {
		expect(() =>
			validateProfile({
				...base,
				topologies: [
					{
						name: "aw-only",
						displays: ["aw"],
						desk: [stackLayout("aw"), stackLayout("aw")],
					},
				],
			}),
		).toThrow('topology "aw-only": duplicate layout for aw');
	});

	test("empty track lists and empty tracks are rejected", () => {
		expect(() =>
			validateProfile({
				...base,
				desk: [{ ...stackLayout("aw"), tracks: [] }],
			}),
		).toThrow("stack must have exactly one track");
		expect(() =>
			validateProfile({
				...base,
				desk: [{ ...stackLayout("aw"), tracks: [[]] }],
			}),
		).toThrow("track 0 must have at least one window name");
	});

	test("stack requires exactly one track and rejects weights", () => {
		expect(() =>
			validateProfile({
				...base,
				desk: [{ ...stackLayout("aw"), tracks: [["a"], ["b"]] }],
			}),
		).toThrow("stack must have exactly one track");
		expect(() =>
			validateProfile({
				...base,
				desk: [{ ...stackLayout("aw"), weights: [1] }],
			}),
		).toThrow("stack cannot set weights");
	});

	test("one-track columns and rows reject weights", () => {
		for (const kind of ["columns", "rows"] as const) {
			expect(() =>
				validateProfile({
					...base,
					desk: [
						{ display: "aw", label: kind, kind, tracks: [["a"]], weights: [1] },
					],
				}),
			).toThrow("one-track layout cannot set weights");
		}
	});

	test("layout weights must match track count", () => {
		expect(() =>
			validateProfile({
				...base,
				desk: [
					{
						display: "aw",
						label: "main",
						kind: "columns",
						tracks: [["a"], ["b"]],
						weights: [1],
					},
				],
			}),
		).toThrow("weights length must match tracks length");
	});

	test("layout and default weights must be finite and positive", () => {
		for (const weight of [0, -1, Number.NaN, Number.POSITIVE_INFINITY]) {
			expect(() =>
				validateProfile({
					...base,
					desk: [
						{
							display: "aw",
							label: "main",
							kind: "columns",
							tracks: [["a"], ["b"]],
							weights: [weight, 1],
						},
					],
				}),
			).toThrow("weights must be finite and greater than 0");
			expect(() =>
				validateProfile({ ...base, weights: { columns: { 2: [weight, 1] } } }),
			).toThrow("weights must be finite and greater than 0");
		}
	});

	test("default keys must match their vector length and be at least two", () => {
		expect(() =>
			validateProfile({ ...base, weights: { columns: { 3: [1, 1] } } }),
		).toThrow("key must equal vector length and be at least 2");
		expect(() =>
			validateProfile({ ...base, weights: { rows: { 1: [1] } } }),
		).toThrow("key must equal vector length and be at least 2");
	});

	test("chain ratios at the lower and upper boundaries are accepted", () => {
		expect(() =>
			validateProfile({ ...base, weights: { columns: { 2: [1, 9] } } }),
		).not.toThrow();
		expect(() =>
			validateProfile({ ...base, weights: { columns: { 2: [9, 1] } } }),
		).not.toThrow();
	});

	test("equal huge weights are as valid as equal small ones", () => {
		expect(() =>
			validateProfile({ ...base, weights: { columns: { 2: [1e308, 1e308] } } }),
		).not.toThrow();
	});

	test("chain ratios outside the bound are rejected in full vectors and subsequences", () => {
		expect(() =>
			validateProfile({ ...base, weights: { columns: { 2: [9.01, 1] } } }),
		).toThrow("weight chain ratios must be between 0.1 and 0.9");
		expect(() =>
			validateProfile({ ...base, weights: { columns: { 3: [9, 0.5, 0.5] } } }),
		).toThrow("weight chain ratios must be between 0.1 and 0.9");
	});

	test("the bundled default profile is valid", () => {
		expect(() => validateProfile(bundled)).not.toThrow();
	});
});
