import { describe, expect, test } from "bun:test";
import { isIntToken, SUBCOMMANDS } from "./grammar.ts";

describe("SUBCOMMANDS grammar", () => {
	test("literal choices match the declared CLI grammar", () => {
		const choices = Object.fromEntries(
			Object.entries(SUBCOMMANDS)
				.filter(([, spec]) => spec.choices !== null)
				.map(([name, spec]) => [name, spec.choices]),
		);
		expect(choices).toEqual({
			snap: ["3col", "50-50", "columns"],
			"stack-cycle": ["next", "prev"],
			resize: ["grow", "shrink"],
			"move-display": ["g9", "aw", "laptop"],
			"cycle-display": ["next", "prev"],
			focus: ["west", "south", "north", "east"],
			swap: ["west", "south", "north", "east"],
			warp: ["west", "south", "north", "east"],
			insert: ["east", "west", "north", "south", "stack"],
			space: ["bsp", "stack"],
		});
	});

	test("integer tokens match the Effect CLI accepted numeric forms", () => {
		for (const token of ["7", "007", "+1", "1.0", "1e2"]) {
			expect(isIntToken(token)).toBe(true);
		}
		for (const token of [
			"0x10",
			"",
			" 1",
			"1.5",
			"abc",
			"9007199254740992",
			"1e16",
		]) {
			expect(isIntToken(token)).toBe(false);
		}
	});
});
