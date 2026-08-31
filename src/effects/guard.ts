// The TTL'd signal guard. A manual `tess apply` or an unplug
// `tess laptop` holds the guard while it rearranges windows; the auto flex waiter
// reads it (`signalsSuppressed()`) and re-waits rather than converge on top of
// the in-flight rearrange (H2). It self-expires
// after GUARD_TTL seconds so a crashed apply can't wedge auto-layout forever —
// long-running phases re-stamp at each boundary to keep it held.
//
// The path defaults to the shared /tmp constant (so the on-disk protocol is
// identical to the bash and a mixed-cutover state interoperates); it is a
// parameter so tests exercise TTL expiry on a temp file without touching the
// live machine's guard.

import { rmSync, writeFileSync } from "node:fs";
import { GUARD_TTL_SECS, SIGNAL_GUARD } from "./constants.ts";
import { readStamp } from "./stamp.ts";

/** Stamp the guard "now" (`suppress_signals`). Re-stamp to extend. */
export function suppressSignals(path: string = SIGNAL_GUARD): void {
	writeFileSync(path, `${nowSecs()}\n`);
}

/** Clear the guard (`release_signals`). */
export function releaseSignals(path: string = SIGNAL_GUARD): void {
	rmSync(path, { force: true });
}

/**
 * True while a guard is held AND unexpired. An expired guard is removed as a side effect and reads
 * false, matching the bash's self-healing GC. `now` is injectable for tests.
 */
export function signalsSuppressed(
	path: string = SIGNAL_GUARD,
	now: number = nowSecs(),
): boolean {
	const stamped = readStamp(path);
	if (stamped == null) {
		return false;
	}
	if (now - stamped >= GUARD_TTL_SECS) {
		rmSync(path, { force: true });
		return false;
	}
	return true;
}

function nowSecs(): number {
	return Math.floor(Date.now() / 1000);
}
