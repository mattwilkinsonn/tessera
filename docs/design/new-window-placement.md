# Design: New-window placement

## Open Questions

1. **Which target policy should new tile-type windows use?** *Load-bearing; tracked in [RIG-8331](https://linear.app/rigelbuild/issue/RIG-8331/choose-targets-for-new-tiled-windows).* Should Code and Linear keep their G9/AW destinations when those spaces use BSP, or should all tile-type arrivals use the laptop stack? (A) Send all arrivals to the laptop stack; this preserves geometry but changes those destinations. (B) Keep G9/AW targets; this preserves destinations but resizes existing windows. (C) Insert only into Tessera-owned named slots; this needs slot-ownership semantics and cannot promise unchanged BSP geometry. **Recommendation:** A until slot insertion can preserve geometry. Decision needed: choose A, B, or C.

2. In laptop-only mode, keep today's flex convergence (new windows go to per-app flex spaces and later flex passes may re-home a keyboard-moved window) — (A) keep as is [Recommendation], (B) extend A2 to laptop mode, which needs a flex-ownership redesign.

## Direction

This extends the resident daemon's socket events and separate display/flex work queues. It changes arrival handling in Tessera's Go daemon and yabai rules, not daemon architecture or laptop flex ownership. Arrival placement runs only in desk mode (more than one connected display); laptop-only mode keeps current flex convergence. It reuses the laptop stack and Tessera-owned BSP spaces described in `laptop-flex-spaces.md` and `grid-layouts.md`.

## Problem / Intent

Yabai applies rule space/display effects before its float heuristic: `window_manager_create_and_add_window` applies effects before classifying a window (yabai `src/window_manager.c`). A new window must stay on its opening display until Tessera places it. In desk mode, Tessera tiles eligible arrivals without moving or resizing existing windows.

## Approach

### Install arrival rules without reapplying them

Replace Tessera's arrival rules with a catch-all `manage=off` rule and remove all Tessera-generated space/display effects. `window_manager_apply_rule_effects_to_window` handles `sid`/`did` independently of `manage` (yabai `src/window_manager.c`). Retire old exception rules and classify Finder, Akiflow dialogs, System Settings, and other float cases in Tessera.

`Rules` must install rules without `rule --apply`. Its current path ends with `ops.Apply` (`internal/commands/orchestration.go`). Yabai's `rule_reapply_all` visits existing root windows and applies manage rules (`src/rule.c`); the `manage=off` handler floats them (`src/window_manager.c`). Use an add-only rule path so installation does not rewrite open windows' management state. Test Rules through the driver with a populated BSP; existing management state, space, and geometry must stay unchanged.

RIG-8327 records a captured 1Password dialog query: window 41411, title `1Password`, subrole `AXStandardWindow`, level 101, `can-resize=false`, frame 400×325. Its non-normal level excludes it; no app-specific exception is needed.

### Dispatch and serialize one arrival

Register `window_created` with `YABAI_WINDOW_ID` and send `window_created <id>` over the existing socket. Keep other events as one-token lines. Yabai documents the variable and that the signal covers implicit launch windows (`doc/yabai.asciidoc`). Parse the ID at the socket boundary and dispatch placement separately from debounced work (`internal/daemon/wiring.go`, `serve.go`, `hub.go`). Run the arrival handler only with more than one connected display. `FlexWork` runs only with one display (`internal/daemon/wiring.go`); preserve laptop-only convergence unchanged.

One in-process mutex serializes arrival placement with the full `WakeRoutine`/display cascade, including `DisplaySetup` and `Rules`, not just `Apply` or laptop command bodies. Keep pending IDs until the cascade completes, then re-evaluate topology. Test an arrival while wake is paused inside `Rules`.

Also acquire the existing Apply and laptop locks in a fixed order and observe the signal guard (`internal/commands/composite.go`, `internal/effects/effects.go`). On contention, retain IDs and wait for a lock with a context-aware bounded wait under `CascadeTimeout` (`internal/daemon/hub.go`, 60 seconds). Check guard expiry on every wait tick. Test a manual command releasing without a yabai event and a crashed holder whose guard expires. Stamp/release the guard around placement so its own yabai events do not schedule another layout. Keep the 3-second display quiet interval and 2-second flex batch debounce; neither delays arrival placement.

### Classify and place tile-type arrivals

Query only the event's window ID. Add role, subrole, root, level, movable, and resizable fields to `wm.Window` and `RawYabaiWindow` (`internal/wm/types.go`, `internal/wm/yabai/normalize.go`). `QueryWindow` may query spaces too: yabai serializes `space` as a Mission Control index, not stable SpaceID (`window_serialize`, yabai `src/window.c`). `NormalizeWindow` needs an index-to-SpaceID mapping. Test with index different from ID.

Mirror yabai's creation heuristic (`window_manager_create_and_add_window`, `window_is_undersized`, yabai `src/window_manager.c`, `src/window.c`). A tile candidate is root, `AXWindow` / `AXStandardWindow`, normal level, movable, non-sticky, and not both fixed-size and undersized. Undersized means width **or** height is at most 500 points. AXUnknown remains out of scope under RIG-8327. Exceptions and heuristic floats stay at their opening location.

Choose a valid target and call `MoveWindowToSpace` only when needed. Then focus the arriving window (`yabai -m window <id> --focus`; `FocusWindow` in the WM API). Focus follows the new window to its target, as macOS normally focuses a window on open. Verify active space equals the target before calling `ToggleFloat`: `window_manager_make_window_floating` tiles on `space_manager_active_space()` when un-floating (`src/window_manager.c`). If focus fails or active space is not the target, leave the window floating at its target and do not unfloat it. Test a laptop-stack target while a populated G9 BSP is active; assert no G9 geometry change and no changes to existing stack windows.

The default target is the laptop home stack. Code→G9 and Linear→AW remain subject to RIG-8331; Akiflow remains laptop-targeted. Use the query's current SpaceID, not a cached Mission Control index. Never rebuild a BSP in the arrival path. Laptop-only mode is unchanged: windows go to per-app flex spaces and later flex passes may re-home a keyboard-moved window. Extending A2 to that mode needs a flex-ownership redesign.

### Startup and failure behavior

Wake and display cascades keep their current full layout reclamation: `RunDisplayCascade` runs `DisplaySetup`, `Rules`, then laptop convergence or `Apply` (`internal/commands/orchestration.go`). This is an explicit rebuild, not an arrival, and retains today's behavior. The no-reshuffle invariant covers arrivals, not these cascades. Startup additionally places only windows that remain floating, are tile-type, and are not on a reclaimed slot.

If the daemon is down, windows remain where the app opened them until recovery. Ignore an ID that disappears before query or mutation. Do not use broad snapshots for arrival handling. AXUnknown remains unsupported because yabai rejects that subrole during creation (`src/window_manager.c`).

### Alternatives considered

- Keeping destination effects and adding only `manage=off` fails because yabai applies destination effects independently of `manage`.
- Debouncing each arrival through flex adds the 2-second delay and couples one-window placement to a full layout rebuild.
- Rebuilding an arrival's BSP destination changes other windows' geometry and is not destination-safe.

## Plan

### T1 — Add a targeted yabai window query model

Extend and normalize fields required for classification while preserving layout-query behavior. Query spaces to translate the serialized Mission Control index to stable SpaceID. Test raw decoding, normalization, differing index/ID, missing ID, and query errors.

**Interfaces:** `QueryWindow(ctx context.Context, id int) (wm.Window, bool, error)`; add `Role`, `Subrole`, `Root`, `Level`, `CanMove`, and `CanResize` to `wm.Window` and the raw query model; retain `Frame` and `Sticky`; resolve SpaceID from a fresh `QuerySpaces` mapping.

### T2 — Make yabai arrival rules destination-neutral

Install a catch-all `manage=off` rule without applying it to existing windows; remove space/display effects and stale arrival rules. Preserve display setup and explicit layout operations. Test the rule, effects, stale labels, and unchanged management state/space/geometry for a populated BSP through Rules plus driver.

**Interfaces:** `Rules(ctx, profile, driver)` with add-only rule installation; no `ops.Apply` / `rule --apply`. Keep `wm.Rule` unchanged.

### T3 — Add immediate desk-mode arrival handling

Send and parse ID-bearing `window_created` events. Under the shared cascade/arrival mutex, retain pending IDs until the full cascade completes; then query, classify, select a target, wait for locks, check the guard, move, focus, verify active space, and unfloat only when safe. Test protocol compatibility, desk-only dispatch, no flex delay, 500-point boundaries, exceptions, display storm, manual lock release, crashed-holder guard expiry, wake paused in Rules, stale ID, and geometry preservation with a populated G9 BSP and existing laptop-stack windows.

**Interfaces:** socket line `window_created <positive-id>`; `ClassifyWindow(wm.Window) WindowPlacement`; `ResolveWindowTarget(profile *config.Profile, app string, openingDisplay int, displays []wm.Display, spaces []wm.Space) (wm.SpaceID, bool)`; `MoveWindowToSpace`, `FocusWindow`, active-space query, and conditional `ToggleFloat`. Wait with context cancellation and `CascadeTimeout`; check guard expiry each tick. Keep event-only lines valid for other signals.

### T4 — Recover eligible floating windows after startup cascade

Keep full wake/display reclamation unchanged. After it completes, in desk mode, use the same policy only for floating tile-type windows not on a reclaimed slot. Test recovery and verify normal layout reclamation still runs.

**Interfaces:** `WakeRoutine` retains `RunDisplayCascade`; use T3's classifier and placer after the cascade. No laptop-only placement path.

## Global Constraints

- The no-reshuffle invariant covers arrivals: do not move or resize pre-existing windows or reflow a BSP during arrival placement.
- Startup/wake cascades remain explicit full layout reclamation with today's behavior.
- Arrival handling and startup recovery run only in desk mode. Laptop-only flex convergence remains unchanged; see Open Question 2.
- The laptop home stack is the default. Code/G9 and Linear/AW depend on RIG-8331; preserve Akiflow's laptop target.
- Preserve the 3-second display quiet interval, 2-second flex batch debounce, and event-only lines for non-arrival signals.
- Reuse existing daemon, WM, lock, and guard patterns. Do not add broad polling, profile abstraction, or AXUnknown handling.
- Use red-green tests. Format, lint, and test touched Go packages after all slices land.

## Tasks

- [ ] **T1 — Targeted query model.** Owner: `implement-go`. **Interfaces:** `QueryWindow(ctx, id) (wm.Window, bool, error)` and normalized fields with stable SpaceID translation. Tests: query args, decoding, differing index/ID, missing and failed lookup. Depends on none.
- [ ] **T2 — Destination-neutral arrival rules.** Owner: `implement-go`. **Interfaces:** `Rules(ctx, profile, driver)` with add-only installation, no `rule --apply`. Tests: catch-all manage-off, no destination effects, stale `auto:` cleanup, unchanged BSP management/space/geometry through Rules and driver. Depends on none.
- [ ] **T3 — Immediate desk-mode placement.** Owner: `implement-go`. **Interfaces:** ID-bearing event, classifier, target resolver, pending IDs, shared serialization, bounded lock/guard wait, focus and active-space verification. Tests: protocol, desk-only behavior, heuristic/exceptions, no flex delay, contention, wake paused in Rules, stale ID, and G9/laptop-stack geometry preservation. Depends on T1 and T2.
- [ ] **T4 — Startup floating-window recovery.** Owner: `implement-go`. **Interfaces:** T3 policy after unchanged full `WakeRoutine` cascade. Tests: place eligible floating windows not on reclaimed slots in desk mode; verify normal reclamation still runs. Depends on T3.

T1–T4 are implementation slices of one coherent feature and MUST land together in one PR. None is independently shippable.
