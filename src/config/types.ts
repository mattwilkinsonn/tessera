// Layer 1 — CONFIG shapes.
//
// Pure data, no logic: this module defines the shapes; `profile.ts` holds the
// values. Layout binds to logical `DisplayName`s and the engine skips an absent
// display, so a different rig is a config selection, not a code change.

/** Logical display name. Width (not the unstable macOS UUID/index) is the identity key. */
export type DisplayName = "g9" | "aw" | "laptop";

/** A WIN logical name — the interchangeable handle a slot claims ("arc", "ghostty-wave", …). */
export type WindowName = string;

/** Which way the tracks run: columns side by side, rows top to bottom. */
export type TrackKind = "columns" | "rows";
/** Relative track sizes, one per track: `[3, 4, 3]` is 30/40/30. */
export type Weights = ReadonlyArray<number>;
/** Default weights keyed by track count. */
export interface WeightDefaults {
	columns?: Readonly<Record<number, Weights>>;
	rows?: Readonly<Record<number, Weights>>;
}

/**
 * A window match spec: `<app-regex>|<title-regex>` insplit into
 * fields. ONE regex engine (JS `RegExp`) matches both the claim and the slug,
 * collapsing the two-engine (Oniguruma vs POSIX ERE) divergence class the bash
 * corpus had to police by convention.
 */
export interface WindowSpec {
	/** App-name matcher, e.g. `/Arc/` ← `"Arc|"`. Anchoring decided per-spec at port time. */
	app: RegExp;
	/** Title matcher; absent = match any title ("Empty title"). */
	title?: RegExp;
	/** The leading-`!` title inversion: match windows whose title does NOT match. */
	titleInvert?: boolean;
	/** Prompt-exit argv to open a window. The new window must match this app spec and no other spec's title. */
	spawn?: ReadonlyArray<string>;
}

/** A desk layout for one space on one display. */
export interface DeskLayout {
	/** The display this space lives on; skipped when the display is absent. */
	display: DisplayName;
	/** Space label: "main" | "plan" | "laptop". */
	label: string;
	/** `columns` / `rows` split the space into tracks; `stack` piles the whole space. */
	kind: TrackKind | "stack";
	/** One entry per track: `track[0]` is the anchor, the rest stack behind it. */
	tracks: ReadonlyArray<ReadonlyArray<WindowName>>;
	/** Overrides the display/profile default for this track count. */
	weights?: Weights;
}

/** A numpad focus slot: a window name, optionally pinned to a display (`name@display`). */
export interface DeskSlot {
	name: WindowName;
	/** The `@display` suffix, parsed instead of string-split on `@`. */
	onDisplay?: DisplayName;
}

/**
 * A named layout for ONE exact set of present displays. Selected when the
 * connected displays are exactly `displays`; its `desk` then replaces
 * `Profile.desk` wholesale for that run.
 *
 * Exact-set, not subset: a subset rule would let a two-display topology claim a
 * three-display rig and silently drop the third display's layout.
 */
export interface Topology {
	/** Human label for the arrangement ("aw-laptop"); diagnostics only. */
	name: string;
	/** The display slots that must be present, exactly — order is irrelevant. */
	displays: ReadonlyArray<DisplayName>;
	/**
	 * Replaces `Profile.desk` under this arrangement. Must lay out every declared
	 * display and no other; the profile loader rejects a mismatch.
	 */
	desk: ReadonlyArray<DeskLayout>;
}

/** The full typed layout profile. */
export interface Profile {
	/** Logical display name → stable width in px, plus per-axis track defaults. */
	displays: Record<DisplayName, { width: number; weights?: WeightDefaults }>;
	/** WIN specs, keyed by logical name. */
	windows: Record<WindowName, WindowSpec>;
	/** Desk layouts by display. */
	desk: ReadonlyArray<DeskLayout>;
	/**
	 * Per-arrangement desk overrides, first exact present-set match winning
	 * (declaration order is the precedence). Absent → `desk` always applies.
	 */
	topologies?: ReadonlyArray<Topology>;
	/** Profile-wide defaults; the last fallback before equal weights. */
	weights?: WeightDefaults;
	/** Numpad focus slots with `@display` preference (`DESK_SLOTS`). */
	deskSlots: ReadonlyArray<DeskSlot>;
	/**
	 * The laptop-mode stable ordered prefix (`LAPTOP_PINNED`).
	 * Repeats claim DISTINCT windows; occurrence-suffixed labels. Selected only by
	 * a topology that has a `laptop` display.
	 */
	laptopPinned: ReadonlyArray<WindowName>;
	/**
	 * Literal yabai app names that stay on the home stack instead of flexing
	 * (`LAPTOP_STACK_APPS`). Keys are LITERAL app names, not WIN
	 * logical names — a small static membership table, hand-edited to demote an
	 * app into the pile.
	 */
	laptopStackApps: Readonly<Record<string, true>>;
}
