# Design — MBP laptop flex-space model

*Design record for the laptop flex-space model. Written against the bash*
*implementation that this repository ports to TypeScript; kept as the rationale*
*behind the laptop converger's behavior.*

## Problem / Intent

The laptop-only layout is a rigid, hand-edited roster: `profile.sh`
declares `LAPTOP_SPACES=(arc ghostty-wave arc ghostty-mbp linear obsidian arc akiflow)`,
and `laptop-layout.sh` gives each entry its own labelled `bsp` space on unplug.
Matt wants the laptop treated as a **flex screen**: the space set should derive
from what is actually running (the app set grows over time — Typora, Notion,
Claude, Orbstack, …; regulars: always-open Arc, Spotify, Qalculate) and flex
up/down without hand-editing a list — while keeping the deterministic, nameable
per-space labels that sketchybar renders (macOS native-fullscreen space names
are unreliable to script; the labelled `bsp` grid stays).

The load-bearing property is **polarity**: today a genuinely new app costs a
`profile.sh` edit to get a space; the flex model inverts that so a new app gets
a space for free, and the only hand-edit left is the *exception* — demoting an
app into the shared stack pile. Slack, though named among the growing app set,
is comms-class and is seeded into that pile by decision D4 below (Matt's call);
the "grows over time" apps that get their own space for free are the non-comms
ones (Typora, Notion, Claude, Orbstack, …).

## Approach

**Pinned-core + flex-tail, with the stack pile kept as a classification.** A
fixed ordered pinned prefix in `profile.sh` keeps stable labels and
muscle-memory order for the regulars; everything else running on the laptop
that is not classified into the catch-all stack pile flexes in after, in a
**stable first-seen order** persisted across sessions. The *pinned core*
answers ordering/label stability for today's roster, the *classification*
answers "which apps deserve a space at all", and the *count* of tail spaces
follows what's running — no fixed roster to grow by hand.

Per Matt (D3), the pinned core stays the full current 8-slot order, so the
observable change for today's app set is small; the flex tail is what serves
tomorrow's apps automatically.

### Mechanism (what the rewritten `laptop-layout.sh` computes)

1. **Pinned core** — `LAPTOP_PINNED`: an ordered list of window names, resolved exactly as today
 via the claim resolver. `lib.sh` `win_id_claim <name> [preferred-display-idx]`
 "sets CLAIM_RESULT" to the first matching non-minimized, non-floating,
 unclaimed window (`lib.sh`:
 `select(."is-minimized" == false and."is-floating" == false)`), so repeated
 names (`arc arc`) still claim distinct windows. **Change from today:** a
 pinned entry with no live window is *skipped* (no empty space created);
 today `laptop-layout.sh` creates the space unconditionally and only
 moves the window "if open"
 (`[[ -n $id ]] && "$YABAI" -m window "$id" --space "$sp"`).
 Flex means absent apps cost nothing (decision D7).
2. **Flex tail** — enumerate live windows (same minimized/floating filter),
 group by app, drop (a) apps whose windows were all claimed by the pinned
 core, (b) apps matching the new `LAPTOP_STACK_APPS` classification (the
 comms/utility pile that stays on the home stack space), and (c) floating
 apps already `manage=off` in `rules.sh` (Finder, dialogs). Each surviving
 app gets ONE flex space holding all of its remaining windows (bsp co-tiles
 them). **Tail order: stable-append** (decision D2) — a persisted first-seen
 ordinal file drives spatial order, so an app keeps the slot it first got and
 a newly-launched app appends at the end; existing positions never shift.
 See "Stable-append ordering" below.
3. **Labels** — pinned spaces keep today's scheme from `laptop-layout.sh`
 (occurrence-suffixed: "labels lap-arc, lap-arc-2, lap-arc-3"); flex spaces
 are `lap-<slug>` where slug = the `WIN` logical name when the app matches a
 `WIN` entry, else the lowercased app name with non-alphanumerics collapsed
 to `-`. **Collision rule** (H4, decision D6): the `WIN` match iterates keys
 in an explicit `sort` order (bash `${!WIN[@]}` order is unspecified), so the
 slug is deterministic; and any *extra* unclaimed window of a **pinned** app
 (e.g. a 4th Arc window beyond the pinned `arc×3`) co-tiles onto that app's
 **last pinned occurrence** rather than creating a colliding `lap-<slug>` —
 flex spaces are only ever minted for apps with no pinned space. sketchybar
 needs no change: `sketchybar/plugins/spaces.sh` already renders any label
 via `(.label | sub("^lap-"; ""))`.
4. **Reconcile, not just create** — today the script is create/reuse-only;
 flex requires convergence: after computing the target label set, destroy any
 `lap-*` space whose label is no longer targeted. **Before destroying**, move
 any windows still on that space to the home space (H5) — the enumerator
 skips minimized/floating windows but they still *belong* to
 the space, and the raw `teardown_laptop_grid` primitive (`lib.sh`,
 destroys by `select(.label | startswith("lap-"))`) lets yabai "relocate any
 windows on it to an adjacent space" — which would scatter
 a minimized window onto an arbitrary neighbouring app's space. Re-homing
 first keeps residuals on the stack pile where they belong. Re-query after
 each destroy — "space indexes renumber". The `lap-` prefix
 already guarantees the home space is never touched. The home space stays
 `stack` (`laptop-layout.sh`:
 `"$YABAI" -m space "$home_space" --layout stack`).
5. **Rebuild trigger** — keep the unplug path (`display-event.sh` runs
 `laptop-layout.sh` when `ndisp <= 1`), and ADD live reactivity (decision
 D5). Subscribe **both** app- and window-level yabai signals:
 `application_launched` / `application_terminated` **and** `window_created` /
 `window_destroyed`, wired in `yabairc` to a new debounced
 `laptop-flex-event.sh`. Window-level events are required, not optional: an
 app can keep running with zero windows (Cmd+W on its last window) or open
 its first window later (Cmd+N) with no `application_*` event — app-only
 signals would leave a stale space or a missing one until some unrelated
 event fired (H3). The window events are *not* the Arc title-churn hazard —
 that is `window_title_changed`, which we do NOT subscribe. The handler
 reuses the `display-event.sh` stamp+waiter+stale-PID-reclaim pattern with a shorter quiet window, exits unless
 `ndisp == 1`, and coordinates with the guard (below) — finally giving the
 `lib.sh` `SIGNAL_GUARD` scaffolding its first reader ("nothing reads
 it yet", `lib.sh`). `laptop-layout.sh` already takes the guard:
 `laptop-layout.sh` `suppress_signals` /
 `trap 'release_signals; rmdir "$LOCK" …' EXIT`.

### Stable-append ordering (T2/T3 detail)

A persisted ordinal file — `${XDG_CACHE_HOME:-$HOME/.cache}/yabai/laptop-flex-order`,
one slug per line — is the canonical tail order (a cache path, not `/tmp`, so
the order survives reboot → muscle memory holds across power cycles). Each
rebuild:

1. Read the file into an ordered slug list.
2. Compute the current flex-app slug set (running, unpinned, not piled).
3. Render flex spaces in file order, filtered to currently-running slugs; a
 slug in the file but not currently running is simply skipped (its space was
 reconciled away) — but its line **stays**, so relaunching that app returns
 it to its original slot.
4. Any current flex slug absent from the file is appended to the file (end of
 order) and gets the last slot.

This is deterministic within and across sessions, and "append at end" is what
`yabai -m space --create` does natively (appends to the display's space list),
so no index-reshuffling reorder step is needed — the property `alphabetical`
could not deliver without an explicit `--move` pass that yanks space indexes
mid-session (H1).

### Convergence robustness (T5 detail)

Two holes the naive handler would have (H2), both fixed at the design level:

- **Never drop an event on a held guard.** When the waiter finds
 `signals_suppressed` true (a manual `apply-workspace` or an unplug
 `laptop-layout` is mid-run), it must **re-stamp and re-wait**, not exit — an
 app launched during that window would otherwise get no space until the next
 unrelated event, possibly hours later.
- **A contended rebuild must be observable.** `laptop-layout.sh` today exits
 `0` when its mkdir-lock is already held, which
 would let the flex waiter record convergence against a *newer* window
 snapshot the winning run never saw. `laptop-layout.sh` must exit **nonzero**
 on lock contention, and both callers (the flex waiter and the
 `display-event.sh` cascade) re-loop on nonzero — mirroring the existing
 `display-event.sh` `acted_on` re-check loop so a
 swallowed rebuild is retried, not silently lost.

### rules.sh interplay

`rules.sh` stays the *arrival* router; the flex grid is the *layout*. Two
touchpoints:

- The catch-all (`rules.sh`:
 `add_rule default-laptop-stack app="^.*$" space="$laptop_stack_space"`) is
 what makes new windows land on the home stack first; the flex signal handler
 then promotes qualifying apps to their own space. `LAPTOP_STACK_APPS` in
 `profile.sh` becomes the shared truth for "stays in the pile" so `rules.sh`
 comments and the flex deriver can't drift.
- The `laptop_bsp_space` pins (`rules.sh`: Messages / Qalculate / System
 Settings → `spaces[1]`) collide with the flex grid: in laptop mode
 `spaces[1]` IS the first `lap-*` space, so today the trio is already dumped
 onto the pinned Arc space — a live bug. Design (decision D8): drop the
 `laptop_bsp_space` pin block; **Messages and System Settings** fold into
 `LAPTOP_STACK_APPS` (utility windows, join the pile), while **Qalculate**
 is deliberately left OUT of the pile so it flexes to its own `lap-qalculate`
 space (a pop-open calculator buried in an 8-window stack is a real
 regression). Dropping the pin fixes the live bug either way.

### Alternatives considered

- **Fully running-window-driven** (one space per window, no roster at all):
 maximally flexible, but loses ordering/label stability entirely — Arc's
 windows have drifting titles (`profile.sh`: "Arc's tab titles drift …
 All Arc windows are treated as interchangeable"), so per-window labels can't
 be meaningful, and per-window granularity multiplies the flex count. Rejected
 as the whole model; its flex-*count* idea is adopted for the tail (at app
 granularity, decision D1).
- **Pure category/rule-driven** (profile classifies own-space vs stack, count
 fully follows what's running, no pinned order): simplest data model, but the
 regulars (always-open Arc first, the two Ghostty windows) would lose their
 fixed positions. The pinned prefix costs one small ordered list and buys
 stable positions 1..k. Rejected alone; its classification half is adopted.
- **Alphabetical tail order:** rejected in favour of stable-append (decision
 D2). Alphabetical re-sorts the whole tail on every rebuild, which
 `yabai -m space --create` (append-only) cannot produce without an explicit
 `--move` reorder pass that reshuffles space indexes mid-session — yanking any
 index-based navigation under the user (H1). Stable-append is deterministic,
 needs no reorder machinery, and never moves an existing app's slot.
- **Keep `LAPTOP_SPACES`, auto-append at layout time**: smallest diff, but the
 roster remains the edit surface and still fuses "which" with "order/count" —
 fails the ask's core ("no hand-editing the list every time the app set
 changes"). Rejected.

## Plan

### T1 — profile.sh: new laptop data shape

Replace `LAPTOP_SPACES` with two declarations, keeping the
file pure data ("Pure data, no logic. Sourced by lib.sh", `profile.sh`):

- `LAPTOP_PINNED=(arc ghostty-wave arc ghostty-mbp linear obsidian arc akiflow)`
 — the full current 8-slot order (decision D3), preserved verbatim so today's
 muscle memory is unchanged; names are `WIN` keys;
 repeats claim distinct windows as today. New apps do NOT get added here —
 they flex.
- `declare -A LAPTOP_STACK_APPS=( ["Messages"]=1 ["System Settings"]=1 ["Slack"]=1 ["Discord"]=1 ["1Password"]=1 ["Activity Monitor"]=1 )`
 — app-name (yabai `.app`) → stays-on-home-stack. Apps here never get a flex
 space. Seed = the comms/system pile Matt named (decision D4) plus the former
 `laptop_bsp_space` demotions (Messages, System Settings). **Qalculate is
 intentionally absent** — it flexes to its own space (decision D8). Spotify
 and other non-comms regulars are absent too, so they flex; this set is the
 single hand-edit surface for demoting an app into the pile.

Update the header comment that still names
`LAPTOP_SPACES`.

Interfaces:

- `profile.sh` — remove `LAPTOP_SPACES` (the array + its comment block); add
  `LAPTOP_PINNED` (indexed array of
 WIN names) and `LAPTOP_STACK_APPS` (assoc array, key = literal yabai app
 name, value ignored).
- Consumers: `laptop-layout.sh` (T3), `rules.sh` (T4). No lib.sh signature
 change to `win_id_claim`.

### T2 — lib.sh: flex-tail enumerator + stable-append order

Add pure helpers beside the claim resolver:

- `laptop_flex_apps` → prints, one per line, `app<TAB>slug` for every app with
 at least one non-minimized, non-floating window whose ids are not all in
 `_CLAIMED` (same filter as `win_id_claim`, `lib.sh`) and whose app is not
 a key of `LAPTOP_STACK_APPS`. Slug = the `WIN` key whose app-regex matches
 the app, iterating keys in **explicit `sort` order**
 (`printf '%s\n' "${!WIN[@]}" | sort` — bash assoc-array key order is
 unspecified, H4), else `tr '[:upper:]' '[:lower:]'` of the app name with
 non-alphanumerics squeezed to `-`. Runs AFTER the pinned loop so `_CLAIMED` already holds the core's windows. It composes
 `laptop_flex_order` internally — computing the unordered app→slug set, then
 emitting its `app<TAB>slug` lines in that function's returned order — so it
 emits in **stable-append order** (below), not alphabetical, and its sole
 caller (T3) consumes only `laptop_flex_apps`.
- `laptop_flex_order <slug...>` (internal ordering helper for
 `laptop_flex_apps`) → reconciles the persisted ordinal file
 `${XDG_CACHE_HOME:-$HOME/.cache}/yabai/laptop-flex-order` against the given
 current slugs: appends any new slug to the file, then echoes the file's slugs
 filtered to the current set (file order preserved; missing slugs' lines
 retained for slot stability across relaunch). Creates the cache dir if
 absent.
- `win_ids_unclaimed_for_app <app>` → sets `CLAIM_RESULT` (space-separated id
 list, matching the no-subshell convention, `lib.sh`) to the unclaimed
 matching window ids, and appends them to `_CLAIMED`.
- `win_ids_on_space <space-idx>` → echoes all window ids on the given space
 with **no** minimized/floating filter (raw `yabai -m query --windows --space
 <idx> | jq '.[].id'`), so T3's H5 re-home can find the residual
 minimized/floating windows the claim filter hides. Read-only;
 touches no `_CLAIMED`.

Interfaces:

- `lib.sh` — new functions `laptop_flex_apps`
 (stdout: `app\tslug` lines, stable-append order; composes `laptop_flex_order`
 internally), `laptop_flex_order` (internal ordering helper: stdout ordered
 slug lines, reads/writes the cache file), `win_ids_unclaimed_for_app <app>`
 (sets `CLAIM_RESULT`, appends `_CLAIMED`), and `win_ids_on_space <space-idx>`
 (stdout: unfiltered window ids on a space, for the H5 re-home). Reads `WIN`,
 `LAPTOP_STACK_APPS`, `_CLAIMED`. No signature change to
 `win_id_claim`/`claim_reset`.

### T3 — laptop-layout.sh: rewrite as converger

Keep the shell: lock, `suppress_signals`/trap,
sole-display guard (`[[ ${ndisp:-0} -ne 1 ]] && exit 0`), home-space
resolution, `space_for_label`, the closing home-stack
re-assert and `"$SKETCHYBAR" --trigger yabai_spaces_changed`.
Replace the body loop:

1. `claim_reset`; iterate `LAPTOP_PINNED` with the existing occurrence-suffix
  labelling — but SKIP entries whose claim comes back empty (no
 space created for an absent app, decision D7). Track the pinned label set +
 per-app last-pinned-occurrence for the collision rule (H4).
2. Iterate `laptop_flex_apps` (already in stable-append order); for each, if a
 pinned space already owns that app, co-tile the extra windows onto its last
 pinned occurrence; else find-or-create `lap-<slug>` (reuse the set-difference
 creation idiom), `--layout bsp`, and move all ids from
 `win_ids_unclaimed_for_app` onto it.
3. Reconcile: for each existing `lap-*` space whose label is not in this run's
 target set, first move any residual windows — via `win_ids_on_space`, which
 includes the minimized/floating ones the claim filter hides — to
 `home_space` (H5), then
 destroy it (query shape of `teardown_laptop_grid`, `lib.sh`; re-query
 after each destroy — indexes renumber, `lib.sh`).

On lock contention, exit **nonzero** (not 0) so callers re-loop (H2).

Interfaces:

- `laptop-layout.sh` — body rewrite; same
 invocation contract (no args, exit 0 on guard, **nonzero on lock
 contention**). Consumes `LAPTOP_PINNED`, `laptop_flex_apps`,
 `win_ids_unclaimed_for_app`, `win_ids_on_space`, `win_id_claim`,
 `space_for_label`. Emits `yabai_spaces_changed` sketchybar trigger.

### T4 — rules.sh: retire the laptop_bsp_space pins

Drop the `laptop_bsp_space` resolution and its rule block
(`rules.sh`, Messages/Qalculate/System Settings → `spaces[1]`). Messages
and System Settings move into `LAPTOP_STACK_APPS` and land on the stack via the
existing catch-all; Qalculate is left to flex (D8). Keep the
catch-all FIRST (ordering contract, `rules.sh`) and the float rules untouched. Update the `rules.sh` comment block to point
at `LAPTOP_STACK_APPS` as the classification source.

Interfaces:

- `rules.sh` — delete `laptop_bsp_space`
 variable + its `add_rule` block; comment updates only elsewhere. No new
 rules.

### T5 — live flex trigger: laptop-flex-event.sh + yabairc wiring

New debounced handler for in-laptop-mode app/window churn:

- `laptop-flex-event.sh`: clone of the `display-event.sh` stamp/waiter/stale-
 PID-reclaim pattern with its own stamp+lock paths
 and `QUIET_SECS=2`; after quiet, exit unless `ndisp == 1`. If
 `signals_suppressed` is held, **re-stamp and re-wait**
 (never exit — H2); else run `laptop-layout.sh` and, mirroring the
 `display-event.sh` `acted_on` loop, re-loop if `laptop-layout.sh` returned
 nonzero (contended) or a fresh stamp landed mid-run. Also require the
 `display-event.sh` stamp to be older than the quiet window before acting, so
 a rebuild never fires mid display-settle storm (H7). First reader of the
 `SIGNAL_GUARD` scaffolding.
- `display-event.sh`: the unplug cascade at `display-event.sh` currently
 discards `laptop-layout.sh`'s exit code (its re-loop is stamp-driven only),
 so a `laptop-layout` run that loses the lock to a concurrent flex rebuild is
 not retried. Capture the exit code and re-loop on nonzero, mirroring the
 `acted_on` loop, so the Mechanism's "both callers re-loop on contention" (H2)
 holds for the display path too (`display-event.sh` runs under `set -u` only,
 no `set -e`, so a nonzero return is safe to branch on).
- `yabairc`: four `yabai -m signal --add` lines —
 `event=application_launched`, `event=application_terminated`,
 `event=window_created`, `event=window_destroyed` — action = the new script
 (decision D5). We do NOT subscribe `window_title_changed` (Arc's churn
 source).

Interfaces:

- NEW `laptop-flex-event.sh` — no args;
 fast-path stamp then background waiter (sources `lib.sh` in the waiter).
- `yabairc` — four signal registrations.
- `display-event.sh` — capture `laptop-layout.sh`'s exit code at the unplug
  cascade and re-loop on nonzero (H2, mirrors the `laptop-flex-event.sh`
  waiter).

### T6 — README + sketchybar verification

Rewrite the laptop-mode sections of the README (the "one labelled `bsp` space"
per `LAPTOP_SPACES` entry text, the default-routing text, and the file map) to
describe pinned-core + flex-tail, stable-append order, the new trigger script,
and the `LAPTOP_STACK_APPS` classification. sketchybar needs no code change —
`plugins/spaces.sh` renders any `lap-*` label (`.label | sub("^lap-"; "")`)
and `laptop-layout.sh` keeps firing
`yabai_spaces_changed` — but verify label width behaviour with a larger flex
set on the notched 1728px bar.

Interfaces:

- `README.md` — doc-only edits.
- `sketchybar plugins/spaces.sh` — read-only
 verification; change only if the bar overflows (then a truncation tweak in
 the jq label pipeline).

## Tasks

- [ ] T1 — `profile.sh`: replace `LAPTOP_SPACES` with `LAPTOP_PINNED` (full
 8-slot) + `LAPTOP_STACK_APPS` (comms/system pile, Qalculate excluded);
 update header comment.
- [ ] T2 — `lib.sh`: add `laptop_flex_apps` (sorted-WIN slug, stack-filtered),
 `laptop_flex_order` (persisted stable-append ordinal file),
 `win_ids_unclaimed_for_app`, and `win_ids_on_space` (unfiltered space
 window ids for the H5 re-home).
- [ ] T3 — `laptop-layout.sh`: rewrite body as pinned + flex + reconcile
 converger; skip absent pinned apps; collision rule for extra pinned-app
 windows; re-home residuals before destroying stale `lap-*`; nonzero exit
 on lock contention.
- [ ] T4 — `rules.sh`: drop `laptop_bsp_space` pins; Messages/System Settings
 → `LAPTOP_STACK_APPS`; leave Qalculate to flex.
- [ ] T5 — new `laptop-flex-event.sh` (app + window signals, re-stamp on guard,
  re-loop on contention, display-settle guard) + `yabairc` four signals +
  `display-event.sh` cascade re-loops on `laptop-layout` nonzero (H2).
- [ ] T6 — README laptop sections rewrite; sketchybar bar-width verification.

## Global Constraints

- **Bash hooks.** yabai execs these hooks directly as shell — there is no
  in-process entry point for a yabai signal action. `#!/usr/bin/env bash`;
 absolute `YABAI=/opt/homebrew/bin/yabai`,
 `SKETCHYBAR=/opt/homebrew/bin/sketchybar`, `JQ=$(command -v jq)`; source
 `lib.sh`/`profile.sh`, never hardcode names/widths.
- **Display resolved by WIDTH, never index** — "display UUIDs/indexes are not
 [stable] (macOS reorders them on connect)";
 `DISPLAY_W=([g9]=5120 [aw]=3440 [laptop]=1728)`;
 `rules.sh` resolves via `idx_for_width`.
- **Labelled `bsp` grid is the only deterministic naming path** — macOS
 auto-names only native-fullscreen spaces, "which drive their own
 WindowServer spaces and are unreliable to script";
 sketchybar renders the `lap-*` labels and refreshes
 on the `yabai_spaces_changed` trigger (fired by `laptop-layout.sh`).
- **Idempotency + /tmp mkdir-lock** — match the `laptop-layout.sh` shape;
  guard: act only when the laptop is the SOLE display.
 The new event script must also carry the stale-PID reclaim. Flex-order state persists in `$XDG_CACHE_HOME`,
 not `/tmp`, so slot order survives reboot.
- **profile.sh stays the single edit surface, pure data**;
 this design REDUCES hand-editing — a fixed pinned list + a classification set
 replace a per-app roster, and new apps need no edit at all.
- **Deploy:** the shell hooks are installed on PATH and referenced from
  `skhdrc`/`yabairc`. Style: shfmt `-i 0 -s` (tabs), shellcheck
  `--external-sources`.

## Decisions

Resolved with Matt (D2/D3/D4/D5/D8 via `ask`); D1/D6/D7 are the designed
defaults ratified in that pass. Frozen on merge.

- **D1 — Granularity.** Flex tail is per **app** (one space co-tiling that
 app's windows); pinned core is per **claimed window** (`arc arc arc` yields
 three Arc spaces). Per-window flex labels can't be meaningful for Arc —
 titles drift.
- **D2 — Tail order = stable-append** (Matt). First-seen ordinal persisted in
 `$XDG_CACHE_HOME/yabai/laptop-flex-order`; new apps append at the end,
 existing positions never move, order survives reboot. Alphabetical rejected
 (needs an index-yanking reorder pass — H1).
- **D3 — Pinned core = today's full 8-slot order** (Matt):
 `arc ghostty-wave arc ghostty-mbp linear obsidian arc akiflow`. Only
 genuinely new apps flex; today's muscle memory is unchanged.
- **D4 — Keep the stack pile** (Matt). Home `stack` space + `rules.sh`
 catch-all survive; comms/system apps (Messages, Slack, Discord, System
 Settings, 1Password, Activity Monitor) pile via `LAPTOP_STACK_APPS`.
 Flex-everything (a ~15-space grid) rejected.
- **D5 — Rebuild trigger = auto on app + window open/close** (Matt).
 `application_launched`/`terminated` **and** `window_created`/`window_destroyed`
 through the debounced `laptop-flex-event.sh`; unplug/display path kept.
 Window events close the last-window-closed / first-window-opened staleness
 gaps app-only signals leave (H3). `window_title_changed` deliberately not
 subscribed. A window *un-minimized* fires `window_deminimized` (also not
 subscribed): an app whose sole window was minimized — its flex space
 reconciled away and the window re-homed to the stack (H5) — stays on the
 stack until the next subscribed event. Accepted as no worse than today (the
 current layout uses the same minimized filter and only rebuilds on unplug);
 `window_deminimized` is a candidate fifth signal if it bites in practice.
- **D6 — Slug determinism + collision** (default). `WIN` keys iterate in
 explicit `sort` order; extra windows of a pinned app co-tile onto its last
 pinned occurrence; flex spaces are minted only for apps with no pinned space
 (H4).
- **D7 — Skip absent pinned slots** (default). A pinned app that isn't running
 creates no placeholder space (flex down); its space reappears on next rebuild
 after launch. Trades fixed absolute positions for flex-down — with
 arc/ghostty near-always alive the practical shift is small.
- **D8 — Retire the `laptop_bsp_space` pins** (Matt). Messages + System
 Settings join `LAPTOP_STACK_APPS`; **Qalculate flexes to its own space**
 (pop-open calculator, not pile-worthy). Dropping the pin also fixes a live
 bug — today `spaces[1]` is the first `lap-*` space, so the trio is already
 dumped onto the pinned Arc space.

## Amendment — per-window flex (supersedes D1 + D6's co-tile clause)

Ratified with Matt (via `ask`, 2026-08). Reverses D1 (per-app tail granularity)
and **D6's co-tile clause** (extra windows of a pinned app co-tiling its last
pinned occurrence). **D6's other clause — WIN-key `sort`-order slug determinism
(H4) — is RETAINED and actively relied on** by `slug_for_window`, which must
iterate `${!WIN[@]}` in explicit `sort` order (bash assoc-array key order is
unspecified); do not drop the `| sort`. Motivation: in practice a pinned app
with more
live windows than pinned slots (routinely 4+ Arc windows vs 3 pinned) left two
windows sharing one space, which Matt does not want — the rule is now **every
window gets its own space, except the deliberate `LAPTOP_STACK_APPS` pile**.

- **D9 — Flex granularity is per WINDOW, not per app** (supersedes D1). Each
 non-minimized, non-floating, unclaimed, non-stack window gets its OWN labelled
 `lap-*` space. The D1 objection ("per-window labels can't be meaningful for
 Arc — titles drift") is resolved by labelling per window with an **ordinal
 occurrence suffix** (`lap-arc-5`), never the drifting title: deterministic and
 stable, exactly as the pinned occurrences already are. A non-pinned app with
 two windows now yields `lap-<slug>` + `lap-<slug>-2`, not one co-tiled space.
- **D10 — Pinned overflow continues the occurrence count** (supersedes D6's
 co-tile clause). An extra window of a pinned app no longer co-tiles onto the
 last pinned occurrence; it mints the next occurrence space (`arc` pinned ×4 →
 a 5th Arc window is `lap-arc-5`). The flex tail seeds each slug's occurrence
 counter from the pinned pass's high-water mark, so pinned and flex share one
 contiguous `lap-<slug>-<n>` namespace with no collision. Slug resolution for a
 flex window matches the window's **app AND title** against `WIN` specs (same
 matcher as the pinned `win_id` claim), falling back to the derived app-name
 slug — fixing a latent bug where the old per-app resolver ignored the title
 regex and mapped both Ghostty variants (`Ghostty|pc`, `Ghostty|mbp`) to
 whichever `WIN` key sorted first.
- **`LAPTOP_PINNED` gains a 4th `arc`** appended (`… akiflow arc`) so the common
 4-Arc set is fully pinned; a 5th+ Arc still flexes per D9/D10. D3's "full
 current order" is otherwise preserved (the 4th arc appends at the end, leaving
 the first three slots' positions unchanged).
- **Stable-append order (D2), the stack pile (D4), the trigger (D5), skip-absent
 (D7), and the retired pins (D8) are unchanged.** The persisted ordinal file
 keys on the per-window slug (`lap-<slug>-<n>`). Note the occurrence suffix is
 **positional, not window-identity-bound**: `lap-arc-5` tracks the 5th Arc by
 `sort_by(.id)` among the arcs present this run, so if a lower-id Arc closes the
 survivors renumber. The persisted slot therefore survives relaunch at the
 *ordinal* granularity (the Nth window of a slug), a weaker guarantee than the
 old per-app slug gave — acceptable here, and the file lines are retained (not
 pruned) by design.
