// The one place the coordination constants and on-disk paths live, so the two
// debounce waiters (display + flex) can never drift out of sync — the flex
// waiter's H7 display-quiet gate MUST use the SAME quiet window the display
// cascade settles on (calls this "keep in sync
// withQUIET_SECS"). The bash corpus enforced this by a code
// comment across two files; one engine, one module, so it is now a type-checked
// import instead of a convention.

import { homedir } from "node:os";
import { join } from "node:path";

/**
 * The XDG cache dir for the flex-order file (—
 * `${XDG_CACHE_HOME:-$HOME/.cache}/yabai`). Read live so a test can point it at
 * a temp dir via the environment.
 */
export function cacheDir(): string {
	const xdg = process.env.XDG_CACHE_HOME;
	const base = xdg != null && xdg !== "" ? xdg : join(homedir(), ".cache");
	return join(base, "yabai");
}

/** Stable-append flex-order file. */
export function flexOrderPath(): string {
	return join(cacheDir(), "laptop-flex-order");
}

// ── /tmp coordination paths (verbatim from the bash, so a mid-cutover mixed
// state — some scripts, some `tess` — shares one lock protocol and can't deadlock).

/** `tess apply` mkdir-lock. */
export const APPLY_LOCK = "/tmp/yabai-apply-workspace.lock";
/** `tess laptop` converge mkdir-lock. */
export const LAPTOP_LOCK = "/tmp/yabai-laptop-layout.lock";

/** display-event debounce stamp + waiter lock. */
export const DISPLAY_STAMP = "/tmp/yabai-display-event.stamp";
export const DISPLAY_WAITER_LOCK = "/tmp/yabai-display-event.waiter.lock";

/** flex-event debounce stamp + waiter lock. */
export const FLEX_STAMP = "/tmp/yabai-laptop-flex.stamp";
export const FLEX_WAITER_LOCK = "/tmp/yabai-laptop-flex.waiter.lock";

/** TTL'd signal guard. */
export const SIGNAL_GUARD = "/tmp/yabai-apply.guard";

// ── Timing (seconds), each grounded in its source. ──────────────────────────

/** Signal-guard self-expiry (GUARD_TTL). */
export const GUARD_TTL_SECS = 8;

/**
 * Display-cascade quiet window (QUIET_SECS). The flex
 * waiter's H7 gate mirrors this exact value — one constant, so they cannot diverge.
 */
export const DISPLAY_QUIET_SECS = 3;

/** Flex-rebuild quiet window (QUIET_SECS). */
export const FLEX_QUIET_SECS = 2;

/** Absolute sketchybar path with `|| true` semantics. */
export const SKETCHYBAR_PATH = "/opt/homebrew/bin/sketchybar";
