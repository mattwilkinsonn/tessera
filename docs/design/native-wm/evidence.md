# Evidence: tess is the macOS window manager

Supporting facts for `design.md`.

**yabai sources.** Matt runs the Christian-SC26 fork (Homebrew `Christian-SC26/formulae/yabai`; `yabai --version` prints `yabai-v7.3.2`). yabai is a reference source and the origin of tess's scripting addition; tess does not depend on it at run time after N14. Two clones were read:

- **SC26**: `github.com/Christian-SC26/yabai` at `a26707a`, two commits past the `v7.3.2` tag (`313bcd1`). `git diff 313bcd1 a26707a -- src` touches `event_loop.c`, `mouse_handler.c` (`mouse_drop_action_warp`), `space_manager.c` (a missing-node guard in `space_manager_tile_window_on_space_with_insertion_point`), `view.c`, `window_manager.c` (`window_manager_warp_window`) and `process_manager.c`. None of these changes touches the scripting addition or a path the design cites. Scripting addition `OSAX_VERSION` `2.1.37` (`src/osax/common.h`).
- **Upstream**: `github.com/koekeishiya/yabai` at `dd84572`. Scripting addition `2.1.30`. The fork has 3,817 added lines in `src` against it.

The **Source** column says where each fact was read. **both** means the cited lines read the same in both clones.

## yabai mechanisms

| Capability | Symbol and file | What it shows | Source |
| --- | --- | --- | --- |
| Per-app AX observer | `application_observe` in `src/application.c` | One `AXObserver` per app for window created, focused, moved, resized, minimized, deminimized and title changed. | both |
| App launch and quit | `process_manager_begin` in `src/process_manager.c` | A Carbon handler for `kEventAppLaunched`, `kEventAppTerminated` and `kEventAppFrontSwitched`. tess uses the public `NSWorkspace` notifications instead, as AeroSpace does. | both |
| Wake, hide, Space and Dock changes | `workspace.m` | Observes `NSWorkspaceDidWakeNotification`, `NSWorkspaceDidHideApplicationNotification`, `NSWorkspaceActiveSpaceDidChangeNotification` and `NSApplicationDockDidRestartNotification`. | both |
| Display changes | `display_handler` in `src/display.c`, registered with `CGDisplayRegisterReconfigurationCallback` in `display_manager_begin` (`src/display_manager.c`) | Public CoreGraphics callback. `display_uuid` uses `CGDisplayCreateUUIDFromDisplayID`. | both |
| Window ID from AX element | `ax_window_id` in `src/misc/helpers.h` calls `_AXUIElementGetWindow` | Private; tess keeps it. | both |
| Frame write | `window_manager_set_window_frame` in `src/window_manager.c` | `AX_ENHANCED_UI_WORKAROUND` turns `AXEnhancedUserInterface` off for the write and back on after. | both |
| Focus | `window_manager_focus_window_with_raise` in `src/window_manager.c` | Private `_SLPSSetFrontProcessWithOptions` plus synthesized events. `window_manager_focus_window_without_raise` sleeps 40 ms (`usleep(40000)`). | both |
| Windows on hidden Spaces | `window_manager_add_existing_application_windows` in `src/window_manager.c` | "MacOS API does not return AXUIElementRef of windows on inactive spaces." It brute-forces `_AXUIElementCreateWithRemoteToken`. tess reads SkyLight's per-Space window lists instead (§ *Capabilities with SIP on*). | both |
| Space list | `SLSCopyManagedDisplaySpaces` in `src/space_manager.c` and `src/display.c` | Called by the yabai daemon, not the payload. | both |
| Windows on a Space | `SLSCopyWindowsWithOptionsAndTags` in `src/space.c`; `SLSCopySpacesForWindows` in `src/window.c` | Called by the yabai daemon, not the payload. | both |
| Space create and destroy notices | `SLSRegisterConnectionNotifyProc(g_connection, connection_handler, 1327, NULL)` and `1328` in `main` (`src/yabai.c`), on macOS 13 and later; posted as `SLS_SPACE_CREATED` and `SLS_SPACE_DESTROYED` in `src/mission_control.c` | Private SkyLight notifications; yabai's `space_created` signal comes from these. | SC26 |
| AX timeout | `AXUIElementSetMessagingTimeout(wm->system_element, 1.0)` in `window_manager_begin` | Set on the system element only. | both |
| Stack size | `NODE_MAX_WINDOW_COUNT 32` in `src/view.h` | 32 windows per node. tess caps stacks at `StackWindowCap = 24` in `internal/wm/stack.go`. | both |
| Run loop | `main` in `src/yabai.c` | `NSApplicationLoad()`, then `[NSApp run]`. | both |
| Space focus fallback | `space_manager_focus_space_using_gesture` in `src/space_manager.c` | Synthesized Dock gesture through `CGEventPost`; used when the scripting addition cannot focus a Space. | both |
| Window to another Space | `space_manager_move_window_to_space` in `src/space_manager.c` | Uses `SLSPerformAsynchronousBridgedWindowManagementOperation` with `SLSBridgedMoveWindowsToManagedSpaceOperation` when present, else `SLSMoveWindowsToManagedSpace`, else the scripting addition, else a compat-ID fallback. The first path is asynchronous. | SC26 |
| Losing a tree | `space_manager_set_layout_for_space` calls `view_clear(view)` | A layout change drops the split tree; the migration reverse cannot restore an exact tree. | both |

## What needs the scripting addition

From `src/sa.h` and `src/sa.m` (both: the same 18 calls plus load and uninstall). With SIP partly off and the payload loaded into Dock:

- Create a native Space: `space_manager_add_space` returns `scripting_addition_create_space(sid) ? SPACE_OP_ERROR_SUCCESS : SPACE_OP_ERROR_SCRIPTING_ADDITION`.
- Destroy a native Space: `space_manager_destroy_space` calls `scripting_addition_destroy_space`.
- Focus a native Space without animation: `space_manager_focus_space` calls `scripting_addition_focus_space`, else the gesture fallback.
- Reorder Spaces and move a Space to another display: `scripting_addition_move_space_after_space`, `scripting_addition_move_space_to_display`.
- Move a window to another Space: the scripting addition is the third of four paths in `space_manager_move_window_to_space`.
- Window layer, opacity, sticky, shadow, scale and order. `do_window_layer` in `src/osax/payload.m` calls `SLSSetWindowSubLevel` (SC26).

What breaks without it: Space create, destroy, reorder and instant focus; window layer and sticky. Focus of a Space falls back to the gesture path.

**Payload maintenance.** Upstream `CHANGELOG.md` lists payload fixes for macOS 26.2 and 26.3 Apple Silicon (#2693, in 7.1.17, 2026-02-21), 26.4 Apple Silicon (#2764) and 26.4 Intel (#2708, in 7.1.25, 2026-05-08), and the 26.6 *add_space* pattern (#2799, after 7.1.25). The SC26 changelog carries those entries and adds its own payload work for macOS 27 (7.1.29 and 7.1.31). SC26 has 16 releases from 2026-09-22 to 2026-10-30; upstream's latest release page is [v7.1.25](https://github.com/asmvik/yabai/releases/tag/v7.1.25) (2026-05-08). Upstream's open issues include macOS 27.2 payload breakage ([#2832](https://github.com/asmvik/yabai/issues/2832)). Matt runs macOS 26.6.2.

## Scripting addition source

Read at SC26 `a26707a`.

| File | Lines | Role |
| --- | --- | --- |
| `src/sa.h` | 31 | client declarations |
| `src/sa.m` | 659 | client: install, load, handshake, send |
| `src/osax/payload.m` | 1,255 | code that runs in Dock |
| `src/osax/loader.m` | 285 | injection into Dock |
| `src/osax/arm64_payload.m` | 277 | arm64 patterns and offsets per macOS version |
| `src/osax/x64_payload.m` | 296 | x86_64 patterns and offsets |
| `src/osax/common.h` | 50 | socket path, version, attribute bits, opcodes |

Counts are `wc -l` of each file, not an estimate of the cut-down fork.

- **Licence.** `LICENSE.txt` is MIT: "Copyright (c) 2019 Åsmund Vikane" and "The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software."
- **Second attribution.** `src/osax/loader.m`: "The arm64e injection path is based on work by Jeremy Legendre (<https://github.com/jslegendre>)".
- **Protocol.** `SA_SOCKET_PATH_FMT "/tmp/yabai-sa_%s.socket"` and `SA_SOCKET_BUFF_LEN 0x1000` in `src/osax/common.h`. A message is a 2-byte length, a 1-byte opcode and packed arguments (`sa_payload_init`, `pack` and `sa_payload_send` macros in `src/sa.m`). `enum sa_opcode` has 19 values, `SA_OPCODE_HANDSHAKE = 0x01` to `SA_OPCODE_WINDOW_TO_SPACE = 0x13`.
- **Handshake.** `do_handshake` in `src/osax/payload.m` replies with `OSAX_VERSION` and an attribute bitmask. Each bit is set only when its function or pattern was found in Dock: `if (add_space_fp) attrib |= OSAX_ATTRIB_ADD_SPACE;`, `if (remove_space_fp) attrib |= OSAX_ATTRIB_REM_SPACE;`, `if (set_front_window_fp) attrib |= OSAX_ATTRIB_SET_WINDOW;`. The reply is the version, a NUL, the 4-byte mask and `\n`: `memcpy(bytes, OSAX_VERSION, version_length); memcpy(bytes + version_length + 1, &attrib, attrib_length);` then `bytes[bytes_length] = '\n';`. `src/sa.m` notes: "The payload ignores a space move when it has no way to move spaces on this macOS version, but the socket write itself still succeeds."
- **Packed arguments.** `#define pack(v) memcpy(bytes+length, &v, sizeof(v)); length += sizeof(v)` in `src/sa.m`. `scripting_addition_create_space(uint64_t sid)`, `scripting_addition_destroy_space(uint64_t sid)` and `scripting_addition_focus_space(uint64_t sid)` each `pack(sid)`. `scripting_addition_move_space_after_space(uint64_t src_sid, uint64_t dst_sid, bool focus)` packs `src_sid`, `dst_sid`, a `dummy_sid` and `focus`; `scripting_addition_move_space_to_display` packs `src_prev_sid` in that third place. `scripting_addition_set_layer(uint32_t wid, int layer)` packs `wid` and `layer`.
- **Space resolution in the payload.** `do_space_create` unpacks one `space_id` and calls `SLSCopyManagedDisplayForSpace(SLSMainConnectionID(), space_id)`, then adds a new `ManagedSpace` to that display. `do_space_move` unpacks `source_space_id`, `dest_space_id`, `source_prev_space_id` and `bool focus_dest_space`; it shows `source_prev_space_id` first only when it is non-zero. `do_window_layer` calls `SLSSetWindowSubLevel(SLSMainConnectionID(), wid, CGWindowLevelForKey(layer))`, so the argument is a level key.
- **Load preconditions.** `src/sa.m` reads `csr_get_active_config` and requires `CSR_ALLOW_UNRESTRICTED_FS` (`0x02`) and `CSR_ALLOW_TASK_FOR_PID` (`0x04`); on arm64 it requires `-arm64e_preview_abi` in `kern.bootargs`; it refuses to load unless root ("scripting-addition must be loaded as root!"). It installs to `/Library/ScriptingAdditions/yabai.osax` and runs `Contents/MacOS/loader`, which injects with `task_for_pid` (`src/osax/loader.m`).
- **Per-release patterns.** `get_add_space_offset` and `get_add_space_pattern` in `src/osax/arm64_payload.m` return different values per macOS major version, and the pattern changes between 26.0 and 26.4. `verify_os_version` in `src/osax/payload.m` gates versions; `hex_find_seq` searches Dock's text for a pattern.
- **Setup.** yabai's [SIP wiki](https://github.com/asmvik/yabai/wiki/Disabling-System-Integrity-Protection) asks users to disable "Filesystem Protections, Debugging Restrictions and NVRAM Protection" and to set the boot-arg, then reboot. Its [install wiki](https://github.com/asmvik/yabai/wiki/Installing-yabai-(latest-release)#configure-scripting-addition) reloads the payload on `dock_did_restart` with a passwordless sudoers entry. A macOS 26 NVRAM setup failure was reported and closed after a corrected command ([#2741](https://github.com/asmvik/yabai/issues/2741)).

## Capabilities with SIP on

- **Window to Space without the payload.** `space_manager_move_window_to_space` tries `SLSPerformAsynchronousBridgedWindowManagementOperation`, then `SLSMoveWindowsToManagedSpace`, before it calls the scripting addition (SC26). These are undocumented SkyLight calls from the daemon's own process. [INFERENCE: that they work with SIP fully on is not measured here; N1 tests it.]
- **Space focus without the payload.** `space_manager_focus_space_using_gesture` posts a synthesized Dock gesture through `CGEventPost` (both). In SC26 it skips a gesture to the same Space within 350 ms (`Guard 1`), refuses one within 150 ms of the last (`Guard 2: Minimum physical gesture spacing (150ms) to prevent Dock queue storms`), and breaks ping-pong between two Spaces (`Guard 3`). Across displays it calls `CGWarpMouseCursorPosition(point)` to the target display's centre, then posts a mouse down and up there if the active Space has not changed. `space_manager_focus_space` uses this path when `scripting_addition_focus_space` fails or `YABAI_SIMULATE_SIP_ON` is set. SC26's README says "core features work with SIP enabled", and its 7.1.28 changelog entry adds SIP-on Space focus for macOS 27, which does not apply to macOS 26.6.2. [INFERENCE: success rate with SIP fully on, across displays and to hidden Spaces, is not measured; N1 measures it.]
- **Dock restart signal.** yabai observes `NSApplicationDockDidRestartNotification` in `src/workspace.m` and posts `DOCK_DID_RESTART`. Whether Space IDs survive a Dock restart is not recorded in either source. [INFERENCE: N11 measures it.]
- **Create and destroy.** No path without the payload exists in yabai. Apple documents Mission Control as an interactive way to add Spaces ([Mac User Guide](https://support.apple.com/guide/mac-help/view-open-windows-spaces-mission-control-mh35798/mac)), not a programmatic one.
- **Moves between native Spaces through public AX.** Amethyst's "throw window to space" broke on macOS 15; the maintainer wrote: "The mechanism by which we do it is no longer possible, it seems" ([#1662](https://github.com/ianyh/Amethyst/issues/1662)).

## Live snapshot

Read-only `yabai -m query --displays` on Matt's Mac, macOS 26.6.2:

| Display | Frame (x, y, w, h) | Notes |
| --- | --- | --- |
| Laptop | 0, 0, 1728, 1117 | origin display |
| G9 | 1728, 0, 5120, 1440 | right of the laptop |
| AW | 2044, -1440, 3440, 1440 | above the G9 |

The AW spans x 2044 to 5484 at y 0. The G9 spans x 1728 to 6848 at y 0. Both AW bottom corners sit on the G9's top edge, so the AW has no free bottom corner. The laptop's bottom-left (0, 1117) and the G9's bottom-right (6848, 1440) are free. This matters only for stage V; V0 measures where macOS clamps a parked window at each candidate corner.

## Virtual workspaces

- AeroSpace's [guide](https://nikitabobko.github.io/AeroSpace/guide#emulation-of-virtual-workspaces) moves windows of an inactive workspace "outside the visible area of the screen, in the bottom right or left corner", restores them on workspace switch or Cmd-Tab, and leaves a 1 px edge visible. It renders parked windows in Mission Control "too small".
- AeroSpace's maintainer, on monitors above other monitors: "It's only possible to move them below the monitor" ([#382](https://github.com/nikitabobko/AeroSpace/issues/382)). [#413](https://github.com/nikitabobko/AeroSpace/issues/413) records windows from a middle monitor leaking along a neighbour's bottom edge. AeroSpace issue #527 records apps that resist the park position.
- AeroSpace observes `NSWorkspace.didActivateApplicationNotification` to restore a workspace on activation ([`GlobalObserver.swift`](https://github.com/nikitabobko/AeroSpace/blob/main/Sources/AppBundle/GlobalObserver.swift)). App activation alone may not say which window of a multi-window app was meant.
- Native fullscreen is a separate Space outside AeroSpace's workspace control ([discussion #2158](https://github.com/nikitabobko/AeroSpace/discussions/2158)).
- [FlashSpace](https://github.com/wojciech-kulik/FlashSpace) hides whole apps with `NSRunningApplication.hide` instead of parking windows; that avoids corners but cannot keep per-window slots.
- [Rift](https://github.com/acsandmann/rift) parks windows in a managed native Space (`src/model/hidden_window_placement.rs`) and says it "does not require disabling SIP".

Verdict of the research: virtual workspaces are viable on macOS with SIP on, but not with AeroSpace's per-display parking on Matt's geometry. V0 defines the spike and its pass bar.

## TCC

- macOS keys an Accessibility grant to the code signature's designated requirement. An ad-hoc signature's requirement is its cdhash, so each rebuild changes it and the grant no longer matches. [INFERENCE: matches yabai's wiki guidance for HEAD builds; not measured on tess. N2 replaces this line with measured results.]
- yabai's wiki asks HEAD-build users to sign with a self-created code-signing certificate so the grant survives rebuilds.
- The grant follows the executable that makes the AX call. `tess rescue` must therefore run the same signed executable as the daemon, not the nix-store copy (design § *N2 — TCC deployment and signing*). [INFERENCE until N2's gate.]
- Carbon `RegisterEventHotKey` needs no grant. go-macos/hotkey reports no dialog and no grant on macOS 26.6.2. Rectangle uses it through MASShortcut (`MASHotKey.m`); AeroSpace through soffes/HotKey.
- macOS 15 returns -9868 for Option-only and Option+Shift combos (Apple developer forum thread 763878).
- `CGEventTap` with key events needs Accessibility or Input Monitoring; skhd uses it.

## Slot-manager prior art

| Product | What tess borrows |
| --- | --- |
| [FancyZones](https://learn.microsoft.com/en-us/windows/powertoys/fancyzones) | Hovered target zone is highlighted before drop; several windows can share a zone and cycle by key. |
| [Divvy](https://mizage.com/downloads/DivvyMacHelp.pdf) | Direct selection of a target with the pointer; Escape cancels. |
| [Rectangle Pro](https://rectangleapp.com/pro/docs/layouts) | Saved layouts that the user updates explicitly; layouts can apply when a "Window opens". |
| [Moom](https://manytricks.com/moom/help/snap.html) | A "saved layout's window locations as onscreen targets"; the hovered target is highlighted. |
| [Hammerspoon `hs.grid`](https://www.hammerspoon.org/docs/hs.grid.html) | Keyboard hints over cells; Enter applies, Escape cancels. |
| [niri tabs](https://niri-wm.github.io/niri/Tabs.html) | "switch a column to present windows as tabs": one slot, many windows, one front, keyboard cycling. |

None of these documents automatic arrival placement into named slots, which tess already has (`new-window-placement.md`).

**Indicator.** Apple describes `NSPanel` as a special kind of window ([NSPanel](https://developer.apple.com/documentation/appkit/nspanel)) and `nonactivatingPanel` as a panel that "does not activate the owning app" ([style mask](https://developer.apple.com/documentation/appkit/nswindow/stylemask-swift.struct/nonactivatingpanel)). A user notification needs a permission ([Asking permission](https://developer.apple.com/documentation/usernotifications/asking-permission-to-use-notifications): "you must obtain permission") and macOS chooses where it appears ([Mac User Guide](https://support.apple.com/guide/mac-help/view-app-notifications-mh40609/mac)). An app with the `prohibited` activation policy "may not create windows or be activated" ([ActivationPolicy.prohibited](https://developer.apple.com/documentation/appkit/nsapplication/activationpolicy-swift.enum/prohibited)), so the daemon uses `accessory`.

## Go library survey

Read on 2026-11-09. Chosen on fit and maintenance only (design Decision 9).

| Need | Candidate | Fit | Maintained |
| --- | --- | --- | --- |
| Global hotkeys | [golang.design/x/hotkey](https://github.com/golang-design/hotkey) | Cross-platform with key-up, but its macOS backend is a `CGEventTap` that needs Accessibility or Input Monitoring; it coalesces repeat keydowns ([`hotkey_darwin.m`](https://raw.githubusercontent.com/golang-design/hotkey/main/hotkey_darwin.m)) | v0.6.4, 2026-09-26 |
| Global hotkeys | [go-macos/hotkey](https://github.com/go-macos/hotkey) | Carbon through purego, no permission; its API handles `kEventHotKeyPressed` only | v0.11.0, 2026-10-04 |
| AX | [go-macos/accessibility](https://github.com/go-macos/accessibility) | purego; frame writes and display moves; no AXObserver | commits to 2026-11-07 |
| Objective-C and run loop | [go-macos/objc](https://github.com/go-macos/objc) | purego; AppKit, blocks, run loops; README warns `App()` binds to the thread of its first call and `LockOSThread` alone does not make it the main thread | commits to 2026-11-05 |
| Objective-C bindings | [progrium/darwinkit](https://github.com/progrium/darwinkit) | broad generated bindings | last release 2024-06-21 |
| MCP server (read 2026-11-12) | [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) | stdio through `mcp.StdioTransport`; `mcp.AddTool` derives the input and output schema from Go structs; `mcp.NewInMemoryTransports` for tests. README: "The official Go SDK for Model Context Protocol servers and clients. Maintained in collaboration with Google." | v1.8.0, 2026-09-14 (after v1.8.0-pre.1 and pre.2, 2026-09-04) |
| MCP server (read 2026-11-12) | [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) | stdio through `server.ServeStdio`; README tools declare arguments with `mcp.WithString(...)` builders and read them with `request.RequireString(...)` | v1.5.0, 2026-10-30; v1.4.0, 2026-10-26 |
| Bundles and signing | [tmc/macgo](https://github.com/tmc/macgo) | bundle, signing and TCC helpers | v0.1.4, 2026-06-04 |
| Prior art, not dependencies | [dhoard/quadrant](https://github.com/dhoard/quadrant), [ujaandas/yumcha](https://github.com/ujaandas/yumcha) | Go macOS window managers on Carbon and AX | 2026-10-23, 2026-05-01 |

No Go package wraps AXObserver, NSWorkspace notifications, `CGDisplayRegisterReconfigurationCallback` or NSPanel as a complete API. Apple notes that NSWorkspace launch and terminate notifications omit background and `LSUIElement` apps ([didLaunchApplicationNotification](https://developer.apple.com/documentation/appkit/nsworkspace/didlaunchapplicationnotification)).

## Binding migration

50 bindings in Matt's `skhdrc`. 41 run `tess`; nine call yabai. The S4 column runs on the yabai backend and keeps today's behaviour. The native column is the meaning on the slot model (design § *Slot verbs*). From N12 the profile holds both sets (design § *Bindings across the switch*): a row whose native column is "deleted" is in the yabai set only, a row whose native column names a different verb is two bindings on one combo, one per set, and every other row is in both. The migration switches sets in one step; N14 deletes the yabai set.

| Key | Today | S4 `run` | Native |
| --- | --- | --- | --- |
| cmd+alt h, j, k, l | `tess focus west/south/north/east` | same (4) | focus neighbouring slot; moves the highlight in move mode |
| cmd+alt n | yabai `--focus stack.next` or `stack.first` | `stack-cycle next` | same, on the slot's stack |
| cmd+alt p | yabai `--focus stack.prev` or `stack.last` | `stack-cycle prev` | same |
| shift+cmd+alt h, j, k, l | `tess swap …` | same (4) | swap with the neighbouring slot's front |
| cmd+alt i | `tess insert stack` | same; arms yabai | `move-mode` (yabai set keeps `insert stack`) |
| cmd+alt o | `tess insert east` | same; arms yabai | deleted |
| cmd+alt s | yabai `--stack recent` | new `stack recent` | same, on slots |
| cmd+alt u | `tess warp east` | same | `stack east` |
| cmd+alt d | toggles yabai `mouse_drop_action` | new `drop-action toggle` | deleted |
| cmd+alt b | `tess space bsp` | same | deleted; `snap columns` covers it |
| cmd+alt e | `tess space stack` | same | same |
| cmd+alt a | `tess apply` | same | same; clears any override |
| shift+cmd+alt a | `tess laptop` | same | same |
| cmd+alt f | `tess toggle-float` | same | float and pin; again returns to the home slot |
| shift+cmd+alt f | yabai `--toggle float --grid 6:6:1:1:4:4` | new `toggle-float center` | same, pinned |
| cmd+alt 0 | `tess balance` | same | `layout reset` |
| cmd+alt `=` (0x18) | `tess reset-splits` | same | deleted |
| cmd+alt c | `tess columns` | same | deleted; `layout reset` covers it |
| ctrl+alt h, l | `tess cycle-display prev/next` | same (2) | same |
| ctrl+alt 1, 2, 3 | `tess move-display laptop/g9/aw` | same (3) | same |
| shift+cmd+alt left, right, up, down | yabai `--resize` with edge fallback; skhd repeats while held | `resize left/right/up/down` (4), `repeat: true` | temporary override weights, `repeat: true` |
| keypad 1 to 9 | `tess focus-slot 1` to `9` | same (9) | same |
| keypad 0 | `tess apply` | same | same |
| keypad decimal | `tess stack-cycle next` | same | same |
| keypad enter | `tess toggle-float` | same | same as cmd+alt f |
| keypad plus, minus | `tess resize grow/shrink` | same (2), `repeat: true` | temporary override weights, `repeat: true` |
| keypad divide, multiply, clear | `tess snap 3col/50-50/columns` | same (3) | same; clears any override |

No binding uses Option-only or Option+Shift. The 17 keypad bindings have no modifier; S3 checks that Carbon registers them. Carbon sends no key repeat; tess's repeater covers every binding with `repeat: true`. skhd repeats any held binding today [INFERENCE: its event tap sees auto-repeat key-downs and does not filter them], so S4 sets `repeat: true` on keypad plus and minus too. The native set adds `layout save`; its combo is a downstream profile choice.

## #76 sleep sites

From draft tessera #76 (`docs/design/event-driven.md`, § *Sleep sites, wakes and budgets*). The tiling rows (`BSPSteps`, `realizeStack`, `unfloatOne` after toggle, `SettleUnit`) disappear on the native backend, because a slot change is a model edit confirmed by a frame read-back. The Space rows (`LabelSpace`, `MoveWindowToSpace`, `CreateSpace`) become Space-service postcondition checks with a budget; `LabelSpace` becomes a tess record edit with no wait. The `unfloatOne` focus poll becomes the focus postcondition of design § *Focus*.

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

Draft #76 also records three ways focus-then-poll misses under yabai: `window_focused` is skipped for a window on an invisible Space; the Space switch depends on macOS activating the app first; and the 2 s focus wait timed out three times on 2026-11-08 (Zen). On the native backend tess first calls `FocusSpace` and confirms the Space switch inside its own budget, then focuses and confirms the focused window inside a second, nested budget (design § *Focus*). Whether that fixes Zen is not known; N1 and N11 record the success rate over 20 trials. In stage V every managed window is on a visible Space, so the first two cases do not arise.
