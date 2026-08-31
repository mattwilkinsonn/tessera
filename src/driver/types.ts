// Layer 3 — the WmDriver contract.
//
// The load-bearing seam of the whole design: the engine and CLI address the
// window manager ONLY through this interface, so a new backend (Hyprland,
// a Rust-core FFI driver) is a driver swap, not a rewrite. YabaiDriver (T5) is
// the only implementation now.
//
// SOFT-FROZEN until T5 merges: T5's PR — the first real driver — may still
// adjust the contract, because interface flaws surface writing the first real
// driver. The HARD freeze (amendment-gated) takes effect when T5 merges.

// Space identity (D1, Matt): the engine addresses spaces by an opaque stable
// `SpaceId` minted by the driver — never yabai's live index, which renumbers
// on create/destroy/move. The driver owns the id → live-index map.
export type SpaceId = string & { readonly __brand: "SpaceId" };

/**
 * A window, normalized to camelCase from the yabai query JSON the scripts
 * consume.
 */
export interface WmWindow {
	id: number;
	app: string;
	title: string;
	displayIdx: number;
	spaceId: SpaceId;
	minimized: boolean;
	floating: boolean;
	sticky: boolean;
	visible: boolean;
	splitType: "vertical" | "horizontal" | "none";
	frame: { x: number; y: number; w: number; h: number };
}

/** A space. `windowIds` is ALL windows on it, including minimized/sticky. */
export interface WmSpace {
	id: SpaceId;
	label: string;
	displayIdx: number;
	windowIds: ReadonlyArray<number>;
	layout: "bsp" | "stack" | "float";
}

/** A display. `spaceIds` is ordered; `[0]` is the home space. */
export interface WmDisplay {
	idx: number;
	frame: { x: number; y: number; w: number; h: number };
	spaceIds: ReadonlyArray<SpaceId>;
}

export type DirSel = "west" | "south" | "north" | "east";
export type StackSel =
	| "stack.next"
	| "stack.prev"
	| "stack.first"
	| "stack.last";
export type DisplaySel = number | "next" | "prev" | "first" | "last";

/**
 * The declarative layout target (D2): backend-neutral intent the driver
 * realizes with its own imperative recipe + settle cadence. `columns` hold
 * resolved window ids; `col[0]` is the anchor, the rest stack.
 */
export interface SpaceLayoutTarget {
	kind: "3col" | "2col" | "stack";
	columns: ReadonlyArray<ReadonlyArray<number>>;
	/** COL3 ratios from the Profile (`{ root, inner }`). */
	ratios?: { root: number; inner: number };
}

/** Arrival-routing rules — yabai-specific shape, optional per backend. */
export interface WmRuleOps {
	list(): Promise<Array<{ label: string }>>;
	remove(label: string): Promise<void>;
	add(rule: {
		label: string;
		app?: string;
		subrole?: string;
		displayIdx?: number;
		spaceIdx?: number;
		manage?: boolean;
	}): Promise<void>;
	apply(): Promise<void>;
}

/** Signal subscription (`yabai -m signal --add`) — used by `tess init` wiring. */
export interface WmEventSource {
	register(event: WmEvent, command: string[]): Promise<void>;
}

export type WmEvent =
	| "display_added"
	| "display_removed"
	| "display_moved"
	| "application_launched"
	| "application_terminated"
	| "window_created"
	| "window_destroyed"
	| "space_changed"
	| "display_changed"
	| "dock_did_restart";

/**
 * The window-manager driver. One object, internally segmented, with optional
 * capability sub-interfaces (`rules`, `events`) for the genuinely backend-shaped
 * parts. Failure convention: mutators the scripts guard with `|| true` resolve
 * on failure (`Promise<boolean>` / resolve-void) rather than throw; queries
 * throw only on driver-gone (yabai not running), which callers surface as a
 * clean exit.
 */
export interface WmDriver {
	// ── Queries (every mutator invalidates; snapshot caching is the executor's
	// job, mirroring _WINDOWS_JSON / win_refresh) ──
	queryWindows(): Promise<WmWindow[]>;
	/**
	 * A narrow, unfiltered re-query of ONE space (C3): a freshness/perf primitive
	 * cheaper than a full `queryWindows`; a separate query today so T3's
	 * re-home finds residual windows the claim filter hides.
	 */
	queryWindowsOnSpace(id: SpaceId): Promise<WmWindow[]>;
	querySpaces(): Promise<WmSpace[]>;
	queryDisplays(): Promise<WmDisplay[]>;
	/** ≙ `query --spaces --space`. */
	queryFocusedSpace(): Promise<WmSpace | null>;
	/** C3: replaces the old `null = focused` arg convention. */
	queryFocusedWindow(): Promise<WmWindow | null>;

	// ── Space lifecycle (SpaceId-typed, D1) ──
	/**
	 * yabai `--create` appends an unlabelled space and returns nothing; on Tahoe
	 * it "always creates on the laptop display". YabaiDriver
	 * resolves the fresh index internally (set-difference idiom), mints the
	 * SpaceId, and returns the handle.
	 */
	createSpace(displayIdx: number): Promise<SpaceId | null>;
	destroySpace(id: SpaceId): Promise<boolean>;
	labelSpace(id: SpaceId, label: string): Promise<void>;
	setSpaceLayout(id: SpaceId, layout: "bsp" | "stack" | "float"): Promise<void>;
	/** The ONE index-typed space method (D1) — ordering is genuinely positional. ≙ `space --move`. */
	moveSpaceToIndex(id: SpaceId, toIdx: number): Promise<void>;
	balanceSpace(id?: SpaceId): Promise<void>;

	// ── Layout realization (D2) ──
	/**
	 * Realize the engine's declarative target end to end with the driver's own
	 * imperative recipe + settle cadence (yabai): build the
	 * column tree (3col/2col) or set the whole space to `stack` from the space's
	 * windows AS-IS. The full resolved window set is in `target`.
	 *
	 * The driver does NOT clear the space first: the ENGINE owns evacuation.
	 * `deskPlan` chooses ONE stable park up front and emits the
	 * `moveWindow` ops that clear every rebuild space of ALL its tiled windows —
	 * targets included — before any realize. Targets are evacuated too because
	 * this driver's recipe arms an insert then re-adds each target with a
	 * cross-space move, and yabai only consumes an armed insert on a REAL move; a
	 * target left on-space would no-op the move and strand the insert unstacked.
	 * A single stable park cannot ping-pong, whereas a per-display park chosen
	 * inside the driver dumped the last display's windows back onto an
	 * already-built earlier display. A driver on a backend without the yabai-Tahoe
	 * build-from-empty requirement can ignore the pre-cleared state entirely.
	 */
	realizeSpaceLayout(id: SpaceId, target: SpaceLayoutTarget): Promise<void>;

	// ── Window placement (always-explicit window ids, C3) ──
	moveWindowToSpace(winId: number, id: SpaceId): Promise<void>;
	moveWindowToDisplay(winId: number, sel: DisplaySel): Promise<boolean>;
	/** ≙ `--ratio abs:`. */
	setSplitRatio(winId: number, absRatio: number): Promise<void>;
	/** ≙ `--toggle split`. */
	toggleSplit(winId: number): Promise<void>;
	toggleFloat(winId: number): Promise<void>;
	/** Keybind surface (`tess insert`) — NOT engine vocabulary under D2. */
	armInsert(
		winId: number,
		dir: "east" | "west" | "north" | "south" | "stack",
	): Promise<void>;
	/** Hyprland has no insert-arm; the yabai impl = armInsert+move. */
	stackOnto(targetWinId: number, winId: number): Promise<void>;
	swapWindows(sel: DirSel): Promise<boolean>;
	warpWindow(sel: DirSel): Promise<boolean>;
	/** ≙ skhdrc arrows. */
	resizeWindow(
		edge: "left" | "right" | "top" | "bottom",
		dx: number,
		dy: number,
	): Promise<boolean>;

	// ── Focus ──
	focusWindow(winId: number): Promise<boolean>;
	focusWindowDir(sel: DirSel | StackSel): Promise<boolean>;
	focusDisplay(sel: DisplaySel): Promise<boolean>;

	// ── Optional capabilities (undefined = unsupported on this backend) ──
	/** Arrival routing — yabai-specific shape. */
	rules?: WmRuleOps;
	/** Signal subscription — used by `tess init` wiring. */
	events?: WmEventSource;
	/** Base settle unit in ms; `{op:"settle",units}` sleeps `units × settleMs` (yabai: 150). No absolute ms in plan data (D2). */
	settleMs: number;
}
