import { describe, expect, test } from "bun:test";
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
});
