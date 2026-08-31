import { afterEach, describe, expect, test } from "bun:test";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { readFlexOrder, writeFlexOrder } from "./state.ts";

let root = "";
afterEach(() => {
	if (root !== "") {
		rmSync(root, { recursive: true, force: true });
	}
	root = "";
});

describe("flex-order file", () => {
	test("write then read round-trips the order", () => {
		root = mkdtempSync(join(tmpdir(), "tess-flex-"));
		const path = join(root, "laptop-flex-order");
		writeFlexOrder(["arc", "obsidian", "lap-arc-5"], path);
		expect(readFlexOrder(path)).toEqual(["arc", "obsidian", "lap-arc-5"]);
	});

	test("a missing file reads as empty (first run, before any touch)", () => {
		root = mkdtempSync(join(tmpdir(), "tess-flex-"));
		expect(readFlexOrder(join(root, "nope"))).toEqual([]);
	});

	test("blank lines are dropped on read", () => {
		root = mkdtempSync(join(tmpdir(), "tess-flex-"));
		const path = join(root, "f");
		writeFileSync(path, "arc\n\nobsidian\n\n");
		expect(readFlexOrder(path)).toEqual(["arc", "obsidian"]);
	});

	test("write is one slug per line with a trailing newline", () => {
		root = mkdtempSync(join(tmpdir(), "tess-flex-"));
		const path = join(root, "f");
		writeFlexOrder(["a", "b"], path);
		expect(readFileSync(path, "utf8")).toBe("a\nb\n");
	});

	test("writing an empty order truncates to an empty file", () => {
		root = mkdtempSync(join(tmpdir(), "tess-flex-"));
		const path = join(root, "f");
		writeFlexOrder(["x"], path);
		writeFlexOrder([], path);
		expect(readFileSync(path, "utf8")).toBe("");
		expect(readFlexOrder(path)).toEqual([]);
	});

	test("write creates the parent cache dir if absent", () => {
		root = mkdtempSync(join(tmpdir(), "tess-flex-"));
		const path = join(root, "nested", "deeper", "laptop-flex-order");
		writeFlexOrder(["arc"], path);
		expect(readFlexOrder(path)).toEqual(["arc"]);
	});
});
