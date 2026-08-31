// Layer 1 — CONFIG shapes.
//
// The typed 1:1 port of the currentdeclarations. Pure data, no
// logic: this module defines the shapes; `profile.ts` holds the values (the ONE
// file Matt edits). Topology-portable by construction — layout binds to logical
// `DisplayName`s and the engine skips a display that is absent (D2-corollary),
// so a no-laptop rig (Hyprland) is a config selection, not a code change.

/** Logical display name. Width (not the unstable macOS UUID/index) is the identity key. */
export type DisplayName = "g9" | "aw" | "laptop";

/** A WIN logical name — the interchangeable handle a slot claims ("arc", "ghostty-wave", …). */
export type WindowName = string;

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
}

/** A desk column set for one space on one display — apply-workspace's three shapes. */
export interface DeskLayout {
	/** The display this space lives on; skipped when the display is absent. */
	display: DisplayName;
	/** Space label: "main" | "plan" | "laptop". */
	label: string;
	/** The layout shape ('s three kinds). */
	kind: "3col" | "2col" | "stack";
	/** Ordered columns of window names; `col[0]` is the anchor, the rest stack. */
	columns: ReadonlyArray<ReadonlyArray<WindowName>>;
}

/** A numpad focus slot: a window name, optionally pinned to a display (`name@display`). */
export interface DeskSlot {
	name: WindowName;
	/** The `@display` suffix, parsed instead of string-split on `@`. */
	onDisplay?: DisplayName;
}

/** The full typed layout profile — the 1:1 shape of. */
export interface Profile {
	/** Logical display name → stable width in px (`DISPLAY_W`). */
	displays: Record<DisplayName, { width: number }>;
	/** WIN specs, keyed by logical name. */
	windows: Record<WindowName, WindowSpec>;
	/** Desk columns: G9_LEFT/MAIN/RIGHT, AW_LEFT/RIGHT, MBP_STACK. */
	desk: ReadonlyArray<DeskLayout>;
	/** COL3_ROOT_RATIO / COL3_INNER_RATIO. */
	ratios: { col3Root: number; col3Inner: number };
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
