# Design — Grid layouts: N tracks, rows, per-track weights

*Design record for RIG-3987. Builds on RIG-3984 (decided: each display carries*
*default splits, a layout may override them; resolution layout → display →*
*profile).*

## Problem / Intent

A desk layout today is one of three fixed shapes, `DeskLayout.kind:
"3col" | "2col" | "stack"` (`src/config/types.ts`), and its split is a
shape-specific pair of keys, `SplitRatios { col3?: { col3Root, col3Inner };
col2?: number }`. Matt: "we could want more than just 2col/3col splits in
future. need to support different col layouts, ex 4col, and also rows if a
user wants rows." The next profile already needs an AW 1/3-1/3-1/3 layout and
G9-solo / AW-solo topologies. Generalize the layout to N tracks along either
axis with per-track weights, keep the RIG-3984 resolution order, and cut the
fixed kinds over cleanly (one consumer: Matt's profile, same release train).

## Approach

**One flat list of tracks along one axis; each track is a stack; weights are
relative sizes.** This is today's `columns: name[][]` (anchor first, the rest
stack) with two generalizations: the axis is a choice (`columns` or `rows`),
and the split is a weight per track instead of a per-shape ratio pair. No
nesting. The `"3col"`/`"2col"` strings go away — the track count is the N.
`"stack"` stays a distinct kind because it is a *space* layout (`--layout
stack` on the whole space absorbs parked refugees), not a one-track grid.

### Config shape

```ts
// src/config/types.ts
/** Which way the tracks run: columns side by side, rows top to bottom. */
export type TrackKind = "columns" | "rows";
/** Relative track sizes, one per track: `[3, 4, 3]` is 30/40/30. */
export type Weights = ReadonlyArray<number>;
/** Default weights keyed by track count: `{ 3: [3, 4, 3] }` applies only to 3-track layouts. */
export interface WeightDefaults {
	columns?: Readonly<Record<number, Weights>>;
	rows?: Readonly<Record<number, Weights>>;
}

export interface DeskLayout {
	display: DisplayName;
	label: string;
	/** `columns` / `rows` split the space into tracks; `stack` piles the whole space. */
	kind: TrackKind | "stack";
	/** One entry per track: `track[0]` is the anchor, the rest stack behind it. A stack has exactly one track. */
	tracks: ReadonlyArray<ReadonlyArray<WindowName>>;
	/** Overrides the display/profile default for this track count. */
	weights?: Weights;
}

export interface Profile {
	displays: Record<DisplayName, { width: number; weights?: WeightDefaults }>;
	/** Profile-wide defaults; the last fallback before equal weights. */
	weights?: WeightDefaults;
	// windows, desk, topologies, deskSlots, laptopPinned, laptopStackApps unchanged.
}
```

Today's shapes map 1:1:

| today | new |
| --- | --- |
| `kind: "3col", columns: [a, b, c]` | `kind: "columns", tracks: [a, b, c]` |
| `kind: "2col", columns: [a, b]` | `kind: "columns", tracks: [a, b]` |
| `kind: "stack", columns: [[…]]` | `kind: "stack", tracks: [[…]]` |
| `ratios: { col3Root: 0.3, col3Inner: 0.5714 }` | `weights: { columns: { 3: [3, 4, 3] } }` |
| `ratios: { col2: 0.6 }` | `weights: { columns: { 2: [3, 2] } }` |
| layout `ratios: { col3: {…} }` | layout `weights: [3, 4, 3]` |

The field stays `tracks` on a stack too, so every consumer that flattens
membership (`deskPlan`'s claim loop, `apply`'s spawn list in `commands.ts`)
is one loop with no kind branch.

### Weights and the resolution order

A weight vector is relative: `[3, 4, 3]`, `[30, 40, 30]` and `[0.3, 0.4, 0.3]`
are the same layout. Resolution keeps RIG-3984's order, now keyed by axis and
track count:

1. the layout's own `weights`;
2. `profile.displays[d].weights[kind][count]`;
3. `profile.weights[kind][count]`;
4. equal weights (`[1, 1, …]`).

A display default only applies to a layout with the *same* kind and track
count. A `columns: { 3: … }` default says nothing about a 2-column or a
3-row layout; those fall through to the next level. So a display carries one
default per (axis, count) it cares about, and a new 4-column layout on that
display is equal-width until someone adds a `4:` entry — never a silent
reuse of the 3-column numbers.

`weightsFor` resolves against the *configured* track count. When the planner
drops an empty track (a window set that did not claim anything —
`deskPlan` already filters `col.length > 0`), it drops that track's weight
too, so the target's weights always align with its tracks. The general rule:
survivors keep their configured weights and the driver renormalizes over the
survivors only. `[3, 4, 3]` with the middle track gone becomes `[3, 3]` →
50/50; with the *first* track gone it becomes `[4, 3]` → 57/43. The second
case is a behavior change: today's 3col recipe gives 50/50 whenever only two
anchors exist (the second `--ratio` on the shared root overwrites the first).
The new behavior is the intended one — surviving tracks keep their relative
sizes — and is not claimed to match today.

### The driver target

```ts
// src/driver/types.ts — amends the D2 layout IR
export interface SpaceLayoutTarget {
	kind: "columns" | "rows" | "stack";
	/** Resolved window ids per track; `track[0]` is the anchor, the rest stack. */
	tracks: ReadonlyArray<ReadonlyArray<number>>;
	/** One weight per track, aligned with `tracks`; absent for `stack`. Relative — the driver normalizes. */
	weights?: ReadonlyArray<number>;
}
```

Weights, not yabai ratios, cross the engine→driver seam: weights are
backend-neutral (a Hyprland driver would set `splitratio` per node from the
same numbers); the chain of bsp ratios below is yabai's realization.

### The yabai realization: N tracks as a chain of bsp splits

yabai is a binary tree, so N tracks are N−1 nested splits. The recipe keeps
today's shape (`realizeSpaceLayout` in `src/driver/yabai.ts`) and generalizes
the ratio step:

1. `space --layout bsp`.
2. Bring the anchors on in track order. Move `a0` onto the space; then for
   each next anchor `a[i]`: arm `--insert east` (columns) or `--insert south`
   (rows) on `a[i-1]`, move `a[i]` onto the space. Each insert splits the
   previous anchor's leaf with the new window as the *second* (right/bottom)
   child, so the tree is a right-nested chain: `a0 | (a1 | (a2 | …))`.
3. Set the ratios. `--ratio abs:r` on a window sets its *parent* node's
   ratio, and that ratio is the first (left/top) child's share. In the chain,
   `a[i]`'s parent is the node whose first child is `a[i]` and whose second
   child is the rest of the tracks, so for `i` in `0 … n−2`:

   `r[i] = w[i] / (w[i] + w[i+1] + … + w[n−1])`

   applied on `a[i]`. The last anchor shares its parent with `a[n−2]` and
   gets no command. For `[3, 4, 3]` this is `r = [0.3, 0.5714]` — today's
   `COL3_ROOT_RATIO` / `COL3_INNER_RATIO` within float rounding; for
   `[1, 1, 1, 1]` it is `[0.25, 0.333, 0.5]`; for a 2-track `[w0, w1]` it is
   today's `col2`. The driver formats each value with `toFixed(4)`, so the
   argv carries `abs:0.5714`, never a long float.
4. Stack each track's extras onto its anchor: arm `--insert stack` on the
   anchor, move the extra on. Unchanged; a stack adds no tree node, so it
   cannot disturb the ratios set in step 3.

Rows are the same recipe with `south` for `east`; the ratio formula does not
change because yabai's first child is the top child of a horizontal split.

**Which window anchors a track:** `tracks[i][0]` after claiming — the same
rule as today's `col[0]`. **Stacking within a track:** today a column's
extras join a yabai *stack* (one visible at a time) on the anchor's leaf, not
rows; the new model keeps that. Rows are a whole-space axis choice, not a
within-column concept. A layout that needs rows inside a column is the tree
model in Alternatives, not this record.

The recipe is extracted as data — `bspSteps(spaceIdx, target)` returns the
ordered `yabai -m` argv list with its settle units — so the whole sequence is
golden-tested with no live yabai, matching the file's construction-vs-
execution split (`yabaiArgs.*` builders + one thin runner).

### Validation

`validateProfile` (`src/config/validate.ts`) replaces `SPLIT_KEY` and the
`ratioViolations` 0–1 check with:

- every layout has ≥1 track and every track has ≥1 window name;
- `kind: "stack"` has exactly one track and no `weights`;
- a one-track `columns`/`rows` layout has no `weights` (dead config — there
  is no split to size);
- a layout's `weights`, when set, has `length === tracks.length`;
- every weight (layout, display default, profile default) is finite and `> 0`;
- every default entry's key equals its vector's length, and the key is ≥ 2;
- for every weight vector, every chain ratio of every non-empty subsequence
  of length ≥ 2 is within `[0.1, 0.9]` — yabai clamps `--ratio abs:` to that
  range (`window_manager_adjust_window_ratio` clamps with
  `clampf_range(ratio, 0.1f, 0.9f)`), so a vector outside it would be
  silently changed. Checking the full vector alone is not enough: the planner
  drops empty tracks at runtime, and `[9, 0.5, 0.5]` passes whole
  (`r0 = 0.9`) but with the middle track dropped gives `r0 = 0.947`. `n` is
  a handful of tracks, so the subsequence walk is cheap. This follows the
  existing precedent of the 0–1 check, which already encodes a yabai bound
  in the config layer.

Messages keep the existing `${where} ${label}: …` form. Track count vs.
display default mismatch is *not* an error — the default just does not apply.

As a backstop, the driver clamps any runtime chain ratio that would leave
`[0.1, 0.9]` before formatting it and writes one stderr line naming the
space and the clamped value. Validation should make that line unreachable.

### Snap

`tess snap 3col|50-50|columns` is the skhd keybind surface and keeps its mode
names. `snapPlan` resolves weights at the mode's *nominal* count — 3 for
`3col`, 2 for `50-50` — with `weightsFor(profile, "columns", count, display)`,
then drops empty tracks and their weights the same way `deskPlan` does. So
`50-50` honors a display's 2-column default exactly as it does today, and
`snap.test`'s "3col with 2 leaves" expects the 3-count weights with the
missing track's weight dropped (`[3, 4, 3]` minus the empty track), not the
2-count default.

### Migration

Clean cutover, no `3col`/`2col` sugar and no `SplitRatios` alias. Matt's
profile (the private `tessera-profile` package that `satisfies Profile`) pins
tessera by commit; its PR bumps the pin and rewrites the profile in one change:

```ts
desk: [
	{ display: "g9", label: "main", kind: "columns",
	  tracks: [["arc", "obsidian"], ["ghostty-wave", "arc"], ["ghostty-mbp", "vscode"]] },
	{ display: "aw", label: "plan", kind: "columns", tracks: [["arc"], ["arc", "linear"]] },
	{ display: "laptop", label: "laptop", kind: "stack",
	  tracks: [["arc", "akiflow", "spotify", "discord", "qalculate"]] },
],
// was ratios: { col3Root: 0.3, col3Inner: 0.5714 }
weights: { columns: { 3: [3, 4, 3] } },
```

The AW thirds layout is `kind: "columns"` with three tracks and no `weights`
(equal), or `weights: [1, 1, 1]` to say so. G9-solo / AW-solo are
`topologies` entries whose AW layout puts the AW windows in the side tracks —
membership only, nothing new in the model. The binary and
`~/.config/tessera/profile.ts` deploy from the same host config, so they
switch together.

## Alternatives considered

- **Recursive split tree** (`{ split: "columns", weights, children: [leaf | node] }`).
  Expresses everything a bsp can (rows inside a column, a 2×2 grid). Rejected
  for now: Matt's stated needs (4col, rows, per-shape ratios) are all one-axis;
  a tree makes the common case verbose (`tracks: [[…]]` becomes a nested
  object per column), every consumer (claim loop, spawn list, snap, focus
  slots) must walk a tree, and the driver recipe needs a recursive insert
  order with per-subtree evacuation. The flat shape is the tree's depth-1
  case; if a nested layout is ever needed, a `children` variant of a track can
  be added without changing `kind`/`tracks`/`weights`. The tree is
  recorded here so the choice is deliberate.
- **Keep `"3col"`/`"2col"` as sugar** (`kind: "3col"` ≡ `columns` + 3 tracks).
  Rejected: the sugar would need its own validation (track count must match
  the name) and a normalize step before every consumer, for two strings the
  one consumer can search-and-replace. The repo prefers clean cutover.
- **Absolute yabai ratios in config** (`splits: [0.3, 0.5714]`, one per inner
  node). Rejected: it leaks the bsp chain into config (a user must compute the
  remainder fraction by hand, the `0.5714` today), and it is meaningless to a
  non-bsp backend. Weights are what a user thinks in.
- **Weights keyed only by count, not by axis** (`weights: { 3: [3,4,3] }`).
  Rejected: a 3-column and a 3-row default on the same display are different
  numbers (an ultrawide's column shares are not its row shares), and the
  axis key costs one word.

## Plan

Sources under `src/`, tests colocated `*.test.ts`. T1–T3 are **one PR with
three commits**, not three PRs: each type change breaks the next module's
typecheck, so no intermediate commit is green on its own and none could merge
alone. T4a is a follow-up PR in tessera; T4b is the orion profile PR.

### Global Constraints

- **Runtime/tooling:** Bun; TypeScript strict; Biome; `bun:test`. Zero npm
  dependencies. Single `bun build --compile` binary.
- **Layering is law** (`architecture.md`): `config/` and `engine/` import
  nothing from `driver/`; `engine/` is pure. The bsp chain is driver-only;
  the engine emits weights.
- **RIG-3984 order is preserved:** layout → display → profile, then equal
  weights. Snap uses the focused display's defaults.
- **Clean cutover:** `"3col"`, `"2col"`, `SplitRatios`, `Profile.ratios`,
  `DeskLayout.columns`, `SpaceLayoutTarget.ratios/split`, `COL3_*_RATIO`,
  `SPLIT_KEY` are deleted, not aliased. No compatibility reader.
- **Naming:** `tracks` (membership), `weights` (sizes), `kind: "columns" |
  "rows" | "stack"`. No "grid", "cells", or "ratios" in new code.
- **Weights are relative** everywhere; only the yabai driver turns them into
  absolute ratios. Validation enforces the 0.1–0.9 chain bound (yabai's clamp)
  on every non-empty subsequence in the config layer, as the current 0–1
  check does; the driver clamps and writes one stderr line as a backstop.
- **Existing tests that pin the old shape are rewritten to the new contract,
  not deleted** (`desk.test.ts`, `snap.test.ts`, `validate.test.ts`,
  `profile.test.ts`, `loader.test.ts`, `fake.test.ts`, `exec.test.ts`,
  `commands.test.ts`).
- **VCS:** jj + jj-vine; this record freezes on merge.

### T1 — Config types + validation + bundled profile

Replace the fixed kinds and ratio keys with tracks/weights in
`src/config/types.ts`, rewrite `src/config/validate.ts`, and port the bundled
`profile.ts` and `profile.fixture.ts`. Delete `SplitRatios`, `Profile.ratios`,
`SPLIT_KEY`, `ratioViolations`. Update the `kind: "3col" | "2col" | "stack"`
text in `docs/design/architecture.md`.

Interfaces:

```ts
// src/config/types.ts
export type TrackKind = "columns" | "rows";
export type Weights = ReadonlyArray<number>;
export interface WeightDefaults {
	columns?: Readonly<Record<number, Weights>>;
	rows?: Readonly<Record<number, Weights>>;
}
export interface DeskLayout {
	display: DisplayName;
	label: string;
	kind: TrackKind | "stack";
	tracks: ReadonlyArray<ReadonlyArray<WindowName>>;
	weights?: Weights;
}
// Profile: `displays: Record<DisplayName, { width: number; weights?: WeightDefaults }>`,
// `weights?: WeightDefaults`; `ratios` removed; other fields unchanged.

// src/config/validate.ts
export function validateProfile(profile: Profile): void; // throws on the first violation set, same as today
/** The bsp chain ratios for a weight vector: r[i] = w[i] / sum(w[i..]). Pure; shared with the driver's test. */
export function chainRatios(weights: Weights): number[];
```

Violations (each its own test): empty track; stack with ≠1 track or with
weights; one-track `columns`/`rows` with weights; weights length ≠ tracks
length; non-positive/non-finite weight; a default keyed `n` whose vector
length ≠ `n`; a chain ratio outside `[0.1, 0.9]` for the full vector and for
a subsequence (`[9, 0.5, 0.5]` fails on `[9, 0.5]`). Keep "the bundled
default profile is valid".

Test cycle: `bun test src/config/`.

### T2 — Engine: `weightsFor`, desk planner, snap planner

Replace `splitFor` in `src/engine/split.ts` with `weightsFor`; `deskPlan`
claims per track, drops empty tracks *and their weights*, and emits the new
target; `snapPlan` emits `columns` targets at the mode's nominal count and
drops empty tracks the same way. `SpaceLayoutTarget` in `src/driver/types.ts`
changes here (it is the engine→driver seam) and `FakeDriver.realizeSpaceLayout`
reads `tracks`. `apply`'s spawn list in `src/commands.ts`
(`layout.columns.flat()`) becomes `layout.tracks.flat()`. Update the
`SpaceLayoutTarget.ratios` text in `docs/design/architecture.md` and the doc
comments in `src/engine/plan.ts` and `src/driver/types.ts`.

Interfaces:

```ts
// src/engine/split.ts
/** Layout override → display default → profile default → equal weights, keyed by kind + count. */
export function weightsFor(
	profile: Profile,
	kind: TrackKind,
	count: number,
	display: DisplayName | undefined,
	override?: Weights,
): number[];
export function displayOfSpace(profile: Profile, world: WorldSnapshot, space: string): DisplayName | undefined; // unchanged

// src/driver/types.ts
export interface SpaceLayoutTarget {
	kind: "columns" | "rows" | "stack";
	tracks: ReadonlyArray<ReadonlyArray<number>>;
	weights?: ReadonlyArray<number>;
}

// src/engine/desk.ts — signature unchanged
export function deskPlan(profile: Profile, world: WorldSnapshot): PlanOp[];
// src/engine/snap.ts — signature unchanged
export function snapPlan(profile: Profile, world: WorldSnapshot, focusedSpace: SpaceId, mode: SnapMode): PlanOp[];
```

Tests: `weightsFor` walks all four levels and ignores a default whose count
or kind differs; `deskPlan` emits `weights` aligned with surviving tracks
(3 configured, middle empty → `[3, 3]`; first empty → `[4, 3]`); a `rows`
layout passes through with `kind: "rows"`; snap `50-50` picks the display's
`columns[2]` default; snap "3col with 2 leaves" gets the `columns[3]` default
with the missing track's weight dropped.

Test cycle: `bun test src/engine/ src/driver/fake.test.ts src/exec.test.ts src/commands.test.ts`.

### T3 — YabaiDriver: the bsp chain recipe

Generalize `realizeSpaceLayout` in `src/driver/yabai.ts`: insert direction
from `kind`, chain ratios from `weights`, extras stacked per track. Delete
`COL3_ROOT_RATIO`/`COL3_INNER_RATIO`. Extract the recipe as a pure step list
so the argv sequence is golden-tested. Each `--ratio abs:` value is clamped to
`[0.1, 0.9]` (one stderr line if it had to move) and formatted with
`toFixed(4)`.

Interfaces:

```ts
// src/driver/yabai.ts
/** One driver step: a yabai argv (no leading path) and the settle to wait after it, in ms. */
export interface BspStep { args: string[]; settleMs: number }
/** The ordered recipe for a columns/rows target on live space `spaceIdx`. Pure apart from `warn`. */
export function bspSteps(spaceIdx: number, target: SpaceLayoutTarget, warn?: (line: string) => void): BspStep[];
/** `--ratio abs:` argument: clamps to [0.1, 0.9], calling `warn` once if it moved, then `toFixed(4)`. */
export function ratioArg(r: number, warn?: (line: string) => void): string;
// realizeSpaceLayout(id, target) runs `bspSteps` through #run/#unfloatOne with warn = one stderr line; stack path unchanged.
```

Golden tests assert the formatted argv strings: 3 columns `[3,4,3]` →
`--insert east` on a0, a1; `--ratio abs:0.3000` on a0, `abs:0.5714` on a1;
4 columns equal → `abs:0.2500`, `abs:0.3333`, `abs:0.5000`; 2 rows `[2,1]` →
`--insert south`, `abs:0.6667`; 1 track → no insert, no ratio; extras →
`--insert stack` + move per extra; backstop `[9, 0.5]` → `abs:0.9000` and
exactly one `warn` call. Existing `yabaiArgs` tests untouched.

Test cycle: `bun test src/driver/yabai.test.ts`, then a live smoke on the
MBP: `tess apply` with the ported bundled profile on one display, confirm
`yabai -m query --windows` frames match the weights (`w` of each anchor /
display width within 1px of `w[i]/sum`).

### T4 — FakeDriver geometry + the profile cutover (orion)

Two halves, each its own PR after the T1–T3 PR merges: (a) a follow-up PR in
tessera; (b) the orion profile PR.

**(a) Fake geometry.** `FakeDriver` today "tracks no pixel geometry"
(`fake.ts` header), so the engine tests can only assert membership. Give it a
minimal per-space model so a `realizeSpaceLayout` test can assert *shape*:
after realize, each anchor's `frame` is the display frame cut along the axis
by the normalized weights, and each extra shares its anchor's frame. This is
the test seam for "did the right weights reach the driver" without live
yabai; it models only the frames the target implies, not yabai's tree.

Interfaces:

```ts
// src/driver/fake.ts — realizeSpaceLayout(id, target) additionally sets, for a columns/rows target:
//   anchor[i].frame = { x: d.x + d.w * off[i], y: d.y, w: d.w * share[i], h: d.h }   (columns)
//   anchor[i].frame = { x: d.x, y: d.y + d.h * off[i], w: d.w, h: d.h * share[i] }   (rows)
// where d = the space's display frame, share[i] = w[i] / sum(w), off[i] = sum(share[0..i)).
// Extras copy their anchor's frame. A stack target leaves frames as they were.
```

Tests (`fake.test.ts`, `commands.test.ts` apply): three columns `[3,4,3]` on
a 1000px display → anchor widths `300, 400, 300` and x `0, 300, 700`; two
rows `[1,1]` → heights `h/2`; extras share the anchor frame; `snap 50-50` on
a seeded display with `columns: { 2: [3, 2] }` → widths `600, 400`.

**(b) Profile cutover** (the private `tessera-profile` package, downstream):
bump the `tessera-wm` pin to the merged commit; rewrite `desk` to
`kind: "columns"`/`tracks` and `ratios` → `weights: { columns: { 3: [3, 4, 3] } }`
(the Migration block above); add the AW thirds layout and the G9-solo /
AW-solo topologies. Its typecheck (`satisfies Profile`) is the gate.

Test cycle: `bun test src/driver/fake.test.ts src/commands.test.ts`; then the
profile package's typecheck.

## Tasks

PR 1 (tessera, three commits — see Plan):

- [ ] T1 — `TrackKind`/`Weights`/`WeightDefaults`; `DeskLayout.tracks` +
  `weights`; `validateProfile` rewrite (subsequence chain check, one-track
  weights rejected) + `chainRatios`; bundled profile and fixture ported;
  old types deleted; `architecture.md` kind text
- [ ] T2 — `weightsFor`; `SpaceLayoutTarget { kind, tracks, weights }`;
  `deskPlan` drops empty tracks with their weights; `snapPlan` on `columns`
  at nominal count; `FakeDriver` reads `tracks`; `commands.ts` spawn list;
  doc comments + `architecture.md` target text; engine tests rewritten
- [ ] T3 — `bspSteps` + `ratioArg` + `realizeSpaceLayout` chain recipe
  (east/south insert, chain ratios, clamp backstop, `toFixed(4)`); argv golden
  tests; live smoke

PR 2 (tessera, follow-up):

- [ ] T4a — `FakeDriver` frame model for columns/rows; geometry tests

PR 3 (orion):

- [ ] T4b — orion profile cutover + AW thirds + solo topologies; pin bump

## Open Questions

Designed against the recommendation in each; only 1–3 are load-bearing.

1. **Weight-default key: by axis and count, or by count only?** *(load-bearing —
   it is the `Profile`/display shape Matt edits).*
   (a) `weights: { columns: { 3: [3,4,3] }, rows: { 2: [1,1] } }` — one
   default per (axis, count). (b) `weights: { 3: [3,4,3] }` — a 3-track
   default applies to both 3 columns and 3 rows. **Recommend (a):** the two
   axes want different numbers on a wide display, and the extra key is one
   word. The record is written against (a).
2. **`stack` as a third `kind`, or a one-track `columns` layout?**
   *(load-bearing — it sets the `kind` union and the driver's branch).*
   (a) keep `kind: "stack"` — the whole space becomes `--layout stack`, which
   is what absorbs parked refugees on the laptop home (`deskPlan`'s park).
   (b) drop it; a single-track `columns` layout with extras stacked on the
   anchor is visually the same for its own windows, but leaves the space in
   `bsp`, so refugees tile beside the stack instead of joining it.
   **Recommend (a):** the refugee behavior is load-bearing for the laptop
   park and is not expressible as a grid.
3. **`rows` = N rows of stacks (this record) vs. a 2-D grid (rows × columns,
   the tree)** *(load-bearing — it sets what `kind: "rows"` means and whether
   `tracks` stays flat).* (a) Flat: `rows` is the same one-axis model as
   `columns` turned 90°; each track is a stack; no row contains columns.
   (b) Grid: `rows` × `columns` nest, the tree model in Alternatives.
   **Recommend (a):** every stated need is one-axis, every consumer stays a
   flat loop, and the driver recipe stays a chain. If a rows-inside-a-column
   layout is ever wanted, add a `children` variant to a track entry;
   `kind`/`tracks`/`weights` stay. That extension is deferred, not designed
   here.
4. **Clamp bound as a validation error vs. a warning** *(non-load-bearing)*.
   A weight vector with a chain ratio outside `[0.1, 0.9]` in any
   subsequence (e.g. `[1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]`, first ratio 0.09)
   is rejected at load; the driver clamps and logs only as a backstop.
   Rejecting matches today's 0–1 error; revisit only if a real layout hits it.
5. **Assumptions about yabai not confirmed live** *(non-load-bearing; T3's
   live smoke verifies them)*. Read from yabai's source, not exercised:
   `--insert east|south` makes the moved-in window the *second* child
   (`window_manager_set_window_insertion` sets `CHILD_SECOND` for east/south)
   so the chain nests to the right/bottom; `--ratio abs:` sets the selected
   window's *parent* ratio and clamps to `[0.1, 0.9]`
   (`window_manager_adjust_window_ratio`); the ratio is the first child's
   share (`area_make_pair`: `left_width = (w - gap) * ratio`). Today's 3col
   recipe already relies on the first two, so the risk is confined to `rows`
   (`south`) and to N ≥ 4. If the smoke shows a different nesting for
   `south`, T3 adjusts the direction/child mapping in `bspSteps`; the config
   shape does not change.
