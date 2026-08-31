import { afterEach, describe, expect, test } from "bun:test";
import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { GUARD_TTL_SECS } from "./constants.ts";
import { releaseSignals, signalsSuppressed, suppressSignals } from "./guard.ts";
import { readStamp } from "./stamp.ts";

let root = "";
afterEach(() => {
	if (root !== "") {
		rmSync(root, { recursive: true, force: true });
	}
	root = "";
});

describe("TTL signal guard", () => {
	test("no guard file → not suppressed", () => {
		root = mkdtempSync(join(tmpdir(), "tess-guard-"));
		expect(signalsSuppressed(join(root, "g"))).toBe(false);
	});

	test("freshly stamped guard suppresses signals", () => {
		root = mkdtempSync(join(tmpdir(), "tess-guard-"));
		const g = join(root, "g");
		suppressSignals(g);
		expect(signalsSuppressed(g)).toBe(true);
	});

	test("a guard just under the TTL still suppresses", () => {
		root = mkdtempSync(join(tmpdir(), "tess-guard-"));
		const g = join(root, "g");
		suppressSignals(g);
		const stamped = readStamp(g) as number;
		// now = stamp + (TTL-1): age < TTL → held.
		expect(signalsSuppressed(g, stamped + GUARD_TTL_SECS - 1)).toBe(true);
	});

	test("a guard at/over the TTL expires, reads false, and is removed", () => {
		root = mkdtempSync(join(tmpdir(), "tess-guard-"));
		const g = join(root, "g");
		suppressSignals(g);
		const stamped = readStamp(g) as number;
		expect(signalsSuppressed(g, stamped + GUARD_TTL_SECS)).toBe(false);
		// self-healing GC: the expired file is gone.
		expect(existsSync(g)).toBe(false);
	});

	test("release clears a held guard", () => {
		root = mkdtempSync(join(tmpdir(), "tess-guard-"));
		const g = join(root, "g");
		suppressSignals(g);
		releaseSignals(g);
		expect(signalsSuppressed(g)).toBe(false);
	});
});
