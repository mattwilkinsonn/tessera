# Design: New-window placement

## Decisions

RIG-8331 rulings: Q1=A originally sent every desk-mode tile-type arrival to the laptop home stack. RIG-8505 supersedes that target rule: use the laptop home stack when the laptop is present, otherwise the AW left slot, then the G9 left slot. Q2=A keeps laptop-only rules and flex behavior unchanged. Q3=A uses best-effort move → focus → verify active space → conditional unfloat.

## Direction

This extends the resident daemon's socket events and display/flex work queues. Arrival placement runs with multiple connected displays when a laptop, AW, or G9 target resolves. It also runs for a single external display that resolves as AW or G9 while the laptop is absent. Laptop-only and unmatched single-display topologies retain their current arrival behavior. A desk-to-laptop cascade hands off surviving floating arrivals before laptop convergence. The design reuses the laptop stack and Tessera-owned BSP spaces in `laptop-flex-spaces.md` and `grid-layouts.md`.

## Problem / Intent

Yabai applies placement-rule space/display effects before its float heuristic. In desk mode, Tessera places eligible arrivals without moving or resizing existing windows during normal operation. External-slot arrivals move, focus, verify the active space, then stack onto the live anchor or unfloat when no anchor exists. A space switch between verification and the final operation remains a best-effort race.

## Approach

### Install rules for the current topology

`Rules` queries connected displays and installs a mode-specific rule set without `rule --apply`. With multiple displays, install a catch-all `manage=off` rule and no space/display effects, including when the topology includes the laptop and an unknown external monitor. With one recognized external AW/G9 display and no laptop, install the same catch-all rule. With a laptop-only display, retain today's laptop rule set: default laptop-stack placement, configured Code/Linear placement, `manage=on` for standard windows, and the Finder, Akiflow, System Settings, and 1Password exceptions.

Do not call `ops.Apply` in either mode. Yabai's `rule_reapply_all` iterates existing root windows and calls `window_manager_apply_manage_rules_to_window(&g_space_manager, &g_window_manager, window, window_title, window_role, window_subrole, false);` (`src/rule.c`). The `manage=off` path floats an already-managed window (`src/window_manager.c`). Use add-only `rule_add`, which replaces a matching label and appends a rule without reapplying it to open windows. Rules tests cover unchanged populated BSP state and the driver's recorded add-only commands.

RIG-8327 comment [9a43ce5c-a02e-44ac-834e-2838c05b0ceb](https://linear.app/rigelbuild/issue/RIG-8327/yabai-never-adopts-the-zoom-meeting-window-so-tessera-cant-tile-it#comment-9a43ce5c-a02e-44ac-834e-2838c05b0ceb) records the captured 1Password dialog query: `id=41411`, title `1Password`, subrole `AXStandardWindow`, level 101, `can-resize=false`, frame 400×325. Its non-normal level excludes it under the heuristic; keep the existing laptop rule exception.

### Dispatch arrivals and wait for display readiness

Register `window_created` with `YABAI_WINDOW_ID` and send `window_created <id>` over the existing socket. Keep other events as one-token lines. Yabai documents the variable and that the signal covers implicit launch windows (`doc/yabai.asciidoc`). Dispatch the ID separately from debounced work (`internal/daemon/wiring.go`, `serve.go`, `hub.go`). Do not wait for the flex debounce.

An arrival waits while display work is stamped but unsettled or its cascade is running. Use the display channel's existing 3-second quiet deadline (`Hub.untilQuiet`, `internal/daemon/hub.go`); retain IDs until the cascade completes. `DisplayWork` invokes `RunDisplayCascade` after the hub's quiet window. That cascade runs `DisplaySetup`, `Rules`, and full layout reclamation (`internal/daemon/wiring.go`, `internal/commands/orchestration.go`). After quiet, enter the shared serialization domain and recheck readiness before querying or mutating. If display activity appeared, release serialization and wait again. This gate does not wait for the flex debounce.

One in-process mutex serializes arrivals with the full `WakeRoutine`/display cascade, including `DisplaySetup` and `Rules`, not just `Apply` or laptop command bodies. Keep pending IDs until the cascade completes. Test an arrival after a display event is stamped but before the quiet deadline, and another while wake is paused inside `Rules`.

After display readiness, acquire the Apply and laptop locks in fixed order and wait out the signal guard before the authoritative window/topology query (`internal/commands/composite.go`, `internal/effects/effects.go`). On contention, retain IDs and use a context-aware bounded lock wait under `CascadeTimeout` (60 seconds; `internal/daemon/hub.go`). Check guard expiry on every wait tick and re-query after any wait. If the fresh query shows the window is already un-floated, or the layout has already moved and un-floated it, treat the arrival as satisfied. Test a manual apply that moves and un-floats a pending window before releasing its lock, manual lock release without a yabai event, and crashed-holder guard expiry. Stamp/release the signal guard around this handler so its own yabai events do not schedule another layout.

### Classify and place arrivals

Query only the event's window ID after locks and guard are acquired. Add role, subrole, root, level, movable, resizable, and floating to the targeted query model. yabai serializes `is-floating` but no managed flag (`WINDOW_PROPERTY_LIST`, `src/window.h`), so tess never derives one (`internal/wm/types.go`, `internal/wm/yabai/normalize.go`). `QueryWindow` may query spaces too: yabai serializes `space` as a Mission Control index, not stable SpaceID: `int space = space_manager_mission_control_index(sid);` (`window_serialize`, yabai `src/window.c`). `NormalizeWindow` needs an index-to-SpaceID mapping. Test with an index that differs from SpaceID.

Mirror yabai's creation heuristic (`window_manager_create_and_add_window`, `src/window_manager.c`). A tile candidate is a root `AXWindow` / `AXStandardWindow` at normal level, movable, non-sticky, and not both fixed-size and undersized. Yabai's predicate is `(!window_can_resize(window) && window_is_undersized(window))`; `window_is_undersized` returns true when width or height is `<= 500.0f` (`src/window_manager.c`, `src/window.c`). AXUnknown remains out of scope under RIG-8327. Exceptions and heuristic floats stay at their opening location.
For each desk-mode tile-type arrival, keep the targeted query, lock and guard order above. Multiple connected displays qualify for placement when a laptop, AW, or G9 target resolves. A single external AW/G9 display also qualifies when the laptop is absent; single-laptop and unmatched single-display arrivals keep the existing no-op path. Resolve the destination in priority order: laptop home stack, AW home space, then G9 home space. For an AW/G9 target, select the live leftmost eligible tiled window on its home space as the stack anchor, regardless of app or title. If an anchor exists, move the arrival to the target space when needed, focus it, verify that the active space is still the target, then call `StackOnto` with the anchor. If no anchor exists, move and focus the arrival, verify the target space, then unfloat it. If focus or verification fails, do not stack or unfloat. Focus follows the new window to its target, matching macOS focus on open. Yabai's unfloat path calls `space_manager_tile_window_on_space(sm, window, space_manager_active_space())` (`window_manager_make_window_floating`, `src/window_manager.c`).

This is best-effort destination safety, not an atomic no-reshuffle guarantee. The active-space check and final `StackOnto` or `ToggleFloat` operation are separate yabai commands; a space switch in that gap can make the arrival land in the newly active space. Eliminating the gap needs an atomic yabai operation, tracked as RIG-8327 option B (the yabai fork). Test failed verification: the arrival remains floating at its target and existing geometry does not change.


### Desk-to-laptop transition

Display changes run the full display cascade. `RunDisplayCascade` calls `DisplaySetup`, then `Rules`, then queries topology and runs laptop convergence when one display remains (`internal/commands/orchestration.go`). On a desk-to-laptop transition, `Rules` first restores laptop arrival rules. Before laptop convergence, classify floating tile-type windows left by desk-mode `manage=off` and hand them off: move to the laptop target, focus, verify, and unfloat so pinned claims and flex can see them. Do not apply rules to all existing windows. Test new pinned, flex-tail, and stack-app windows in laptop-only mode, plus a desk-created window surviving unplug and being claimed by laptop convergence.

### Startup and failure behavior

Wake and display cascades keep current full layout reclamation: `RunDisplayCascade` runs `DisplaySetup`, `Rules`, then laptop convergence or `Apply` (`internal/commands/orchestration.go`). This is an explicit rebuild, not an arrival; the no-reshuffle invariant does not cover it. Startup additionally recovers eligible floating tile-type windows in desk mode, excluding windows on reclaimed slots. Laptop-only startup retains the existing rule and flex behavior.

If the daemon is down, windows remain where the app opened them until recovery. Ignore an ID that disappears before query or mutation. Do not use broad snapshots for arrival handling. Yabai ignores AXUnknown windows during creation (`src/window_manager.c`).

### Alternatives considered

- A global catch-all `manage=off` rule is rejected: it strands laptop arrivals outside pinned and flex claims.
- Applying rules to open windows is rejected: `rule_reapply_all` runs manage effects on current root windows.
- Debouncing each arrival through flex adds a delay and couples one-window placement to a full layout rebuild.
- Rebuilding an arrival's BSP destination changes other windows' geometry.

## Plan

### T1 — Add a targeted yabai window query model

Extend and normalize classification fields while preserving layout queries. Query spaces to translate the serialized Mission Control index to stable SpaceID. Test raw decoding, normalization, differing index/ID, floating state, missing ID, and query errors.

**Interfaces:** `QueryWindow(ctx context.Context, id int) (wm.Window, bool, error)`; add `Role`, `Subrole`, `Root`, `Level`, `CanMove`, `CanResize`, and `Floating` to the query model; resolve SpaceID from a fresh spaces mapping.

### T2 — Make rule installation topology-aware and add-only

With multiple displays, install catch-all manage-off and no destination effects, even if an external display is unmatched. With exactly one display, install catch-all manage-off only when it resolves as AW/G9 and the laptop does not resolve; otherwise keep the current laptop rule set. Never apply rules to existing windows. Add the desk-to-laptop handoff before laptop convergence. Test laptop pinned/flex-tail/stack apps, desk arrivals across unplug, laptop plus unknown external recovery, and unchanged populated BSP state through Rules plus driver.

**Interfaces:** `Rules(ctx, profile, driver)` installs catch-all manage-off for multiple displays or a single recognized external AW/G9 without laptop; otherwise it installs laptop rules. Uses add-only rule operations; no `ops.Apply` / `rule --apply`. Keep `wm.Rule` unchanged.

### T3 — Add immediate placement with readiness, locking, and focus verification

Parse ID-bearing arrivals. Wait for display quiet and cascade completion; serialize with cascades; acquire locks and guard before the authoritative query; re-query after any wait; then classify and resolve the target. For an external target with a live anchor, move if needed, focus, verify the active space, then `StackOnto` the anchor. Without an anchor, move, focus, verify, then conditionally unfloat. Test quiet-window and in-Rules arrivals, manual lock release, guard expiry, an already-un-floated arrival, manual apply moving and un-floating a pending window, failed verification, an empty external home space with two arrivals, and a laptop-stack target with populated G9 geometry.

**Interfaces:** `window_created <positive-id>`; classifier and target resolver; pending IDs; shared serialization; bounded lock/guard wait; `MoveWindowToSpace`, `FocusWindow`, active-space query, `StackOnto`, and conditional `ToggleFloat`. Other socket events remain one-token lines. No flex debounce.

### T4 — Recover floating windows after startup cascade

Keep full startup/wake reclamation unchanged. After it completes, classify eligible floating tile-type windows not on reclaimed slots and apply desk-mode placement. On desk-to-laptop transition, run the laptop handoff before laptop convergence. Test recovery and normal layout reclamation.

**Interfaces:** `WakeRoutine` retains `RunDisplayCascade`; reuse T3's classifier and placement operations.

## Global Constraints

- Desk arrivals have a best-effort no-reshuffle contract: failed focus or active-space verification changes no existing geometry and leaves the arrival floating. A space switch between verification and the final stack operation remains a documented race.
- When the laptop resolves, use its home stack as the target; otherwise use AW's home space when AW resolves, then G9's home space. For an external target, stack onto the live leftmost eligible tiled window on that home space. After a successful placement into an empty target space, re-query the placed window and use it as the anchor for later arrivals in the same handoff or recovery batch. With no anchor, move and focus the arrival, verify the active space, then unfloat it. Laptop-only arrival behavior remains unchanged.
- Install catch-all manage-off for every multi-display topology, including laptop plus unmatched external displays; also install it for a single recognized AW/G9 without laptop. Keep laptop-only placement, manage-standard, and exception rules.
- Desk-to-laptop transition hands off surviving floating tile-type arrivals before laptop convergence.
- Preserve the 3-second display quiet interval, do not add flex debounce to arrivals, and keep event-only lines for non-arrival signals.
- Reuse existing daemon, WM, lock, and guard patterns. Do not add broad polling, a profile abstraction, or AXUnknown handling.
- Use red-green tests. Format, lint, and test touched Go packages after all slices land.

## Tasks

- [x] **T1 — Targeted query model.** Add and normalize classification fields in `wm.QueryWindow`; resolve stable SpaceIDs from yabai spaces. Add raw decode, normalized index-to-ID, floating-state, missing-ID, and query-error tests.
- [x] **T2 — Topology-aware rules.** Install add-only topology-specific rule sets and hand off desk arrivals before laptop convergence. Test laptop pinned, flex-tail and stack apps, unplug handoff, and unchanged populated BSP state.
- [x] **T3 — Immediate desk arrivals.** Queue positive IDs, wait for quiet and shared cascade serialization, acquire command locks and guard before the authoritative query, and place after re-query. Test quiet window, cascade serialization, lock release and expiry, stale and already-tiled windows, failed verification, and no geometry changes.
- [x] **T4 — Startup recovery.** Recover unclaimed floating tile candidates after startup reclamation, excluding windows on reclaimed slots. Test startup recovery and normal reclamation.

T1–T4 are implementation slices of one coherent feature and MUST land together in one PR. None is independently shippable.

## Implementation coverage

- T1: `TestQueryWindow` covers classification, differing index/SpaceID, missing IDs, query failures, and cancellation. `TestNormalizeWindows` covers raw decoding and floating-state normalization.
- T2: `TestRules` covers laptop rules, 1Password exclusion, desk manage-off rules, and unchanged populated BSP state. `TestRulesAddOnly` records real driver commands. `TestLaptopArrivalConvergence` covers pinned, flex-tail, and stack-app placement. `TestRunDisplayCascadeHandoff` proves laptop rules precede handoff, which precedes pinned convergence.
  `TestRunDisplayCascadeRetriesHandoff` covers failed Rules and handoff attempts followed by successful retries on unchanged laptop topology. Rule coverage also includes the G9-only topology and laptop plus AW/G9 monitors.
- T3: `TestHubWindowCreated` covers display quiet, cascade completion, and worker survival after errors. `TestRunArrivalWaitsInsideWakeRules` covers shared wake serialization. `TestHandleWindowCreatedWaits` covers lock release without events, manual apply satisfying the arrival, crashed-guard expiry, and bounded waits. `TestHandleWindowCreatedRechecksReadiness` covers display work appearing during a wait. `TestWindowPlacement` covers classification, stale IDs, already-tiled no-ops, laptop targeting, failed verification, and unchanged existing geometry.
  `TestRunTopologyCommandsSerializePlacement` covers standalone `rules` and `display-setup` command dispatch sharing the cross-process placement locks. `TestHubArrivalDefersDisplayEventDuringQuery` covers display activity during window and topology queries. `TestWindowPlacement` asserts move → focus → active-space query → unfloat ordering. `TestWindowPlacementFailedFocus` covers failed-focus gating. `TestIsTileCandidate` covers independent classifier boundaries.
- T4: `TestRecoverDeskWindows` runs full reclamation before recovery and proves reclaimed slot owners and heuristic floats stay excluded. `TestArrivalBatchRefreshesAnchor` covers two floating arrivals targeting an initially empty external home space during both handoff and recovery. `TestRecoverDeskWindowsWithLaptopAndUnknownExternal` covers recovery when a laptop is connected with an unmatched external monitor.
  `TestRecoverDeskWindowsIgnoresHistoricalClaims` covers a floating historical slot owner on the same topology. Recovery excludes only this wake's completed desk claims. `TestWakeRoutineReclaimsRuleFloatedSpawns` covers Code, Linear, and terminal windows created floating by desk rules, adopted before planning, and reclaimed to their configured displays in the same wake.
  The terminal case retains its configured title predicate while spawning a blank-title window. `TestAdoptSpawnedWindowsClaimPolicy` covers exact-match priority, app-only fallback, rejection of windows classified under another spec, baseline IDs, and heuristic exceptions through the same claim policy used by desk planning.

The verify/unfloat gap remains best-effort. Tests use fake window managers and a recording yabai subprocess; no live desktop smoke is implied.

