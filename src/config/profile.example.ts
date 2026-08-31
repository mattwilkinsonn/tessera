// EXAMPLE profile — copy this to `profile.ts` and edit it for your own setup.
//
// A neutral example with placeholder app names, meant to show the shape of
// every field. `profile.ts` is the file the binary actually reads; this file is
// never imported, so it ships with the source as a reference, not as live
// config.
//
// There are three logical display slots, currently named `g9`, `aw`, and
// `laptop` in the type (`types.ts`) — read them as "external display 1",
// "external display 2", and "the built-in laptop screen". Renaming the slots
// means editing `DisplayName` in `types.ts` and the few engine call sites that
// resolve a slot by name; this example keeps the stock names. Layout binds to
// these logical names, and the engine skips any slot whose display is not
// connected — so the two-monitor arrangement below (one external + laptop)
// simply leaves the `aw` slot out of `desk` and `deskSlots`, and it is skipped
// at runtime when that display is absent.
//
// Displays are matched by frame WIDTH, not the macOS display index (which is
// reordered on connect), so the widths below are the identity keys: set them to
// your own monitors' pixel widths.
//
// Every app/title matcher is an UNANCHORED `RegExp` with substring semantics
// (`/Browser/` matches any app whose name contains "Browser"). Tighten with
// anchors (`/^Browser$/`) if a loose match collides.

import type { Profile } from "./types.ts";

export const profile = {
	// Logical display slot → stable width in px. All three slots are declared
	// even in a two-monitor setup; `aw` here is defined but left out of the desk
	// below, so the engine skips it whenever that display isn't connected.
	displays: {
		g9: { width: 3440 }, // external display 1 — a 3440px ultrawide
		aw: { width: 2560 }, // external display 2 — declared but unused below
		laptop: { width: 1512 }, // the built-in laptop display
	},

	// Logical window name → `<app>|<title>` matcher. An absent `title` matches
	// any window of that app; give a `title` only when one app hosts several
	// distinct windows you want in different slots.
	windows: {
		browser: { app: /Browser/ },
		"terminal-work": { app: /Terminal/, title: /work/ },
		"terminal-scratch": { app: /Terminal/, title: /scratch/ },
		editor: { app: /Editor/ },
		notes: { app: /Notes/ },
		calendar: { app: /Calendar/ },
		music: { app: /Music/ },
		chat: { app: /Chat/ },
	},

	// Desk columns per space. `col[0]` is the column anchor; the rest of a
	// column stacks behind it. `3col`/`2col`/`stack` are the three shapes. This
	// example lays the external display out as a 3-column desk and the laptop as
	// a single stack; the `aw` slot is intentionally omitted.
	desk: [
		{
			display: "g9",
			label: "main",
			kind: "3col",
			columns: [
				["browser", "notes"],
				["terminal-work"],
				["editor", "calendar"],
			],
		},
		{
			display: "laptop",
			label: "laptop",
			kind: "stack",
			columns: [["browser", "terminal-work", "chat", "music"]],
		},
	],

	// Split ratios for the 3-column desk: for a 30/40/30 layout the outer root
	// split is 0.30 and the inner split is 0.40/0.70 = 0.5714.
	ratios: { col3Root: 0.3, col3Inner: 0.5714 },

	// Numpad 1-9 focus order. `onDisplay` pins a slot to a logical display when
	// the same window name can appear on more than one screen.
	deskSlots: [
		{ name: "terminal-work" }, // 1
		{ name: "browser", onDisplay: "g9" }, // 2
		{ name: "editor" }, // 3
		{ name: "notes" }, // 4
		{ name: "calendar" }, // 5
		{ name: "music" }, // 6
	],

	// Laptop-mode stable ordered prefix. A repeated name claims a DISTINCT
	// window each time (occurrence-suffixed labels), so `browser` twice pins two
	// separate browser windows.
	laptopPinned: ["browser", "terminal-work", "editor", "notes", "browser"],

	// LITERAL app names (not the logical window names above) that stay on the
	// home stack instead of flexing out to their own space — the comms/system
	// pile. Edit this table to demote an app into the pile.
	laptopStackApps: {
		Chat: true,
		Music: true,
		Calendar: true,
	},
} satisfies Profile;
