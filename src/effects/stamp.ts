// Epoch-second stamp files — the shared primitive behind the debounce waiters
// and the TTL signal guard. A stamp is one line: the integer `date +%s` of the last event.
// A missing/garbage stamp reads as absent (the bash `cat … || echo 0` idiom,
// mapped to null here so callers decide the sentinel).

import { readFileSync, writeFileSync } from "node:fs";

/** Write "now" (epoch seconds) to `path` (`date +%s >"$stamp"`). */
export function writeStamp(path: string): void {
	writeFileSync(path, `${Math.floor(Date.now() / 1000)}\n`);
}

/**
 * Read the epoch-second value at `path`, or null when the file is
 * absent/empty/non-numeric. The bash reads a missing stamp as `0`; callers that
 * want that sentinel use `readStamp(path) ?? 0`.
 */
export function readStamp(path: string): number | null {
	let raw: string;
	try {
		raw = readFileSync(path, "utf8");
	} catch {
		return null;
	}
	const n = Number.parseInt(raw.trim(), 10);
	return Number.isNaN(n) ? null : n;
}
