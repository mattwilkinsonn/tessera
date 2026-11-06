# Design: New-window placement

## Open Questions

1. **Which target policy should new desk-mode tile-type windows use?** *Load-bearing; tracked in [RIG-8331](https://linear.app/rigelbuild/issue/RIG-8331/choose-targets-for-new-tiled-windows).* Should Code and Linear keep their G9/AW destinations when those spaces use BSP, or should arrivals use the laptop stack? (A) Send all arrivals to the laptop stack; this preserves geometry but changes destinations. **Recommendation:** A until slot insertion can preserve geometry. (B) Keep G9/AW targets; this preserves destinations but resizes existing windows. (C) Insert only into Tessera-owned named slots; this needs slot ownership and cannot promise unchanged BSP geometry. Decision needed: choose A, B, or C.

## Direction

This extends the resident daemon's socket events and display/flex work queues. The arrival path runs only in desk mode (more than one connected display). Laptop-only mode retains its current arrival rules and flex convergence. A desk-to-laptop cascade hands off surviving floating arrivals before laptop convergence. The design reuses the laptop stack and Tessera-owned BSP spaces in `laptop-flex-spaces.md` and `grid-layouts.md`.

## Problem / Intent

Yabai applies placement-rule space/display effects before its float heuristic. In desk mode, Tessera places eligible arrivals without moving or resizing existing windows during normal operation. A space switch between active-space verification and unfloat remains a best-effort race.

## Approach

### Install rules for the current topology

`Rules` queries connected displays and installs a mode-specific rule set without `rule --apply`. With more than one display, install a catch-all `manage=off` rule and no space/display effects. Yabai applies destination effects independently of manage: `if (effects->sid || effects->did)` (`window_manager_apply_rule_effects_to_window`, yabai `src/window_manager.c`). With one display, retain today's laptop rule set: default laptop-stack placement, configured Code/Linear placement, `manage=on` for standard windows, and the Finder, Akiflow, System Settings, and 1Password exceptions.

Do not call `ops.Apply` in either mode. `Rules` currently ends with `ops.Apply` (`internal/commands/orchestration.go`). Yabai's `rule_reapply_all` iterates existing root windows and calls `window_manager_apply_manage_rules_to_window(&g_space_manager, &g_window_manager, window, window_title, window_role, window_subrole, false);` (`src/rule.c`). The `manage=off` path floats an already-managed window (`src/window_manager.c`). Use add-only `rule_add`, which replaces a matching label and appends a rule without reapplying it to open windows. Test Rules through the driver on a populated BSP; existing management state, space, and geometry must stay unchanged.

RIG-8327 comment [9a43ce5c-a02e-44ac-834e-2838c05b0ceb](https://linear.app/rigelbuild/issue/RIG-8327/yabai-never-adopts-the-zoom-meeting-window-so-tessera-cant-tile-it#comment-9a43ce5c-a02e-44ac-834e-2838c05b0ceb) records the captured 1Password dialog query: `id=41411`, title `1Password`, subrole `AXStandardWindow`, level 101, `can-resize=false`, frame 400×325. Its non-normal level excludes it under the heuristic; keep the existing laptop rule exception.

### Dispatch arrivals and wait for display readiness

Register `window_created` with `YABAI_WINDOW_ID` and send `window_created <id>` over the existing socket. Keep other events as one-token lines. Yabai documents the variable and that the signal covers implicit launch windows (`doc/yabai.asciidoc`). Dispatch the ID separately from debounced work (`internal/daemon/wiring.go`, `serve.go`, `hub.go`). Do not wait for the flex debounce.

An arrival waits while display work is stamped but unsettled or its cascade is running. Use the display channel's existing 3-second quiet deadline (`Hub.untilQuiet`, `internal/daemon/hub.go`); retain IDs until the cascade completes. `DisplayWork` invokes `RunDisplayCascade` after the hub's quiet window. That cascade runs `DisplaySetup`, `Rules`, and full layout reclamation (`internal/daemon/wiring.go`, `internal/commands/orchestration.go`). After quiet, enter the shared serialization domain and recheck readiness before querying or mutating. If display activity appeared, release serialization and wait again. This gate does not wait for the flex debounce.

One in-process mutex serializes arrivals with the full `WakeRoutine`/display cascade, including `DisplaySetup` and `Rules`, not just `Apply` or laptop command bodies. Keep pending IDs until the cascade completes. Test an arrival after a display event is stamped but before the quiet deadline, and another while wake is paused inside `Rules`.

After display readiness, acquire the Apply and laptop locks in fixed order and wait out the signal guard before the authoritative window/topology query (`internal/commands/composite.go`, `internal/effects/effects.go`). On contention, retain IDs and use a context-aware bounded lock wait under `CascadeTimeout` (60 seconds; `internal/daemon/hub.go`). Check guard expiry on every wait tick and re-query after any wait. If the fresh query shows the window is already managed, or the layout has already moved and un-floated it, treat the arrival as satisfied. Test a manual apply that moves and un-floats a pending window before releasing its lock, manual lock release without a yabai event, and crashed-holder guard expiry. Stamp/release the signal guard around this handler so its own yabai events do not schedule another layout.

### Classify and place arrivals

Query only the event's window ID after locks and guard are acquired. Add role, subrole, root, level, movable, resizable, floating, and management state to the targeted query model (`internal/wm/types.go`, `internal/wm/yabai/normalize.go`). `QueryWindow` may query spaces too: yabai serializes `space` as a Mission Control index, not stable SpaceID: `int space = space_manager_mission_control_index(sid);` (`window_serialize`, yabai `src/window.c`). `NormalizeWindow` needs an index-to-SpaceID mapping. Test with an index that differs from SpaceID.

Mirror yabai's creation heuristic (`window_manager_create_and_add_window`, `src/window_manager.c`). A tile candidate is a root `AXWindow` / `AXStandardWindow` at normal level, movable, non-sticky, and not both fixed-size and undersized. Yabai's predicate is `(!window_can_resize(window) && window_is_undersized(window))`; `window_is_undersized` returns true when width or height is `<= 500.0f` (`src/window_manager.c`, `src/window.c`). AXUnknown remains out of scope under RIG-8327. Exceptions and heuristic floats stay at their opening location.

For a desk-mode tile-type arrival, resolve a valid target SpaceID, then call `MoveWindowToSpace` only when needed. Focus the arriving window (`yabai -m window <id> --focus`; `FocusWindow` in the WM API), then verify that the active space equals the target immediately before `ToggleFloat`. Focus follows the new window to its target, matching macOS focus on open. Yabai's unfloat path calls `space_manager_tile_window_on_space(sm, window, space_manager_active_space())` (`window_manager_make_window_floating`, `src/window_manager.c`). If verification fails, leave the window floating at its target and do not unfloat it.

This is best-effort destination safety, not an atomic no-reshuffle guarantee. The active-space check and `ToggleFloat` are separate yabai commands; a space switch in that gap can make yabai insert into the newly active BSP. Eliminating the gap needs an atomic yabai operation, tracked as RIG-8327 option B (the yabai fork). Test failed verification: the arrival remains floating at its target and existing geometry does not change.

The default desk target is the laptop home stack. Code→G9 and Linear→AW remain subject to RIG-8331; Akiflow remains laptop-targeted. Never rebuild a BSP in the arrival path. In laptop-only mode, new windows use today's placement and management rules; the existing laptop flex convergence then places pinned, flex-tail, and stack-app windows as before.

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

Extend and normalize classification fields while preserving layout queries. Query spaces to translate the serialized Mission Control index to stable SpaceID. Test raw decoding, normalization, differing index/ID, floating/managed state, missing ID, and query errors.

**Interfaces:** `QueryWindow(ctx context.Context, id int) (wm.Window, bool, error)`; add `Role`, `Subrole`, `Root`, `Level`, `CanMove`, `CanResize`, `Floating`, and management state to the query model; resolve SpaceID from a fresh spaces mapping.

### T2 — Make rule installation topology-aware and add-only

With multiple displays, install catch-all manage-off and no destination effects. With one display, restore the current laptop placement, manage-standard, and exception rule set. Never apply rules to existing windows. Add the desk-to-laptop handoff before laptop convergence. Test laptop pinned/flex-tail/stack apps, desk arrivals across unplug, and unchanged populated BSP state through Rules plus driver.

**Interfaces:** `Rules(ctx, profile, driver)` selects rules by connected display count and uses add-only rule operations; no `ops.Apply` / `rule --apply`. Keep `wm.Rule` unchanged.

### T3 — Add immediate placement with readiness, locking, and focus verification

Parse ID-bearing arrivals. Wait for display quiet and cascade completion; serialize with cascades; acquire locks and guard before the authoritative query; re-query after any wait; then classify, target, move, focus, verify, and conditionally unfloat. Test quiet-window and in-Rules arrivals, manual lock release, guard expiry, an already-managed arrival, manual apply moving and un-floating a pending window, failed verification, and a laptop-stack target with populated G9 geometry.

**Interfaces:** `window_created <positive-id>`; classifier and target resolver; pending IDs; shared serialization; bounded lock/guard wait; `MoveWindowToSpace`, `FocusWindow`, active-space query, and conditional `ToggleFloat`. Other socket events remain one-token lines. No flex debounce.

### T4 — Recover floating windows after startup cascade

Keep full startup/wake reclamation unchanged. After it completes, classify eligible floating tile-type windows not on reclaimed slots and apply desk-mode placement. On desk-to-laptop transition, run the laptop handoff before laptop convergence. Test recovery and normal layout reclamation.

**Interfaces:** `WakeRoutine` retains `RunDisplayCascade`; reuse T3's classifier and placement operations.

## Global Constraints

- Desk arrivals have a best-effort no-reshuffle contract: failed active-space verification leaves the arrival floating and changes no existing geometry. A space switch between verification and unfloat remains a documented race.
- Startup/wake cascades remain explicit full layout reclamation with today's behavior.
- In desk mode, install catch-all manage-off. In laptop-only mode, retain today's placement, manage-standard, and exception rules.
- Desk-to-laptop transition hands off surviving floating tile-type arrivals before laptop convergence.
- Preserve the 3-second display quiet interval, do not add flex debounce to arrivals, and keep event-only lines for non-arrival signals.
- Reuse existing daemon, WM, lock, and guard patterns. Do not add broad polling, a profile abstraction, or AXUnknown handling.
- Use red-green tests. Format, lint, and test touched Go packages after all slices land.

## Tasks

- [ ] **T1 — Targeted query model.** Owner: `implement-go`. **Interfaces:** `QueryWindow(ctx, id) (wm.Window, bool, error)` and normalized classification, floating, management, and stable SpaceID fields. Tests: decoding, differing index/ID, managed/floating, missing and failed lookup. Depends on none.
- [ ] **T2 — Topology-aware rules.** Owner: `implement-go`. **Interfaces:** add-only `Rules(ctx, profile, driver)`. Tests: laptop old rule set, desk catch-all rule, no bulk apply, unchanged populated BSP, desk-to-laptop handoff for pinned/flex-tail/stack apps and unplugged arrival. Depends on none.
- [ ] **T3 — Immediate desk arrivals.** Owner: `implement-go`. **Interfaces:** ID event, display readiness, shared serialization, locks/guard before query, classifier/target, focus verification, conditional unfloat. Tests: quiet window, cascade overlap, contention, already-managed/manual-apply result, failed verification, stale ID, and geometry. Depends on T1 and T2.
- [ ] **T4 — Startup recovery.** Owner: `implement-go`. **Interfaces:** reuse T3 policy after full `WakeRoutine` cascade. Tests: eligible floating windows not on reclaimed slots in desk mode and unchanged full-reclamation behavior. Depends on T3.

T1–T4 are implementation slices of one coherent feature and MUST land together in one PR. None is independently shippable.
