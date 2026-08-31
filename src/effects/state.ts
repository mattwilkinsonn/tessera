// The flex-order file effect. Read/write ONLY — the
// stable-append reconciliation is pure in `engine/flex.ts` (`reconcileFlexOrder`).
// The file is one bare slug per line at
// `${XDG_CACHE_HOME:-~/.cache}/yabai/laptop-flex-order`; a slug's line position
// is its stable slot across relaunch, which is what keeps a flex window on the
// same space when unrelated apps come and go.
//
// The path defaults to `flexOrderPath()` (the live location); it is a parameter
// so tests round-trip against a temp file without clobbering the live machine's
// order — the constant already honors XDG_CACHE_HOME for the real path.

import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";
import { flexOrderPath } from "./constants.ts";

/**
 * Read the persisted flex order — the file's lines in order, blanks dropped.
 * Missing file → empty (the bash `touch`es it; a first run has no order yet).
 */
export function readFlexOrder(path: string = flexOrderPath()): string[] {
	let raw: string;
	try {
		raw = readFileSync(path, "utf8");
	} catch {
		return [];
	}
	return raw.split("\n").filter((line) => line !== "");
}

/**
 * Persist the flex order — one slug per line, trailing newline (the shape the
 * bash appends). Creates the parent dir if absent.
 */
export function writeFlexOrder(
	order: readonly string[],
	path: string = flexOrderPath(),
): void {
	mkdirSync(dirname(path), { recursive: true });
	writeFileSync(path, order.map((s) => `${s}\n`).join(""));
}
