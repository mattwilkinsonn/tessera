import { afterEach, describe, expect, test } from "bun:test";
import {
	existsSync,
	mkdirSync,
	mkdtempSync,
	rmSync,
	writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { acquireLock, acquireLockOrSkip } from "./locks.ts";

let root = "";
afterEach(() => {
	if (root !== "") {
		rmSync(root, { recursive: true, force: true });
	}
	root = "";
});

describe("acquireLock (reclaim variant)", () => {
	test("first caller acquires; release frees the dir", () => {
		root = mkdtempSync(join(tmpdir(), "tess-lock-"));
		const dir = join(root, "l.lock");
		const lock = acquireLock(dir);
		expect(lock).not.toBeNull();
		expect(existsSync(dir)).toBe(true);
		lock?.release();
		expect(existsSync(dir)).toBe(false);
	});

	test("release is idempotent", () => {
		root = mkdtempSync(join(tmpdir(), "tess-lock-"));
		const lock = acquireLock(join(root, "l.lock"));
		lock?.release();
		expect(() => lock?.release()).not.toThrow();
	});

	test("a LIVE holder (our own pid) is genuine contention → null", () => {
		root = mkdtempSync(join(tmpdir(), "tess-lock-"));
		const dir = join(root, "l.lock");
		// Simulate a live holder: a lock dir with THIS process's pid, which is
		// trivially alive.
		mkdirSync(dir);
		writeFileSync(join(dir, "pid"), `${process.pid}\n`);
		expect(acquireLock(dir)).toBeNull();
	});

	test("a DEAD holder's stale lock is reclaimed", () => {
		root = mkdtempSync(join(tmpdir(), "tess-lock-"));
		const dir = join(root, "l.lock");
		// A pid that cannot be alive: kill(0) of a never-used high pid throws
		// ESRCH. 2^31-1 is above any real pid on macOS.
		mkdirSync(dir);
		writeFileSync(join(dir, "pid"), `${2 ** 31 - 1}\n`);
		const lock = acquireLock(dir);
		expect(lock).not.toBeNull();
		lock?.release();
	});

	test("a stale lock with no pidfile is reclaimed (nothing to prove alive)", () => {
		root = mkdtempSync(join(tmpdir(), "tess-lock-"));
		const dir = join(root, "l.lock");
		mkdirSync(dir);
		const lock = acquireLock(dir);
		expect(lock).not.toBeNull();
		lock?.release();
	});
});

describe("acquireLockOrSkip (surrender variant)", () => {
	test("acquires when free", () => {
		root = mkdtempSync(join(tmpdir(), "tess-lock-"));
		const lock = acquireLockOrSkip(join(root, "a.lock"));
		expect(lock).not.toBeNull();
		lock?.release();
	});

	test("surrenders on ANY contention — even a stale/dead holder — no reclaim", () => {
		root = mkdtempSync(join(tmpdir(), "tess-lock-"));
		const dir = join(root, "a.lock");
		mkdirSync(dir); // a pre-existing dir, no pidfile: apply just exits 0
		expect(acquireLockOrSkip(dir)).toBeNull();
	});
});
