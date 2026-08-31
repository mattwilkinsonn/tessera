// Fire-and-forget status-bar nudge (the `$SKETCHYBAR --trigger …
// 2>/dev/null || true` sites). SketchyBar names the yabai spaces and loses its
// per-display attachment across a topology change; a nudge tells its plugin to
// re-read labels/focus. Deliberately OUTSIDE `WmDriver` — a status-bar concern,
// not a window-manager op. NEVER throws and is a silent no-op when sketchybar is
// absent (the `|| true` semantics), so a converge is never gated on the bar.

import { $ } from "bun";
import { SKETCHYBAR_PATH } from "./constants.ts";

/**
 * Trigger a custom sketchybar event (e.g. `yabai_spaces_changed`,
 * `display_relatch`). Resolves regardless of outcome — a missing binary or a
 * nonzero exit is swallowed, matching the bash `2>/dev/null || true`.
 */
export async function nudgeSketchybar(event: string): Promise<void> {
	await $`${SKETCHYBAR_PATH} --trigger ${event}`.quiet().nothrow();
}
