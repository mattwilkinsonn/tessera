import { afterEach, describe, expect, test } from "bun:test";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { readStamp, writeStamp } from "./stamp.ts";

let root = "";
afterEach(() => {
	if (root !== "") {
		rmSync(root, { recursive: true, force: true });
	}
	root = "";
});

describe("stamp file", () => {
	test("writeStamp then readStamp round-trips a recent epoch second", () => {
		root = mkdtempSync(join(tmpdir(), "tess-stamp-"));
		const path = join(root, "s");
		const before = Math.floor(Date.now() / 1000);
		writeStamp(path);
		const got = readStamp(path);
		expect(got).not.toBeNull();
		// Within a couple seconds of "now" — it is the current time, not a fixed 0.
		expect(got as number).toBeGreaterThanOrEqual(before);
		expect(got as number).toBeLessThanOrEqual(before + 2);
	});

	test("a missing file reads as null (the bash `cat || echo 0` absence)", () => {
		root = mkdtempSync(join(tmpdir(), "tess-stamp-"));
		expect(readStamp(join(root, "nope"))).toBeNull();
	});

	test("a non-numeric file reads as null, not NaN", () => {
		root = mkdtempSync(join(tmpdir(), "tess-stamp-"));
		const path = join(root, "junk");
		writeFileSync(path, "not-a-number\n");
		expect(readStamp(path)).toBeNull();
	});
});
