# Evidence: tess is the macOS window manager

Supporting facts for `design.md`.

**yabai sources.** Matt runs the Christian-SC26 fork (Homebrew `Christian-SC26/formulae/yabai`; `yabai --version` prints `yabai-v7.3.2`). Two clones were read:

- **SC26**: `github.com/Christian-SC26/yabai` at `a26707a`, two commits past the `v7.3.2` tag (`313bcd1`). `git diff 313bcd1 a26707a -- src` touches `event_loop.c`, `mouse_handler.c` (`mouse_drop_action_warp`), `space_manager.c` (a missing-node guard in `space_manager_tile_window_on_space_with_insertion_point`), `view.c`, `window_manager.c` (`window_manager_warp_window`) and `process_manager.c`. None of these changes touches a path the hybrid relies on. Scripting addition `OSAX_VERSION` `2.1.37`.
- **Upstream**: `github.com/koekeishiya/yabai` at `dd84572`. Scripting addition `2.1.30`. The fork has 3,817 added lines in `src` against it.

The **Source** column says where each fact was read. **both** means the cited lines read the same in both clones. Nothing the hybrid relies on is cited from upstream alone.

## yabai mechanisms

| Capability | Symbol and file | What it shows | Source |
| --- | --- | --- | --- |
| Per-app AX observer | `application_observe` in `src/application.c` | One `AXObserver` per app for window created, focused, moved, resized, minimized, deminimized and title changed. | both |
| App launch and quit | `process_manager_begin` in `src/process_manager.c` | A Carbon handler for `kEventAppLaunched`, `kEventAppTerminated` and `kEventAppFrontSwitched`. tess uses the public `NSWorkspace` notifications instead, as AeroSpace does. | both |
| Wake, hide, Space and Dock changes | `workspace.m` | Observes `NSWorkspaceDidWakeNotification`, `NSWorkspaceDidHideApplicationNotification`, `NSWorkspaceActiveSpaceDidChangeNotification` and `NSApplicationDockDidRestartNotification`. | both |
| Display changes | `display_handler` in `src/display.c`, registered with `CGDisplayRegisterReconfigurationCallback` in `display_manager_begin` (`src/display_manager.c`) | Public CoreGraphics callback. `display_uuid` uses `CGDisplayCreateUUIDFromDisplayID`. | both |
| Window ID from AX element | `ax_window_id` in `src/misc/helpers.h` calls `_AXUIElementGetWindow` | The one private symbol tess keeps. | both |
| Frame write | `window_manager_set_window_frame` in `src/window_manager.c` | `AX_ENHANCED_UI_WORKAROUND` turns `AXEnhancedUserInterface` off for the write and back on after. | both |
| Focus | `window_manager_focus_window_with_raise` in `src/window_manager.c` | Private `_SLPSSetFrontProcessWithOptions` plus synthesized events. `window_manager_focus_window_without_raise` sleeps 40 ms (`usleep(40000)`). | both |
| Windows on hidden Spaces | `window_manager_add_existing_application_windows` in `src/window_manager.c` | "MacOS API does not return AXUIElementRef of windows on inactive spaces." It brute-forces `_AXUIElementCreateWithRemoteToken`. tess reads yabai's window query instead. | both |
| Window query | `window_manager_query_windows_for_spaces`, `…_for_display`, `…_for_displays` in `src/window_manager.c` | `yabai -m query --windows` walks every Space of every display. | SC26 |
| AX timeout | `AXUIElementSetMessagingTimeout(wm->system_element, 1.0)` in `window_manager_begin` | Set on the system element only. | both |
| Stack size | `NODE_MAX_WINDOW_COUNT 32` in `src/view.h` | 32 windows per node. tess caps stacks at `StackWindowCap = 24` in `internal/wm/stack.go`. SC26 `a26707a` stops a 33rd window from overflowing the node (CHANGELOG *Unreleased*). | both (constant); SC26 (fix) |
| Run loop | `main` in `src/yabai.c` | `NSApplicationLoad()`, then `[NSApp run]`. | both |
| Space focus fallback | `space_manager_focus_space_using_gesture` in `src/space_manager.c` | Used when the scripting addition cannot focus a Space. | both |
| Window to another Space | `window_manager_send_window_to_space` (`src/window_manager.c`) calls `space_manager_move_window_to_space` (`src/space_manager.c`) | Uses `SLSPerformAsynchronousBridgedWindowManagementOperation` when present, else `SLSMoveWindowsToManagedSpace`, else the scripting addition, else a SkyLight compat-ID fallback. The first path is asynchronous. If the moved window had focus, it focuses the next window on the source Space (`window_manager_focus_window_with_raise(&next->application->psn, next->id, next->ref);`). | both |
| Signal vocabulary | `signal_type_str` in `src/event_signal.h` | Has `space_created`, `space_destroyed`, `space_changed`; no signal for Space labels or order. | SC26 |
| Space label | `COMMAND_SPACE_LABEL` branch in `src/message.c` calls `space_manager_set_label_for_space` | Runs inside the command handler, so the label is set when the command returns. | SC26 |

## What `layout float` does and does not stop

Stops (both):

- Tiling a window: `space_manager_tile_window_on_space_with_insertion_point` starts with `if (view->layout == VIEW_FLOAT) return view;`.
- Display reflow: `window_manager_handle_display_add_and_remove` guards with `if (view->layout != VIEW_FLOAT) {`.
- Re-adding windows on a layout change: `space_manager_set_layout_for_space` skips `window_manager_validate_and_check_for_windows_on_space` for `VIEW_FLOAT`.

Does not stop (both unless marked):

- **New Spaces taking the global layout.** `view_create` in `src/view.c` gives a user Space the global layout unless it has the `VIEW_LAYOUT` flag (`view->layout = g_space_manager.layout;`). `--layout` on one Space sets `VIEW_LAYOUT` (`COMMAND_SPACE_LAYOUT` branch, SC26). yabai's built-in default is `VIEW_FLOAT` (`space_manager_begin` sets `sm->layout = VIEW_FLOAT;`, SC26), but Matt's yabairc sets `layout bsp`.
- **Rules.** `window_manager_apply_rule_effects_to_window` in `src/window_manager.c` moves windows (`window_manager_send_window_to_space(sm, wm, window, sid, true);`) and applies grids (`window_manager_apply_grid(...)`) whatever the layout.
- **Mouse gestures.** See § *Mouse gestures*.
- **Losing a tree.** `space_manager_set_layout_for_space` calls `view_clear(view)`, so switching a Space to `float` and back does not restore its split tree.

## Mouse gestures

All SC26; the upstream lines read the same where marked.

- The tap starts unconditionally: `main` in `src/yabai.c` calls `mouse_handler_begin(&g_mouse_state, MOUSE_EVENT_MASK)` and aborts if it fails.
- Defaults in `mouse_state_init` (`src/mouse_handler.c`): `modifier = MOUSE_MOD_FN`, `action1 = MOUSE_MODE_MOVE`, `action2 = MOUSE_MODE_RESIZE`, `drop_action = MOUSE_MODE_SWAP`.
- The parser for `mouse_modifier` accepts `alt`, `shift`, `cmd`, `ctrl` and `fn`; `mouse_action1` and `mouse_action2` accept `move` and `resize` (`COMMAND_CONFIG_MOUSE_MOD`, `…_ACTION1`, `…_ACTION2` branches in `src/message.c`). There is no `none`. The enum has `MOUSE_MOD_NONE = 0x01` and `mouse_mod_str` prints `none`, but the parser cannot set it.
- `mouse_handler` consumes a left or right mouse-down only when `mod == mouse_state->modifier`. `mouse_mod_from_cgflags` builds `mod` from the five modifier flags and never sets `0x01`, so a modifier value of `MOUSE_MOD_NONE` would never match. This is the basis of question 8 option A's patch.
- `MOUSE_DOWN` in `src/event_loop.c` starts `action1` for the left button and `action2` for the right button when the modifier matches.
- `MOUSE_DRAGGED` moves with `scripting_addition_move_window`, else `window_manager_move_window(g_mouse_state.window, new_point.x, new_point.y)`, and resizes with `window_manager_resize_window_relative_internal(...)`. Neither checks whether the window is tiled (both).
- `MOUSE_UP` finds the window's view with `window_manager_find_managed_window` and exits at `if (!src_view) goto err;` when the window is not tiled. On a `float` Space no swap, stack or warp runs on release.
- Matt's yabairc sets `mouse_modifier fn`, `mouse_action1 move`, `mouse_action2 resize`, `mouse_drop_action swap`, under a comment that mouse window management is meant to be off.

## Layout save and restore (SC26 only)

SC26 adds `space --save-layout`, `--load-layout`, `--dump-layout` and session forms (`COMMAND_SPACE_SAVE_LAYOUT` and related in `src/message.c`; `layout_store.m`). `layout_store_reconstruct_node` matches each leaf by app name and otherwise takes the first unused window, and `layout_store_restore` sets `view->layout = VIEW_BSP`. Two windows of one app can swap places, so the migration reverse does not depend on it.

## What needs the scripting addition

From `src/sa.h` and `src/sa.m` (both: the same 18 calls plus load and uninstall). With SIP partly off and the payload loaded into Dock:

- Create a native Space: `space_manager_add_space` returns `scripting_addition_create_space(sid) ? SPACE_OP_ERROR_SUCCESS : SPACE_OP_ERROR_SCRIPTING_ADDITION`.
- Destroy a native Space: `space_manager_destroy_space` calls `scripting_addition_destroy_space`.
- Focus a native Space without animation: `space_manager_focus_space` calls `scripting_addition_focus_space`, else the gesture fallback.
- Move a native Space to another display: `space_manager_move_space_to_display`.
- Move a window to another Space: the scripting addition is the third of four paths in `space_manager_move_window_to_space`.
- Window opacity, layer, sticky, shadow, scale and order.

What breaks without it, under yabai: Space create, destroy and instant focus; Space-to-display moves; window layer and sticky. Focus of a window on another Space falls back to the gesture path.

What tess needs from this list: under question 1 option D, yabai keeps using it for Space operations. Under option A, nothing after the one-time consolidation (V2), which runs while yabai and its payload are still loaded.

Payload maintenance. Upstream `CHANGELOG.md` lists payload fixes for macOS 26.2 and 26.3 Apple Silicon (#2693, in 7.1.17, 2026-02-21), 26.4 Apple Silicon (#2764) and 26.4 Intel (#2708, in 7.1.25, 2026-05-08), and the 26.6 *add_space* pattern (#2799, after 7.1.25). The SC26 changelog carries those entries and adds its own payload work for macOS 27 (7.1.29 and 7.1.31). SC26 7.1.28 says Space focus works with SIP on for macOS 27; Matt runs macOS 26.6.2, so that does not apply to him yet.

## Live snapshot

Read-only `yabai -m query --displays` on Matt's Mac, macOS 26.6.2:

| Display | Frame (x, y, w, h) | Notes |
| --- | --- | --- |
| Laptop | 0, 0, 1728, 1117 | origin display |
| G9 | 1728, 0, 5120, 1440 | right of the laptop |
| AW | 2044, -1440, 3440, 1440 | above the G9 |

The AW spans x 2044 to 5484 at y 0. The G9 spans x 1728 to 6848 at y 0. Both AW bottom corners sit on the G9's top edge, so the AW has no free bottom corner. The laptop's bottom-left (0, 1117) and the G9's bottom-right (6848, 1440) are free. This matters only under question 1 option A; V0 measures where macOS clamps a parked window at each candidate corner.

AeroSpace's guide asks for monitor arrangements with a free bottom corner and notes that a 1 px line of a parked window stays visible. AeroSpace issue #527 records apps that resist the park position.

## TCC

- macOS keys an Accessibility grant to the code signature's designated requirement. An ad-hoc signature's requirement is its cdhash, so each rebuild changes it and the grant no longer matches. [INFERENCE: matches yabai's wiki guidance for HEAD builds; not measured on tess. N2 replaces this line with measured results.]
- yabai's wiki asks HEAD-build users to sign with a self-created code-signing certificate so the grant survives rebuilds.
- The grant follows the executable that makes the AX call. `tess rescue` must therefore run the same signed executable as the daemon, not the nix-store copy (design § *N2 — TCC deployment*). [INFERENCE until N2's gate.]
- Carbon `RegisterEventHotKey` needs no grant. go-macos/hotkey reports no dialog and no grant on macOS 26.6.2. Rectangle uses it through MASShortcut (`MASHotKey.m`); AeroSpace through soffes/HotKey.
- macOS 15 returns -9868 for Option-only and Option+Shift combos (Apple developer forum thread 763878).
- `CGEventTap` with key events needs Accessibility or Input Monitoring; skhd uses it.

## Binding migration

50 bindings in Matt's `skhdrc`. 41 run `tess`; nine call yabai. The S4 column runs on the yabai backend and keeps today's behaviour. The N11 column is the meaning once tess tiles (question 5); the two deletions land at N13.

| Key | Today | S4 `run` | N11 meaning |
| --- | --- | --- | --- |
| cmd+alt h, j, k, l | `tess focus west/south/north/east` | same (4) | same |
| cmd+alt n | yabai `--focus stack.next` or `stack.first` | `stack-cycle next` | same |
| cmd+alt p | yabai `--focus stack.prev` or `stack.last` | `stack-cycle prev` | same |
| shift+cmd+alt h, j, k, l | `tess swap …` | same (4) | same |
| cmd+alt i | `tess insert stack` | same; arms yabai | sets the workspace arrival target |
| cmd+alt o | `tess insert east` | same; arms yabai | sets the workspace arrival target |
| cmd+alt s | yabai `--stack recent` | new `stack recent` | same, on tracks |
| cmd+alt u | `tess warp east` | same | `unstack east` |
| cmd+alt d | toggles yabai `mouse_drop_action` | new `drop-action toggle` | deleted at N13 |
| cmd+alt b | `tess space bsp` | same | `space columns` |
| cmd+alt e | `tess space stack` | same | same |
| cmd+alt a | `tess apply` | same | same |
| shift+cmd+alt a | `tess laptop` | same | same |
| cmd+alt f | `tess toggle-float` | same | same |
| shift+cmd+alt f | yabai `--toggle float --grid 6:6:1:1:4:4` | new `toggle-float center` | same |
| cmd+alt 0 | `tess balance` | same | equal weights |
| cmd+alt `=` (0x18) | `tess reset-splits` | same | deleted at N13 |
| cmd+alt c | `tess columns` | same | equal weights |
| ctrl+alt h, l | `tess cycle-display prev/next` | same (2) | same |
| ctrl+alt 1, 2, 3 | `tess move-display laptop/g9/aw` | same (3) | same |
| shift+cmd+alt left, right, up, down | yabai `--resize` with edge fallback; skhd repeats while held | `resize left/right/up/down` (4), `repeat: true` (question 9) | track weights |
| keypad 1 to 9 | `tess focus-slot 1` to `9` | same (9) | same |
| keypad 0 | `tess apply` | same | same |
| keypad decimal | `tess stack-cycle next` | same | same |
| keypad enter | `tess toggle-float` | same | same |
| keypad plus, minus | `tess resize grow/shrink` | same (2) | same |
| keypad divide, multiply, clear | `tess snap 3col/50-50/columns` | same (3) | same |

No binding uses Option-only or Option+Shift. The 17 keypad bindings have no modifier; S3 checks that Carbon registers them. Carbon sends no key repeat; S3's repeater covers the four resize arrows.

## #76 sleep sites

From draft tessera #76 (`docs/design/event-driven.md`, § *Sleep sites, wakes and budgets*). The tiling rows (`BSPSteps`, `realizeStack`, `unfloatOne` after toggle, `SettleUnit`) disappear on the native backend, because the step is a model edit confirmed by a frame read-back. The Space rows (`LabelSpace`, `MoveWindowToSpace`, `CreateSpace`) remain yabai calls under option D, and N4 turns their sleeps and polls into postcondition checks with a budget, on both backends. The `unfloatOne` focus poll becomes the focus postcondition of design § *Focus*.

| Site | Today |
| --- | --- |
| `LabelSpace` | 50 ms sleep |
| `MoveWindowToSpace` | 100 ms sleep |
| `realizeStack` move | 350 ms sleep |
| `BSPSteps` anchor move | `Settle: 400` |
| `BSPSteps` extra-window move | `Settle: 350` |
| `BSPSteps` anchor insert, stack insert, ratio | `Settle: 150` each |
| `unfloatOne` focus | 50 ms poll, 2 s budget |
| `unfloatOne` after toggle | 150 ms sleep |
| `CreateSpace` | 50 ms poll, 10 tries |
| `SettleUnit` | 150 ms, no callers |

Draft #76 also records three ways focus-then-poll misses under yabai: `window_focused` is skipped for a window on an invisible Space; the Space switch depends on macOS activating the app first; and the 2 s focus wait timed out three times on 2026-11-08 (Zen). Under question 1 option D, tess first calls `FocusSpace` and confirms the Space switch, then focuses and confirms the focused window, all inside one budget. Whether that fixes Zen is not known; N1 and N9 record the success rate over 20 trials. Under option A every managed window is on a visible Space, so the first two cases do not arise.
