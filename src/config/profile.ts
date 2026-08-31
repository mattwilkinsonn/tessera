// THE ONE FILE YOU EDIT to change your layout.
//
// Pure data, no logic — the typed 1:1 port of the layout profile. The engine and the
// scripts/keybinds resolve everything through these names, so changing apps or
// layout is an edit here, never a code change. `satisfies Profile` gives the
// compile-time validation the bash file's shellcheck-disabled data block could
// not.
//
// Regex note (design Q7, port-verbatim): each WIN app/title is ported as an
// UNANCHORED JS `RegExp`, matching the substring semantics of today's jq
// `test()` ("Arc|" matches any app containing "Arc"). ONE engine now, so the
// Oniguruma-vs-ERE divergence the bash corpus policed by convention
// is gone; anchoring can be tightened later as a
// config-only change. The T2 re-validation test asserts each spec classifies a
// recorded live-window fixture identically to the documented bash behavior.

import type { Profile } from "./types.ts";

export const profile = {
	// DISPLAY_W. Width is the identity key — macOS reorders
	// display indexes on connect, widths are stable per monitor.
	displays: {
		g9: { width: 5120 },
		aw: { width: 3440 },
		laptop: { width: 1728 },
	},

	// WIN. `<app>|<title>` split into RegExp fields; empty
	// title = match any window of that app.
	windows: {
		arc: { app: /Arc/ },
		"ghostty-wave": { app: /Ghostty/, title: /pc/ },
		"ghostty-mbp": { app: /Ghostty/, title: /mbp/ },
		vscode: { app: /Code/ },
		akiflow: { app: /Akiflow/ },
		obsidian: { app: /Obsidian/ },
		linear: { app: /Linear/ },
		spotify: { app: /Spotify/ },
		discord: { app: /Discord/ },
		qalculate: { app: /Qalculate/ },
	},

	// Desk columns. G9 30/40/30 `main`, AW 50/50 `plan`, MBP single-stack
	// `laptop`. col[0] is the anchor, the rest of a column stacks.
	desk: [
		{
			display: "g9",
			label: "main",
			kind: "3col",
			columns: [
				["arc", "obsidian"],
				["ghostty-wave"],
				["ghostty-mbp", "vscode"],
			],
		},
		{
			display: "aw",
			label: "plan",
			kind: "2col",
			columns: [
				["linear", "arc"],
				["arc", "akiflow"],
			],
		},
		{
			display: "laptop",
			label: "laptop",
			kind: "stack",
			columns: [["arc", "spotify", "discord", "qalculate"]],
		},
	],

	// COL3_ROOT_RATIO / COL3_INNER_RATIO: for 30/40/30,
	// root = 0.30, inner = 0.40/0.70 = 0.5714.
	ratios: { col3Root: 0.3, col3Inner: 0.5714 },

	// DESK_SLOTS: numpad 1-9 focus order, `@display`
	// parsed into a field. Arc slots resolve to whichever Arc is on that display.
	deskSlots: [
		{ name: "ghostty-wave" }, // 1 — the G9 centre wave (primary dev)
		{ name: "arc", onDisplay: "g9" }, // 2 — G9 left Arc
		{ name: "ghostty-mbp" }, // 3 — G9 right MBP terminal
		{ name: "arc", onDisplay: "aw" }, // 4 — an AW Arc
		{ name: "arc", onDisplay: "aw" }, // 5 — an AW Arc (stack-cycle to reach the other)
		{ name: "linear" }, // 6
		{ name: "obsidian" }, // 7
		{ name: "akiflow" }, // 8
		{ name: "vscode" }, // 9
	],

	// LAPTOP_PINNED: the stable ordered prefix. `arc` repeats —
	// each occurrence claims a DISTINCT Arc window (occurrence-suffixed labels).
	laptopPinned: [
		"arc",
		"ghostty-wave",
		"arc",
		"ghostty-mbp",
		"linear",
		"obsidian",
		"arc",
		"akiflow",
		"arc",
	],

	// LAPTOP_STACK_APPS: the comms/system pile that stays on
	// the home stack instead of flexing. LITERAL yabai app names, not WIN names.
	laptopStackApps: {
		Messages: true,
		"System Settings": true,
		Slack: true,
		Discord: true,
		"1Password": true,
		"Activity Monitor": true,
	},
} satisfies Profile;
