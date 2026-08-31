// mkdir-based /tmp locks with pidfile + stale-PID reclaim. mkdir is atomic on
// the local filesystem, so it is the primitive every yabai script serializes on
// Same on-disk paths as the bash, so a
// mid-cutover mixed state (some scripts, some `tess`) shares one lock and can't
// double-run.
//
// The reclaim rule (the load-bearing subtlety): an EXIT trap frees the lock on
// a clean exit, but a SIGKILL (macOS memory pressure) leaves the dir behind
// forever, which would wedge every future run. So on contention, read the
// recorded PID: a LIVE holder is genuine contention; a DEAD one is a stale lock
// to steal. `kill(pid, 0)` is the liveness probe (throws ESRCH when gone).

import { mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";

/** A held lock; call `release()` (idempotent) to free it. */
export interface Lock {
	release(): void;
}

function pidAlive(pid: number): boolean {
	if (!Number.isInteger(pid) || pid <= 0) {
		return false;
	}
	try {
		// Signal 0 performs error checking without sending a signal: it succeeds
		// if the process exists (and we may signal it), throws ESRCH if not.
		process.kill(pid, 0);
		return true;
	} catch (err) {
		// EPERM = the process exists but is not ours to signal → treat as a LIVE
		// holder (don't steal the lock). This INTENTIONALLY diverges from the bash
		// probe `kill -0 "$pid" 2>/dev/null`, which returns exit 1 on EPERM and so
		// treats an EPERM pid as DEAD and steals the lock. The TS behavior is the
		// safer of the two (never steal a live lock); the two only disagree for a
		// lock held by a different-uid process, which is unreachable here — every
		// holder is one of Matt's own same-uid `tess`/script processes. Worth a note
		// so a future multi-user port doesn't assume bash-identical reclaim.
		return (err as NodeJS.ErrnoException).code === "EPERM";
	}
}

function readPid(lockDir: string): number | null {
	try {
		const raw = readFileSync(`${lockDir}/pid`, "utf8");
		const n = Number.parseInt(String(raw).trim(), 10);
		return Number.isNaN(n) ? null : n;
	} catch {
		return null;
	}
}

function claim(lockDir: string): Lock | null {
	try {
		mkdirSync(lockDir);
	} catch {
		return null;
	}
	writeFileSync(`${lockDir}/pid`, `${process.pid}\n`);
	let released = false;
	const release = (): void => {
		if (released) {
			return;
		}
		released = true;
		rmSync(lockDir, { recursive: true, force: true });
	};
	// Free the lock on a clean process exit (the bash EXIT trap). A SIGKILL
	// skips this — that is exactly the stale lock the reclaim path below steals.
	process.once("exit", release);
	return { release };
}

/**
 * Acquire `lockDir` with stale-PID reclaim (shape).
 * Returns the held lock, or null when a LIVE holder owns it (genuine
 * contention — the caller decides whether that is a re-loop or a no-op).
 */
export function acquireLock(lockDir: string): Lock | null {
	const first = claim(lockDir);
	if (first != null) {
		return first;
	}
	// Contended. A live holder is real contention; a dead one is stale — steal.
	const pid = readPid(lockDir);
	if (pid != null && pidAlive(pid)) {
		return null;
	}
	// Stale lock from a dead holder. A concurrent run may win the reclaim race
	// and steal it first; that's fine — the work these locks guard is idempotent.
	rmSync(lockDir, { recursive: true, force: true });
	return claim(lockDir);
}

/**
 * Acquire `lockDir`, surrendering immediately on ANY contention — no reclaim,
 * no pidfile probe (a second apply just exits 0).
 * Returns the held lock or null.
 */
export function acquireLockOrSkip(lockDir: string): Lock | null {
	return claim(lockDir);
}
