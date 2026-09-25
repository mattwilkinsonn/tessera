import { describe, expect, test } from "bun:test";
import { profile as bundled } from "./profile.ts";
import type { Profile } from "./types.ts";
import { validateProfile } from "./validate.ts";

const base = {
	displays: { g9: { width: 1 }, aw: { width: 2 }, laptop: { width: 3 } },
	windows: {},
	desk: [],
	ratios: { col3Root: 0.3, col3Inner: 0.5714 },
	deskSlots: [],
	laptopPinned: [],
	laptopStackApps: {},
} satisfies Profile;

const layout = (display: "g9" | "aw" | "laptop") => ({
	display,
	label: display,
	kind: "stack" as const,
	columns: [["app"]],
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
						desk: [layout("aw"), layout("laptop")],
					},
				],
			}),
		).not.toThrow();
	});

	test("a missing display names the topology and display", () => {
		expect(() =>
			validateProfile({
				...base,
				topologies: [
					{
						name: "aw-laptop",
						displays: ["aw", "laptop"],
						desk: [layout("aw")],
					},
				],
			}),
		).toThrow('topology "aw-laptop": no layout for laptop');
	});

	test("an extra display is rejected", () => {
		expect(() =>
			validateProfile({
				...base,
				topologies: [
					{ name: "aw-only", displays: ["aw"], desk: [layout("laptop")] },
				],
			}),
		).toThrow('topology "aw-only": layout for undeclared display laptop');
	});

	test("all missing and extra display violations are reported", () => {
		let message = "";
		try {
			validateProfile({
				...base,
				topologies: [
					{ name: "one", displays: ["aw", "laptop"], desk: [layout("aw")] },
					{ name: "two", displays: ["g9"], desk: [layout("laptop")] },
				],
			});
		} catch (error) {
			if (error instanceof Error) message = error.message;
		}
		expect(message).toContain('topology "one": no layout for laptop');
		expect(message).toContain('topology "two": no layout for g9');
		expect(message).toContain(
			'topology "two": layout for undeclared display laptop',
		);
	});

	test("two layouts for one display are rejected", () => {
		expect(() =>
			validateProfile({
				...base,
				topologies: [
					{
						name: "aw-only",
						displays: ["aw"],
						desk: [layout("aw"), layout("aw")],
					},
				],
			}),
		).toThrow('topology "aw-only": duplicate layout for aw');
	});

	test("a layout may only set the split for its own kind", () => {
		const cases = [
			["2col", { col3: { col3Root: 0.4, col3Inner: 0.5 } }, "col3", "2col"],
			["3col", { col2: 0.6 }, "col2", "3col"],
			["stack", { col2: 0.6 }, "col2", "stack"],
			["stack", { col3: { col3Root: 0.4, col3Inner: 0.5 } }, "col3", "stack"],
		] as const;
		for (const [kind, ratios, key, named] of cases) {
			expect(() =>
				validateProfile({
					...base,
					desk: [{ ...layout("aw"), kind, ratios }],
				}),
			).toThrow(`desk aw: ratios.${key} does not apply to a ${named} layout`);
		}
	});

	test("a ratio outside (0, 1) is rejected", () => {
		expect(() =>
			validateProfile({
				...base,
				desk: [
					{
						...layout("aw"),
						kind: "3col",
						ratios: { col3: { col3Root: 1, col3Inner: 0.5 } },
					},
				],
			}),
		).toThrow("desk aw: ratios must be between 0 and 1");
	});

	test("display and 2col layout ratios must be strictly between zero and one", () => {
		expect(() =>
			validateProfile({
				...base,
				displays: { ...base.displays, aw: { width: 2, ratios: { col2: 0 } } },
			}),
		).toThrow("display aw: ratios must be between 0 and 1");
		expect(() =>
			validateProfile({
				...base,
				desk: [{ ...layout("aw"), kind: "2col", ratios: { col2: 1 } }],
			}),
		).toThrow("desk aw: ratios must be between 0 and 1");
	});
	test("the bundled default profile is valid", () => {
		expect(() => validateProfile(bundled)).not.toThrow();
	});
});
