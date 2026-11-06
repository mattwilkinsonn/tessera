# Design: New-window placement

## Open Questions

1. **Which target policy should new tile-type windows use?** *Load-bearing; tracked in [RIG-8331](https://linear.app/rigelbuild/issue/RIG-8331/choose-targets-for-new-tiled-windows).* Should app-specific startup targets send Code to G9 and Linear to AW when those spaces use BSP, or should all new tiled windows stay on the laptop stack to preserve the no-reshuffle promise? (A) Send all tile-type arrivals to the laptop stack; this preserves geometry but changes Code and Linear destinations. (B) Keep G9/AW app targets; this preserves destinations but resizes existing windows. (C) Insert only into Tessera-owned named slots and send unmatched arrivals to the laptop stack; this needs slot-ownership semantics and cannot promise unchanged geometry in BSP. **Recommendation:** A until a slot-insertion contract can preserve geometry. Decision needed: choose A, B, or C.
2. **Should the laptop flex rebuild be allowed to move a just-placed new window?** *Load-bearing; tracked in [RIG-8331](https://linear.app/rigelbuild/issue/RIG-8331/choose-targets-for-new-tiled-windows).* `window_created` currently triggers the flex channel (`FlexEvents`, `internal/daemon/hub.go`); `LaptopFlexWindows` and `stepFlex` can move that window into an app flex space (`internal/engine/flex.go`, `internal/engine/laptop.go`), overriding its immediate target or a keyboard move made in the meantime. (A) Keep existing flex behavior; the 2-second rebuild may move it. (B) Protect the arrival for one flex pass, then return it to normal flex convergence; this needs an ID marker and one-pass exclusion. (C) Exclude new arrivals from flex until an explicit full layout; this may leave windows outside flex spaces. **Recommendation:** B, preserving the opening target through one delayed pass while retaining later convergence. Decision needed: choose A, B, or C.

## Direction

This is the next Tessera daemon feature after [resident-daemon.md](resident-daemon.md): keep yabai’s arrival heuristics but let Tessera, not yabai rules, decide the final destination. The problem and remedy are in RIG-8329. Scope is Tessera’s Go daemon, WM query contract, and yabai rules; no daemon redesign or separate profile feature. The least machinery that meets the ask is a per-window notification handled immediately, alongside the existing debounced whole-layout work. `resident-daemon.md` already uses one-line unix-socket events and the hub’s independent display/flex quiet windows; this design extends that protocol rather than adding a second subscriber. `laptop-flex-spaces.md` and `grid-layouts.md` establish the laptop stack and Tessera-owned BSP layouts to reuse. The feature has ongoing cost only at window creation and stays limited to macOS/yabai fields available in `query --windows`.

## Problem / Intent

Yabai applies placement-rule space/display effects before its float heuristic (`window_manager_create_and_add_window`, yabai `src/window_manager.c`), so dialogs and fixed-size windows can be sent off their original display before yabai floats them. New arrivals must open without moving or resizing existing windows; Tessera then tiles eligible windows so the user can move them with keyboard shortcuts.

## Approach

### Let new windows open where the app put them

Replace Tessera’s standard-window `manage=on` and arrival placement rules with a catch-all `manage=off` rule. Remove or strip all space/display effects from Tessera’s arrival rules; `manage=off` alone is insufficient because yabai applies `sid`/`did` effects separately from manage (`window_manager_apply_rule_effects_to_window`, yabai `src/window_manager.c`). Keep no yabai rule that sends a newly created window to another space/display. Retire Tessera’s exception rules from yabai and evaluate the Finder, Akiflow dialog, and System Settings cases in Tessera’s handler instead. 1Password needs no exception: its dialog is level 101 and fixed-size, so the heuristic below floats it. The managed-rule registry remains replaceable through `Rules` (`internal/commands/orchestration.go`).

### Handle one arrival without the layout debounce

Register `window_created` with an action that sends both the event and `YABAI_WINDOW_ID` over the daemon socket. Yabai documents that signal variable in `doc/yabai.asciidoc`; `window_created` also covers implicit launch windows. Extend the newline message as `window_created <id>` while preserving the existing one-token event lines for all other events. The current notifier is built in `RegisterSignals` (`internal/daemon/wiring.go`), parsed by `handle` (`internal/daemon/serve.go`), and dispatched through `Hub.Notify` (`internal/daemon/hub.go`). Add a distinct immediate placement path rather than making the window-created signal wait for the flex channel’s 2-second quiet window.

On receipt, defer the request while the display channel is active or its 3-second quiet interval is unsettled. Recheck after the cascade completes so the target uses stable topology. Then serialize placement against the apply or laptop command; each holds its own lock (`Apply` and `laptop`, `internal/commands/composite.go`), so the arrival handler must acquire both non-blocking locks in a fixed order before it mutates windows. Observe the existing 8-second signal guard (`SignalsSuppressed`, `internal/effects/effects.go`): if it is active or either lock is busy, retain the window ID and retry after the blocking work completes, not via a new fixed delay or dropped event. Stamp/release the signal guard around the placement mutation so its own yabai events do not schedule a second layout.

Keep the display cascade’s 3-second quiet interval and laptop flex rebuild’s 2-second interval. Display events change topology and target availability, so one cascade should run after the storm settles. The flex rebuild handles launch/termination batches and may move a new arrival: `FlexEvents` includes `window_created` (`internal/daemon/hub.go`), and `LaptopFlexWindows`/`stepFlex` move eligible windows to app-specific flex spaces (`internal/engine/flex.go`, `internal/engine/laptop.go`). RIG-8331 asks whether to preserve an arrival’s target for one flex pass or keep existing flex behavior; implement the selected option and its tests. Keep the 2-second batch debounce and its display-quiet gate (`Hub.untilQuiet`, `internal/daemon/hub.go`); it is not the per-window placement latency.

### Mirror yabai’s float heuristic, then place only tile-type windows

Query the indicated window by ID and use the raw yabai values for `level`, `can-move`, `can-resize`, `subrole`, `frame`, plus role/root and sticky state as needed. The existing Tessera `wm.Window` omits these fields (`internal/wm/types.go`); `RawYabaiWindow` and `NormalizeWindow` currently discard them (`internal/wm/yabai/normalize.go`). Extend the model and targeted query path rather than querying every window for each arrival. `QueryWindowArgs` already forms the ID-targeted yabai query (`internal/wm/yabai/args.go`).

Mirror the predicates in `window_manager_create_and_add_window` and `window.c` (yabai `src/`): yabai’s manage-off path already floats the window, and Tessera’s tile candidate must satisfy the inverse conditions: root window, `AXWindow` role, `AXStandardWindow` subrole, normal window level, movable, non-sticky, and not both fixed-size and undersized. “Undersized” means width **or** height is at most 500 points (`window_is_undersized`, `window.c`). A non-root window is not eligible for management. Keep AXUnknown out of scope under RIG-8327. Apply the existing exceptions in Tessera first: Finder, Akiflow `AXDialog`, and System Settings. All exceptions and heuristic floats remain at their opening location.

For a tile-type arrival, resolve its target `SpaceID` from the current window’s opening display/space, then move it only when the chosen target differs; if it is still floating, toggle it off after the move. Default target is the laptop home stack. Keep Code→G9 and Linear→AW only if Matt’s RIG-8331 decision allows them; Akiflow remains laptop-targeted. `WindowOps` provide `MoveWindowToSpace` and `ToggleFloat` (`internal/wm/types.go`). Read the current space from the targeted query before moving; do not use cached Mission Control indexes. The home target is a stack (`DisplaySetup`, `internal/commands/orchestration.go`). Do not rebuild or reflow a BSP space in this arrival path. Tessera’s `DeskPlan` assigns non-floating arrivals to existing slots and parks extras (`internal/engine/desk.go`), and laptop convergence can create spaces and reorder them (`internal/engine/laptop.go`); either whole-layout path can change other windows’ geometry. Later explicit Tessera layout actions own that reflow.

### Startup and failure behavior

The startup `WakeRoutine` registers signals and runs `RunDisplayCascade` (`internal/daemon/wiring.go`), which re-applies rules and reclaims the current layout. Once destination rules are removed, windows that are already tiled keep their place; the startup path never moves them. After the startup display cascade completes, classify only windows that are still floating through the same policy as arrivals; place tile-type ones and leave floats alone. Yabai also emits `window_created` for windows created implicitly at app launch, but it cannot deliver events from while Tessera was down.

If the daemon is down, new windows remain floating where the app opened them; this is the fail-open fallback. Startup/wake later classifies existing windows. If the window is destroyed before its ID query or mutation, treat it as a stale event and do nothing. Do not retry with broad snapshots. AXUnknown stays out of scope under RIG-8327; yabai rejects its unknown subrole during `window_manager_create_and_add_window` (yabai `src/window_manager.c`).

### Alternatives considered

- **Keep space/display rules and only add `manage=off`:** rejected because yabai applies destination effects before the float decision, the failure described in RIG-8329.
- **Debounce all arrivals through flex:** rejected because this adds the 2-second quiet delay to one window and couples placement to a whole-laptop rebuild. Keep the delay for its existing batch-reclaim job.
- **Move every arrival and rebuild its destination BSP space:** rejected because rebuilding the space changes other windows’ geometry and violates the no-reshuffle requirement.

## Plan

### T1 — Add a targeted yabai window query model

Add and normalize fields needed to classify one ID-targeted query. Keep existing layout query behavior intact. A yabai per-window response includes current display and space, so do not add a separate topology query for the ID. Write tests first for raw decoding, normalization, and missing-ID handling.

**Interfaces:** Extend `RawYabaiWindow` and `wm.Window` with `Role`, `Subrole`, `Root`, `Level`, `CanMove`, and `CanResize`, retaining `Frame` and `Sticky`. Add `QueryWindow(ctx context.Context, id int) (wm.Window, bool, error)` to the yabai driver, implemented with `QueryWindowArgs(id)` and `NormalizeWindow`. A missing ID returns `false, nil`; query errors are returned.

### T2 — Make yabai arrival rules destination-neutral

Replace Tessera’s managed rules with one catch-all `manage=off` rule and remove all Tessera-generated space/display effects. Tests assert the new rule, absence of effects, and cleanup of old `auto:` labels; then implement and update argument goldens.

**Interfaces:** `Rules(ctx, profile, driver) error` remains the entry point; `wm.Rule` remains unchanged. Preserve display setup and explicit user-driven layout commands.

### T3 — Add immediate per-window socket dispatch and placement policy

Extend the `window_created` notifier with `YABAI_WINDOW_ID`. Parse event plus ID while preserving event-only messages for other signals. Handle placements immediately unless a display cascade is unsettled; then check apply/laptop locks and the signal guard, classify the new window, and move/unfloat only tile-type arrivals. Write tests first for protocol compatibility, no flex delay, heuristic boundaries (including 500 points), exceptions, display storms, lock/guard contention, and destroyed IDs.

**Interfaces:** Socket line `window_created <positive-id>`; existing `<event>` remains valid. Keep `Hub.Notify` for flex/display debounce and add an immediate placement callback at the socket boundary. `ClassifyWindow(wm.Window) WindowPlacement` returns `Tile` or `Float`. `ResolveWindowTarget(profile *config.Profile, app string, openingDisplay int, displays []wm.Display, spaces []wm.Space) (wm.SpaceID, bool)` returns a currently valid target. Placement consumes the targeted query’s current `SpaceID`, calls `MoveWindowToSpace` only if the target differs, and calls `ToggleFloat` only if the window remains floating. On contention, keep the ID pending until the guard is released or conflicting work ends; never drop it or race the layout. The flex protection behavior follows RIG-8331.

### T4 — Reclaim startup windows through the same classification

After signal registration and display readiness, run the same policy over windows that are still floating, so eligible windows are not stranded. Already-tiled windows are never moved. Keep the display cascade and existing 3s/2s debounce. Write tests first for existing tiled windows (untouched), stranded tile-type floats, exceptions, and recovery after daemon downtime.

**Interfaces:** `WakeRoutine` retains signal registration and the display cascade; factor the per-window policy for use over `QueryWindows(ctx)` results. Reuse T3’s target, lock, guard, and `WindowOps` contract.

## Global Constraints

- New window arrival must not move or resize other windows. Do not reflow a BSP space in the per-window path.
- The laptop home stack is the default. Whether Code/G9 and Linear/AW remain enabled is pending RIG-8331; Akiflow targets laptop if app targets remain enabled.
- Preserve explicit Tessera layout commands, the 3-second display debounce, and the 2-second flex debounce. Do not debounce per-window placement; follow RIG-8331 on one-pass flex protection.
- Preserve event-only socket lines. Only `window_created` carries an ID.
- Reuse current daemon, WM, locks, and signal guard patterns. Do not add broad polling, a profile abstraction, retries, or AXUnknown handling.
- Use red-green tests per task. Run formatting, lint, and tests for touched Go packages after all slices land.

## Tasks

- [ ] **T1 — Targeted query fields.** Owner: `implement-go`. **Interfaces:** `QueryWindow(ctx, id) (wm.Window, bool, error)` plus normalized role/subrole/root/level/move/resize fields. Tests: query argv, raw decoding, normalization, missing versus failed lookup. Depends on none.
- [ ] **T2 — Destination-neutral rules.** Owner: `implement-go`. **Interfaces:** `Rules(ctx, profile, driver) error` and `wm.Rule`. Tests: catch-all manage-off, no space/display effects, stale `auto:` removal. Ships in the same PR as T3: alone it leaves every new window floating.
- [ ] **T3 — Immediate arrival handler.** Owner: `implement-go`. **Interfaces:** `window_created <id>`, immediate callback, `ClassifyWindow`, `ResolveWindowTarget`, targeted query, and `WindowOps`. Tests: event-only compatibility, no 2s delay, heuristic/exception cases, display storm, contention, stale ID, and the flex-policy outcome from RIG-8331. Depends on T1 and T2.
- [ ] **T4 — Startup reclaim.** Owner: `implement-go`. **Interfaces:** reuse T3 classifier/placer in `WakeRoutine`. Tests: existing tiled window, float exceptions, daemon restart. Depends on T3.
