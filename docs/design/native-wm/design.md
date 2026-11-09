# Design: tess is the macOS window manager

Parent: RIG-8858. This record supersedes draft tessera #76 (`event-driven.md`, RIG-8692). It keeps #76's fact inventory and drops its approach of waiting on yabai. Evidence, the binding table and the private-API list are in [evidence.md](evidence.md).

**yabai sources.** Matt runs the Christian-SC26 fork of yabai (Homebrew `Christian-SC26/formulae/yabai`, v7.3.2; RIG-8327). Citations marked **SC26** are from that fork at `a26707a`, two commits past its v7.3.2 tag; neither commit touches a cited function. Citations marked **both** read the same in SC26 and in upstream at `dd84572`. Evidence § *yabai mechanisms* gives the source of every citation.

**Freeze.** This record freezes after Matt rules on questions 1, 3, 7, 8 and 9. N1 checks the assumptions of the chosen option. If a probe fails, the record reopens; N1 does not choose.

<!-- LENGTH §Approach: answers six required capability questions plus the migration, Space-service and actor contracts -->

## Open Questions

### 1. Workspaces: who owns native Spaces once tess tiles?

**Context:** in every option tess owns tiling: its tracks are the layout, and it moves windows with Accessibility (AX). The fork is only about native macOS Spaces. Creating, destroying and focusing a native Space works only through yabai's scripting addition, which is code injected into Dock and needs SIP partly off (evidence § *What needs the scripting addition*). Matt's AW display sits above the G9 with no free bottom corner, which is the case AeroSpace's parking does not support (evidence § *Live snapshot*). § *Workspaces: D against A* compares the two leading options on Matt's geometry.

- **A. Virtual workspaces.** tess keeps one native Space per display. A workspace is a tess record. Switching moves windows on screen and parks the rest in a corner. No scripting addition, no SIP change. Costs: AW windows must park on another display, which AeroSpace does not support and V0 must prove; tess must restore a workspace when Cmd-Tab or the Dock activates a parked window; a crash leaves windows parked; Mission Control shows parked windows small; no Space animation; the migration destroys native Spaces and its reverse needs the scripting addition.
- **B. Native Spaces through yabai's scripting addition, loaded by tess.** tess speaks the `yabai.osax` socket protocol itself. Costs: SIP stays off; tess tracks the osax protocol version (`2.1.37` in SC26) and a payload that needs a patch for most macOS point releases.
- **C. Native Spaces through tess's own scripting addition.** As B, but tess owns the injected payload. Highest cost: arm64e code injection and per-release byte patterns become tess maintenance.
- **D. Hybrid: yabai keeps native Spaces, tess tiles.** yabai runs a restricted configuration (§ *yabai's restricted configuration*): every Space `float`, no rules, no signals that mutate windows. tess calls yabai's Space commands through a new `wm.SpaceService` (N4). Costs: SIP stays off and yabai's payload patches remain; yabai's mouse gestures need an owner (question 8); a focus on another Space still waits for the Space switch, with a verified postcondition and one budget; two processes run.

**Recommendation:** D, because it removes the split tree, insert arms, staging and the stack cap, keeps Spaces, Mission Control and native fullscreen as Matt uses them, needs no parking on the AW, and has a defined reverse. V0 researches A on its own schedule and does not block D.

**Blocks:** N10 to N13, and V1 to V3.

### 2. If tess removes yabai, does Matt re-enable SIP?

**Context:** depends on question 1. Only option A removes yabai; under D, SIP stays partly off. Re-enabling SIP is a Recovery-mode step and cannot be undone without another reboot into Recovery.

- **A. Re-enable SIP after V3 and a two-week soak with no rollback.** Closes the attack surface that the partial SIP disable opens. Rollback to yabai then loses Space create and destroy until SIP is off again.
- **B. Leave SIP as it is.** Rollback stays complete. The machine keeps the weaker protection with no remaining user.

**Recommendation:** A, because after the soak the rollback is unlikely and SIP protects every process, not only the WM.

**Blocks:** the downstream SIP step in V3 only.

### 3. Which signing identity keeps the Accessibility grant across nix rebuilds?

**Context:** every option needs the AX permission for tess. macOS keys that grant to the binary's designated requirement. An ad-hoc signature has no stable identity, so each rebuild looks like a new binary and the grant is lost [INFERENCE until N2 measures it] (evidence § *TCC*). `release-binaries.md` rules ad-hoc signing until a Developer ID exists.

- **A. A local self-signed code-signing certificate.** Matt creates it once in the login keychain. Activation copies the daemon bundle to a stable path and signs it with that identity and a fixed identifier (N2). This is the method yabai documents for HEAD builds. One manual setup step.
- **B. Wait for the Developer ID.** No local setup. The native work cannot ship until the DUNS number and Apple Developer Program membership exist.
- **C. Accept a re-grant after each rebuild.** No setup. Matt clicks through System Settings after every tess change, and tess moves nothing until he does.

**Recommendation:** A, conditional on N2's gate: two real activations keep the grant. It becomes a one-line identity change when the Developer ID arrives.

**Blocks:** N2, and the live gates of N6 onwards. Stage S does not need it: Carbon hotkeys need no permission.

### 4. What does tess do when Matt drags a tiled window with the mouse?

**Context:** a plain drag is a title-bar drag with no modifier. tess sees it as moved events while a mouse button is down (§ *Desired and observed state*). Modifier gestures are question 8.

- **A. Snap back, or move across displays.** A tiled window dropped on its own display returns to its slot. A tiled window dropped on another display joins that display's active workspace as a new track at the drop side.
- **B. Swap on drop.** Dropping onto another tiled window swaps the two, as `mouse_drop_action swap` does. Needs hit-testing of the drop point against slot frames.
- **C. Float on drag.** A dragged tiled window becomes floating where it was dropped.

**Recommendation:** A, because it keeps the layout the source of truth and needs no new gesture, while still letting a drag move a window to another monitor.

**Blocks:** the drag handling in N7.

### 5. Which tree-only verbs are deleted and which are remapped when tess tiles?

**Context:** six verbs and one raw binding exist only because yabai keeps a binary split tree: `insert`, `warp`, `reset-splits`, `balance`, `columns`, `space bsp`, and the `cmd + alt - d` drop toggle. Stage S keeps every one of them working on yabai unchanged (§ *Hotkeys*). The remap happens when tess tiles, where a workspace is a list of tracks with weights. The full table is in evidence § *Binding migration*.

- **A. Remap where a track meaning exists, delete the rest.** `insert stack|east` sets the arrival target of the focused workspace (a queryable field); `warp east` becomes `unstack east`; `balance` and `columns` set equal weights; `space bsp` becomes `space columns`; `reset-splits` and the drop toggle are deleted.
- **B. Delete all of them.** Smaller command set. Matt loses keys he uses today and must learn `snap`.
- **C. Keep the names as aliases forever.** No muscle-memory change. The CLI keeps names that describe a tree that no longer exists.

**Recommendation:** A, because it keeps muscle memory for the keys that still make sense and removes only the two whose behaviour cannot exist without a tree.

**Blocks:** N11 (remaps) and N13 (deletions).

### 6. Does the Windows build also leave its third-party WM later?

**Context:** `windows-backend/design.md` drives komorebi behind `wm.Driver`. This record keeps that seam and does not design a Win32 backend.

- **A. Keep komorebi.** No Windows work here. The two platforms differ: macOS owns its tiling, Windows drives a WM.
- **B. Plan a native Win32 backend on the same `native.Platform` seam later.** One model on both platforms. A second WM to own.

**Recommendation:** A for now, because Windows has no user today; revisit when it does. This is a non-load-bearing deferral.

### 7. How does Go call the macOS frameworks: cgo or purego?

**Context:** tess calls Accessibility, CoreFoundation run loops, NSWorkspace notifications, CoreGraphics display callbacks and Carbon events. Several of these call back into Go from a C thread. `tess-app.md` § *Global Constraints* says "The `tess` module stays cgo-free", and the flake builds `tess` with `CGO_ENABLED = "0"`. [purego](https://github.com/ebitengine/purego) (about 4,100 stars, Apache-2.0) calls C without cgo; its `RegisterFunc` doc says "there is no way to verify that fptr matches the C function", and its `NewCallback` doc says "Only a limited number of callbacks may be created in a single Go process, and any memory allocated for these callbacks is never released". tess needs a fixed set of about ten callbacks, so the limit alone does not decide this. The only purego AX and hotkey wrappers found ([go-macos/accessibility](https://github.com/go-macos/accessibility), [go-macos/hotkey](https://github.com/go-macos/hotkey)) have no adoption yet.

- **A. cgo, in Darwin-only packages.** Small C and Objective-C files compiled against the macOS SDK, with signatures checked by the compiler. Linux and Windows builds stay `CGO_ENABLED=0`. Choosing A supersedes the `tess-app.md` constraint for darwin builds of `tess`; S2 carries the build and packaging work.
- **B. purego.** `tess` stays cgo-free on every platform and the frozen constraint holds. Every C signature, block and Objective-C class is declared by hand in Go and checked only at run time.

**Recommendation:** A, because the callback-heavy run-loop code is the part most likely to crash, and compile-time signature checks are worth one darwin-only build change.

**Blocks:** S2.

### 8. Under D, who owns yabai's modifier mouse gestures?

**Context:** depends on question 1 (D). yabai's mouse tap stays on in every configuration. With the modifier held, a left drag moves a window and a right drag resizes it. Neither checks whether the window is tiled: `MOUSE_DRAGGED` in `src/event_loop.c` (both) calls `window_manager_move_window(g_mouse_state.window, new_point.x, new_point.y)` and `window_manager_resize_window_relative_internal(...)`. `mouse_modifier` accepts only `alt`, `shift`, `cmd`, `ctrl` or `fn`, and `mouse_action1` and `mouse_action2` accept only `move` or `resize` (`COMMAND_CONFIG_MOUSE_MOD` parser branch in `src/message.c`, SC26). No supported setting turns the gestures off. Matt's yabairc sets `mouse_modifier fn` and says mouse window management is meant to be off. On a `float` Space yabai's release handler does nothing: `MOUSE_UP` (SC26) exits at `if (!src_view) goto err;` because the window is not tiled.

- **A. tess owns all drags; disable yabai's gestures by patch.** A small patch on the fork (one argument constant and one parser branch in `src/message.c`) adds `mouse_modifier none`, which sets `MOUSE_MOD_NONE`. `mouse_mod_from_cgflags` never produces that value, so no gesture starts. Invariant: zero yabai frame writes. Costs: a patch on a fork Matt does not own, carried in a nix overlay until the fork maintainer merges it, and rebased on each fork release.
- **B. yabai keeps modifier move and resize; tess yields during the gesture.** When a mouse button goes down with the `fn` flag held over a managed window, tess stops writing that window. When every button is up, tess reads the frame. A changed position follows question 4; a changed size on a tiled window snaps back. Invariant: yabai writes frames only to the window under an `fn` gesture, between mouse-down and mouse-up. No patch.
- **C. No modifier gestures at all.** As A, but tess does not add its own replacement for modifier gestures later either. The patch is the same as A's; the difference is product scope.

**Recommendation:** B, because it needs no patch on a third-party fork, keeps today's behaviour on the rare `fn` drag, and has an invariant N1 can test.

**Blocks:** N1's mouse probe, N7's gesture handling and N9.

### 9. Do the resize arrows keep key repeat?

**Context:** under skhd, holding `shift + cmd + alt - left` repeats the 100 px resize. Carbon `RegisterEventHotKey` sends one pressed event and one released event per press (`kEventHotKeyPressed`, `kEventHotKeyReleased`), with no repeat.

- **A. Accept the loss.** One press is one 100 px step. No code. A visible change from today on four bindings.
- **B. Repeat in tess.** A binding can set `repeat: true`. On press tess runs the command, waits the system's initial key-repeat delay (`NSEvent.keyRepeatDelay`), then runs it again every `NSEvent.keyRepeatInterval` until the released event. About 60 lines, tested with `synctest`.

**Recommendation:** B, because it keeps the stage S promise that every binding behaves as today, at a small cost.

**Blocks:** S3 and S4.

## Direction

**Product fit.** Matt's ruling of 2026-11-09 asks to port everything into tess, move the keybinds into tess, align with the native app, and replace yabai as the backend, "not considering how we implemented it before". RIG-8858 tracks it. The frozen records already put tess's model at the centre: arrangements and topologies (`tess-app.md`, `grid-layouts.md`), named slots (`window-slot-memory.md`, `deskSlots`), the laptop flex model (`laptop-flex-spaces.md`) and arrival placement (`new-window-placement.md`). Today each of these is translated into yabai's split tree by `internal/wm/yabai`, and most of the pain in RIG-8691, #73 and #75 sits in that translation. This record removes the translation.

**Scope.** Repo `mattwilkinsonn/tessera` for the code. Matt's config (skhd, yabai, sketchybar, launchd agents and the profile) changes downstream in his nix repo, named here only as the downstream consumer. The Windows backend keeps working through the same seam (question 6). No hosted service and no usage-billed resource.

**Prior art.**

- [yabai](https://github.com/koekeishiya/yabai) (MIT) and the [Christian-SC26 fork](https://github.com/Christian-SC26/yabai) Matt runs. The reference for how macOS window handling works: AX observers per app (`application_observe` in `src/application.c`), frame writes (`window_manager_set_window_frame` in `src/window_manager.c`) and focus (`window_manager_focus_window_with_raise`), all **both**. Its Space operations can stay behind a narrow adapter while tess owns tiling: `space_manager_add_space` in `src/space_manager.c` returns `SPACE_OP_ERROR_SCRIPTING_ADDITION` when the scripting addition fails (**both**). A `float` view does not tile: `space_manager_tile_window_on_space_with_insertion_point` starts with `if (view->layout == VIEW_FLOAT) return view;`, and `window_manager_handle_display_add_and_remove` guards its reflow with `if (view->layout != VIEW_FLOAT) {` (**both**). Rules, the mouse tap and focus side effects stay active (§ *yabai's restricted configuration*, question 8).
- [AeroSpace](https://github.com/nikitabobko/AeroSpace). Its README says it uses "only a single private API to get window ID of accessibility object `_AXUIElementGetWindow`" and "will never require you to disable SIP". Its [guide](https://nikitabobko.github.io/AeroSpace/guide#emulation-of-virtual-workspaces) places inactive windows "outside the visible area of the screen, in the bottom right or left corner", requires that "every monitor has free space in the bottom right or left corner", and restores on activation: "Once you switch back to the workspace, (e.g. by the means of workspace command, or cmd + tab) windows are placed back to the visible area of the screen." Its hotkeys use [soffes/HotKey](https://github.com/soffes/HotKey), which calls Carbon `RegisterEventHotKey`. Borrow the hotkey API and the per-app AX threads; under option A, also the park and activation-restore rules. Matt's AW is outside the arrangement AeroSpace supports.
- [Amethyst](https://github.com/ianyh/Amethyst). Public AX on top of native Spaces. Its "throw window to space" broke on macOS 15 ([#1662](https://github.com/ianyh/Amethyst/issues/1662), closed not planned); the maintainer states: "The mechanism by which we do it is no longer possible, it seems." The thread also records "failed to set compat aside id: CGError(rawValue: 717863)". Keep the warning: native Space moves without private APIs are not supported. It does not show that public AX alone can move windows between native Spaces, nor that delegating supported moves to yabai means keeping yabai's tiling tree.
- [Rectangle](https://github.com/rxhanson/Rectangle). Hotkeys only, through [MASShortcut](https://github.com/rxhanson/MASShortcut), whose `MASHotKey.m` calls `RegisterEventHotKey`. Borrow: Carbon hotkeys need no permission prompt.
- [skhd](https://github.com/koekeishiya/skhd), today's hotkey daemon, uses a `CGEventTap` (`event_tap_begin` in `src/event_tap.c`), which needs Accessibility. Avoid for the hotkey path.

**Lifetime.** Years: this is the window manager of Matt's daily machine. The design is sized to that: each stage ships alone, every migration step is recorded and has a defined reverse, and the private-API list in tess stays at one symbol.

**Simpler option.** Keep yabai, finish #76 (events instead of sleeps) and move only the hotkeys and command routing into the daemon. That is stage S of this plan, and it ships value alone. It does not remove insert arms, the stack cap or staging floats, because those come from driving yabai's split tree from outside. The hybrid (question 1, D) is the next smallest step that does: it keeps yabai for native Spaces only.

## Problem / Intent

tess decides layouts in its own terms but realizes them by issuing yabai commands against yabai's split tree and native Spaces, then sleeping or polling until yabai catches up. The mismatch causes the known failures: insert arms that cannot be queried, `ErrSpaceFull`, focus waits that time out (Zen), rule-managed windows, floating staging windows stranded by a failed apply, and fixed sleeps. Make tess the window manager on macOS: its own workspace and track model is the state, it moves and focuses windows itself, it owns the hotkeys, and every command runs in its daemon, with a staged cutover that never leaves Matt's desktop broken.

## Approach

### End state and stages

On macOS, tess owns window tracking, tiling, focus, hotkeys and the state feed for the app and sketchybar. Under question 1's recommendation, yabai remains only as the native-Space service. Each stage ships alone and leaves a working desktop.

| Stage | Tasks | What changes | yabai | skhd | Rollback |
| --- | --- | --- | --- | --- | --- |
| S | S1–S4 | Every command runs in the daemon. tess registers the hotkeys. Every binding keeps today's behaviour, including repeat (question 9). | tiles | **removed** at S4 | re-enable the skhd agent |
| N-build | N1–N9 | Space service, model, platform layer and native driver; observe mode; actuation gate. Nothing moves on the daily desktop. | tiles | gone | none needed |
| N-hybrid | N10–N12 | tess owns yabai's configuration; the recorded migration restricts yabai and tess tiles. | Spaces only | gone | `tess migrate --reverse` |
| N-clean | N13 | Staging, insert arms, BSP verbs and yabai tiling code are deleted. | Spaces only | gone | revert the release |
| V | V0–V3 | V0 researches A at any time. V1–V3 run only if question 1 later picks A; yabai is removed at V3. | **removed** at V3 | gone | `tess migrate --reverse` until V3 |

Two WMs never write the same window at the same time. In N-hybrid, yabai's restricted configuration stops its tiling, rules and mutating signals; question 8 names the one gesture exception. N1 tests this invariant.

### The window model is tess's state

A new package, `internal/wm/native`, holds a `Model` with two parts.

- **Lifecycle** is committed state: which windows exist, their pid, native Space, observed frame, apply status, AX attachment and tombstones of closed windows. Only platform events, Space-service notices and operation results change it. Lifecycle is never part of a job's working copy and is never discarded.
- **Desired** is the layout: workspace membership, tracks, weights, front windows, modes and arrival targets. Jobs edit it on a working copy (§ *Job transactions*). The screen is made to match it.

The parts hold these records:

- **Display:** keyed by the UUID from `CGDisplayCreateUUIDFromDisplayID` (`display_uuid` in `src/display.c`, **both**), so profile `deviceId` values and `ResolveDisplay` keep working.
- **Workspace:** an ID, a label, its display, a kind (`columns`, `rows` or `stack`), an ordered list of tracks, one weight per track, a front window per track, and an arrival target. Under D a workspace is bound to one native Space ID; under A it is a tess record only.
- **Window:** a CGWindowID from `_AXUIElementGetWindow`, which is the number yabai reports, so `window-slots.json` survives the switch. Its mode is tiled (in a track), floating (own frame), excluded (not managed) or suspended (native fullscreen, or on a native Space tess does not manage).
- **Layout** is a pure function: `Frames(area wm.Frame, ws Workspace) map[int]wm.Frame`. The area is the display frame minus the bar inset, 32 pt at the top, the same as `external_bar all:32:0` in Matt's yabairc today. Every window in a track gets the track's frame. The front window is raised.

The native backend implements `wm.Driver` against this model and a `wm.SpaceService` (§ *The Space service*).

| `wm.Driver` call | Native meaning |
| --- | --- |
| `CreateSpace`, `DestroySpace`, `LabelSpace`, `MoveSpaceToIndex` | D: Space-service call, an irreversible checkpoint; then bind or unbind the workspace record. A: edit the record only. |
| `MoveWindowToSpace` | Move desired membership. D: also a Space-service call and an irreversible checkpoint. |
| `RealizeSpaceLayout(target)` | Set the workspace's kind, tracks and weights to `target`. |
| `SetSpaceLayout`, `BalanceSpace` | Set the kind; set equal weights. |
| `ToggleFloat`, `StackOnto`, `SwapWindows`, `FocusWindowDir`, `ResizeWindow` | Edit tracks or weights; resolve direction from the computed frames. |
| `ArmInsert(dir)` | Set the workspace's arrival target, which can be queried. |
| `FocusWindow` | Checkpoint, then focus per § *Focus*. |
| `Query*` | Read desired and lifecycle state together. |

### What the model deletes, and what must change first

- **Insert arms.** The arrival target is a field in the model, not a one-shot toggle inside yabai.
- **`ErrSpaceFull` and the stack cap.** A track is a Go slice. The cap is enforced before the driver sees any work: `evacuateWindows` in `internal/engine/desk.go` checks `if managed >= wm.StackWindowCap {`. N3 moves the cap behind `wm.Capabilities`. The retained yabai Space calls no longer check it (N4).
- **Staging floats.** `DeskPlan` floats slot owners (`FloatWindowOp{Staging: true}`) so that yabai's tree can be rebuilt in order, and `stagingPlan.before` in `internal/commands/staging.go` persists them with `effects.WriteStagedWindows` before the driver is called. N3 makes both depend on `Capabilities().NeedsStaging`.
- **Settle waits.** `SettleUnit` and the tiling sleeps in evidence § *#76 sleep sites* go, because a tiling step is a model edit confirmed by read-back. The Space sleeps (`LabelSpace`'s 50 ms, `MoveWindowToSpace`'s 100 ms, `CreateSpace`'s poll) become postcondition checks in N4.
- **yabai rules.** `Rules` in `internal/commands/orchestration.go` installs `auto:*` rules. In the hybrid tess installs no rules and the restricted configuration removes every rule. Native admission is a model decision: a window is managed when its subrole is `AXStandardWindow`, it is not native fullscreen, and it matches no entry in a new profile list `unmanaged`. The defaults reproduce today's exceptions (Finder, System Settings, 1Password, Akiflow dialogs). The laptop routing rules (`auto:code`, `auto:linear`, `auto:akiflow`, `auto:default-laptop-stack`) become arrival targets in the native `HandleWindowCreated` path.
- **BSP terms.** `SplitType`, `ToggleSplit`, `SetSplitRatio` and `LayoutBSP` have no native meaning. They are removed at N13.

### Workspaces: D against A

| Concern, on Matt's displays | D: yabai holds Spaces | A: virtual workspaces |
| --- | --- | --- |
| AW above the G9, no free bottom corner | No parking | Must park on another display; AeroSpace does not support this; V0 measures it |
| Cmd-Tab, Dock or a notification activates a hidden window | macOS switches the Space; tess attaches AX and flushes the newly visible workspace | tess must detect the activation and restore the workspace |
| Native fullscreen | Native; window is suspended in its slot | Suspended; its Space is a stray Space |
| Mission Control, Space animation, laptop flex "a new app gets a space for free" | Unchanged | Parked windows show small; no animation; flex spaces become records |
| Crash of tess | Windows stay where they were | Windows stay parked until restart or rescue |
| Focus on another Space (Zen) | Waits for the Space switch with a verified postcondition and one budget. N1 and N9 measure the success rate with a stated trial count; this record does not claim the Zen failure is fixed | No Space wait |
| Mouse gestures | Owner per question 8 | tess only |
| SIP and payload patches | Stay | Removed after V3 |
| Migration and its reverse | Restrict yabai's configuration; reverse restores it (§ *Migration*) | Destroy native Spaces; reverse recreates them through the scripting addition |

D removes every tiling failure named in § *Problem / Intent* and turns the Zen wait into a measured, bounded wait. A also retires SIP and the payload, but adds parking on an arrangement that the one mature implementation does not support.

### yabai's restricted configuration

`float` on a view stops tiling and display reflow, but not every path that moves a window. These paths stay active and the restricted configuration closes each one (all **both** unless marked):

- **New Spaces.** `view_create` in `src/view.c` gives a user Space the global layout unless the Space has the `VIEW_LAYOUT` flag (`view->layout = g_space_manager.layout;`). The restricted configuration sets the global `layout float`, so a Space created by Mission Control or by a yabai restart is `float` too. It also sets `float` on each Space that has its own layout, because `--layout` sets `VIEW_LAYOUT` (`COMMAND_SPACE_LAYOUT` branch in `src/message.c`, SC26).
- **Rules.** `window_manager_apply_rule_effects_to_window` in `src/window_manager.c` moves windows across Spaces (`window_manager_send_window_to_space(sm, wm, window, sid, true);`) and applies grids (`window_manager_apply_grid(...)`) whatever the layout. The restricted configuration removes every rule.
- **Signals.** Any signal action that calls a mutating yabai command is removed. Two kinds stay: tess's own `tessera-` signals, which `RegisterSignals` in `internal/daemon/wiring.go` adds and whose actions only notify the daemon socket or trigger sketchybar, and `dock_did_restart` → `sudo yabai --load-sa`, which loads the payload and moves no window.
- **Focus side effects.** `window_manager_send_window_to_space` focuses the next window on the source Space when the moved window had focus: `window_manager_focus_window_with_raise(&next->application->psn, next->id, next->ref);`. This is expected. After a delegated move, tess focuses the window the job asked for; if the job asked for none, tess records the new focus in lifecycle state and does not fight it.
- **Settings.** `mouse_follows_focus off`, `focus_follows_mouse off`, `auto_balance off`, `window_origin_display default`. The mouse tap is question 8.

**tess owns yabai's configuration (N10).** The downstream yabairc shrinks to loading the payload and running `tess yabai-config apply`, then `tess wake`. That command applies one of two sets: `legacy`, a profile block that holds today's yabairc settings, or `restricted`, a fixed set in tess. It picks the set from the migration state, not the profile. So a yabai restart re-applies the right set before any `layout bsp` can retile, and migrating needs no nix rebuild. With no migration state, it applies `legacy`.

### The Space service

`wm.SpaceOps` has no focus or window-to-Space call, and today's yabai driver hides fixed waits inside them. N4 adds a separate interface, `wm.SpaceService`, that the native driver uses. The yabai driver's Space methods call it too, so the fixed sleeps go on both backends.

**Contract for every operation.** The service subscribes to its notices before it runs the command. Right after the command it checks the postcondition once, which covers both an already-satisfied target and a notice that arrived early. It then waits for a relevant notice or a poll tick, whichever comes first, within the operation's budget. A met postcondition returns nil. A spent budget returns `*wm.SpaceOpError{Op, Outcome: wm.OutcomeUnknown}`. The caller then resolves the outcome with `Snapshot` (§ *Job transactions*).

| Operation | Postcondition (from `Snapshot`) | Notice | Poll fallback | Budget |
| --- | --- | --- | --- | --- |
| `CreateSpace(display)` | A user Space on `display` that was not in the before-set | `space_created` | 50 ms | 1 s |
| `DestroySpace(sid)` | `sid` absent; already met if absent before | `space_destroyed` | 50 ms | 1 s |
| `LabelSpace(sid, label)` | `sid` has `label` | none exists | none: `space_manager_set_label_for_space` runs inside the command handler (`COMMAND_SPACE_LABEL` branch, SC26), so one check after return | 0 |
| `MoveSpace(sid, index)` | `sid` at `index` on its display | none exists | 50 ms | 1 s |
| `FocusSpace(sid)` | `sid` is the active Space of its display; already met if visible | `space_changed` and `NSWorkspaceActiveSpaceDidChangeNotification` | 50 ms | 1 s |
| `MoveWindowToSpace(win, sid)` | window's Space is `sid`; already met before the call | none: the move is asynchronous (`SLSPerformAsynchronousBridgedWindowManagementOperation` in `space_manager_move_window_to_space`, **both**) and the active Space does not change | 25 ms | 500 ms |

The yabai signal list has `space_created`, `space_destroyed` and `space_changed`, and no signal for labels or Space order (`signal_type_str` in `src/event_signal.h`, SC26). The budgets are first values; N9 records each operation's measured time and N12 tunes them.

**Discovery of hidden windows.** Public AX does not return windows on inactive Spaces. yabai notes this in `window_manager_add_existing_application_windows` ("MacOS API does not return AXUIElementRef of windows on inactive spaces") and brute-forces `_AXUIElementCreateWithRemoteToken` (**both**). tess does not copy that. `Snapshot` reads yabai's window query, which already covers every Space, and gives tess full metadata for every window: ID, pid, Space, display, frame, role, subrole, app, title, minimized and hidden. AX attaches lazily: when a Space becomes visible, the app's worker reads its `kAXWindowsAttribute` and matches elements to IDs with `_AXUIElementGetWindow`. A window with no AX element has status `hidden` and gets no writes. Its membership is still complete, so the planners see every window.

### Job transactions

A job is one CLI request, hotkey, Hub cascade or arrival. It edits a working copy of desired state.

- **Checkpoint.** A checkpoint commits the working copy and flushes it. Checkpoints happen at job end, before every `FocusWindow`, and before every Space-service call.
- **Commit merges with lifecycle state.** Lifecycle wins on existence: a working-copy edit that names a tombstoned window is dropped and logged, so a closed window is never brought back. A window that appeared after the copy was taken is added from lifecycle state at its arrival target. The working copy wins on layout.
- **Abort.** On error or cancel, edits since the last checkpoint are discarded, in-flight worker operations of the job are fenced, and nothing more is flushed. `PreserveFocus` in `internal/commands/focus.go` restores focus after the discard.
- **Space operations are irreversible checkpoints.** A Space-service call changes macOS, and a later abort cannot undo it. Its result goes into lifecycle state at once. Desired state follows reality: after a successful `MoveWindowToSpace`, the window's membership is its new Space even if the job later aborts. One compensation exists: a `CreateSpace` whose job aborts before anything is placed on the new Space destroys it again. An `OutcomeUnknown` is resolved by `Snapshot`; the resolved state goes into lifecycle state, and the job returns an error that names the resolved outcome.

The yabai backend has no working copy and keeps today's behaviour.

### Desired and observed state

Each window's lifecycle state carries an observed frame, its read time and an apply status: `ok`, `pending`, `constrained` (position matches, size differs, as with a terminal that snaps to cells), `refused` (position or visibility does not match after a retry), `hidden` (no AX element yet), `gesture` (question 8) or `suspended`.

**Operations and fencing.** Every request to a per-app worker carries an operation ID from one counter, and every frame write carries the window's write generation. The actor accepts a result only if its operation ID is the latest one issued for that window and was not fenced. A fenced or older result is dropped. If the window then has no operation in flight, the actor asks for a fresh read-back. A result that arrives after its waiter timed out is therefore never applied as current state.

**Pending writes.** A frame write has a deadline of 500 ms [INFERENCE: tuned in N9]. Moved and resized events for that window before the deadline schedule a read-back and are never classified as a drag. Several events in one run-loop cycle coalesce: only the latest read-back counts. A matching read-back sets `ok`. At the deadline a mismatch gets one more write; a second mismatch sets `constrained` or `refused`. An AX write returns success even when the app ignores it; the [go-macos/accessibility](https://github.com/go-macos/accessibility) README measured this: "A write to `kAXPositionAttribute` returns `kAXErrorSuccess` whether or not the application honours it."

**Moves with no pending write.** If a mouse button went down with `fn` held over the window, the window is in `gesture` until every button is up (question 8). If a button is down without `fn`, the move is a plain drag (question 4). Otherwise it is an OS relocation, such as a clamp after a display change. An OS relocation updates lifecycle state only and does not change membership; the next flush re-asserts the desired frame once.

**Reporting.** `Flush` returns `*wm.ApplyError{Refused []int}` when a visibility or focus change was refused; a `constrained` size is not an error. The state stream carries each window's status, and a workspace with a `refused` window reports `fresh: false`. `tess query` shows the same.

**The actor never blocks.** One actor goroutine owns the `Model`. It selects over job requests, worker results, platform events, Space-service notices and timers. A driver call from a job goroutine is a request with a reply channel. Model edits reply at once. `Flush`, `FocusWindow` and Space-service calls register a waiter and send work out. The actor keeps processing events and resolves the waiter only when its postcondition holds in lifecycle state, or when the budget ends. cgo callbacks only send small values (pid, window ID, event kind) to channels.

### Focus

`FocusWindow(id)` resolves on one postcondition: lifecycle state shows `id` as the focused window, from the AX focused-window event or a read of `kAXFocusedWindowAttribute` on the frontmost app. A worker that returns success does not complete the focus. Steps, all inside one 1 s budget:

1. If the window's Space is hidden (D), `FocusSpace` on the Space service.
2. Attach AX if needed, then flush the now-visible workspace.
3. `AXMain` true, `AXRaise`, then `NSRunningApplication activateWithOptions`.

A miss returns `wm.ErrFocusTimeout` with the focused window, the target and its app. This replaces the 50 ms poll in `unfloatOne` (`internal/wm/yabai/driver.go`). yabai focuses with private SkyLight calls (`window_manager_focus_window_with_raise`, **both**); tess uses AeroSpace's public path, and N1 measures whether it is enough for Zen.

### Lifecycle contract

Desired membership changes only through: a tess command or hotkey, arrival placement, a plain drag (question 4), a Space-service result, a window closing, and display loss handled by the existing desk-to-laptop cascade. Every other event changes lifecycle state only.

| Event | D | A |
| --- | --- | --- |
| Cmd-Tab, Dock or notification activates a window on a hidden workspace | macOS switches the Space; tess attaches AX and flushes the dirty workspace | tess makes the window's workspace active and un-parks it |
| Native fullscreen enter, exit | `suspended` in its slot; on exit, the next flush returns it | same; the fullscreen Space is a stray Space |
| A native Space tess did not create (user or app) | `float` by the global setting; tess binds a new workspace record to it | stray Space; its windows are `suspended`; tess never destroys it |
| Window created while its Space is hidden | Snapshot or `window_created` adds it to lifecycle state; it joins that Space's workspace at the arrival target, `hidden` | parked at once |
| yabai restart | yabairc runs `tess yabai-config apply` before `tess wake`; the daemon re-reads `Snapshot` | n/a after V3 |
| Dock restart | `dock_did_restart` reloads the payload; Spaces keep their IDs [INFERENCE: N9 checks] | no effect |
| Unplug with inactive workspaces | macOS moves the Spaces; the display cascade re-homes them per the arrangement | the cascade re-homes them; parked windows are re-parked after a ledger write |
| Lid close, reconnect | display UUID returns; the 3 s `DisplayQuiet` debounce runs the cascade | same, then un-parks the active workspaces |
| Daemon cold start | Snapshot gives every window on every Space; `native-state.json` gives desired membership; windows unknown to it join their Space's workspace at the arrival target; windows gone from Snapshot get tombstones | ledger first, then the same |

### Migration: recorded and reversible

**Guarantee.** The reverse gives a deterministic, usable rebuild from the profile. It does not restore the exact desktop from before the migration. yabai cannot rebuild a split tree after a layout change, because `space_manager_set_layout_for_space` calls `view_clear(view)` (**both**). SC26's `space --save-layout` and `--load-layout` serialize a tree, but `layout_store_reconstruct_node` matches leaves by app name and falls back to the first unused window (SC26), so tess does not depend on them. After a reverse:

- every window is on the native Space it was on when the reverse started;
- every Space keeps its current label, order and display;
- yabai's settings, rules and signals are the ones in the snapshot;
- `tess apply` (desk) or `tess laptop` rebuilds each profile-defined layout, and yabai tiles every other Space in its own insertion order.

**State files.** `migration.json` records the direction (`forward` or `reverse`), the target (`hybrid` or `virtual`), the current phase, and per-item progress. It is synced before each item. `migration-snapshot.json` is written once in the snapshot phase and never changed. `native-state.json` is the native model and is separate from both. Startup with an unfinished migration continues in the recorded direction, whatever the profile says. The profile's `backend` is read only when a new migration starts. When the profile and the recorded state disagree, the daemon moves nothing new, reports `fresh: false`, and names the command that settles it.

**To the hybrid (N11).**

1. **Preflight.** AX is trusted for the stable daemon identity (N2). yabai runs with the scripting addition. yabairc delegates to `tess yabai-config apply` (N10). N9 has passed. The profile is valid. No other migration is open.
2. **Snapshot.** Spaces (ID, index, label, display, layout and whether it has its own layout), windows (ID, Space, frame, floating, stack index), every setting in the restricted set, the rule list and the signal list. Written to `migration-snapshot.json`.
3. **Adopt.** One workspace per native Space. Where a profile arrangement covers the Space (desk slots, the laptop flex model), tracks and weights are the planner's target for it. Otherwise tess groups windows into tracks by geometry: windows whose left edges are within 8 pt form one track, ordered left to right; a weight is the track's width divided by the area's width; windows that share one frame form a stack, with the topmost as front. Written to `native-state.json`.
4. **Restrict.** One recorded item each: the global `layout float`; then each rule removed; then each mutating signal removed; then `float` on each Space that has its own layout.
5. **Activate.** The daemon starts the native backend, takes a `Snapshot` and flushes.

**Reverse.** `tess migrate --reverse` records `direction: reverse` first. Then, one recorded item each and in the opposite order: tess stops writing frames; each Space that still exists gets its snapshot layout back; signals and rules are added again (add-only, never `rule --apply`, as in `new-window-placement.md`); the global settings are restored. Spaces created since the snapshot inherit the restored global layout. Spaces deleted since are skipped. A display change during the reverse pauses it until the 3 s display debounce settles. Each item is idempotent, so a crash resumes the reverse. An item that fails three times stops the migration in `reverse-blocked`: tess moves nothing, yabai keeps its current settings, and `tess migrate --reverse` runs the rest. The last step runs `tess apply` or `tess laptop` for the current topology. Matt then sets `backend: "yabai"` in the profile.

**To virtual workspaces (V2, only under A).** Snapshot; seed workspaces from native Space memberships; write the ledger; park the inactive workspaces; move every window to the first native Space of its display and destroy the others through yabai; check that AX is trusted and every window is adopted; only then stop yabai. Reverse: start yabai with the scripting addition, recreate the Spaces, move windows back per the snapshot, un-park and restore labels.

### Parking and recovery (A only)

A window of an inactive workspace is parked so that only a 1 px column stays on screen, at a park corner. For each display, tess picks a bottom corner where the area below and beside it belongs to no other display. The AW has none, so its windows park at the free corner of the nearest other display (evidence § *Live snapshot*). This is outside what AeroSpace supports; V0 measures it. `native-state.json` is synced before a window is parked and holds its last visible frame.

`tess rescue` (N7, both D and A) runs without the daemon and without the profile. It reads the ledger if one exists; otherwise it finds every window less than 10 % on screen. It moves each to the centre of the nearest display. It runs as the same signed executable as the daemon, so it has the same AX grant (N2).

### Hotkeys

**API: Carbon `RegisterEventHotKey`.** It needs no permission. [go-macos/hotkey](https://github.com/go-macos/hotkey) reports that registering three combos "on macOS 26.6.2 produced no dialog and needed no grant", and Rectangle (through MASShortcut) and AeroSpace (through soffes/HotKey) use the same call. A `CGEventTap`, which skhd uses, needs the Accessibility grant. [golang.design/x/hotkey](https://github.com/golang-design/hotkey) uses an event tap on macOS, so tess does not use it. Known limits:

- macOS 15 rejects Option-only and Option+Shift combos with error -9868 ([Apple forum thread 763878](https://developer.apple.com/forums/thread/763878)). None of Matt's 50 bindings use those modifier sets.
- Carbon sends one pressed and one released event per press, with no repeat. Question 9 recommends a tess repeat for bindings with `repeat: true`.
- A registered combo is taken from every app, as skhd does today.

**Threading.** Carbon hotkey events and AX observers need a run loop. `cmd/tess` locks the main goroutine to the main thread. `tess daemon` runs `NSApplication` with the prohibited activation policy on that thread (yabai calls `NSApplicationLoad` in `yabai.c`, **both**). All Go work runs on other goroutines.

**Config in `profile.cue`.** A new `hotkeys` list:

```cue
hotkeys: [
	{key: "h", mods: ["cmd", "alt"], run: ["focus", "west"]},
	{key: "keypad1", run: ["focus-slot", "1"]},
	{key: "left", mods: ["shift", "cmd", "alt"], run: ["resize", "left"], repeat: true},
]
```

Key names cover letters, digits, arrows, `equal` and `keypad0` to `keypad9`, `keypadDecimal`, `keypadEnter`, `keypadPlus`, `keypadMinus`, `keypadDivide`, `keypadMultiply` and `keypadClear`. Each maps to one macOS virtual key code; skhd's `0x53` is `keypad1`. Profile load rejects an unknown key, a duplicate combo, and a `run` list that `cli.Parse` rejects. A press dispatches the parsed command into the daemon's job queue. A profile reload unregisters and registers the changed bindings.

**Binding transport, then semantics.** Stage S moves all 50 bindings without changing what any of them does on yabai. 41 already run `tess` and move unchanged. Nine call yabai directly and get tess verbs that run the same yabai commands:

| skhd binding | tess binding in S4 | yabai command it runs |
| --- | --- | --- |
| `cmd + alt - n`, `- p` | `stack-cycle next`, `stack-cycle prev` | `--focus stack.next`, else `stack.first` (and the reverse) |
| `cmd + alt - s` | new `stack recent` | `--stack recent` |
| `shift + cmd + alt - f` | new `toggle-float center` | `--toggle float --grid 6:6:1:1:4:4` |
| `shift + cmd + alt - arrows` | `resize left`, `right`, `up`, `down`, with `repeat: true` | `--resize left:-100:0`, else `right:-100:0` (and so on) |
| `cmd + alt - d` | new `drop-action toggle` | flips `mouse_drop_action` and posts the same notification |

The tree verbs (`insert`, `warp`, `space bsp`, `reset-splits`) keep their yabai behaviour through stage S. Question 5's remaps land with the hybrid switch (N11). `drop-action toggle` is deleted at N13 under question 5. Evidence § *Binding migration* has all 50 rows.

### Daemon and IPC

**Every command runs in the daemon.** The CLI sends its argv over the existing socket, `tessera-daemon.sock`, with #76's protocol: one line `run 1 {"argv":[…],"profile":"<path>"}`, then NDJSON frames `accepted`, `stdout`, `stderr` and `exit` (or `reject`). After `accepted` the client never runs the command itself. Hotkeys enter the same queue in-process.

**One writer.** The daemon runs one job at a time; all jobs already pass through `TopologyState.SerializeCascade`. On native, jobs run as § *Job transactions* describes, against the actor in § *Desired and observed state*.

**Fallback.** On the yabai backend a failed dial runs the command in-process, as today. On native the CLI exits 1 with `tess daemon not running`, because only the daemon holds the model. `tess rescue` and `tess yabai-config apply` are the commands that run without it.

**State for the app and the bar.** tess-app's A3 state stream (`subscribe 1`, full `state` frames) is the one feed. `daemon.StatePublisher` is specified in `tess-app.md` and is not in the code yet; N7 depends on A3 landing first. On native the actor calls `StatePublisher.Request` after each flush; the frame carries workspace membership and each window's status. A new `tess query` prints one `state` frame as JSON. Matt's sketchybar scripts call `tess query` instead of `yabai -m query` before N11. The daemon keeps triggering the sketchybar event `yabai_spaces_changed` until those scripts rename it.

### Risks, weighed against keeping yabai

| Risk | Keeping yabai | Hybrid (D) | Virtual (A) |
| --- | --- | --- | --- |
| macOS releases | Payload patches per point release (evidence § *What needs the scripting addition*) | Same payload risk, but only Space operations depend on it | One private symbol in tess, which AeroSpace also depends on |
| SIP | Partly off | Partly off | Can be turned back on (question 2) |
| A third-party fork | Matt depends on Christian-SC26's releases | Same; tess also depends on the fork's query output and Space commands | Removed after V3 |
| TCC | Stable Homebrew path | Stable identity from N2; a lost grant is detected with `AXIsProcessTrusted`: tess moves nothing and reports `fresh: false` | Same |
| Crash | Windows stay | Windows stay | Windows stay parked until restart or `tess rescue` |
| App quirks | Handled for years | tess meets them again: Electron, terminals, apps with minimum sizes. N8 finds enumeration and event gaps; N9 finds write and focus quirks before N11 | Same, plus park offsets (AeroSpace issue #527, Zoom) |
| Code to own | 0 lines, but tess fights a 20,165-line C program (upstream reference) from outside | [INFERENCE] about 3,500 lines of Go and 800 of C and Objective-C | About 1,000 more for parking, activation restore and the ledger [INFERENCE] |

The pain in RIG-8691, #73 and #75 comes from driving yabai's split tree through a CLI. The hybrid removes that cause and keeps the parts of yabai that tess cannot replace without a payload or parking. Every stage has a reverse until N13.

## Alternatives considered

- **Keep yabai and finish #76.** This is the simpler option in § *Direction*. Stage S delivers it. Rejected as the end state because it keeps the split tree.
- **Native Spaces through a scripting addition tess loads or owns.** Question 1, options B and C.
- **Virtual workspaces now.** Question 1, option A; researched in V0.
- **Fork yabai further** (RIG-8327 option B). Removes the CLI gap but keeps 20,000 lines of C, the payload and the BSP model. Rejected; question 8 A is the only patch this record considers.
- **Run AeroSpace as the backend.** It has its own tree, not tess's tracks, and no client API for tess's slot and arrangement model. tess would drive a second WM from outside again. Rejected.
- **CGEventTap hotkeys.** Needs the Accessibility grant and receives every key event. Rejected for Carbon.
- **purego instead of cgo.** Question 7, option B.

## Global Constraints

- Go 1.27.3 as `go.mod` pins. This record supersedes the `tess-app.md` § *Global Constraints* rule "The `tess` module stays cgo-free" for darwin builds of `tess` only (question 7). cgo lives only in files with `//go:build darwin` under `internal/wm/native/darwin` and `internal/hotkey/darwin`. Linux and Windows builds stay `CGO_ENABLED=0`. The module gains no new Go module.
- Private API in tess: `_AXUIElementGetWindow` only, resolved with `dlsym` at start; a missing symbol stops the native backend with a clear error. tess loads no scripting addition and writes no SkyLight state.
- One writer: only the actor goroutine changes the `Model`. The actor never blocks on a worker or an event. cgo callbacks send values to channels and never call Go code that takes a lock.
- Lifecycle state is never discarded by an abort. Every worker result carries an operation ID; stale results are dropped. A waiter completes only on its postcondition.
- Every AX element has a 250 ms messaging timeout. Every frame write is read back; status is reported, never assumed.
- Two WMs never write the same window at the same time. In the hybrid, yabai runs the restricted configuration, and only the gesture owner of question 8 may write during a gesture.
- Every Space-service call subscribes before it commands, checks its postcondition, and has a budget. No fixed sleep remains in a Space operation.
- Every migration item is recorded in `migration.json` before it runs. The direction is recorded apart from the profile. The snapshot file is never changed after it is written.
- The AX grant belongs to one signed executable at a stable path. The daemon and `tess rescue` both run it.
- The `wm` seam stays: the engine, the commands and the komorebi backend see `wm.Driver`. Pure logic (layout, park corners, model edits, migration phases, adoption grouping) has no cgo and is tested on every OS.
- yabai claims cite SC26 or say **both**; a claim true only in the upstream reference is not used for the hybrid.
- Tests use `gotest.tools/v3` and `synctest`; no `time.Sleep`. Errors are wrapped with `%w`. Comments are 1–2 lines with no issue IDs.
- Public repo: name Matt's config only as the downstream consumer. No private paths or hosts.
- No usage-billed resource is added.

## Plan

### S1 — Every command through the daemon

Interfaces: #76 T5's protocol, unchanged: `run 1 {"argv":[…],"profile":"<path>"}`; frames `{"v":1,"accepted":true}`, `stdout`, `stderr`, `exit`, `reject`. `daemon.Options.PrepareRequest func(argv []string) (daemon.Job, error)`, built in `cmd/tess/main.go` from `cli.Parse` and `commands.Run`. `cli.Deps.Dispatch func(ctx context.Context, argv []string, stdout, stderr io.Writer) (int, bool, error)`; `false` means fall back in-process.

Gate: `net.Pipe` tests for each frame kind; a reject and a dial failure fall back; a stream that ends after `accepted` exits 1 with `tess: daemon request outcome unknown`; a request and a Hub cascade serialize through `SerializeCascade`.

### S2 — Darwin cgo build and packaging

Interfaces: `flake.nix` builds `tess` on `aarch64-darwin` with `CGO_ENABLED = "1"` and the darwin SDK frameworks (ApplicationServices, AppKit, Carbon, CoreGraphics); other systems keep `"0"`. The release tarball contract in `release-binaries.md` is unchanged. `cmd/tess` gains the main-thread lock and the `NSApplication` run loop for `tess daemon` in a darwin file.

Gate: in the `nix` CI job, `otool -L result/bin/tess` lists only `/usr/lib` and `/System/Library` paths, never `/nix/store`; the unpacked release tarball runs `tess --version` on the runner outside Nix; `codesign --verify --strict` passes; `GOOS=linux` and `GOOS=windows` builds with `CGO_ENABLED=0` pass.

### S3 — Hotkeys in the daemon

Interfaces: `config.Hotkey{Key string; Mods []string; Run []string; Repeat bool}` and `#Hotkey` in `schema.cue`. `internal/hotkey` (portable): `type Combo struct{Key uint16; Mods Mods}`, `Parse(h config.Hotkey) (Combo, error)`, `Diff(old, new []Binding) (add, remove []Binding)`, and `Repeater` (question 9): `func (r *Repeater) Handle(ctx context.Context, ev Event, run func(context.Context))` with `Event{ID uint32; Down bool}`, using the delay and interval from `internal/hotkey/darwin.RepeatTiming() (delay, interval time.Duration)`. `internal/hotkey/darwin`: `Register(c Combo, id uint32) error`, `Unregister(id uint32) error`, `Events() <-chan Event`.

Gate: table tests for every key name and every profile rejection; `synctest` tests for repeat (one run on a short press; runs at delay, then each interval while held; none after release). On Matt's Mac, `tess daemon` registers the 50 combos with skhd stopped and logs each status; any failure, including a bare keypad key, blocks S4.

### S4 — Binding transport and skhd removal

Interfaces: new verbs on the yabai backend: `stack recent` (`wm.WindowOps.RecentWindow(ctx context.Context) (int, bool, error)`, then `StackOnto`), `toggle-float center` (`wm.WindowOps.FloatGrid(ctx context.Context, winID int, g wm.GridCell) error`), `resize left|right|up|down` (existing `ResizeWindow` with the opposite-edge fallback) and `drop-action toggle` (yabai only). The tree verbs are unchanged. Downstream: Matt's profile gains `hotkeys` from evidence § *Binding migration*; the skhd agent and `skhdrc` go; `DefaultServices` returns only yabai.

Gate: a table test sends each of the 50 bindings through the dispatcher to the recording yabai driver and asserts the exact yabai arguments: today's for the nine raw bindings, today's `tess` path for the rest. Matt runs all 50 on the desk with skhd stopped, holding each resize arrow for 2 s.

### N1 — Feasibility spike for D

Interfaces: live tests in `internal/wm/native/darwin` with `//go:build darwin && live`, on a minimal cgo bridge that N6 grows. N1 adds its results to evidence.md as a new section, *Spike results*, with the trial count of each measurement.

Probes, on a scratch Space under the restricted configuration, with the gesture owner from question 8:

- AX frame write and read-back for Zen, a terminal with size increments, an Electron app, Finder and a window with a minimum size.
- Public focus, including Zen on another display and Zen on a hidden Space after `FocusSpace`: 20 trials each, success rate and time to postcondition.
- The yabai write invariant: tess writes frames, a window opens, a new Space is created through Mission Control, yabai restarts, Dock restarts, and a display is reconfigured; yabai must write no frame (read-back plus yabai's debug output). Then an `fn` left drag and an `fn` right drag: yabai writes only to the dragged window, only until release, and its release handler does nothing.
- `pressedMouseButtons` and `modifierFlags` during a plain drag, an `fn` drag and an OS clamp.
- Each Space-service operation's time to postcondition, 20 trials.

Gate: every probe has a recorded result and a negative control (a skipped write must read back unchanged). A failed probe reopens this record.

### N2 — TCC deployment

Interfaces: the home-manager module adds `programs.tessera.daemon.signingIdentity`. Activation copies `tess` out of the store into `~/Applications/Tessera Daemon.app/Contents/MacOS/tess`, with `CFBundleIdentifier` `io.github.mattwilkinsonn.tessera.daemon`, and signs it with `codesign --force -s <identity> -i io.github.mattwilkinsonn.tessera.daemon`. It writes the new bundle beside the old one, signs it, then swaps the two and restarts the launchd agent, whose `ProgramArguments` name the stable path. `tess rescue` from the store re-executes the stable-path binary.

Gate, recorded in evidence § *TCC* and marked [INFERENCE] until done: two different builds pass through real activations and keep the grant; it survives logout and reboot; a revoked grant makes the daemon report `fresh: false` and move nothing; `tess rescue --dry-run` with the daemon stopped is trusted. Record `codesign -d -r-` and the executable path of each process.

### N3 — Planner and staging behind driver capabilities

Interfaces: `type Capabilities struct{ StackCap int; NeedsStaging bool }` and `Capabilities() Capabilities` on `wm.Driver`. yabai returns `{24, true}`; `fake.Driver` is configurable; native returns `{0, false}`, where 0 means no cap. `engine` reads the cap from its plan input, not `wm.StackWindowCap`; `DeskPlan` emits staging floats only when `NeedsStaging`; `commands.apply` loads and writes staging only when `NeedsStaging`. `type Transactional interface{ Commit(ctx context.Context) error; Abort(ctx context.Context) }` in `wm`; the daemon's job runner calls `Commit` at job end and `Abort` on error or cancel when the driver implements it.

Gate: the real command and executor path with `{0, false}` places 40 windows in one stack with no staging write; with `{24, true}` today's behaviour and tests are unchanged; a failure injected between staging and realization restores staged windows on yabai and flushes nothing on a transactional fake.

### N4 — Space service

Interfaces, in `internal/wm`:

```go
type NativeSpace struct {
	ID       uint64
	Index    int
	Label    string
	Display  string
	Visible  bool
	Fullscreen bool
	OwnLayout bool
}

type NativeWindow struct {
	ID, PID                    int
	Space                      uint64
	Display                    string
	Frame                      Frame
	App, Title, Role, Subrole  string
	Minimized, Hidden          bool
}

type SpaceSnapshot struct {
	Spaces  []NativeSpace
	Windows []NativeWindow
	Focused int
}

type SpaceNotice struct {
	Kind  Event
	Space uint64
	Window int
}

type SpaceService interface {
	Snapshot(ctx context.Context) (SpaceSnapshot, error)
	CreateSpace(ctx context.Context, display string) (uint64, error)
	DestroySpace(ctx context.Context, sid uint64) error
	LabelSpace(ctx context.Context, sid uint64, label string) error
	MoveSpace(ctx context.Context, sid uint64, index int) error
	FocusSpace(ctx context.Context, sid uint64) error
	MoveWindowToSpace(ctx context.Context, winID int, sid uint64) error
	Notices(ctx context.Context) (<-chan SpaceNotice, error)
}

type SpaceOpError struct {
	Op      string
	Outcome Outcome // OutcomeFailed or OutcomeUnknown
	Err     error
}
```

`internal/wm/yabai.SpaceService` implements it over the existing query and run helpers, with notices from the daemon's signal path, per § *The Space service*. The yabai driver's `CreateSpace`, `DestroySpace`, `LabelSpace`, `MoveSpaceToIndex` and `MoveWindowToSpace` call it. Their fixed sleeps and polls go. `MoveWindowToSpace` keeps `wm.CanAddToSpace` only while the target Space tiles.

Gate: tests on the recording runner for each operation: already satisfied; notice before the command (subscribed first); no notice, met by poll inside the budget; never met, returns `OutcomeUnknown`; `LabelSpace` with no wait. `MoveWindowToSpace` with 30 windows on a `float` target and an unchanged active Space. `Snapshot` decodes windows on hidden Spaces.

### N5 — Model, layout and status

Interfaces: `native.Model{Lifecycle Lifecycle; Desired Desired}`. `Lifecycle` holds `map[int]WindowLife` with `WindowLife{ID, PID int; Space uint64; Observed wm.Frame; ReadAt time.Time; Status Status; Attached bool; Op uint64; Gen uint64}`, tombstones `map[int]uint64`, and the focused window. `Desired` holds `[]Workspace` with `Workspace{ID, Label, Display string; Space uint64; Kind wm.LayoutKind; Tracks [][]int; Weights []float64; Front []int; Arrival Arrival}` and `map[int]WindowWant` with `WindowWant{Workspace string; Mode Mode; Float wm.Frame}`. `Frames(area wm.Frame, ws Workspace) map[int]wm.Frame`. `ParkCorners(displays []wm.Display) map[string]wm.Frame`. `AdoptTracks(windows []wm.NativeWindow, area wm.Frame) ([][]int, []float64, []int)`. `Desired.Copy() *WorkingCopy`; `(*WorkingCopy).Commit(l *Lifecycle) (dropped []int)`.

Gate: pure table tests on the three-display fixture: 40 windows in one stack; weights that do not sum to 1; a commit that names a tombstoned window drops it; a window that appeared after the copy joins at the arrival target; `AdoptTracks` on columns, stacks and overlapping floats.

### N6 — Darwin platform layer

Interfaces: `native.Platform`: `Apps() ([]App, error)`, `Attach(pid int, ids []int) (attached []int, err error)`, `SetFrame(op uint64, id int, f wm.Frame) error`, `ReadFrame(op uint64, id int) error`, `Focus(op uint64, id int) error`, `FocusedWindow(pid int) (int, error)`, `Displays() ([]wm.Display, error)`, `Mouse() MouseState`, `Events() <-chan PlatformEvent`, `Results() <-chan Result`, `Trusted() bool`. `MouseState{Buttons uint; Fn bool}`. `Result{Op uint64; Window int; Frame wm.Frame; Err error}`. `PlatformEvent{Kind EventKind; PID, Window int}` with kinds for app launch and quit, window created, destroyed, focused, moved, resized, minimized, deminimized, title, display changed and native Space changed. `internal/wm/native/darwin` implements it with cgo and per-app workers.

Gate: N1's live tests pass against this layer; a hung app (a test helper that blocks its main thread) times out at 250 ms while another app's write completes; `Attach` on a hidden Space returns nothing and, after the Space becomes visible, returns the window.

### N7 — Native driver, actor and rescue

Interfaces: `native.Driver` implements `wm.Driver`, `wm.Transactional`, `wm.EventStream` and `wm.ProcessSupervisor` over `Model`, `Platform` and `wm.SpaceService`. The actor loop, operation fencing, pending writes and status per § *Desired and observed state*; focus per § *Focus*; checkpoints and compensation per § *Job transactions*; plain-drag handling per question 4; gesture handling per question 8; cold start per § *Lifecycle contract*; `*wm.ApplyError`; `StatePublisher.Request` after each flush; `tess query`; `tess rescue`.

Gate: `synctest` tests on a fake platform and a fake Space service:

- a window closes while a job's working copy holds it, and the commit does not bring it back;
- a result arrives after its timeout and after `Abort`, and is dropped;
- two results for one window arrive in reverse order, and only the latest operation's counts;
- a job is cancelled after a successful `MoveWindowToSpace`, and desired membership follows the move;
- a `CreateSpace` whose job aborts with the Space still empty is destroyed;
- an `OutcomeUnknown` is resolved by `Snapshot`;
- a focus worker returns success with no focused-window event, and `FocusWindow` returns `ErrFocusTimeout`;
- intermediate and delayed moved events during a pending write raise no drag;
- an `fn` gesture suspends writes until all buttons are up;
- a refused write returns `ApplyError` and `fresh: false`;
- cold start with windows on three hidden Spaces and one window unknown to `native-state.json`;
- the existing `internal/commands` and `internal/daemon` suites pass on `native.Driver`.

### N8 — Observe mode

Interfaces: `backend: "observe"` runs the yabai driver and, read-only, the native platform and Space service. The daemon logs each window whose native membership or frame differs from yabai's, including windows on hidden Spaces, and each event yabai saw that tess did not.

Gate: one week of Matt's use. Every difference is fixed. A difference may be accepted only when it does not change the membership the planners read. This proves enumeration and event coverage only, not writes or focus.

### N9 — Actuation gate

Interfaces: N1's live suite, extended, run by Matt on the desk against `native.Driver` on one scratch Space under the restricted configuration.

Gate: tiling, swap, stack, resize, focus (Zen across displays and on a hidden Space, 20 trials each, success rate recorded), float, a window that refuses a frame, a terminal, an Electron app, a plain drag and an `fn` left and right drag with release, `tess rescue`, a daemon `kill -9` mid-flush, and a cold restart with windows on hidden Spaces and windows created while the daemon was down. No drag misclassified, no write loop, no yabai write outside question 8's exception.

### N10 — tess owns yabai's configuration

Interfaces: `tess yabai-config apply` reads `migration.json` (if any) and applies `legacy` or `restricted` through the yabai driver, removing and adding rules and signals by label. Profile block `yabai: legacy: {settings: {[string]: string}, rules: [...#Rule], signals: [...#Signal]}`. `restricted` is a fixed Go value per § *yabai's restricted configuration*. Downstream: yabairc becomes `sudo yabai --load-sa`, the `dock_did_restart` signal, `tess yabai-config apply` and `tess wake`; the profile gains `yabai.legacy` with today's settings.

Gate: on the yabai backend, with no migration, `yabai -m config <key>` returns today's value for every key in `legacy` after a yabai restart (parity test against a recorded table); with `restricted` applied on a scratch machine state, a new Space is `float`, a new window is neither tiled nor moved, and yabai restart, Dock restart and a display change keep it so.

### N11 — Hybrid migration and switch

Interfaces: `tess migrate --to hybrid`, `tess migrate --reverse` and `tess migrate --status` per § *Migration*; profile `backend: "yabai" | "observe" | "hybrid"`; `migration.json` and `migration-snapshot.json`; question 5's remaps on the native driver. Downstream: sketchybar reads `tess query`.

Gate: fake tests inject a crash after each item, forward and in reverse, and prove resume in the recorded direction; a profile that disagrees with the recorded state moves nothing; a reverse after Spaces were added, removed and moved to another display; a display change mid-reverse; a reverse item that fails three times ends in `reverse-blocked`. On Matt's Mac: one forward, one full reverse, then forward again; then the live checklist: apply on desk, unplug to laptop, replug, focus-slot 1–9, Zen focus, Cmd-Tab to a hidden Space, native fullscreen and back, yabai restart, Dock restart.

### N12 — Soak

Gate: two weeks on `hybrid` with no reverse, with Space-service budgets tuned from the recorded timings. Matt then confirms question 1 or opens V1.

### N13 — Remove tree code

Interfaces: delete `internal/commands/staging.go`, `staged-windows.json` handling, `ArmInsert`, `ToggleSplit`, `SetSplitRatio`, `SplitType`, `LayoutBSP`, `WarpWindow`, `wm.RuleOps`, `StackWindowCap`, `ErrSpaceFull`, `drop-action`, `yabai.legacy` handling after a completed hybrid soak, and the yabai tiling arguments. The yabai package keeps `SpaceService`, queries and process supervision. komorebi keeps the `wm.Driver` methods that still exist.

Gate: `go build ./...` on darwin, linux and windows; `moon ci` green.

### V0 — Virtual workspace research (independent)

Interfaces: live probes in the N1 suite, behind `//go:build darwin && live && virtual`. Needs only S2. Does not block any N task.

Probes: park clamping at each candidate corner of the three displays, including AW windows parked on another display; the event sequence when Cmd-Tab, the Dock and a notification activate a parked window; Mission Control with parked windows. Results go to evidence.md.

Gate: every probe recorded. Matt uses the results if he revisits question 1.

### V1 — Virtual workspace lifecycle (only under question 1, A)

Interfaces: parking and the ledger per § *Parking and recovery*; activation restore, stray Spaces and unplug per § *Lifecycle contract*; a model-backed `wm.SpaceService`.

Gate: fake tests for every row of the lifecycle table under A; live: AW windows parked and restored, Cmd-Tab and Dock activation of a parked window, native fullscreen and back, unplug with inactive workspaces, lid close and reconnect, daemon `kill -9` with parked windows then restart and `tess rescue`.

### V2 — Consolidation migration (only under A)

Interfaces: `tess migrate --to virtual` and `--reverse` per § *Migration*.

Gate: a crash after each item resumes in the recorded direction on fakes; on Matt's Mac, one forward and one full reverse with parked windows before the daily switch.

### V3 — Remove yabai (only under A)

Interfaces: delete `internal/wm/yabai`, `wm.EventSource`, `tess yabai-config` and the hybrid backend value. Downstream: the yabai agent, yabairc, the sudoers entry and, per question 2, SIP.

Gate: two weeks on `virtual` with no reverse, then `go build ./...` on all three systems and `moon ci` green.

## Tasks

- [ ] S1 — every command through the daemon (#76 T5 protocol)
- [ ] S2 — darwin cgo build, `otool` and bare-runner checks; Linux and Windows stay cgo-free
- [ ] S3 — `hotkeys` in the profile; Carbon registration; key repeat; 50-combo live check
- [ ] S4 — nine raw bindings as tess verbs on yabai; behaviour test for all 50; downstream skhd removal
- [ ] N1 — feasibility spike for D: writes, focus, yabai write invariant, gestures, Space timings
- [ ] N2 — stable signed daemon bundle; TCC gate through real activations
- [ ] N3 — `wm.Capabilities` and `wm.Transactional`; planner and staging behind them
- [ ] N4 — `wm.SpaceService` with postconditions; yabai Space calls lose their fixed sleeps
- [ ] N5 — `native.Model` with lifecycle and desired state, `Frames`, `AdoptTracks`, working copy
- [ ] N6 — `native.Platform`, lazy `Attach`, the cgo darwin layer with per-app workers
- [ ] N7 — `native.Driver`, actor, fencing, focus postcondition, gestures, `tess query`, `tess rescue`
- [ ] N8 — observe mode for one week, including hidden Spaces
- [ ] N9 — actuation gate on a scratch Space
- [ ] N10 — `tess yabai-config apply`; legacy and restricted sets; delegating yabairc
- [ ] N11 — recorded hybrid migration, reverse, question 5 remaps, sketchybar on `tess query`
- [ ] N12 — two-week hybrid soak; tune Space budgets
- [ ] N13 — delete staging, insert arms, BSP verbs and yabai tiling code
- [ ] V0 — virtual workspace research (independent of D)
- [ ] V1 — virtual workspace lifecycle (only if question 1 becomes A)
- [ ] V2 — consolidation migration and reverse (only under A)
- [ ] V3 — remove yabai; SIP per question 2 (only under A)
