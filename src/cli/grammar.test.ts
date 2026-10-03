import { describe, expect, test } from "bun:test";
import { parseArgs } from "../index.ts";
import { isIntToken, SUBCOMMANDS } from "./grammar.ts";

describe("SUBCOMMANDS grammar", () => {
	test("literal choices match the existing parser", () => {
		for (const [name, spec] of Object.entries(SUBCOMMANDS)) {
			if (spec.choices === null) continue;
			for (const choice of spec.choices) {
				const parsed = parseArgs([name, choice]);
				expect(parsed.ok).toBe(true);
			}
		}
	});

	test("integer tokens match the Effect CLI accepted numeric forms", () => {
		for (const token of ["7", "007", "+1", "1.0", "1e2"]) {
			expect(isIntToken(token)).toBe(true);
		}
		for (const token of ["0x10", "", " 1", "1.5", "abc"]) {
			expect(isIntToken(token)).toBe(false);
		}
	});
});
