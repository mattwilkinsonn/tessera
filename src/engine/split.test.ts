import { describe, expect, test } from "bun:test";
import { profile } from "../config/profile.fixture.ts";
import type { Profile } from "../config/types.ts";
import { weightsFor } from "./split.ts";

describe("weightsFor", () => {
	test("resolves layout, display, profile, then equal weights", () => {
		const configured = {
			...profile,
			weights: { columns: { 3: [7, 2, 1] } },
			displays: {
				...profile.displays,
				g9: { ...profile.displays.g9, weights: { columns: { 3: [4, 3, 3] } } },
			},
		} satisfies Profile;

		expect(weightsFor(configured, "columns", 3, "g9", [1, 2, 3])).toEqual([
			1, 2, 3,
		]);
		expect(weightsFor(configured, "columns", 3, "g9")).toEqual([4, 3, 3]);
		expect(weightsFor(configured, "columns", 3, "aw")).toEqual([7, 2, 1]);
		expect(weightsFor(configured, "columns", 4, "g9")).toEqual([1, 1, 1, 1]);
	});

	test("only uses defaults with the requested kind and track count", () => {
		const configured = {
			...profile,
			weights: { columns: { 2: [5, 1] }, rows: { 2: [2, 3] } },
			displays: {
				...profile.displays,
				g9: {
					...profile.displays.g9,
					weights: { columns: { 3: [4, 3, 3] }, rows: { 3: [1, 2, 1] } },
				},
			},
		} satisfies Profile;

		expect(weightsFor(configured, "columns", 2, "g9")).toEqual([5, 1]);
		expect(weightsFor(configured, "rows", 2, "g9")).toEqual([2, 3]);
		expect(weightsFor(configured, "columns", 4, "g9")).toEqual([1, 1, 1, 1]);
		expect(weightsFor(configured, "rows", 3, "g9")).toEqual([1, 2, 1]);
		expect(weightsFor(configured, "columns", 3, "g9")).toEqual([4, 3, 3]);
	});
});
