# Design: tess is the macOS window manager

Parent: RIG-8858. This record supersedes draft tessera #76 (`event-driven.md`, RIG-8692). It keeps #76's fact inventory and drops its approach of waiting on yabai. Evidence, the binding table and the private-API list are in [evidence.md](evidence.md).

**yabai sources.** Matt runs the Christian-SC26 fork of yabai (Homebrew `Christian-SC26/formulae/yabai`, v7.3.2; RIG-8327). Citations marked **SC26** are from that fork at `a26707a`, two commits past its v7.3.2 tag; neither commit touches a cited function. Citations marked **both** read the same in SC26 and in upstream at `dd84572`. After stage N, yabai is only a reference source, the origin of tess's scripting addition, and the rollback target. Evidence § *yabai mechanisms* gives the source of every citation.

**Freeze.** Matt has ruled on every open question of the first two rounds (Decisions 10 to 12). His rulings of 2026-11-12 add Decisions 13 to 15 and leave four questions open. The record designs against each recommendation until he answers, with one exception: § *Splits within a slot* describes the one-axis panes of Open Question 1 option A, because option B keeps every rule there and adds nesting, and option C replaces it. N6 and N9 do not start until Matt rules on questions 1 and 2; questions 1 and 2 say what N6 and N9 change for each option. N1 checks the assumptions of the chosen design. If a probe fails, the record reopens; N1 does not choose.

<!-- LENGTH §Approach: answers the required capability questions plus the migration, Space-service, scripting-addition and actor contracts -->

## Decisions

Matt ruled on the nine questions of the first draft on 2026-11-09, on the three open questions on 2026-11-10, and added Decisions 13 to 15 on 2026-11-12.

1. **Workspaces: tess owns native Spaces through its own scripting addition (C).** tess forks yabai's scripting addition (SA) from SC26 `a26707a` (MIT) and changes it as it needs; yabai itself is not maintained well enough to depend on. tess must still run on a Mac with SIP fully on, with fewer capabilities (§ *Capabilities and SIP*). Virtual workspaces (A) are preferred if a parking spike passes on Matt's displays (stage V). The other first-draft options are removed, with yabai's restricted configuration, `tess yabai-config apply` and yabai's mouse gestures. yabai stays only as the SA's source, in attribution, and in migration and its reverse.
2. **SIP.** If virtual workspaces pass V0 and run through V3, Matt re-enables SIP. No date is set.
3. **Signing identity.** A self-signed certificate now (A). The Developer ID replaces it when it exists (B); that is a one-line identity change in N2.
4. **Mouse drag: float on drag (C).** tess is a slot manager first, not a BSP tiler: slots are the top level of every layout. Windows go into slots and stacks by themselves, and the keyboard moves them between slots. Inside one slot the user may split (Decision 13). Dragging a slotted window floats it at once and pins it on top. The same shortcut puts it back in the slot it came from. Two new asks come with this ruling: move mode with a top-right indicator, and temporary slot resize with save and reset (§ *Slot interaction*).
5. **Tree verbs go.** `insert`, `warp`, `reset-splits`, `balance`, `columns`, `space bsp` and the `cmd + alt - d` drop toggle have no meaning in the slot model. Keyboard slot moves stay (§ *Slot verbs*).
6. **Windows: drop the komorebi code now (S0).** Native Windows support comes later. The macOS work sits behind `wm.Driver`, `wm.SpaceService`, `native.Platform` and `internal/hotkey`, so a later platform plugs in at those seams.
7. **cgo** for every macOS framework call that needs it (A), in darwin-only packages.
8. **Mouse gestures.** Not applicable; the question is removed.
9. **Key repeat.** Any binding can set `repeat: true`; tess times the repeat itself. tessera is a personal project, so libraries are chosen on fit and maintenance only, not on adoption (§ *Library choices*).
10. **Space pool with SIP on (A).** tess binds workspaces to the native Spaces that already exist. `CreateSpace` takes an unbound Space from the pool; if none is free it returns `wm.ErrCapability{Name: "space.create"}` and the job places the window on the active workspace. `tess doctor` prints how many Spaces each topology needs, so the user adds them once in Mission Control. The shortage shows in the top-right indicator area, the same space as the move-mode indicator. Costs: unused Spaces stay in Mission Control; one manual step per new Mac.
11. **Overrides save into the profile source file.** Matt's own choice ("just have tess update it"). Downstream, home-manager deploys `~/.config/tessera/profile.cue` with `mkOutOfStoreSymlink` to the profile source in Matt's nix repo checkout, so the daemon reads the live source file, not a store copy. `tess layout save` resolves the symlink, checks the file's hash against the hash read at load and refuses on a mismatch, rewrites `weights` with a `cuelang.org/go` AST edit and `format` (which keeps comments), vets the result against `#Profile`, writes a temp file and renames it into place, then reloads the profile. The saved weights become the profile weights, so the override clears at once and no frame moves. A missing symlink target is an error and nothing is saved. `cue vet` still runs at switch, and the edit shows as a diff in Matt's repo for him to commit. Costs: tess writes into Matt's repo checkout, and an uncommitted change is lost if he resets the checkout.
12. **Root for the scripting addition: sudoers (A).** nix-darwin writes a passwordless sudoers entry for `tess sa load` and `tess sa restore`. The daemon runs `tess sa load` at start and after `NSApplicationDockDidRestartNotification`. The entry names a path, so N2 installs the bundle at a root-owned path.
13. **Splits within slots.** Matt: "ability to BSP within slots. sometimes you do want to split screen one window in a slot, or 'break' a stack into a BSP." The slot stays the top level. Inside one slot, the user can split the slot (for example, two windows side by side in that slot), break a stack into parts, and merge the parts back into one stack. How deep a split may go, the cap on parts, and whether a profile can hold splits are pending Open Questions 1 and 2. § *Splits within a slot* has the model they share.
14. **Pause and move budget.** Matt asked for `tess stop` and `tess start`: a pause that survives a daemon restart. He also asked for a move budget: if tess moves the same window too often in a short time, or wakes keep failing, the daemon pauses itself and says why. The native driver has both from N8 on; the yabai path gets them in separate work (§ *Pause and move budget*).
15. **tess works with AI agents through MCP.** Matt: "tess should be very AI integrated. MCP server in particular. Can tell an agent your ideas for a slot setup, it just builds it for you in CUE, can immediately switch you to it to try it out, can have an agent write multiple 'Profiles' to try out, you see them in the app and switch to them." tess gains a profile set (the live profile plus named drafts), `tess profile` verbs to validate, write, preview, switch and go back, and `tess mcp`, a stdio MCP server that exposes those verbs to an agent (§ *Profiles and the MCP server*).

## Open Questions

1. **What shape may a split inside a slot take?** Matt asked for "BSP within slots". Every option keeps the slot as the top level and keeps a split inside its slot: no option brings back the workspace-wide tree verbs of Decision 5 (`insert`, `warp`, `balance`, `reset-splits`). Each option also needs a cap and an overflow rule, which Matt approves with it.
   - (A) **One-axis panes.** A slot holds one to four panes on one axis, and each pane is a stack. A pane is never split again. Cap: four panes. Overflow: `split all` on more windows than free panes leaves the rest stacked in the last new pane. This gives side-by-side and top-and-bottom splits, but not a mixed layout such as one tall pane beside two short ones, so it is not BSP.
   - (B) **A split tree confined to the slot.** A slot holds a tree. Each leaf is a stack; each inner node splits its area in two on one axis, with one ratio. `split <dir>` splits the focused leaf on the axis of `<dir>`, whatever its parent's axis, so mixed layouts work. `merge` joins the focused leaf with its sibling (or the sibling's whole subtree) into one stack; `merge all` puts the whole slot back into one stack. `resize` moves the ratio of the nearest ancestor on the axis of the direction, else the slot weight. `split all` breaks the focused leaf by halving the newest leaf along its longer side each time, so five windows give a dwindle of five leaves. Cap: six leaves per slot, so a leaf on the G9's 1/3 slot keeps at least about 400 pt of width [INFERENCE: from a 5120 pt G9 split in thirds; N9 checks it on the desk]. Overflow: as in A, the rest stay stacked in the last leaf. N6 replaces `Panes []Pane` with `Root *Node`, `Node{Axis wm.LayoutKind; Ratio float64; A, B *Node; Leaf *Pane}`; `PaneAt`, `Neighbour` and `Frames` walk the tree; the § *Splits within a slot* rules for focus, arrival, move mode, drag and status apply per leaf.
   - (C) **Tiled or tabbed per slot.** No panes: one `layout tiled|tabbed` toggle per slot. Tiled shows every window of the stack side by side on one axis; tabbed is today's stack. Cap: the stack's size, no overflow rule. This is niri's column toggle. It breaks a stack in one key, but cannot split one window out of a stack and keep the others stacked, and it is not BSP.

   **Prior art:** [i3](https://i3wm.org/docs/userguide.html#_tree): "Every window can be split horizontally or vertically again, just like the workspace" (B, but workspace-wide); [niri](https://niri-wm.github.io/niri/Tabs.html): "You can switch a column to present windows as tabs, rather than as vertical tiles" (C); FancyZones zones do not nest (A's flat shape).
   **Recommendation:** **B, a split tree confined to the slot, capped at six leaves.** Matt said "BSP within slots", and only B gives a BSP inside a slot. Keeping the tree inside one slot keeps Decision 4's slot model and Decision 5's deletions: no verb reaches past the slot.
2. **Can a profile hold splits?** Depends on question 1. Options: (A) No: a split is runtime state in `native-state.json`, like a float; it survives daemon restarts; `layout save` writes slot weights only, as Decision 11 says. (B) Yes: a track in the profile may carry a split (under 1A an `axis` and a list of panes; under 1B a nested `split` node with `axis`, `ratio` and two children), each leaf naming the profile window keys that go there. `apply` and `profile switch` build the split; `layout save` writes the live split and ratios back; an arrival whose window key a leaf names goes into that leaf. Under 1C the profile gains one `tabbed: bool` per track.
   **Prior art:** Rectangle Pro and Moom save named layouts with every zone; FancyZones saves zone layouts, not window placement. All three keep structure in the saved layout.
   **Recommendation:** **B, save splits into profiles.** Decision 15 has agents write profiles from Matt's description of a slot setup; a setup with a split inside a slot can only be written, previewed and switched to if the profile can hold it. Runtime splits stay for the "sometimes" case: a split made by a key is runtime until `layout save`.
3. **What may an agent do beyond drafts and switching?** The MCP server lets an agent read state, validate, write drafts, preview, switch and go back. Two more verbs exist for the user. Options: (A) neither is an MCP tool: `tess profile promote` (copy a draft into the live profile source in Matt's repo checkout) and `tess start` (resume after a pause) stay with the user, through the CLI, a hotkey or the app. An agent may call `tess stop`. (B) Both are MCP tools, so an agent can finish the loop alone. (C) Promote is a tool, `start` is not.
   **Prior art:** trivial; this is the same line Decision 11 draws: tess writes into Matt's repo only on his command. The MCP spec's [tool annotations](https://modelcontextprotocol.io/specification/2025-06-18/server/tools) can mark a tool `destructiveHint`, but they are hints to the client: the spec says "the protocol itself does not mandate any specific user interaction model". This choice is a workflow guard, not a security boundary: an agent with a shell can run any `tess` verb. The boundary on what a profile can execute is question 4.
   **Recommendation:** **A.** A draft and a switch are undone in one step; a promote edits Matt's repo, and a `start` after a budget pause overrides the safety stop that just fired. Both stay one keypress for Matt.
4. **What may a draft profile change?** A profile is not only geometry. `#WindowSpec.spawn` is a launcher argv that `apply` runs for a missing window (`startMissingWindows` in `internal/commands/composite.go` calls `driver.SpawnWindow(ctx, spec.Spawn)`, which runs `exec.Command(argv[0], argv[1:]...)` in `internal/wm/spawn`). Each hotkey `run` is a tess argv. So an agent that writes a draft and switches to it can run any command as Matt, for example one that edits his repo checkout or runs `tess start`. Options: (A) Trust: a draft may change any field; the agent is treated as able to run local code, and Q3's omissions are workflow guards only. (B) Restrict: a draft may change displays, layouts, slots, weights, arrivals, window match patterns and (per Q2) splits only. Every `windows.<key>.spawn` in a draft must equal the live profile's `spawn` for the same key, a key the live profile lacks may not set `spawn`, and `hotkeys` must equal the live profile's `hotkeys`; `Vet` rejects any other draft. Matt changes launchers and bindings in his repo, where review happens. (C) Restrict `spawn` as in B, but let a draft rebind combos to an allowlist of `run` verbs (`focus`, `swap`, `stack`, `split`, `merge`, `resize`, `layout`, `move-mode`, `toggle-float`).
   **Prior art:** the MCP spec quote under question 3: the client may ask the user, but nothing makes it. Browser extensions and VS Code workspace trust both split "may change settings" from "may run code" in the same way as B.
   **Recommendation:** **B.** It is the smallest rule that makes "an agent writes and switches profiles" safe to leave on: nothing an agent writes can start a process tess did not already start for Matt's live profile. C can come later if Matt wants agents to suggest bindings.

## Direction

**Product fit.** Matt's ruling of 2026-11-09 asks to port everything into tess, move the keybinds into tess, align with the native app, and replace yabai as the backend, "not considering how we implemented it before". RIG-8858 tracks it. The frozen records already put tess's model at the centre: arrangements and topologies (`tess-app.md`, `grid-layouts.md`), named slots (`window-slot-memory.md`, `deskSlots`), the laptop flex model (`laptop-flex-spaces.md`) and arrival placement (`new-window-placement.md`). Today each of these is translated into yabai's split tree by `internal/wm/yabai`, and most of the pain in RIG-8691, #73 and #75 sits in that translation. This record removes the translation. tess becomes a **slot manager**: a layout is a set of slots, each slot holds a stack or a split of stacks that stays inside the slot (Decision 13; its shape is Open Question 1), and windows fill slots by profile rules. It is not a workspace-wide BSP tiler. Matt's rulings of 2026-11-12 add a pause and a move budget, so a fault stops tess instead of moving windows in a loop (Decision 14), and a way for an AI agent to build and try profiles (Decision 15).

**Scope.** Repo `mattwilkinsonn/tessera` for the code, including tess's own scripting addition under `sa/`. Matt's config (skhd, yabai, sketchybar, launchd agents, sudoers and the profile) changes downstream in his nix repo, named here only as the downstream consumer. The Windows (komorebi) code is deleted (Decision 6); macOS is built behind interfaces that a later platform can implement. No hosted service and no usage-billed resource.

**Prior art.**

- [yabai](https://github.com/koekeishiya/yabai) (MIT) and the [Christian-SC26 fork](https://github.com/Christian-SC26/yabai) Matt runs. The reference for how macOS window handling works: AX observers per app (`application_observe` in `src/application.c`), frame writes (`window_manager_set_window_frame` in `src/window_manager.c`) and focus (`window_manager_focus_window_with_raise`), all **both**. Its scripting addition is the starting point of tess's own: `src/sa.m` (client), `src/osax/payload.m` (code that runs in Dock), `src/osax/loader.m` (injection) and the per-release tables `src/osax/arm64_payload.m` and `x64_payload.m` (SC26). Borrow: the socket protocol, the handshake that reports a version and a capability bitmask, and the byte-pattern tables. Do not borrow: the BSP tree, rules or the mouse tap.
- [AeroSpace](https://github.com/nikitabobko/AeroSpace). Its README says it uses "only a single private API to get window ID of accessibility object `_AXUIElementGetWindow`" and "will never require you to disable SIP". Its [guide](https://nikitabobko.github.io/AeroSpace/guide#emulation-of-virtual-workspaces) places inactive windows "outside the visible area of the screen, in the bottom right or left corner", requires that "every monitor has free space in the bottom right or left corner", and restores on activation. Its hotkeys use [soffes/HotKey](https://github.com/soffes/HotKey), which calls Carbon `RegisterEventHotKey`. Borrow the hotkey API and the per-app AX threads; for stage V, also the park and activation-restore rules. Matt's AW is outside the arrangement AeroSpace supports ([#382](https://github.com/nikitabobko/AeroSpace/issues/382)).
- [FancyZones](https://learn.microsoft.com/en-us/windows/powertoys/fancyzones), [Divvy](https://mizage.com/downloads/DivvyMacHelp.pdf), [Rectangle Pro](https://rectangleapp.com/pro/docs/layouts) and [Moom](https://manytricks.com/moom/help/snap.html): slot and zone managers. Borrow: a highlighted target while the user picks a slot (FancyZones, Moom), Escape to cancel (Divvy, Hammerspoon `hs.grid`), and saved layouts that the user updates explicitly (Rectangle Pro, Moom). None of them has tess's automatic arrival placement.
- [niri tabs](https://niri-wm.github.io/niri/Tabs.html): a column shown as tabs with a side indicator. Borrow for stacks: one slot, many windows, one front window, keyboard cycling.
- [Amethyst](https://github.com/ianyh/Amethyst). Public AX on top of native Spaces. Its "throw window to space" broke on macOS 15 ([#1662](https://github.com/ianyh/Amethyst/issues/1662)); the maintainer states: "The mechanism by which we do it is no longer possible, it seems." Keep the warning: native Space moves need private APIs.
- [Rectangle](https://github.com/rxhanson/Rectangle) and [skhd](https://github.com/koekeishiya/skhd): Rectangle registers hotkeys with Carbon through MASShortcut and needs no permission prompt; skhd uses a `CGEventTap` (`event_tap_begin` in `src/event_tap.c`), which needs Accessibility. tess follows Rectangle.
- Splits within a slot (Decision 13). [i3](https://i3wm.org/docs/userguide.html#_tree): "Every window can be split horizontally or vertically again, just like the workspace." [niri](https://niri-wm.github.io/niri/Tabs.html): "You can switch a column to present windows as tabs, rather than as vertical tiles." Borrow i3's split-and-merge on a leaf, but only inside one slot, and niri's rule that the slot (column) keeps its place in the layout whatever happens inside it. Open Question 1 compares the three shapes.
- Move budget (Decision 14). systemd stops restarting a unit that starts more than `StartLimitBurst` times in `StartLimitIntervalSec`, and the unit stays failed until a user resets it ([systemd.unit](https://www.freedesktop.org/software/systemd/man/latest/systemd.unit.html)). Borrow the shape: a count in a time window, a stop that persists, and a manual restart.
- MCP (Decision 15). The [official Go SDK](https://github.com/modelcontextprotocol/go-sdk) (v1.8.0, released 2026-09-14, maintained with Google) runs a server over stdio with `mcp.AddTool` and `server.Run(ctx, &mcp.StdioTransport{})`; [mcp-go](https://github.com/mark3labs/mcp-go) (v1.5.0, released 2026-10-30) is the third-party option (§ *Library choices*). The MCP spec's [tool annotations](https://modelcontextprotocol.io/specification/2025-06-18/server/tools) let a server mark a tool read-only or destructive. They are hints to the client, not security: "the protocol itself does not mandate any specific user interaction model". Rectangle Pro and Moom switch between saved layouts from a menu; borrow the menu and a one-step undo.

**Lifetime.** Years: this is the window manager of Matt's daily machine. The design is sized to that: each stage ships alone, every migration step is recorded and has a defined reverse, and every private call sits behind a capability that can report itself missing.

**Simpler option.** Keep yabai, finish #76 (events instead of sleeps) and move only the hotkeys and command routing into the daemon. That is stage S of this plan, and it ships value alone. It does not remove insert arms, the stack cap or staging floats, because those come from driving yabai's split tree from outside. Stage V without stage N (virtual workspaces, no SA) is simpler still, but it fails on the AW unless the parking spike passes.

## Problem / Intent

tess decides layouts in its own terms but realizes them by issuing yabai commands against yabai's split tree and native Spaces, then sleeping or polling until yabai catches up. The mismatch causes the known failures: insert arms that cannot be queried, `ErrSpaceFull`, focus waits that time out (Zen), rule-managed windows, floating staging windows stranded by a failed apply, and fixed sleeps. Make tess the macOS window manager as a slot manager: its own workspace and slot model is the state, it moves and focuses windows itself, it owns the hotkeys and the scripting addition, and every command runs in its daemon. The cutover is staged and never leaves Matt's desktop broken, and no part of tess depends on yabai at run time after stage N.

## Approach

### End state and stages

On macOS, tess owns window tracking, slot placement, focus, hotkeys, native Spaces (through its own scripting addition) and the state feed for the app and sketchybar. yabai does not run. Each stage ships alone and leaves a working desktop.

| Stage | Tasks | What changes | yabai | skhd | Rollback |
| --- | --- | --- | --- | --- | --- |
| S | S0–S4 | The Windows code goes. Every command runs in the daemon. tess registers the hotkeys from the profile, with repeat. Every binding keeps today's behaviour. | tiles | **removed** at S4 | re-enable the skhd agent |
| N-build | N1–N11, M1 | tess's scripting addition, Space service, slot model, platform layer, native driver with pause and move budget, slot interaction with splits, the profile set and `tess mcp`; observe mode; actuation gate. Nothing moves on the daily desktop. | tiles | gone | none needed |
| N-switch | N12–N13 | The recorded migration stops yabai and starts the native backend; two-week soak. | stopped, installed | gone | `tess migrate --reverse` |
| N-clean | N14 | The yabai driver, staging, insert arms and tree verbs are deleted. tess has no yabai code path. | **removed** | gone | revert the release, reinstall yabai |
| V (optional) | V0–V3 | V0 runs the parking spike on Matt's geometry at any time. V1–V3 run only if V0 passes and Matt opts in. | removed | gone | `tess migrate --reverse` to native Spaces until V3 |

Two WMs never run at once. From N12 on, yabai is stopped before tess's native backend starts, and Dock is restarted in between so yabai's payload is gone before tess's loads (§ *Migration*).

### Slots: the window model is tess's state

tess is a slot manager. A **workspace** is one layout on one display. A layout is an ordered list of **slots** along one axis, one weight per slot, as `grid-layouts.md` defines tracks: a slot is one track. A slot holds one or more **panes**, and a pane holds a **stack** of windows with one front window, as a niri tab column does. Most slots have one pane, so the slot is a stack. The user can split a slot into panes (Decision 13, § *Splits within a slot*); how deep a split goes is Open Question 1, and the record shows its option A, which option B extends. New windows go into slots by the profile's arrival rules (`new-window-placement.md`). The keyboard moves windows between slots and panes. There is no workspace-wide split tree: a split never reaches past its slot.

A new package, `internal/wm/native`, holds a `Model` with two parts.

- **Lifecycle** is committed state: which windows exist, their pid, native Space, observed frame, apply status, AX attachment and tombstones of closed windows. Only platform events, Space-service notices and operation results change it. Lifecycle is never part of a job's working copy and is never discarded.
- **Desired** is the layout: workspace membership, slots, panes, weights, temporary weight overrides, front windows, window modes and arrival targets. Jobs edit it on a working copy (§ *Job transactions*). The screen is made to match it.

The parts hold these records:

- **Display:** keyed by the UUID from `CGDisplayCreateUUIDFromDisplayID` (`display_uuid` in `src/display.c`, **both**), so profile `deviceId` values and `ResolveDisplay` keep working.
- **Workspace:** an ID, a label, its display, a kind (`columns`, `rows` or `stack`), an ordered list of slots, one weight per slot from the profile, an optional override weight vector (§ *Temporary slot resize*), and an arrival target. A workspace is bound to one native Space ID. In stage V it is a tess record only. The label is a tess field; macOS has no Space labels, and yabai's labels were yabai state.
- **Slot:** a name (from `deskSlots` or the layout's track names), an ordered list of one to four panes, a split axis (`columns` or `rows`, used only with two or more panes), one weight per pane, and the index of the pane focused last.
- **Pane:** an ordered window list and a front window. A pane is never empty: when its last window leaves, the pane is removed, and a slot left with one pane is a plain stack again.
- **Window:** a CGWindowID from `_AXUIElementGetWindow`, which is the number yabai reports, so `window-slots.json` survives the switch. Its mode is `slotted` (in a pane of a slot), `floating` (own frame, pinned on top, with a home slot), `excluded` (not managed) or `suspended` (native fullscreen, or on a native Space tess does not manage).
- **Layout** is a pure function: `Frames(area wm.Frame, ws Workspace) map[int]wm.Frame`. The area is the display frame minus the bar inset, 32 pt at the top, the same as `external_bar all:32:0` in Matt's yabairc today. It uses the override weights when they are set. It divides each slot's frame along the slot's axis by the pane weights. Every window in a pane gets the pane's frame. Each pane's front window is raised.

The native backend implements `wm.Driver` against this model and a `wm.SpaceService` (§ *The Space service*).

| `wm.Driver` call | Native meaning |
| --- | --- |
| `CreateSpace`, `DestroySpace`, `MoveSpaceToIndex` | Space-service call, an irreversible checkpoint; then bind or unbind the workspace record. Without the capability, see Decision 10. |
| `LabelSpace` | Edit the workspace record only. |
| `MoveWindowToSpace` | Move desired membership, then a Space-service call and an irreversible checkpoint. |
| `RealizeSpaceLayout(target)` | Set the workspace's kind, slots and profile weights to `target`. Whether the override survives is decided by the command, not the driver (§ *Temporary slot resize*): the driver takes `clearOverride bool` from the planner. |
| `ToggleFloat` | Float and pin the window, recording its home slot; or return a floating window to its home slot (§ *Float and pin*). |
| `StackOnto`, `SwapWindows`, `FocusWindowDir`, `ResizeWindow` | Edit panes, slots or weights; resolve direction from the computed pane frames (§ *Splits within a slot*). |
| `FocusWindow` | Checkpoint, then focus per § *Focus*. |
| `Query*` | Read desired and lifecycle state together. |

### What the model deletes, and what must change first

- **Insert arms.** The arrival target is a field in the model, not a one-shot toggle inside yabai. `ArmInsert` and the `insert` verb go (Decision 5).
- **`ErrSpaceFull` and the stack cap.** A slot is a Go slice. The cap is enforced before the driver sees any work: `evacuateWindows` in `internal/engine/desk.go` checks `if managed >= wm.StackWindowCap {`. N3 moves the cap behind `wm.Capabilities`.
- **Staging floats.** `DeskPlan` floats slot owners (`FloatWindowOp{Staging: true}`) so that yabai's tree can be rebuilt in order, and `stagingPlan.before` in `internal/commands/staging.go` persists them with `effects.WriteStagedWindows` before the driver is called. N3 makes both depend on `Capabilities().NeedsStaging`.
- **Settle waits.** `SettleUnit` and the tiling sleeps in evidence § *#76 sleep sites* go, because a slot change is a model edit confirmed by read-back. The Space sleeps become postcondition checks in the Space service (N5).
- **yabai rules.** `Rules` in `internal/commands/orchestration.go` installs `auto:*` rules. Native admission is a model decision: a window is managed when its subrole is `AXStandardWindow`, it is not native fullscreen, and it matches no entry in a new profile list `unmanaged`. The defaults reproduce today's exceptions (Finder, System Settings, 1Password, Akiflow dialogs). The laptop routing rules (`auto:code`, `auto:linear`, `auto:akiflow`, `auto:default-laptop-stack`) become arrival targets in the native `HandleWindowCreated` path.
- **Tree verbs and BSP terms.** `insert`, `warp`, `reset-splits`, `balance`, `columns`, `space bsp` and `drop-action`, and the driver terms `SplitType`, `ToggleSplit`, `SetSplitRatio`, `LayoutBSP`, `WarpWindow` and `ArmInsert`, have no slot meaning. They are removed at N14 (Decision 5).

### Workspaces: native Spaces now, virtual later

Stage N binds each workspace to a native Space and drives Spaces through tess's own scripting addition (§ *tess's scripting addition*). Stage V replaces native Spaces with virtual workspaces if the parking spike passes. The table compares them on Matt's displays.

| Concern, on Matt's displays | N: native Spaces, tess SA | V: virtual workspaces |
| --- | --- | --- |
| AW above the G9, no free bottom corner | No parking | Must park on another display; AeroSpace does not support this; V0 measures it |
| Cmd-Tab, Dock or a notification activates a hidden window | macOS switches the Space; tess attaches AX and flushes the newly visible workspace | tess must detect the activation and restore the workspace |
| Native fullscreen | Native; window is suspended in its slot | Suspended; its Space is a stray Space |
| Mission Control, Space animation, laptop flex "a new app gets a space for free" | Unchanged | Parked windows show small; no animation; flex spaces become records |
| Crash of tess | Windows stay where they were | Windows stay parked until restart or rescue |
| Focus on another Space (Zen) | SA focus, else gesture fallback, with a verified postcondition and one budget. N1 and N11 measure the success rate with a stated trial count; this record does not claim the Zen failure is fixed | No Space wait |
| SIP | Partly off for full capability; tess still runs with SIP on (§ *Capabilities and SIP*) | Can be turned back on (Decision 2) |
| Per-release payload patches | tess owns them | None after V3 |
| Migration and its reverse | Stop yabai, load tess's SA; reverse restarts yabai (§ *Migration*) | Destroy native Spaces through tess's SA; reverse recreates them |

N removes every tiling failure named in § *Problem / Intent* with a payload tess controls. V also retires SIP and the payload, but needs parking on an arrangement that AeroSpace, the one mature implementation, does not support.

### tess's scripting addition

**Why an SA.** Creating and destroying a native Space works only from inside Dock. yabai does it with code injected into Dock: `space_manager_add_space` returns `scripting_addition_create_space(sid) ? SPACE_OP_ERROR_SUCCESS : SPACE_OP_ERROR_SCRIPTING_ADDITION` (**both**). tess owns that code now (Decision 1).

**Source.** tess forks the SA from SC26 `a26707a`, which is MIT licensed (`LICENSE.txt`: "Copyright (c) 2019 Åsmund Vikane"). The files are `src/sa.h`, `src/sa.m`, `src/osax/common.h`, `src/osax/payload.m`, `src/osax/loader.m`, `src/osax/arm64_payload.m` and `src/osax/x64_payload.m`, about 2,850 lines in total (evidence § *Scripting addition source*). The payload, loader, pattern table and `common.h` move into a new top-level `sa/` directory with yabai's copyright and the MIT text. The loader keeps its comment `The arm64e injection path is based on work by Jeremy Legendre (https://github.com/jslegendre)`. The client half (`sa.h`, `sa.m`) is not copied: tess writes its client in Go (`internal/sa`) and reads `sa.m` as the reference. `THIRD_PARTY_NOTICES.md` (added empty in S0) gains the yabai notice and the Legendre attribution in N4.

**What tess keeps.** The payload in SC26 handles 19 opcodes (`enum sa_opcode` in `src/osax/common.h`). tess keeps six and removes the rest:

| Opcode | tess use |
| --- | --- |
| `HANDSHAKE` | version and capability bits |
| `SPACE_CREATE`, `SPACE_DESTROY` | `CreateSpace`, `DestroySpace` |
| `SPACE_FOCUS` | `FocusSpace` without animation |
| `SPACE_MOVE` | `MoveSpace` (Space order on one display) |
| `WINDOW_LAYER` | pin a floating window on top (§ *Float and pin*) |

`WINDOW_TO_SPACE` and `WINDOW_LIST_TO_SPACE` are dropped, because tess moves windows between Spaces from its own process (§ *Capabilities and SIP*). Opacity, shadow, sticky, scale, order, focus and the animation proxies have no slot meaning and go. Fewer opcodes means fewer byte patterns and Dock ivars to keep per macOS release [INFERENCE: N4 counts the patterns left after the cut].

**Protocol.** tess keeps yabai's wire format: a 2-byte length, a 1-byte opcode and packed arguments, over a Unix socket that the payload opens in Dock. `pack(v)` copies `sizeof(v)` bytes in host order with no padding (`#define pack(v) memcpy(bytes+length, &v, sizeof(v)); length += sizeof(v)` in `src/sa.m`, SC26). The payload finds the display of every Space operation itself, with `SLSCopyManagedDisplayForSpace` on a Space ID; no opcode takes a display ID. The kept arguments, from `src/sa.m` and the `do_*` handlers in `src/osax/payload.m` (SC26):

| Opcode | Packed arguments, in order | Meaning |
| --- | --- | --- |
| `HANDSHAKE` | none | reply: `OSAX_VERSION`, a NUL, a `uint32` attribute mask, then `\n` (`do_handshake`) |
| `SPACE_CREATE` | `uint64 sid` | adds a new Space on the display of `sid`, an existing Space; `do_space_create` resolves it with `SLSCopyManagedDisplayForSpace(..., space_id)` |
| `SPACE_DESTROY` | `uint64 sid` | removes `sid`; if it was active, Dock's new current Space is recorded |
| `SPACE_FOCUS` | `uint64 sid` | shows `sid` and hides the current Space of its display |
| `SPACE_MOVE` | `uint64 src`, `uint64 dst`, `uint64 src_prev`, `bool focus` (1 byte) | moves `src` after `dst`. `src_prev` is the Space to show on the source display first; tess sends 0, as `scripting_addition_move_space_after_space` does, because it only reorders on one display. `focus` is `false`: tess focuses with `SPACE_FOCUS` |
| `WINDOW_LAYER` | `uint32 wid`, `int32 key` | `SLSSetWindowSubLevel(..., wid, CGWindowLevelForKey(key))`: the argument is a `CGWindowLevelKey`, not a level |

tess renames the socket from `/tmp/yabai-sa_<user>.socket` (`SA_SOCKET_PATH_FMT`) to `/tmp/tess-sa_<user>.socket`, the bundle from `yabai.osax` to `tess.osax`, and sets its own `OSAX_VERSION`, starting at `tess-1`. So yabai's payload and tess's payload never answer each other's socket.

**Handshake.** yabai's handshake returns the payload version and a bitmask of what it found in Dock (`do_handshake` in `src/osax/payload.m`, SC26): `OSAX_ATTRIB_ADD_SPACE`, `OSAX_ATTRIB_REM_SPACE`, `OSAX_ATTRIB_MOV_SPACE` and others, each set only when its function pointer or pattern was found. tess reads the bitmask at load and after each Dock restart, and maps it to capabilities. A version that differs from the binary's own is a reload, never a silent use. yabai's client already notes the reason in `src/sa.m`: "The payload ignores a space move when it has no way to move spaces on this macOS version, but the socket write itself still succeeds." For the same reason a socket write is never taken as success: every Space operation checks its postcondition (§ *The Space service*).

**Install and load (`tess sa`).** Same steps as `scripting_addition_load` in `src/sa.m` (SC26), in Go with a small cgo part:

1. `tess sa status` (no root) reads SIP with `csr_get_active_config`, the boot-args for `-arm64e_preview_abi`, the installed bundle version, and the handshake. It prints the capability table below.
2. `tess sa load` (root) checks `CSR_ALLOW_UNRESTRICTED_FS` and `CSR_ALLOW_TASK_FOR_PID` and the boot-arg, installs `/Library/ScriptingAdditions/tess.osax` when the version differs, runs its loader, and waits for the handshake. Root comes from Decision 12.
3. **Only the native backend loads it.** The daemon runs `tess sa load` at start and after `NSApplicationDockDidRestartNotification` (the notification yabai observes in `src/workspace.m`, SC26) only when the profile says `backend: "native"`. With `backend: "yabai"`, `"observe"` or `"virtual"` it never loads, installs or talks to tess's payload. The one other caller is a test run by hand, `tess sa load --test`: it writes the restore record (step 5), boots out yabai's agent, then loads. `tess sa restore` ends every test run. A failed load is not fatal: the daemon logs it and runs with the capabilities it has.
4. `tess sa uninstall` (root) unloads by restarting Dock and removes `/Library/ScriptingAdditions/tess.osax`. Every reverse path runs it before yabai starts again (§ *Migration*).
5. **Crash-safe restore.** `tess sa load` first writes `sa-restore.json` in the state directory: `{"loaded_by": "tess", "yabai_agent_was_loaded": bool, "yabai_sa_was_installed": bool}`. `tess sa restore` reads it and returns the machine to that state: uninstall tess's payload, restart Dock, reinstall yabai's SA with `yabai --load-sa` if it was installed, and `launchctl bootstrap` yabai's agent if it was loaded. Each step checks its result first, so a second run after a crash does only what is left. The file is deleted only after the last step succeeds.

**Build.** The payload and loader are Objective-C built by `flake.nix` for `aarch64-darwin` into the release bundle, beside `tess`. tess embeds no payload binary in Go; `tess sa load` copies it from the bundle. The x86_64 table stays in the tree only if Matt keeps an Intel Mac [INFERENCE: none in the profile; N4 deletes `x64_payload.m` unless one exists].

**Per-release work.** The payload finds Dock functions by byte pattern and offset per macOS version and per CPU (`get_add_space_offset`, `get_add_space_pattern` in `src/osax/arm64_payload.m`, SC26). A new macOS release can break a pattern; a bad assumption can crash Dock (evidence § *What needs the scripting addition*). tess's rule: the payload refuses to hook a macOS version it has no entry for, and reports the capability as missing. It never guesses a pattern. Upstream yabai and SC26 changelogs stay the first place to look for a new pattern; tess ports a fix by hand and records its source commit in the table.

### Capabilities and SIP

tess runs with SIP fully on. It does less. Every capability is reported, never assumed.

| Capability | Needs | SIP fully on | Source of the fact |
| --- | --- | --- | --- |
| Move, resize, focus, slot layout | AX grant | yes | public AX |
| Hotkeys | none | yes | Carbon `RegisterEventHotKey` |
| Read Spaces and the windows on each | private SkyLight reads, no SA | yes [INFERENCE: N1 checks with SIP on] | `SLSCopyManagedDisplaySpaces`, `SLSCopyWindowsWithOptionsAndTags`, `SLSCopySpacesForWindows`, all called by yabai's daemon, not its payload (**both**) |
| Move a window to an existing Space | private SkyLight call, no SA | yes on macOS versions that have it [INFERENCE: N1 checks] | `space_manager_move_window_to_space` tries `SLSPerformAsynchronousBridgedWindowManagementOperation`, then `SLSMoveWindowsToManagedSpace`, before the SA (SC26) |
| Focus a Space | SA (`SPACE_FOCUS`); else a synthesized Dock gesture | with animation [INFERENCE: N1 checks with SIP on, across displays and to hidden Spaces] | `space_manager_focus_space_using_gesture` (**both**) posts Dock swipe events with `CGEventPost`; across displays it first warps the cursor with `CGWarpMouseCursorPosition` and then clicks the display centre; it skips a repeat to the same Space within 350 ms and refuses one within 150 ms of the last (SC26) |
| Create or destroy a Space | SA (`ADD_SPACE`, `REM_SPACE`) | **no** | `space_manager_add_space` (**both**) |
| Reorder Spaces | SA (`MOV_SPACE`) | **no** | `space_manager_move_space_after_space` (**both**) |
| Pin a floating window above slotted ones | SA (`WINDOW_LAYER`); else raise only | partly: raised, not kept on top | `do_window_layer` in `src/osax/payload.m` calls `SLSSetWindowSubLevel` (SC26) |

`wm.SpaceService.Capabilities()` returns this table as `wm.SpaceCaps`. A call to a missing capability returns `wm.ErrCapability{Name}` at once and runs nothing. The state stream and `tess sa status` carry the table, so the app and sketchybar can show it. Decision 10 decides what a job does when `space.create` is missing. With SIP on and no SA, the laptop flex model uses only the pooled Spaces; with the SA it works as today.

**Private API in tess.** The daemon calls `_AXUIElementGetWindow` and the SkyLight read and window-move calls in the table, each resolved with `dlsym` at start. A missing symbol turns off its capability; it does not stop the daemon. Every other private call lives in the payload.

### The Space service

`wm.SpaceOps` has no focus or window-to-Space call, and today's yabai driver hides fixed waits inside them. N5 adds a separate interface, `wm.SpaceService`, with one implementation, `internal/wm/native/spaces`, that uses tess's SA client and the SkyLight calls above.

**Contract for every operation.** The service subscribes to its notices before it acts. Right after the action it checks the postcondition once, which covers both an already-satisfied target and a notice that arrived early. It then waits for a relevant notice or a poll tick, whichever comes first, within the operation's budget. A met postcondition returns nil. A spent budget returns `*wm.SpaceOpError{Op, Outcome: wm.OutcomeUnknown}`. The caller then resolves the outcome with `Snapshot` (§ *Job transactions*).

| Operation | Postcondition (from `Snapshot`) | Notice | Poll fallback | Budget |
| --- | --- | --- | --- | --- |
| `CreateSpace(display)` | A user Space on `display` that was not in the before-set. The service picks the anchor: the display's current Space ID from `Snapshot`, sent as `SPACE_CREATE`'s `sid`; a display with no user Space returns `ErrCapability{"space.create.no-anchor"}` | SkyLight notify 1327 | 50 ms | 1 s |
| `DestroySpace(sid)` | `sid` absent; already met if absent before | SkyLight notify 1328 | 50 ms | 1 s |
| `MoveSpace(sid, index)` | `sid` at `index` on its display | none | 50 ms | 1 s |
| `FocusSpace(sid)` | `sid` is the active Space of its display; already met if visible | `NSWorkspaceActiveSpaceDidChangeNotification` | 50 ms | 1 s on the SA path; 2 s on the gesture path |
| `MoveWindowToSpace(win, sid)` | window's Space is `sid`; already met before the call | none: the bridged move is asynchronous and the active Space does not change | 25 ms | 500 ms |

yabai learns of Space create and destroy from private SkyLight connection notifications 1327 and 1328, which it registers on macOS 13 (Ventura) and later (`SLSRegisterConnectionNotifyProc` in `src/yabai.c`, posted as `SLS_SPACE_CREATED` and `SLS_SPACE_DESTROYED` from `src/mission_control.c`, SC26). tess registers the same two and also polls `SLSCopyManagedDisplaySpaces`. Labels are tess records and need no Space call. The budgets are first values; N11 records each operation's measured time and N13 tunes them.

**Discovery of hidden windows.** Public AX does not return windows on inactive Spaces. yabai notes this in `window_manager_add_existing_application_windows` ("MacOS API does not return AXUIElementRef of windows on inactive spaces") and brute-forces `_AXUIElementCreateWithRemoteToken` (**both**). tess does not copy that. `Snapshot` reads every Space with `SLSCopyManagedDisplaySpaces`, the windows on each with `SLSCopyWindowsWithOptionsAndTags`, and pid, frame, title and layer from `CGWindowListCopyWindowInfo` (public). AX attaches lazily: when a Space becomes visible, the app's worker reads its `kAXWindowsAttribute` and matches elements to IDs with `_AXUIElementGetWindow`, then reads role and subrole. A window with no AX element has status `hidden` and gets no writes. Its membership is still complete, so the planners see every window.

### Job transactions

A job is one CLI request, hotkey, Hub cascade or arrival. It edits a working copy of desired state.

- **Checkpoint.** A checkpoint commits the working copy and flushes it. Checkpoints happen at job end, before every `FocusWindow`, and before every Space-service call.
- **Commit merges with lifecycle state.** Lifecycle wins on existence: a working-copy edit that names a tombstoned window is dropped and logged, so a closed window is never brought back. A window that appeared after the copy was taken is added from lifecycle state at its arrival target. The working copy wins on layout.
- **Abort.** On error or cancel, edits since the last checkpoint are discarded, in-flight worker operations of the job are fenced, and nothing more is flushed. `PreserveFocus` in `internal/commands/focus.go` restores focus after the discard.
- **Space operations are irreversible checkpoints.** A Space-service call changes macOS, and a later abort cannot undo it. Its result goes into lifecycle state at once. Desired state follows reality: after a successful `MoveWindowToSpace`, the window's membership is its new Space even if the job later aborts. One compensation exists: a `CreateSpace` whose job aborts before anything is placed on the new Space destroys it again. An `OutcomeUnknown` is resolved by `Snapshot`; the resolved state goes into lifecycle state, and the job returns an error that names the resolved outcome.

The yabai backend has no working copy and keeps today's behaviour until N14 deletes it.

### Desired and observed state

Each window's lifecycle state carries an observed frame, its read time and an apply status: `ok`, `pending`, `constrained` (position matches, size differs, as with a terminal that snaps to cells), `refused` (position or visibility does not match after a retry), `hidden` (no AX element yet), `dragging` (a mouse button is down on it) or `suspended`.

**Operations and fencing.** Every request to a per-app worker carries an operation ID from one counter, and every frame write carries the window's write generation. The actor accepts a result only if its operation ID is the latest one issued for that window and was not fenced. A fenced or older result is dropped. If the window then has no operation in flight, the actor asks for a fresh read-back. A result that arrives after its waiter timed out is therefore never applied as current state.

**Pending writes.** A frame write has a deadline of 500 ms [INFERENCE: tuned in N11]. Moved and resized events for that window before the deadline schedule a read-back and are never classified as a drag. Several events in one run-loop cycle coalesce: only the latest read-back counts. A matching read-back sets `ok`. At the deadline a mismatch gets one more write; a second mismatch sets `constrained` or `refused`. An AX write returns success even when the app ignores it; the [go-macos/accessibility](https://github.com/go-macos/accessibility) README measured this: "A write to `kAXPositionAttribute` returns `kAXErrorSuccess` whether or not the application honours it."

**Moves with no pending write.** If a mouse button is down (`NSEvent.pressedMouseButtons`), the move is a user drag: the window enters `dragging`, and § *Float and pin* applies. tess writes no frame to a `dragging` window. Otherwise it is an OS relocation, such as a clamp after a display change. An OS relocation updates lifecycle state only and does not change membership; the next flush re-asserts the desired frame once. tess has no mouse tap and no modifier gestures.

**Reporting.** `Flush` returns `*wm.ApplyError{Refused []int}` when a visibility or focus change was refused; a `constrained` size is not an error. The state stream carries each window's status, and a workspace with a `refused` window reports `fresh: false`. `tess query` shows the same.

**The actor never blocks.** One actor goroutine owns the `Model`. It selects over job requests, worker results, platform events, Space-service notices and timers. A driver call from a job goroutine is a request with a reply channel. Model edits reply at once. `Flush`, `FocusWindow` and Space-service calls register a waiter and send work out. The actor keeps processing events and resolves the waiter only when its postcondition holds in lifecycle state, or when the budget ends. cgo callbacks only send small values (pid, window ID, event kind) to channels.

### AX threading

AX calls block until the target app answers or the messaging timeout ends. So no AX call runs on the main thread, and no app can stall another app or a hotkey.

- **One worker per app.** Each managed pid gets one worker: an OS thread (a goroutine under `runtime.LockOSThread`) with its own `CFRunLoop`. The worker owns the app's `AXUIElement` and every window element of it. Every request for that app (attach, frame write, read-back, focus) goes to the worker's channel and runs in order. Requests for different apps run in parallel. The actor never waits on a worker; it gets a `Result` later (§ *Desired and observed state*).
- **Observers.** The worker creates the app's `AXObserver` with `AXObserverCreate`, adds its run-loop source to its own run loop, and adds notifications for the app element (window created, focused window changed, app hidden and shown) and for each attached window (moved, resized, minimized, deminimized, title changed, destroyed). It adds a window's notifications when it attaches the window and removes them on the destroyed notice. The observer callback copies `(pid, window ID, kind)` into a C ring and signals Go; it never calls Go directly.
- **Reference lifetime.** A window element is retained when the worker attaches it and released on the destroyed notice, on detach (the window leaves management), or when the worker stops. No AX reference leaves its worker; the actor and jobs know windows only by ID. An element that returns `kAXErrorInvalidUIElement` is released and its window gets a read through `Snapshot`.
- **App quits.** On `NSWorkspaceDidTerminateApplicationNotification` the actor fences every in-flight operation for that pid and tells the worker to stop. The worker removes the observer source, releases all elements and exits its run loop. Results that arrive after the fence are dropped. A request queued for a stopped worker returns `ErrAppGone` without an AX call.
- **Hung app.** Each element has `AXUIElementSetMessagingTimeout` of 250 ms. A request that times out returns `kAXErrorCannotComplete`; the worker marks the app `slow` and drops queued reads for it, keeping only the latest write per window. Three timeouts in a row mark the app `unresponsive`: the actor stops sending it writes until an AX notification arrives from it.
- **AppKit on the main thread.** Everything that touches AppKit (the indicator and pick panels, `NSRunningApplication activateWithOptions`, `NSEvent` reads) is sent to the main thread with `dispatch_async(dispatch_get_main_queue(), …)` from the cgo layer, never called from a Go goroutine. Main-thread work is short and never makes an AX call, so hotkey events are never queued behind an app.

### Focus

`FocusWindow(id)` resolves on one postcondition: lifecycle state shows `id` as the focused window, from the AX focused-window event or a read of `kAXFocusedWindowAttribute` on the frontmost app. A worker that returns success does not complete the focus. Steps:

1. If the window's Space is hidden, `FocusSpace` on the Space service, inside its own budget: 1 s on the SA path, 2 s on the gesture path (§ *The Space service*).
2. Attach AX if needed, then flush the now-visible workspace.
3. `AXMain` true, `AXRaise`, then `NSRunningApplication activateWithOptions`.

**Budgets nest.** Steps 2 and 3 share a 1 s window budget that starts when step 1 meets its postcondition, or at once when the Space is already visible. So the whole call is at most 2 s on the SA path and 3 s on the gesture path. A step-1 timeout returns at once with the Space outcome, and step 3 does not run. The hotkey repeater (§ *Hotkeys*) never queues a run while one is in flight, so a 3 s focus delays only its own binding.

**Gesture pacing.** The gesture path posts at most one gesture sequence at a time, and waits at least 150 ms after the last one, because yabai's own path refuses a gesture within 150 ms of the previous (`Guard 2` in `space_manager_focus_space_using_gesture`, SC26). Across displays it warps the cursor to the target display first, as yabai does, and puts it back after the postcondition.

**No Space focus.** When `SpaceCaps.Focus` is false (no SA and no gesture path, for example when N1 finds the gesture path fails with SIP on), step 1 is skipped and step 3 runs on the hidden window. macOS may switch to the app's Space on activation [INFERENCE: depends on the "When switching to an application, switch to a Space with open windows" setting; N1 measures it]. If the postcondition is not met, `FocusWindow` returns `wm.ErrFocusTimeout` with `Reason: "space-focus-unavailable"`, and `focus-slot` on a hidden workspace reports the same. tess never focuses a different window instead.

A miss returns `wm.ErrFocusTimeout` with the focused window, the target, its app and a reason (`space-timeout`, `window-timeout` or `space-focus-unavailable`). This replaces the 50 ms poll in `unfloatOne` (`internal/wm/yabai/driver.go`). yabai focuses with private SkyLight calls (`window_manager_focus_window_with_raise`, **both**); tess uses AeroSpace's public path, and N1 measures whether it is enough for Zen. If it is not, N1 reopens this record with the measured rate; the SA's `WINDOW_FOCUS` opcode is the fallback to restore.

### Lifecycle contract

Desired membership changes only through: a tess command or hotkey, arrival placement, a drag (which floats the window), a Space-service result, a window closing, and display loss handled by the existing desk-to-laptop cascade. Every other event changes lifecycle state only.

| Event | N: native Spaces | V: virtual workspaces |
| --- | --- | --- |
| Cmd-Tab, Dock or notification activates a window on a hidden workspace | macOS switches the Space; tess attaches AX and flushes the dirty workspace | tess makes the window's workspace active and un-parks it |
| Native fullscreen enter, exit | `suspended` in its slot; on exit, the next flush returns it | same; the fullscreen Space is a stray Space |
| A native Space tess did not create (user or app) | tess binds a new workspace record to it, kind `stack`, and adopts its windows | stray Space; its windows are `suspended`; tess never destroys it |
| Window created while its Space is hidden | Snapshot adds it to lifecycle state; it joins that Space's workspace at the arrival target, `hidden` | parked at once |
| Dock restart | § *Dock restart* below | no effect |
| SA load fails or a capability is missing | the daemon keeps running with fewer capabilities; `tess sa status` and the state stream name what is missing | n/a |
| Unplug with inactive workspaces | macOS moves the Spaces; the display cascade re-homes them per the arrangement | the cascade re-homes them; parked windows are re-parked after a ledger write |
| Lid close, reconnect | display UUID returns; the 3 s `DisplayQuiet` debounce runs the cascade | same, then un-parks the active workspaces |
| Daemon cold start | `pause.json` first: when paused, tess moves nothing (§ *Pause and move budget*). Snapshot gives every window on every Space; `native-state.json` gives desired membership, splits and floating windows; windows unknown to it join their Space's workspace at the arrival target; windows gone from Snapshot get tombstones | ledger first, then the same |

**Dock restart.** The payload dies with Dock, and Space IDs may change [INFERENCE: N11 measures it]. On `NSApplicationDockDidRestartNotification` the actor, in order:

1. Sets every SA bit in `SpaceCaps` to false at once, so a new Space call returns `ErrCapability` and sends nothing to a dead socket.
2. Fences every Space-service operation started before the notice. Its waiter returns `OutcomeUnknown`; a later result for it is dropped. Window frame writes are not fenced, because they go to apps, not Dock.
3. Holds jobs: new jobs wait in the queue. Hotkeys keep registering presses into that queue. This recovery hold is a separate flag from the durable pause (§ *Pause and move budget*); ending the hold never clears a pause.
4. Takes a `Snapshot` and reconciles it. A workspace bound to a Space ID that no longer exists is re-bound by matching its window set: the new Space that holds the most of its windows, on the same display. A workspace with no match keeps its windows' memberships and gets its Space from them. Every reconcile is logged with old and new IDs.
5. Runs `tess sa load` (backend `native` only) and reads capabilities from the new handshake. A failed load leaves the SA bits false; tess keeps running with SIP-on behaviour and the state stream names the failure.
6. If tess is not paused, re-applies `WINDOW_LAYER` to every pinned window when `PinLayer` is back; otherwise marks them `pinned: false`. If tess is paused, it sends no `WINDOW_LAYER`, marks every pinned window `pinned: false`, and `tess start` re-pins them with its flush.
7. Ends the hold. If tess is paused, queued jobs then meet the pause and return `ErrPaused`, and `tess start` stays reachable because it is a control message, not a job. A job that was running when the notice came gets `OutcomeUnknown` from its fenced Space call and aborts per § *Job transactions*.

Steps 4 and 5 read state and reload the payload; neither moves a window, so they run while paused too.

### Pause and move budget

Decision 14 gives tess a way to stop by hand and a way to stop itself. Both rest on one record, `pause.json` in the state directory beside `native-state.json`: `{"by": "user" | "budget", "reason": "<text>", "at": "<RFC 3339 time>"}`. The file exists only while tess is paused.

**Persistence, fail closed.** The in-memory pause flag is the gate; the file makes it survive a restart. To pause, the actor sets the flag first, then writes `pause.json.tmp`, `fsync`s it, renames it to `pause.json`, and `fsync`s the state directory, so the rename itself survives a power loss. Only then does `tess stop` reply. If any step fails, the flag stays set, the reply is an error that says the pause holds only until the daemon exits, and the state stream shows `paused.durable: false`; the actor retries the write on each later event. At daemon start, a `pause.json` that is present but unreadable or invalid counts as a pause with `by: "budget"` and reason `pause.json unreadable: <error>`, never as running.

**The control path.** `stop` and `start` are not jobs. The daemon recognizes their argv when it reads the request and routes them as control messages straight to the actor, ahead of the job queue, so a blocked or long job never delays them. The CLI, a hotkey and the MCP `stop` tool all send the same argv, so all three take this path. Every other command goes through the queue as before. The actor alone owns cancellation: each job runs under a context that the actor cancels.

**`tess stop [reason]`.** The actor, in order:

1. Sets the pause flag. From this point, the **dispatch check** applies: every send to an app worker and every Space-service call first checks the flag in the actor, and a paused actor sends nothing. The worker also checks a shared atomic copy of the flag before each AX write it takes off its queue, so a write queued before the stop is dropped, not sent.
2. Cancels the running job's context; its next driver call returns `ErrPaused`, and it aborts per § *Job transactions*.
3. Fences every frame write and Space-service call in flight, so a late result is dropped.
4. Persists `pause.json` with `by: "user"` as above, then replies, and shows the top-right indicator (§ *Move mode*, *Indicator*) with `tess paused: <reason> — tess start to resume`.

The stop boundary is step 1. After it, tess dispatches no new action. An action already handed to macOS before it cannot be recalled: an AX write the app is already running, or a Space call Dock already took. tess does not claim to undo those; their results go into lifecycle state as § *Job transactions* says for irreversible steps, and the reply lists them as `in flight at stop: <n>`.

**While paused**, tess changes nothing on screen:

- no frame write, no focus, no Space-service call, no `WINDOW_LAYER`;
- a command or hotkey that would do one of those returns `wm.ErrPaused{By, Reason}`, which the CLI prints as `tess is paused (<by>): <reason>; run tess start` with exit code 3;
- platform events, Space notices and `Snapshot` still update lifecycle state, so tess knows the screen when it resumes;
- read-only commands still work: `tess query`, `tess sa status`, `tess profile list|show|vet`, and the state stream, which carries `paused: {by, reason, at}`;
- `tess rescue` still works, because it runs without the daemon;
- arrivals are recorded at their arrival target in desired state but not placed.

**`tess start`** is a control message too. With the flag still set and `pause.json` still on disk, it takes a `Snapshot` and reconciles it. That is the preflight. If the preflight fails, tess stays paused and `start` returns the error. If it succeeds, tess removes `pause.json`, `fsync`s the state directory, then clears the flag, resets the move budget and flushes once. If the removal fails, tess stays paused and `start` returns the error. The flush counts against the new budget, so a fault that is still there pauses tess again within one budget window, and the reason names it.

**Move budget.** The actor counts every frame write and every `MoveWindowToSpace` it issues, per window, in a sliding window. A read-back, a user drag and an OS relocation are not counted; the one re-assert after an OS relocation is. It also counts failed wakes: a `WakeRoutine` or `DisplayQuiet` cascade that returns an error. The first limits are:

| Limit | Value | Reason |
| --- | --- | --- |
| moves of one window | 10 in 30 s | an `apply` writes each window once, a mismatch gets one retry, and arrival plus a cascade add a few more; a fight with an app or a feedback loop writes far more |
| failed wakes | 3 in a row within 10 min | one failure can be a slow display; three in a row is a fault that a retry will not fix |

[INFERENCE: both values are first guesses; N11 records the highest honest count per window, and N13 tunes them.] On a breach the actor does what `tess stop` does, with `by: "budget"` and a reason that names the window, its app and the count (for example `window 4711 (Zen) moved 11 times in 30 s`), or the last wake error. It also logs the last ten operations for that window. Only `tess start` resumes. A budget pause is never cleared by a daemon restart, a Dock restart or a profile switch.

The limits are `native.Budget{WindowMoves int; Window time.Duration; FailedWakes int; WakeWindow time.Duration}` with the defaults above; they are not in the profile, so an agent's profile cannot raise them (§ *Profiles and the MCP server*). The yabai path gets its own pause and budget in separate work; this record specifies only the native driver's.

### Migration: recorded and reversible

**Guarantee.** The reverse gives a deterministic, usable rebuild from the profile on yabai. It does not restore the exact desktop from before the migration, because yabai cannot rebuild a split tree it has lost (`space_manager_set_layout_for_space` calls `view_clear(view)`, **both**). After a reverse:

- every window is on the native Space it was on when the reverse started;
- every Space keeps its order and display; yabai labels come back from the snapshot;
- yabai runs with its own SA and today's yabairc;
- `tess apply` (desk) or `tess laptop` rebuilds each profile-defined layout on the yabai backend.

**State files.** `migration.json` records the direction (`forward` or `reverse`), the target (`native` or `virtual`), the current phase, and per-item progress. It is synced before each item. `migration-snapshot.json` is written once in the snapshot phase and never changed. `native-state.json` is the native model and is separate from both. Startup with an unfinished migration continues in the recorded direction, whatever the profile says. The profile's `backend` is read only when a new migration starts. When the profile and the recorded state disagree, the daemon moves nothing new, reports `fresh: false`, and names the command that settles it.

**To native (N12).** yabai stays installed until N14, so the reverse can start it again.

1. **Preflight.** AX is trusted for the stable daemon identity (N2). `tess sa status` shows the tess payload builds for this macOS version. N11 has passed. The profile is valid. No other migration is open.
2. **Snapshot.** Spaces (ID, index, yabai label, display), windows (ID, Space, frame, floating, stack index). Written to `migration-snapshot.json`.
3. **Adopt.** One workspace per native Space, with its yabai label as the tess label. Where a profile arrangement covers the Space (desk slots, the laptop flex model), slots and weights are the planner's target for it. Otherwise tess groups windows into slots by geometry: windows whose left edges are within 8 pt form one slot, ordered left to right; a weight is the slot's width divided by the area's width; windows that share one frame form a stack, with the topmost as front. yabai floating windows become `floating` with no home slot. Written to `native-state.json`.
4. **Stop yabai.** Two recorded items. First, `launchctl bootout` of the yabai agent, so yabai stops and no `dock_did_restart` signal reloads its payload. Second, restart Dock (terminate it; launchd starts it again), which drops yabai's injected payload; wait until Dock runs again. yabai's bundle stays in `/Library/ScriptingAdditions` but nothing loads it.
5. **Load.** `tess sa load`, which first writes `sa-restore.json` (§ *tess's scripting addition*); the handshake must report `tess-<n>`, not yabai's version.
6. **Switch bindings.** One recorded item, in one actor step: validate the native binding set, unregister every yabai-set binding, register the native set (§ *Bindings across the switch*). A validation failure stops the migration before any binding changes, and the reverse starts.
7. **Activate.** The daemon starts the native backend, takes a `Snapshot` and flushes.

**Reverse.** `tess migrate --reverse` records `direction: reverse` first. Then, one recorded item each and in the opposite order: tess stops writing frames; the binding set returns to the yabai set in one actor step; `tess sa restore` (uninstall tess's payload, restart Dock, bootstrap the yabai agent, which loads its own SA through its yabairc); labels from the snapshot are set again on Spaces that still exist; `backend` returns to `yabai`; the last step runs `tess apply` or `tess laptop` for the current topology. Spaces deleted since the snapshot are skipped. A display change during the reverse pauses it until the 3 s display debounce settles. Each item is idempotent, so a crash resumes the reverse. An item that fails three times stops the migration in `reverse-blocked`: tess moves nothing, and `tess migrate --reverse` runs the rest. The reverse works until N14 removes the yabai driver.

**After N14: the rollback recipe.** Before N14 deletes anything, N14's first commit adds `docs/runbooks/rollback-to-yabai.md`. It names:

- the tess release tag of the last release before N14, and its tarball checksum;
- the downstream commit that still holds yabairc, the yabai agent, the yabai Homebrew formula, the yabai sudoers entry, the yabai binding set and `backend: "yabai"`;
- the steps, in order: stop the tess daemon; `tess sa uninstall` with the current release; switch the downstream config to that commit and run the nix switch; install the pinned tess release; start it, which loads the yabai binding set; `tess apply` or `tess laptop`.

N14's gate runs the recipe once on Matt's Mac, checks the result against the § *Guarantee* list, then goes forward again with `tess migrate --to native`. The downstream commit is never garbage-collected: it is tagged `tess-yabai-rollback` in the downstream repo.

**To virtual workspaces (V2, only if V0 passes).** Snapshot; seed workspaces from native Space memberships; write the ledger; park the inactive workspaces; move every window to the first native Space of its display and destroy the others through tess's SA; check that AX is trusted and every window is adopted; only then `tess sa uninstall`. Reverse: load tess's SA, recreate the Spaces, move windows back per the snapshot, un-park.

### Bindings across the switch

The native backend has no tree verbs, and the yabai backend has no move mode. So from N12 one profile holds both sets, and the backend picks one. `#Hotkey` gains `backends?: [...("yabai" | "native")]`; a binding with no `backends` is in both sets. Matt's profile marks:

- yabai only: `insert stack`, `insert east`, `warp east`, `drop-action toggle`, `space bsp`, `reset-splits`, `columns`, `balance`;
- native only: `stack east`, `move-mode`, `layout reset` on cmd+alt 0, `layout save`, `split <dir>`, `split all`, `merge`, `stop`, `start`, `profile pick` and `profile back`;
- both: every other row of evidence § *Binding migration*.

A combo may appear once per set, so cmd+alt i is `insert stack` on yabai and `move-mode` on native. Profile load validates each set apart: every `run` must parse with `cli.Parse` and name a verb the set's backend supports (`wm.Driver.Supports(argv []string) bool`, new in N12). The switch is the one actor step in § *Migration* step 6; there is never a moment with half of each set registered. Before N12 every binding is in the yabai set, as S4 leaves them. N14 deletes the yabai-only bindings and the `backends` field, because one set remains.

### Parking and recovery (stage V only)

A window of an inactive workspace is parked so that only a 1 px column stays on screen, at a park corner. For each display, tess picks a bottom corner where the area below and beside it belongs to no other display. The AW has none, so its windows must park at the free corner of another display (evidence § *Live snapshot*). This is outside what AeroSpace supports ([#382](https://github.com/nikitabobko/AeroSpace/issues/382), [#413](https://github.com/nikitabobko/AeroSpace/issues/413)); V0 measures it. `native-state.json` is synced before a window is parked and holds its last visible frame.

`tess rescue` (N8, for both N and V) runs without the daemon and without the profile. It reads the ledger if one exists; otherwise it finds every window less than 10 % on screen. It moves each to the centre of the nearest display. It runs as the same signed executable as the daemon, so it has the same AX grant (N2).

### Hotkeys

**API: Carbon `RegisterEventHotKey`.** It needs no permission. [go-macos/hotkey](https://github.com/go-macos/hotkey) reports that registering three combos "on macOS 26.6.2 produced no dialog and needed no grant", and Rectangle (through MASShortcut) and AeroSpace (through soffes/HotKey) use the same call. A `CGEventTap`, which skhd uses, needs the Accessibility grant. Known limits:

- macOS 15 rejects Option-only and Option+Shift combos with error -9868 ([Apple forum thread 763878](https://developer.apple.com/forums/thread/763878)). None of Matt's 50 bindings use those modifier sets.
- Carbon sends `kEventHotKeyPressed` once per press and `kEventHotKeyReleased` once per release, with no repeat events. tess times repeat itself (below).
- A registered combo is taken from every app, as skhd does today.

**Repeat.** Any binding can set `repeat: true` (Decision 9). On press tess runs the command, waits the system's initial key-repeat delay (`NSEvent.keyRepeatDelay`), then runs it again every `NSEvent.keyRepeatInterval` until the released event. A repeat is queued only when the previous run of the same binding has finished, so a slow command never builds a backlog. A lost released event (for example, the screen locks while the key is held) is covered by a 10 s cap on one hold [INFERENCE: value is a guess; S3 logs real holds].

**Threading.** Carbon hotkey events, NSWorkspace notifications, display callbacks and the move-mode panels need the main thread's run loop. `cmd/tess` locks the main goroutine to the main thread in `init`. `tess daemon` runs `NSApplication` on that thread with the `accessory` activation policy: no Dock icon, but it may own windows. The `prohibited` policy is not usable, because Apple documents that such an app "may not create windows" ([NSApplication.ActivationPolicy.prohibited](https://developer.apple.com/documentation/appkit/nsapplication/activationpolicy-swift.enum/prohibited)). yabai calls `NSApplicationLoad` then `[NSApp run]` in `main` (`src/yabai.c`, **both**). All Go work runs on other goroutines; § *AX threading* says where AX work runs.

**Config in `profile.cue`.** A new `hotkeys` list:

```cue
hotkeys: [
	{key: "h", mods: ["cmd", "alt"], run: ["focus", "west"]},
	{key: "keypad1", run: ["focus-slot", "1"]},
	{key: "left", mods: ["shift", "cmd", "alt"], run: ["resize", "left"], repeat: true},
]
```

Key names cover letters, digits, arrows, `equal` and `keypad0` to `keypad9`, `keypadDecimal`, `keypadEnter`, `keypadPlus`, `keypadMinus`, `keypadDivide`, `keypadMultiply` and `keypadClear`. Each maps to one macOS virtual key code; skhd's `0x53` is `keypad1`. Profile load rejects an unknown key, a duplicate combo, and a `run` list that `cli.Parse` rejects. A press dispatches the parsed command into the daemon's job queue. A profile reload unregisters and registers the changed bindings.

**Binding transport in stage S.** Stage S moves all 50 bindings without changing what any of them does on yabai. 41 already run `tess` and move unchanged. Nine call yabai directly and get tess verbs that run the same yabai commands:

| skhd binding | tess binding in S4 | yabai command it runs |
| --- | --- | --- |
| `cmd + alt - n`, `- p` | `stack-cycle next`, `stack-cycle prev` | `--focus stack.next`, else `stack.first` (and the reverse) |
| `cmd + alt - s` | new `stack recent` | `--stack recent` |
| `shift + cmd + alt - f` | new `toggle-float center` | `--toggle float --grid 6:6:1:1:4:4` |
| `shift + cmd + alt - arrows` | `resize left`, `right`, `up`, `down`, with `repeat: true` | `--resize left:-100:0`, else `right:-100:0` (and so on) |
| `cmd + alt - d` | new `drop-action toggle` | flips `mouse_drop_action` and posts the same notification |

The tree verbs keep their yabai behaviour through stage S. From N12 they are bindings in the yabai set only (§ *Bindings across the switch*); N14 deletes them (§ *Slot verbs*). Evidence § *Binding migration* has all 50 rows.

### Slot interaction

The slot-manager prior art (evidence § *Slot-manager prior art*) agrees on four points that tess follows: slot identity is the source of truth, not raw coordinates; arrival placement is separate from direct moves; a target is highlighted before commit and Escape cancels; and a layout change is temporary until the user saves it.

#### Slot verbs

| Verb | Meaning on the native backend |
| --- | --- |
| `focus west\|east\|north\|south` | focus the front window of the neighbouring pane, found from the computed pane frames: a pane in the same slot first, then the nearest pane of the next slot |
| `swap <dir>` | swap the focused window with the front window of the neighbouring pane |
| `stack <dir>` | move the focused window into the stack of the neighbouring pane found by the `focus` rule, as its front; one rule whether that pane is in the same slot or the next |
| `stack recent` | move the focused window into the pane of the window focused before it |
| `stack-cycle next\|prev` | change the front window of the focused pane |
| `focus-slot <n>` | focus slot `n` of the active workspace, in the pane focused there last |
| `split <dir>` | move the focused window out of its pane into a new pane on the `<dir>` side, in the same slot (§ *Splits within a slot*) |
| `split all [columns\|rows]` | break the focused window's pane: one window per pane, up to the slot's free panes |
| `merge` | put every pane of the focused slot back into one stack |
| `stop`, `start` | § *Pause and move budget* |
| `profile list\|show\|vet\|write\|delete\|switch\|preview\|keep\|back\|pick\|promote` | § *Profiles and the MCP server* |
| `move-mode` | § *Move mode* |
| `toggle-float`, `toggle-float center` | § *Float and pin* |
| `resize <dir>\|grow\|shrink` | temporary weight change of a slot, or of a pane inside a split slot (§ *Temporary slot resize*) |
| `layout save\|reset` | § *Temporary slot resize* |
| `snap 3col\|50-50\|columns`, `space stack`, `apply`, `laptop` | unchanged meaning; set slots and profile weights, clear the override, merge every split |

`warp east` was used to pull a window out of a stack; `stack east` replaces it. `insert`, `balance`, `columns`, `reset-splits`, `space bsp` and `drop-action` are deleted (Decision 5); `layout reset` covers what `balance` and `columns` did. Evidence § *Binding migration* maps every key.

#### Splits within a slot

Decision 13 lets the user split one slot. The slot stays where it is, with the same weight; only its inside changes.

- **Shape.** A slot holds one to four panes on one axis, and each pane is a stack. A pane is never split again (Open Question 1, option A; the cap and overflow below are part of that choice). The cap of four keeps every pane at least a quarter of its slot; `split` on a slot with four panes returns `slot has 4 panes` and changes nothing.
- **Axis.** The first split sets the axis: `split east` or `split west` gives `columns`, `split north` or `split south` gives `rows`. Later splits must use the same axis; `split north` on a `columns` slot returns `slot is split as columns; merge first` and changes nothing. `split all` uses the axis it is given, or else the axis across the slot's longer side, so a tall laptop slot splits into rows and a wide G9 slot into columns.
- **Split one window.** `split east` moves the focused window out of its pane into a new pane directly east of it. The pane weights become equal. The window keeps focus. A pane with one window cannot be split; tess returns `pane has one window` and changes nothing.
- **Break a stack.** `split all` acts on the pane that holds the focused window. The slot has room for `4 - (panes in the slot) + 1` panes where that pane now sits. If the room is 1, tess returns `slot has 4 panes` and changes nothing. Otherwise the pane's windows, in stack order, each get a pane in its place, up to the room; when there are more windows than room, the last new pane keeps the rest as a stack. Sibling panes keep their order and weights; the new panes share the old pane's weight equally. Axis: in a slot that is already split, `split all` uses the slot's axis, and an explicit other axis returns `slot is split as <axis>; merge first`; in an unsplit slot it uses the axis it is given, or else the axis across the slot's longer side. The focused window keeps focus.
- **Merge.** `merge` puts every pane of the focused slot back into one stack. The window that had focus becomes the front. The other windows keep pane order, then stack order. The pane weights are dropped.
- **Moves between panes.** `focus`, `swap` and `stack` with a direction treat a pane as the unit and use one rule, `Neighbour` (§ *Slot verbs*): the nearest pane in that direction, inside the slot first, then in the next slot. The pane focused last in a slot is used only by `focus-slot`, arrivals and a float's return, never by a directional verb. When `stack` takes the last window out of a pane, that pane is removed and the other panes share its space by their weights.
- **Arrivals.** An arrival target names a slot, never a pane. A new window goes into the pane of that slot that was focused last, as its front. Under Open Question 2 option A the profile does not know about panes, so `new-window-placement.md` is unchanged; option B adds the leaf rule that question describes.
- **Lifetime.** Splits are runtime state in `native-state.json`, with the override weights, so they survive a daemon restart and a Dock restart. Each command's effect on splits is in this table; N9 implements the table, not the override table.

  | Command or event | Override | Splits | Pane weights |
  | --- | --- | --- | --- |
  | `apply`, `laptop`, `snap 3col\|50-50\|columns`, `space stack`, `profile switch`, `preview`, `back` | cleared | merged (under Open Question 2 B: rebuilt from the profile) | dropped with the splits |
  | a slot-count change, or a topology change that moves the workspace to another display | cleared | merged | dropped |
  | `layout reset` | cleared | kept | set equal |
  | `layout save` | saved, then cleared | kept, no frame moves (under Open Question 2 B: saved too) | kept |
  | `merge` | kept | that slot merged | that slot's dropped |
  | `split`, `split all` | kept | that slot changed | that slot's: new panes equal |
  | `resize` | changed when it resizes a slot | kept | changed when it resizes inside a split slot along its axis |
  | arrival, `focus`, `focus-slot`, `swap`, `stack`, move mode, float and return, a cascade to the same display kind, daemon or Dock restart | kept | kept | kept |

- **Status.** The state stream gives each slot its `axis` and its `panes`, each with its window IDs and front window, so the app can draw them.

#### Move mode

Today a cross-slot move takes three steps: click window A, click window B, press the shortcut. Move mode takes the shortcut first:

1. Matt clicks or focuses window A and presses the `move-mode` shortcut. tess records A as the source.
2. tess shows the indicator (below) and highlights the pane under the pointer. A slot with one pane is one pane.
3. Matt clicks a destination: a window, which puts A into that window's pane as its front, or an empty area of a slot, which puts A into the pane under the point. Keyboard path: while move mode is on, the `focus <dir>` bindings move the highlight between panes instead of focus, by the same neighbour rule as `focus`, and the `move-mode` shortcut commits to the highlighted pane. Move mode never creates or removes a split, except that a pane emptied by the move is removed.
4. Escape or 10 s with no input cancels. tess registers Escape as a Carbon hotkey only while move mode is on, so Escape reaches apps normally at every other time. The indicator closes on commit or cancel.

tess sees the destination click without a mouse tap. In move mode it opens one transparent, borderless, non-activating `NSPanel` per display, at a window level above normal windows and with `ignoresMouseEvents = false`, and takes the click on that panel. The panel reports the screen point; tess hit-tests it against the computed pane frames. Because the click lands on tess's panel, the app under it never sees it. When move mode ends, the panels close. [INFERENCE: N1 checks that a non-activating panel receives `mouseDown` without activating tess or changing the focused window.]

**Indicator.** A small borderless `NSPanel` with the `nonactivatingPanel` style, in the **top-right** corner of the display with the source window, below the 32 pt bar inset. It shows `Move: <app> — click a slot, Esc to cancel`. It does not take focus and is not a macOS notification: notifications need a permission and macOS places them ([Apple: asking permission](https://developer.apple.com/documentation/usernotifications/asking-permission-to-use-notifications)). Apple documents that a `nonactivatingPanel` "does not activate the owning app" ([style mask](https://developer.apple.com/documentation/appkit/nswindow/stylemask-swift.struct/nonactivatingpanel)). The indicator is part of the platform layer (`native.Platform.ShowIndicator`), so a later platform supplies its own.

#### Float and pin

Dragging a slotted window floats it at once (Decision 4).

- **Drag start.** The first moved or resized event with a mouse button down and no pending write sets the window to `dragging`, mode `floating`, and records its **home slot** (workspace ID, slot name, pane index, index in the pane's stack). The pane closes up: the next window in its stack becomes front, or, when it was the pane's last window, the pane is removed and the other panes of the slot take its space, or, when it was the slot's last window, the other slots take the space. tess writes nothing to the dragged window.
- **Drag end.** When every button is up, tess reads the frame and keeps it as the floating frame. It pins the window above slotted windows with the SA's `WINDOW_LAYER` opcode at the floating level. Without that capability, tess raises the window once and marks it `pinned: false` in the state stream (§ *Capabilities and SIP*).
- **Back to the slot.** `toggle-float` on a floating window with a home slot returns it to that slot, into the recorded pane at its old stack index. If that pane was removed, the window goes into a new pane at the recorded pane index when the slot has fewer than four panes, so a split comes back as it was; otherwise it goes into the pane focused last. A float and return never changes the slot's axis. It clears the pin. If the home slot no longer exists (a topology change), it goes to the workspace's arrival target. `toggle-float` on a slotted window floats it in place with its current frame and the same home-slot record; `toggle-float center` floats it at the 4×4 centre cell of a 6×6 grid, as today.
- **Across displays.** A drag that ends on another display moves the window's membership to that display's active workspace, still floating, with its home slot unchanged.
- **Persistence.** Floating windows and their home slots are in `native-state.json`, so they survive a daemon restart.

#### Temporary slot resize

`resize` changes the active workspace's **override** weights, not its profile weights. For example, `resize grow` on the AW's left slot widens it by the step and narrows its neighbour. The override is used by `Frames` until it is cleared. It lives in `native-state.json` and survives a daemon restart. In a split slot, `resize` along the slot's axis changes the **pane weights** of the focused pane and its neighbour; `resize` across the axis, or at the slot's outer edge, changes the slot weights as before. Pane weights are runtime state like the split itself.

**What clears the override.** One rule: a command that the user runs to choose a layout clears it; anything tess does by itself keeps it.

| Clears the override | Keeps the override |
| --- | --- |
| `apply`, `laptop`, `snap 3col\|50-50\|columns`, `space stack` | arrival placement of a new window |
| `layout reset`; `layout save` (after the reload) | `focus`, `focus-slot`, `swap`, `stack`, `stack-cycle`, `split`, `merge`, move mode, float and return |
| a change of the workspace's slot count (a slot added or removed by a command) | a Hub cascade or `DisplayQuiet` cascade that re-homes a workspace to the same display kind |
| a topology change that moves the workspace to another display | daemon restart, Dock restart |

The planner sets `clearOverride` on `RealizeSpaceLayout` from this table. A cascade that keeps the override but changes the slot count clears it, because the weight vector no longer fits.

- `tess layout save` writes the override as the layout's `weights` in the profile source file, through the symlink (Decision 11), then reloads the profile. The saved weights are the profile weights, so the override clears and no frame moves. Splits and pane weights stay as they are on screen and are not written under Open Question 2 option A (§ *Splits within a slot*, *Lifetime*). A hash mismatch, a vet failure or a missing target returns an error and saves nothing. With a draft active (§ *Profiles and the MCP server*), `layout save` stores the edited source as a new revision of that draft and points `active` at it, with no frame move; `good` and `prior` do not change, so `back` still reaches the last kept revision.
- `tess layout reset` clears the override and sets every split slot's pane weights equal, so the profile weights apply again. It keeps the splits; `merge` removes them.

Both are CLI verbs, so a hotkey can run them. The app (`tess-app.md`) adds "Save layout" and "Reset layout" actions that send the same verbs over the daemon socket. The state stream marks a workspace with an override as `modified: true`, so the app and sketchybar can show it.

### Profiles and the MCP server

Decision 15 lets Matt describe a slot setup to an AI agent, have the agent write it as a CUE profile, try it at once, and keep several to switch between.

**The profile set.** Decision 11 stays: `~/.config/tessera/profile.cue` is the **live** profile, a `mkOutOfStoreSymlink` to the source in Matt's repo checkout. **Drafts** sit beside it under `~/.config/tessera/profiles/`. tess writes the drafts; home-manager does not manage that directory, so a nix switch never removes them. A name matches `^[a-z0-9][a-z0-9-]{0,39}$`, and `live` is reserved. A draft must pass `Vet` (below): it must vet against `#Profile` on its own, keep the live profile's `backend`, because the backend changes only through a migration (§ *Migration*), and follow the draft limits of Open Question 4.

**Revisions never change.** A name points at a list of **revisions**. A revision is one complete profile source, stored once as `profiles/<name>/<hash>.cue`, where `<hash>` is the first 16 hex digits of the SHA-256 of its bytes, and never rewritten. `profile write` adds a new revision and makes it the name's latest; it never edits an old one. The live profile is a revision too: each time the daemon loads `profile.cue`, it copies the bytes it read to `profiles/live/<hash>.cue`, so an edit in Matt's repo cannot change what `good` points at. A revision is removed only when no name lists it as latest and no pointer below names it.

**Which profile is in use.** `profile-active.json` in the state directory records four pointers, each a `{"name", "rev"}` pair: `active` (what the daemon runs), `good` (the last revision kept), `prior` (the kept revision before it), and `trial` (with `until` and `by`, or null). A missing file means `live` at its current revision for all of them. tess writes the file as it writes `pause.json`: temp file, `fsync`, rename, directory `fsync`. At daemon start it loads `active`; if that revision file is missing or fails `Vet`, it loads `good`, then `live`, logs each step, and reports the fallback in the state stream. When `active` names `live` and B2 reloads an edited `profile.cue`, the new bytes become a new `live` revision and are run as a `switch` to it, so `good` moves only if that switch ends cleanly. The daemon's profile load (B2 in `tess-app.md`) goes through this file.

**Verbs.** All run in the daemon, as every command does (§ *Daemon and IPC*).

| Verb | Effect |
| --- | --- |
| `profile list` | names, `active`, `good`, the trial, and each file's vet status |
| `profile show <name>` | the CUE source |
| `profile vet <name>\|-` | vet a draft, or CUE read from the request's stdin; print `cue vet` errors with line and column. Changes nothing |
| `profile write <name> -` | `Vet` the CUE from stdin; on success store it as a new revision and make it the name's latest. A `Vet` failure writes nothing. It never changes `active`, `good`, `prior` or `trial`, and never reloads: to run the new revision, `switch` or `preview` it |
| `profile delete <name>` | remove the name from the list; refused for `live`. A revision that a pointer names stays on disk until the pointer moves, so `back` always works |
| `profile switch <name>` | `Vet` the name's latest revision; set `active` to it; reload; realize the layout for the current topology (`apply` on the desk, `laptop` on the laptop). When that ends with no error and no pause: `prior := good`, `good := active`, `trial := null` |
| `profile preview <name> [--for 10m]` | the same steps, but at the end only `trial := {active, until, by}`; `good` and `prior` stay. At the deadline, or at a daemon start with an open trial, tess switches back to `good`. Default 10 min, at most 1 h |
| `profile keep` | end the trial: `prior := good`, `good := trial`, `trial := null` |
| `profile back` | one step back. If `active` is not `good` (a trial or a failed switch): `active := good`, `trial := null`. Else: `active := prior`, then swap `good` and `prior`, so a second `back` undoes the first |
| `profile pick` | open the picker panel (below) |
| `profile promote <name>` | copy a draft over the live profile source through the Decision 11 steps (symlink, hash check, vet, temp file, rename), then switch to `live`. Matt runs it; it is not an MCP tool (Open Question 3) |

**Safety.** A switch is an ordinary job. It runs the same `Vet` as every other profile path, runs through the job queue, and every frame write it causes counts against the move budget (§ *Pause and move budget*). While tess is paused, `switch`, `preview` and `back` change only `profile-active.json`, and `tess start` applies the result; so `back` then `start` recovers from a profile that tripped the budget. The budget limits are not in the profile, so no profile can raise them. A switch changes overrides and splits as § *Splits within a slot*, *Lifetime* says.

**One `Vet`.** Every path that accepts a profile calls the same `config.Vet(src, live)`: daemon load, `write`, `switch`, `preview`, `promote`, `profile vet`, and `tess mcp` with or without a daemon. `Vet` runs `Parse` against the embedded `schema.cue`, then compares the draft with `live`, the live profile as last loaded: same `backend`; under Open Question 4 option B, every `windows.<key>.spawn` equal to live's for that key, no `spawn` on a key live lacks, and `hotkeys` equal to live's; under options A and C, the protected-binding rule below. A draft named `live` is not compared with itself.

**Hotkeys across a switch.** Under Open Question 4 option B a draft's `hotkeys` equal live's, so a switch changes no binding. Under options A or C a draft carries its own `hotkeys`; while it is active, tess registers the draft's bindings plus every live-profile binding whose `run` starts with `stop`, `start` or `profile`, so a draft can never take away the way back. A draft binding on one of those combos fails `Vet`. The switch uses `hotkey.Diff` in one actor step, as § *Bindings across the switch* does.

**Panels.** During a trial the top-right indicator (§ *Move mode*, *Indicator*) shows `Trying <name> — <minutes> left` with two buttons, **Keep** and **Back**, which run `profile keep` and `profile back`. The panel takes clicks as the move-mode panels do and never activates tess. `profile pick` opens a non-activating panel at the centre of the focused display with one row per profile, the active one marked. Arrow keys and Return are registered as Carbon hotkeys only while it is open, as Escape is in move mode; Return or a click runs `profile switch`, Shift-Return runs `profile preview`, and Escape closes it. Move mode and the picker never run at once: opening one closes the other. The state stream gains `profiles` (the list above) and `trial`, so the app (`tess-app.md`) lists profiles in its tray menu and switches with the same verbs.

**`tess mcp`.** A stdio MCP server built on the [official Go SDK](https://github.com/modelcontextprotocol/go-sdk) (`github.com/modelcontextprotocol/go-sdk/mcp`). An agent's MCP client starts it as a child process. It holds no state: each tool call sends one `run 1` request to the daemon socket and returns the `stdout` frames as the tool result, and a non-zero `exit` or a `reject` as a tool error with the `stderr` text. With no daemon it returns `tess daemon not running` on every call except `get_schema` and `validate_profile`. Those two load the live profile in process, from the same `~/.config/tessera/profile.cue` the daemon reads, and run the same `Vet`, so a draft gets the same answer with and without the daemon. If the live profile cannot be loaded, both return that error; neither falls back to a schema-only check.

**Request size.** The `run 1` request gains an optional `stdin` string for `profile vet -` and `profile write <name> -`; the CLI fills it from its own stdin. Two limits apply. The decoded `stdin` is at most 262144 bytes of UTF-8. The whole request line is at most 1638400 bytes, which fits 262144 bytes that each escape to six (`\u001f`), plus 65536 bytes for `argv` and `profile`. The daemon reads the line through a reader capped at that size, so an oversize line is rejected after at most 1638400 bytes and never reaches the JSON decoder; a decoded `stdin` over its limit is a `reject`. Both checks happen before `accepted`, so no job starts. Requests without `stdin` are unchanged.

**Annotations are hints.** The tool table's annotations tell the client which tools change state, so it can ask Matt first. The MCP spec leaves that to the client: "the protocol itself does not mandate any specific user interaction model" ([tools](https://modelcontextprotocol.io/specification/2025-06-18/server/tools)). tess's safety does not rest on them. It rests on `Vet` (what a draft may contain, Open Question 4), on the move budget, and on the pause.

| Tool | Runs | Annotation |
| --- | --- | --- |
| `get_state` | `tess query`: displays, workspaces, slots, panes, windows, `paused`, `trial`, the capability table | read-only |
| `get_schema` | the embedded `schema.cue` and Matt's live profile, so the agent writes CUE that fits | read-only |
| `list_profiles`, `get_profile {name}` | `profile list`, `profile show` | read-only |
| `validate_profile {source}` | `profile vet -` | read-only |
| `write_profile {name, source}` | `profile write <name> -` | not read-only, idempotent |
| `delete_profile {name}` | `profile delete` | destructive |
| `preview_profile {name, minutes?}` | `profile preview` | not read-only |
| `switch_profile {name}`, `keep_profile`, `profile_back` | `profile switch`, `profile keep`, `profile back` | not read-only |
| `stop {reason}` | `tess stop` | not read-only |

Tool inputs and outputs are Go structs, so the SDK derives their JSON schemas. A switch or preview from `tess mcp` records `by: "mcp"` in `trial` and in the log, and the indicator shows `(agent)` after the name. There is no MCP tool for `start`, `promote` or `layout save` (Open Question 3).

### Library choices

Libraries are chosen on fit and maintenance (Decision 9), and cgo is allowed (Decision 7). Evidence § *Go library survey* has the full table.

| Need | Choice | Why |
| --- | --- | --- |
| Global hotkeys with release events | own cgo wrapper over Carbon `RegisterEventHotKey` in `internal/hotkey/darwin` | [go-macos/hotkey](https://github.com/go-macos/hotkey) is purego Carbon and needs no permission, but its API exposes only press events; tess needs `kEventHotKeyReleased` to stop a repeat. [golang.design/x/hotkey](https://github.com/golang-design/hotkey) is cross-platform and reports key-up, but on macOS it uses a `CGEventTap`, which needs an Accessibility or Input Monitoring grant. The wrapper is about 100 lines [INFERENCE]. |
| AX, AXObserver | own cgo in `internal/wm/native/darwin` | [go-macos/accessibility](https://github.com/go-macos/accessibility) (purego, active) has no AXObserver support; tess reads it as reference for frame writes, not as a dependency |
| NSWorkspace, CGDisplay callbacks, run loop, NSPanel | own cgo and Objective-C | no Go wrapper covers them; [go-macos/objc](https://github.com/go-macos/objc) (purego, active) warns that `App()` binds to the first calling thread, which is easy to get wrong |
| Bundle and signing | the nix activation in N2 | [tmc/macgo](https://github.com/tmc/macgo) builds bundles and signs them, but N2 needs a fixed identifier and a stable path that activation already controls |
| MCP server over stdio | [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) `mcp` package, latest v1 release (v1.8.0, released 2026-09-14) | Fit, against tess's three needs: a stdio server (`server.Run(ctx, &mcp.StdioTransport{})`), typed tools whose JSON schema comes from Go structs (`mcp.AddTool(server, tool, handler)` with a typed input and output), and in-process tests (`mcp.NewInMemoryTransports()`, two connected transports over `net.Pipe`). Maintenance: the README calls it "The official Go SDK for Model Context Protocol servers and clients. Maintained in collaboration with Google", and v1.8.0 shipped after two pre-releases in September 2026. [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) (v1.5.0, released 2026-10-30) is also maintained and serves stdio with `server.ServeStdio(s)`. Its README examples declare each argument with builder calls (`mcp.NewTool("x", mcp.WithString("name", mcp.Required()))`) and read it in the handler with `request.RequireString("name")`, so tess would keep a schema and a Go struct in step by hand. Both fit; the struct-derived schema decides it. Evidence § *Go library survey* has both rows. Pure Go, no cgo |

The cgo surface stays narrow: each wrapper exposes Go functions and sends callback values to channels. Pure logic stays in packages with no cgo.

### Daemon and IPC

**Every command runs in the daemon.** The CLI sends its argv over the existing socket, `tessera-daemon.sock`, with #76's protocol: one line `run 1 {"argv":[…],"profile":"<path>"}`, then NDJSON frames `accepted`, `stdout`, `stderr` and `exit` (or `reject`). After `accepted` the client never runs the command itself. Hotkeys enter the same queue in-process.

**One writer.** The daemon runs one job at a time; all jobs already pass through `TopologyState.SerializeCascade`. On native, jobs run as § *Job transactions* describes, against the actor in § *Desired and observed state*. The one exception is `stop` and `start`: they are control messages to the actor and skip the queue (§ *Pause and move budget*, *The control path*).

**Fallback.** On the yabai backend a failed dial runs the command in-process, as today. On native the CLI exits 1 with `tess daemon not running`, because only the daemon holds the model. `tess rescue`, `tess sa status|load|uninstall` and `tess migrate --status` are the commands that run without it; `tess mcp` starts without it and reports it missing per tool (§ *Profiles and the MCP server*).

**State for the app and the bar.** tess-app's A3 state stream (`subscribe 1`, full `state` frames) is the one feed. `daemon.StatePublisher` is specified in `tess-app.md` and is not in the code yet; N8 depends on A3 landing first. On native the actor calls `StatePublisher.Request` after each flush; the frame carries workspace membership, each slot's axis and panes, each window's status and mode, the `modified` flag, move mode, `paused`, `profiles` and `trial`, and the capability table. A new `tess query` prints one `state` frame as JSON. Matt's sketchybar scripts call `tess query` instead of `yabai -m query` before N12. The daemon keeps triggering the sketchybar event `yabai_spaces_changed` until those scripts rename it.

### Risks, weighed against keeping yabai

| Risk | Keeping yabai | N: tess SA | V: virtual |
| --- | --- | --- | --- |
| macOS releases | yabai or SC26 must ship payload patches (evidence § *What needs the scripting addition*) | tess ships them; fewer opcodes to patch; a version with no entry runs with fewer capabilities, never with a guessed pattern | One private symbol for AX plus the SkyLight reads |
| SIP | Partly off | Partly off for full capability; runs with SIP on | Can be turned back on (Decision 2) |
| A third-party project | Matt depends on Christian-SC26's releases | None at run time; upstream changelogs are a source of fixes | None |
| Dock crash | yabai's payload can crash Dock | Same risk, smaller payload; a crash restarts Dock and tess reloads | None |
| TCC | Stable Homebrew path | Stable identity from N2; a lost grant is detected with `AXIsProcessTrusted`: tess moves nothing and reports `fresh: false` | Same |
| Crash of tess | Windows stay | Windows stay | Windows stay parked until restart or `tess rescue` |
| App quirks | Handled for years | tess meets them again: Electron, terminals, apps with minimum sizes. N10 finds enumeration and event gaps; N11 finds write and focus quirks before N12 | Same, plus park offsets (AeroSpace issue #527, Zoom) |
| Code to own | 0 lines, but tess fights a 20,165-line C program (upstream reference) from outside | [INFERENCE] about 3,500 lines of Go, 1,000 of cgo and Objective-C, and the SA fork cut down from about 2,850 lines | About 1,000 more for parking, activation restore and the ledger [INFERENCE] |

The pain in RIG-8691, #73 and #75 comes from driving yabai's split tree through a CLI. Stage N removes that cause and owns the one part of yabai that tess cannot replace without parking: the payload. Every stage has a reverse until N14; after N14 the exercised rollback recipe replaces it.

## Alternatives considered

- **Keep yabai and finish #76.** This is the simpler option in § *Direction*. Stage S delivers it. Rejected as the end state because it keeps the split tree.
- **Hybrid: yabai keeps native Spaces, tess tiles.** Rejected by Matt (Decision 1): yabai stays a run-time dependency and is poorly maintained.
- **Load yabai's SA unchanged and speak its socket protocol.** Rejected by Matt (Decision 1): tess would track a payload it cannot change.
- **Virtual workspaces now.** Preferred if it works (Decision 1), but parking on the AW is unproven; stage V runs after V0.
- **Run AeroSpace as the backend.** It has its own tree, not tess's slots, and no client API for tess's slot and arrangement model. tess would drive a second WM from outside again. Rejected.
- **Workspace-wide BSP or tree tiling in tess.** Rejected (Decision 4): slots stay the top level, and the tree verbs stay deleted (Decision 5). Decision 13 allows splits inside one slot; Open Question 1 decides whether that is one axis of panes, a split tree confined to the slot, or a tiled/tabbed toggle.
- **Mutable named drafts, with `good` and `prior` as names.** Simpler, but overwriting or deleting the kept draft would also destroy the fallback, and `back` could land on the same bad file. Rejected for revisions that never change.
- **The MCP server inside the daemon, on a TCP or HTTP port.** The daemon already serves a Unix socket; an HTTP MCP transport adds a listener, auth and a port for no gain, because every MCP client Matt uses can start a stdio child. Rejected for `tess mcp` over stdio.
- **Drafts as branches of the live profile in Matt's repo.** An agent would write into Matt's checkout for every try. Rejected: drafts live beside the live profile, and only `profile promote`, which Matt runs, writes his repo (Decision 11 and Open Question 3).
- **CGEventTap hotkeys.** Needs the Accessibility grant and receives every key event. Rejected for Carbon.
- **purego instead of cgo.** Rejected (Decision 7).

## Global Constraints

- Go 1.27.3 as `go.mod` pins. This record supersedes the `tess-app.md` § *Global Constraints* rule "The `tess` module stays cgo-free" for darwin builds of `tess` (Decision 7). cgo lives only in files with `//go:build darwin` under `internal/wm/native/darwin`, `internal/wm/native/spaces/darwin`, `internal/hotkey/darwin` and `internal/sa/darwin`. Builds for other systems stay `CGO_ENABLED=0`.
- macOS only. The Windows code is deleted in S0 and no Windows build or CI job remains (Decision 6). Platform-neutral seams stay: `wm.Driver`, `wm.SpaceService`, `native.Platform` and `internal/hotkey`. Pure logic (slot layout, park corners, model edits, migration phases, adoption grouping, repeat timing) has no cgo and is tested with `GOOS=linux` in CI.
- Private API in the tess daemon: `_AXUIElementGetWindow` and the SkyLight reads and window-move calls in § *Capabilities and SIP*, each resolved with `dlsym` at start. A missing symbol turns off its capability. Every other private call lives in tess's payload.
- tess runs with SIP fully on. Every capability is reported by `wm.SpaceCaps`; a missing one returns `wm.ErrCapability` and never pretends to succeed. The payload hooks no macOS version it has no entry for.
- After N14, nothing in tess calls, queries or starts yabai. yabai appears only in `sa/` attribution, in evidence, in the migration reverse before N14, and in the rollback recipe after it.
- Only `backend: "native"` loads tess's payload. Every live SA test starts with `tess sa load --test` and ends with `tess sa restore`; the restore record is written before the first change.
- The SA fork keeps yabai's MIT copyright and licence text and the loader's Jeremy Legendre attribution. `THIRD_PARTY_NOTICES.md` lists both.
- Pause and budget: while paused, no frame write, focus, Space-service call or `WINDOW_LAYER` is dispatched; the actor and each worker check the flag before sending. `stop` and `start` are control messages that skip the job queue. The pause is set in memory first, then made durable (temp file, `fsync`, rename, directory `fsync`) before `stop` replies; a failed write keeps the pause and reports it. `start` removes the file only after its preflight passes. An unreadable `pause.json` counts as paused. A Dock-restart hold never clears a pause. The budget limits live in code, not in any profile.
- Profiles: every profile path runs the one `config.Vet(src, live)`, online and offline; a failed vet changes no file and no state. A revision never changes once written; `active`, `good`, `prior` and `trial` point at revisions. Only `profile promote` and `layout save` on `live` write the profile source in Matt's repo. A draft keeps the live `backend`, and follows Open Question 4 on `spawn` and `hotkeys`. MCP annotations are hints, not a safety control.
- Daemon socket: a request line is read through a reader capped at 1638400 bytes; decoded `stdin` is at most 262144 bytes. Both are checked before `accepted`.
- One writer: only the actor goroutine changes the `Model`. The actor never blocks on a worker or an event. cgo callbacks send values to channels and never call Go code that takes a lock.
- Lifecycle state is never discarded by an abort. Every worker result carries an operation ID; stale results are dropped. A waiter completes only on its postcondition.
- Every AX element has a 250 ms messaging timeout. Every frame write is read back; status is reported, never assumed.
- Two WMs never run at once. tess has no mouse tap and never writes to a window in `dragging`.
- Every Space-service call subscribes before it acts, checks its postcondition, and has a budget. No fixed sleep remains in a Space operation.
- Every migration item is recorded in `migration.json` before it runs. The direction is recorded apart from the profile. The snapshot file is never changed after it is written.
- The AX grant belongs to one signed executable at a stable, root-owned path. The daemon, `tess rescue` and `tess sa load` all run it.
- UI: the move-mode indicator and click panels are borderless, non-activating `NSPanel`s; tess never posts a macOS notification for interaction state.
- yabai claims cite SC26 or say **both**.
- Tests use `gotest.tools/v3` and `synctest`; no `time.Sleep`. Errors are wrapped with `%w`. Comments are 1–2 lines with no issue IDs.
- Public repo: name Matt's config only as the downstream consumer. No private paths or hosts.
- No usage-billed resource is added.

## Plan

Stage S ships first and alone. Stage N tasks land in order; N1 is a spike whose results can reopen this record. M1 lands after N9 and before N12. Stage V is optional and starts only after V0 passes.

### S0 — Delete the Windows code

Interfaces: delete `internal/wm/komorebi/` (including `spike/` and `spikestate/`), `internal/effects/guard_windows.go` and `guard_windows_test.go`, `internal/cli/cli_root_windows_test.go`, `internal/commands/test_root_windows_test.go`, `internal/daemon/test_root_windows_test.go`, and `docs/design/windows-backend/`. Rename `cmd/tess/backend_other.go` to `backend_darwin.go` with `//go:build darwin`, and drop the komorebi half of the `Bootstrap` comment in `cmd/tess/backend.go`. In `.github/workflows/ci.yml`, delete the `windows` job and the "Vet Windows packages" and "Lint Windows packages" steps; add one step that runs `GOOS=linux go vet` on the packages with no cgo, so the pure logic stays portable. Remove komorebi from `tess-app.md`'s capability table and from `README.md` if named. Add `THIRD_PARTY_NOTICES.md` at the repo root with a heading and no entries.

Gate: `go build ./...` and `go test -race ./...` on darwin; `GOOS=linux go vet` on the portable packages; `grep -ri komorebi` over the repo finds only this record and its evidence; `moon ci` green.

### S1 — Every command through the daemon

Interfaces: #76 T5's protocol, unchanged: `run 1 {"argv":[…],"profile":"<path>"}`; frames `{"v":1,"accepted":true}`, `stdout`, `stderr`, `exit`, `reject`. `daemon.Options.PrepareRequest func(argv []string) (daemon.Job, error)`, built in `cmd/tess/main.go` from `cli.Parse` and `commands.Run`. `cli.Deps.Dispatch func(ctx context.Context, argv []string, stdout, stderr io.Writer) (int, bool, error)`; `false` means fall back in-process.

Gate: `net.Pipe` tests for each frame kind; a reject and a dial failure fall back; a stream that ends after `accepted` exits 1 with `tess: daemon request outcome unknown`; a request and a Hub cascade serialize through `SerializeCascade`.

### S2 — Darwin cgo build and packaging

Interfaces: `flake.nix` builds `tess` on `aarch64-darwin` with `CGO_ENABLED = "1"` and the darwin SDK frameworks (ApplicationServices, AppKit, Carbon, CoreGraphics). The release tarball contract in `release-binaries.md` is unchanged. `cmd/tess` gains the main-thread lock in `init` and, in a darwin file, the `NSApplication` run loop with the `accessory` activation policy for `tess daemon`.

Gate: in the `nix` CI job, `otool -L result/bin/tess` lists only `/usr/lib` and `/System/Library` paths, never `/nix/store`; the unpacked release tarball runs `tess --version` on the runner outside Nix; `codesign --verify --strict` passes; `tess daemon` shows no Dock icon on Matt's Mac.

### S3 — Hotkeys in the daemon, with repeat

Interfaces: `config.Hotkey{Key string; Mods []string; Run []string; Repeat bool}` and `#Hotkey` in `schema.cue`, with `hotkeys?: [...#Hotkey]` on `#Profile`. `internal/hotkey` (portable, no cgo): `type Mods uint8`; `type Combo struct{Key uint16; Mods Mods}`; `type Binding struct{ID uint32; Combo Combo; Run []string; Repeat bool}`; `Parse(h config.Hotkey) (Combo, error)`; `Diff(old, new []Binding) (add, remove []Binding)`; `type Event struct{ID uint32; Down bool}`; `type Repeater struct{Delay, Interval, MaxHold time.Duration}` with `func (r *Repeater) Handle(ctx context.Context, ev Event, run func(context.Context) error)`, which runs on down, repeats only when the binding sets `Repeat`, waits for each run to finish before the next, and stops on up or `MaxHold`. `internal/hotkey/darwin` (cgo over Carbon): `Register(c Combo, id uint32) error`, `Unregister(id uint32) error`, `Events() <-chan Event` carrying both `kEventHotKeyPressed` and `kEventHotKeyReleased`, and `RepeatTiming() (delay, interval time.Duration)` from `NSEvent.keyRepeatDelay` and `keyRepeatInterval`.

Gate: table tests for every key name and every profile rejection; `synctest` tests for repeat (one run on a short press; runs at delay, then each interval while held; none after release; none queued while a slow run is in flight; stop at `MaxHold` with no release). On Matt's Mac, `tess daemon` registers the 50 combos with skhd stopped and logs each status, and a held key logs the down and up events; any failure, including a bare keypad key, blocks S4.

### S4 — Binding transport and skhd removal

Interfaces: new verbs on the yabai backend: `stack recent` (`wm.WindowOps.RecentWindow(ctx context.Context) (int, bool, error)`, then `StackOnto`), `toggle-float center` (`wm.WindowOps.FloatGrid(ctx context.Context, winID int, g wm.GridCell) error`), `resize left|right|up|down` (existing `ResizeWindow` with the opposite-edge fallback) and `drop-action toggle` (yabai only). The tree verbs are unchanged. Downstream: Matt's profile gains `hotkeys` from evidence § *Binding migration*, with `repeat: true` on the four resize arrows and on `resize grow|shrink`; the skhd agent and `skhdrc` go; `DefaultServices` returns only yabai.

Gate: a table test sends each of the 50 bindings through the dispatcher to the recording yabai driver and asserts the exact yabai arguments: today's for the nine raw bindings, today's `tess` path for the rest. Matt runs all 50 on the desk with skhd stopped, holding each repeat binding for 2 s.

### N1 — Feasibility spike

Interfaces: live tests in `internal/wm/native/darwin` with `//go:build darwin && live`, on a minimal cgo bridge that N7 grows. N1 adds its results to evidence.md as a new section, *Spike results*, with the trial count of each measurement. N1 runs while yabai still tiles, on one scratch Space that Matt sets to `float` by hand for the test.

Probes:

- AX frame write and read-back for Zen, a terminal with size increments, an Electron app, Finder and a window with a minimum size.
- Public focus, including Zen on another display and Zen on a hidden Space after a Space focus: 20 trials each, success rate and time to postcondition.
- `NSEvent.pressedMouseButtons` during a title-bar drag, and the moved events it causes, against an OS clamp after a display change.
- With SIP as it is and, on a second boot, with SIP fully on: the SkyLight reads in `Snapshot` and the bridged window-to-Space move, 20 trials each.
- With SIP fully on and no payload: the gesture focus path (§ *Focus*, *Gesture pacing*) to a hidden Space on the same display, to a Space on another display, and to a hidden Space on another display, 20 trials each, with success rate, time to postcondition and where the cursor ends. Also activation of a window on a hidden Space with no Space focus, with the "switch to a Space with open windows" setting on and off. A success rate under 19 of 20 for any case sets `SpaceCaps.Focus` false for that case and reopens § *Focus*.
- A non-activating `NSPanel` under the `accessory` policy: it shows above normal windows, takes a click, and does not change the focused window or activate tess.

Gate: every probe has a recorded result and a negative control (a skipped write must read back unchanged). A failed probe reopens this record.

### N2 — TCC deployment and signing

Interfaces: the home-manager module adds `programs.tessera.daemon.signingIdentity`, set to Matt's self-signed certificate now and to the Developer ID later (Decision 3). Activation copies the release bundle out of the store into `/Applications/Tessera Daemon.app`, owned by root, with `CFBundleIdentifier` `io.github.mattwilkinsonn.tessera.daemon`, and signs `Contents/MacOS/tess` with `codesign --force -s <identity> -i io.github.mattwilkinsonn.tessera.daemon`. The bundle also holds `Contents/Resources/tess.osax`. Activation writes the new bundle beside the old one, signs it, then swaps the two and restarts the launchd agent, whose `ProgramArguments` name the stable path. `tess rescue` and `tess sa` from the store re-execute the stable-path binary.

Gate, recorded in evidence § *TCC* and marked [INFERENCE] until done: two different builds pass through real activations and keep the grant; it survives logout and reboot; a revoked grant makes the daemon report `fresh: false` and move nothing; `tess rescue --dry-run` with the daemon stopped is trusted. Record `codesign -d -r-` and the executable path of each process.

### N3 — Planner and staging behind driver capabilities

Interfaces: `type Capabilities struct{ StackCap int; NeedsStaging bool }` and `Capabilities() Capabilities` on `wm.Driver`. yabai returns `{24, true}`; `fake.Driver` is configurable; native returns `{0, false}`, where 0 means no cap. `engine` reads the cap from its plan input, not `wm.StackWindowCap`; `DeskPlan` emits staging floats only when `NeedsStaging`; `commands.apply` loads and writes staging only when `NeedsStaging`. `type Transactional interface{ Commit(ctx context.Context) error; Abort(ctx context.Context) }` in `wm`; the daemon's job runner calls `Commit` at job end and `Abort` on error or cancel when the driver implements it.

Gate: the real command and executor path with `{0, false}` places 40 windows in one stack with no staging write; with `{24, true}` today's behaviour and tests are unchanged; a failure injected between staging and realization restores staged windows on yabai and flushes nothing on a transactional fake.

### N4 — tess's scripting addition

Interfaces: a new `sa/` directory with the fork of SC26 `a26707a` (§ *tess's scripting addition*): `sa/payload/payload.m`, `sa/payload/arm64_payload.m`, `sa/loader/loader.m`, `sa/common.h`, and `sa/LICENSE.txt` with yabai's MIT text. The payload keeps opcodes `HANDSHAKE`, `SPACE_FOCUS`, `SPACE_CREATE`, `SPACE_DESTROY`, `SPACE_MOVE` and `WINDOW_LAYER`, and their patterns and offsets, with the argument layout in § *tess's scripting addition*, *Protocol*; every other handler, pattern and ivar lookup is deleted. Socket `/tmp/tess-sa_<user>.socket`; bundle `tess.osax`; `OSAX_VERSION` `tess-1`. `flake.nix` builds the payload and loader for `aarch64-darwin` into the bundle. `THIRD_PARTY_NOTICES.md` gains the yabai MIT notice and the Jeremy Legendre attribution.

Go side, `internal/sa` (portable protocol, no cgo):

```go
type Attrib uint32 // mirrors OSAX_ATTRIB_* in sa/common.h

type Handshake struct {
	Version string
	Attrib  Attrib
}

type Client interface {
	Handshake(ctx context.Context) (Handshake, error)
	// CreateSpace adds a Space on the display that holds anchor, an existing Space.
	CreateSpace(ctx context.Context, anchor uint64) error
	DestroySpace(ctx context.Context, sid uint64) error
	FocusSpace(ctx context.Context, sid uint64) error
	// MoveSpaceAfter sends SPACE_MOVE{src: sid, dst: after, src_prev: 0, focus: false}.
	MoveSpaceAfter(ctx context.Context, sid, after uint64) error
	// SetWindowLayer sends WINDOW_LAYER{wid, key}; key is a CGWindowLevelKey.
	SetWindowLayer(ctx context.Context, wid uint32, key int32) error
}

func Encode(op Opcode, args ...any) ([]byte, error)
func Dial(path string) Client
```

`internal/sa/darwin` (cgo): `Status() (Status, error)` with `Status{SIPFilesystem, SIPTaskForPID, BootArg bool; Installed string; Handshake *Handshake}`; `Install(bundle string) error`; `Load() error`; `Uninstall() error`; `Restore() error` per § *tess's scripting addition* step 5, with `type RestoreRecord struct{LoadedBy string; YabaiAgentWasLoaded, YabaiSAWasInstalled bool}` in `internal/sa`. CLI: `tess sa status`, `tess sa load [--test]`, `tess sa uninstall`, `tess sa restore`. The daemon calls `Load` only on `backend: "native"`. Downstream: a sudoers entry for `/Applications/Tessera Daemon.app/Contents/MacOS/tess sa load` and `… sa restore` (Decision 12).

Gate: `Encode` golden tests for every kept opcode, byte for byte against a C harness built from yabai's `sa_payload_init`, `pack` and `sa_payload_send` macros: `SPACE_CREATE` with one `uint64` anchor, `SPACE_MOVE` with `src`, `dst`, `src_prev = 0` and a 1-byte `focus = 0`, and `WINDOW_LAYER` with a `uint32` and an `int32`. A handshake parser test on `tess-1\0` plus four mask bytes plus `\n`. A fake payload socket tests the handshake, a version mismatch (reload, never use) and a missing attribute (`ErrCapability`). A test that the daemon with `backend: "yabai"` or `"observe"` never calls `Load`. `Restore` tests on a fake system: a crash after each step, then a second `Restore`, ends in the recorded state; a missing record is a no-op.

Live, on Matt's Mac, always through `tess sa load --test` and always ended by `tess sa restore`: `tess sa load` reports `tess-1` and the expected bits; create a scratch Space on the G9 using the G9's current Space as the anchor, and check it lands on the G9, not the laptop or the AW; focus, move and destroy it; `SetWindowLayer` keeps a test window above a normal one; Dock restart drops the payload and `tess sa load` restores it. Then `kill -9` the test between load and restore, run `tess sa restore`, and check that yabai runs with its own SA and its handshake. With SIP fully on (second boot, or a second Mac): `tess sa status` reports every SA capability missing and `tess sa load` exits non-zero with a clear message.

### N5 — Space service and capabilities

Interfaces, in `internal/wm`:

```go
type NativeSpace struct {
	ID         uint64
	Index      int
	Display    string
	Visible    bool
	Fullscreen bool
}

type NativeWindow struct {
	ID, PID    int
	Space      uint64
	Display    string
	Frame      Frame
	App, Title string
	Layer      int
	Minimized  bool
}

type SpaceSnapshot struct {
	Spaces  []NativeSpace
	Windows []NativeWindow
	Focused int
}

type SpaceNotice struct {
	Kind   Event
	Space  uint64
	Window int
}

type SpaceCaps struct {
	Read, MoveWindow, Focus, FocusInstant bool
	Create, Destroy, Reorder, PinLayer    bool
}

type ErrCapability struct{ Name string }

type SpaceService interface {
	Capabilities() SpaceCaps
	Snapshot(ctx context.Context) (SpaceSnapshot, error)
	CreateSpace(ctx context.Context, display string) (uint64, error)
	DestroySpace(ctx context.Context, sid uint64) error
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

`internal/wm/native/spaces` implements it per § *The Space service* over `sa.Client` and a cgo SkyLight layer in `internal/wm/native/spaces/darwin` (`dlsym` for each private symbol; a missing one clears its `SpaceCaps` bit). `CreateSpace(display)` resolves the display's current Space from `Snapshot` and sends it as the anchor. `Capabilities` recomputes after each SA load and is cleared by `InvalidateSA()`, which the actor calls on a Dock restart. With no `Create` capability, `CreateSpace` follows Decision 10. `fake.SpaceService` is configurable per capability.

Gate: tests on the fake SA socket and a fake SkyLight layer for each operation: already satisfied; notice before the action (subscribed first); no notice, met by poll inside the budget; never met, returns `OutcomeUnknown`; missing capability returns `ErrCapability` with no call made. `CreateSpace` on a display sends that display's current Space ID, and returns the no-anchor error for a display with none. `FocusSpace` uses a 1 s budget on the SA path and 2 s on the gesture path, and the gesture path never posts twice within 150 ms. `InvalidateSA` makes the next SA call return `ErrCapability` with no socket write. `Snapshot` returns windows on hidden Spaces. `MoveWindowToSpace` with 30 windows and an unchanged active Space.

### N6 — Slot model, layout and status

Starts after Matt rules on Open Questions 1 and 2. The interfaces below are for option 1A; under 1B, `Slot.Panes` and `PaneWeights` become `Root *Node` with `Node{Axis wm.LayoutKind; Ratio float64; A, B *Node; Leaf *Pane}` and `const MaxLeaves = 6`, and every pane edit below keeps its name and signature but acts on leaves.

Interfaces: `native.Model{Lifecycle Lifecycle; Desired Desired}`. `Lifecycle` holds `map[int]WindowLife` with `WindowLife{ID, PID int; Space uint64; Observed wm.Frame; ReadAt time.Time; Status Status; Attached bool; Op uint64; Gen uint64}`, tombstones `map[int]uint64`, and the focused window. `Desired` holds `[]Workspace` with `Workspace{ID, Label, Display string; Space uint64; Kind wm.LayoutKind; Slots []Slot; Weights []float64; Override []float64; Arrival Arrival}`, `Slot{Name string; Axis wm.LayoutKind; Panes []Pane; PaneWeights []float64; LastPane int}`, `Pane{Windows []int; Front int}`, and `map[int]WindowWant` with `WindowWant{Workspace string; Mode Mode; Float wm.Frame; Home *HomeSlot}`, `HomeSlot{Workspace, Slot string; Pane, Index int}`. A slot always has at least one pane; `Axis` is `wm.LayoutColumns` or `wm.LayoutRows` and is read only when `len(Panes) > 1`; `const MaxPanes = 4`. `Mode` is `slotted`, `floating`, `excluded` or `suspended`. `Frames(area wm.Frame, ws Workspace) map[int]wm.Frame` uses `Override` when set and divides each slot by `PaneWeights`; `(*Workspace).ClearOverride()`. Pane edits, each pure and returning an error that changes nothing: `(*Workspace).Split(win int, dir wm.Direction) error`; `(*Workspace).SplitAll(win int, axis wm.LayoutKind, slotFrame wm.Frame) error`, which breaks the pane that holds `win` per § *Splits within a slot*, *Break a stack* (an empty `axis` in an unsplit slot picks the longer side of `slotFrame`; a non-empty `axis` that differs from a split slot's returns `ErrSplitAxis`); `(*Workspace).Merge(slot string, front int) error`; `(*Workspace).MergeAll()`; `(*Workspace).EqualPanes()`; with `ErrSplitAxis`, `ErrPaneLimit` and `ErrPaneSingle`; a pane emptied by any edit is removed. `PaneAt(area wm.Frame, ws Workspace, p wm.Point) (slot string, pane int, ok bool)` replaces `SlotAt`. `Neighbour(area wm.Frame, ws Workspace, win int, dir wm.Direction) (slot string, pane int, ok bool)` is the one direction rule for `focus`, `swap` and `stack`. `ParkCorners(displays []wm.Display) map[string]wm.Frame`. `AdoptSlots(windows []wm.NativeWindow, area wm.Frame) ([]Slot, []float64)` returns one pane per slot. `Desired.Copy() *WorkingCopy`; `(*WorkingCopy).Commit(l *Lifecycle) (dropped []int)`. `native-state.json` stores panes, pane weights and `LastPane`.

Gate: pure table tests on the three-display fixture: 40 windows in one stack; weights that do not sum to 1; an override replaces profile weights and `ClearOverride` restores them; `PaneAt` on slot and pane edges; a commit that names a tombstoned window drops it; a window that appeared after the copy joins at the arrival target, in the pane focused there last; `AdoptSlots` on columns, stacks and overlapping floats. Splits: `Split` east then west on a columns slot gives three equal panes in order; `Split` north on that slot returns `ErrSplitAxis`; a fifth pane returns `ErrPaneLimit`; a one-window pane returns `ErrPaneSingle`; `SplitAll` on a six-window unsplit slot gives three single panes and a three-window fourth pane; `SplitAll` on the second pane of a two-pane slot whose second pane has five windows gives panes 2, 3 and 4, the last with three windows, keeps pane 1 and its weight, and splits pane 2's weight in three; `SplitAll` in a four-pane slot returns `ErrPaneLimit`; `SplitAll` with `rows` on a `columns` slot returns `ErrSplitAxis`; `SplitAll` with no axis picks rows on a tall slot and columns on a wide one; `Merge` keeps the named front and pane-then-stack order; removing the last window of a pane removes the pane, and of the second-last pane leaves a plain stack; `Neighbour` moves between panes of one slot first, then crosses from the last pane of a slot into the next slot's nearest pane, and is the target of `stack <dir>` whether the next slot is split or not; a `native-state.json` round trip keeps panes and weights.

### N7 — Darwin platform layer

Interfaces: `native.Platform`: `Apps() ([]App, error)`, `Attach(pid int, ids []int) (attached []int, err error)`, `SetFrame(op uint64, id int, f wm.Frame) error`, `ReadFrame(op uint64, id int) error`, `Focus(op uint64, id int) error`, `FocusedWindow(pid int) (int, error)`, `Displays() ([]wm.Display, error)`, `MouseButtons() uint`, `ShowIndicator(display, text string) error`, `HideIndicator() error`, `PickSlot(ctx context.Context, displays []string) (<-chan wm.Point, error)`, `Events() <-chan PlatformEvent`, `Results() <-chan Result`, `Trusted() bool`. `Result{Op uint64; Window int; Frame wm.Frame; Err error}`. `PlatformEvent{Kind EventKind; PID, Window int}` with kinds for app launch and quit, window created, destroyed, focused, moved, resized, minimized, deminimized, title, display changed, native Space changed and Dock restarted. `internal/wm/native/darwin` implements it with cgo and per-app workers; the indicator and the pick panels are `NSPanel`s per § *Move mode*.

Gate: N1's live tests pass against this layer. Worker tests, per § *AX threading*:

- a hung app (a test helper that blocks its main thread) times out at 250 ms while another app's write completes, and a hotkey registered in the same daemon fires and is logged within 50 ms during the hang;
- an app that quits while a frame write and a read-back are queued: the worker removes its observer and releases every element, the queued requests return `ErrAppGone`, and a late result is dropped;
- attach then destroy of 100 windows leaves no retained element (a retain-count hook in the test build);
- AppKit calls from a non-main goroutine are rejected by a debug assertion in the cgo layer;
- `Attach` on a hidden Space returns nothing and, after the Space becomes visible, returns the window;
- `PickSlot` returns a point on click and closes its panels on context cancel.

### N8 — Native driver, actor and rescue

Interfaces: `native.Driver` implements `wm.Driver`, `wm.Transactional`, `wm.EventStream` and `wm.ProcessSupervisor` over `Model`, `Platform` and `wm.SpaceService`. The actor loop, operation fencing, pending writes and status per § *Desired and observed state*; focus per § *Focus*; checkpoints and compensation per § *Job transactions*; cold start per § *Lifecycle contract*; SA reload on Dock restart, with the recovery hold separate from the pause; `*wm.ApplyError`; `StatePublisher.Request` after each flush; `tess query`; `tess rescue`. Pause and budget per § *Pause and move budget*: `type ErrPaused struct{ By, Reason string }` in `wm`; `native.Pause{By, Reason string; At time.Time}`; `native.PauseStore` with `Save(p Pause) error` (temp file, `fsync`, rename, directory `fsync`), `Load() (*Pause, error)` (an unreadable file returns a `by: "budget"` pause and the error) and `Clear() error` (remove, directory `fsync`), over a `native.FS` interface so tests can fail each step; `native.Budget{WindowMoves int; Window time.Duration; FailedWakes int; WakeWindow time.Duration}` with `DefaultBudget = Budget{10, 30 * time.Second, 3, 10 * time.Minute}`; `(*Driver).Stop(ctx context.Context, reason string) (StopReport, error)` with `StopReport{InFlight int; Durable bool}`, and `(*Driver).Start(ctx context.Context) error`, both sent to the actor on a control channel that the actor selects before the job channel; a `pauseGate` (an `atomic.Bool` shared with each per-app worker) that the actor and the workers check before every dispatch; `daemon.Options.Control func(argv []string) (handled bool, code int, out string)`, which the daemon calls on each request before it queues a job; `commands.StopCommand{Reason string}` and `commands.StartCommand{}`; CLI kinds `stop` and `start`, with exit code 3 for `ErrPaused`; the daemon's wake path reports each `WakeRoutine` and `DisplayQuiet` result to the driver with `(*Driver).WakeResult(err error)`. The state stream gains `paused` with `durable`.

Gate: `synctest` tests on a fake platform and a fake Space service:

- a window closes while a job's working copy holds it, and the commit does not bring it back;
- a result arrives after its timeout and after `Abort`, and is dropped;
- two results for one window arrive in reverse order, and only the latest operation's counts;
- a job is cancelled after a successful `MoveWindowToSpace`, and desired membership follows the move;
- a `CreateSpace` whose job aborts with the Space still empty is destroyed;
- an `OutcomeUnknown` is resolved by `Snapshot`;
- a focus worker returns success with no focused-window event, and `FocusWindow` returns `ErrFocusTimeout`;
- intermediate and delayed moved events during a pending write raise no drag;
- Dock restart, per § *Lifecycle contract*, *Dock restart*: SA capabilities are false before the reload returns; a Space operation in flight at the notice returns `OutcomeUnknown` and its late result is dropped; jobs queued during recovery run after it;
- Dock restart while paused (user and budget): `Snapshot` and `tess sa load` run; no `WINDOW_LAYER`, frame write, focus or mutating Space call is sent; pinned windows report `pinned: false`; queued jobs return `ErrPaused` after the hold ends; `pause.json` is unchanged; `tess start` then succeeds and re-pins;
- Dock restart where Space IDs change: each workspace is re-bound to the new Space that holds its windows, and the reconcile is logged;
- Dock restart where `tess sa load` fails: the daemon keeps running, SA bits stay false, the state stream names the failure, and `CreateSpace` follows Decision 10;
- Dock restart with two pinned windows: `WINDOW_LAYER` is sent again for both when `PinLayer` returns, and `pinned: false` is reported when it does not;
- with every SA capability off, apply, focus and slot moves still work and `CreateSpace` follows Decision 10;
- with `SpaceCaps.Focus` off, `FocusWindow` on a hidden window returns `ErrFocusTimeout` with reason `space-focus-unavailable` and focuses nothing else;
- a refused write returns `ApplyError` and `fresh: false`;
- cold start with windows on three hidden Spaces and one window unknown to `native-state.json`;
- `tess stop` while a job is blocked on a 2 s `FocusSpace`: the reply comes before the job finishes, from the control channel; the job's context is cancelled and it aborts;
- `tess stop` with a frame write queued in a worker but not yet sent: the worker drops it, and the fake platform records no AX call after the stop boundary;
- `tess stop` with a frame write and a `MoveWindowToSpace` already handed to the fake platform: the reply reports `in flight at stop: 2`, their late results are fenced, and no new call follows;
- after `tess stop` replies, every move, focus and Space call returns `ErrPaused` with no platform call; `tess query` still answers; the MCP `stop` tool takes the same control path;
- persistence failure injection on the fake `FS`, one case per step (temp write, `fsync`, rename, directory `fsync`): the pause holds in memory, `stop` returns an error, the state stream shows `durable: false`, and a later retry writes the file;
- a daemon start with a valid `pause.json` takes a `Snapshot`, writes nothing and reports `paused`; with a truncated or invalid `pause.json` it reports a `by: "budget"` pause naming the parse error and writes nothing;
- `tess start` whose `Snapshot` fails leaves `pause.json` and the flag in place; `tess start` whose `Clear` fails leaves tess paused; a successful `start` removes the file, then clears the flag, flushes once and resets the budget, in that order;
- a fake app that undoes every frame write: the 11th write to that window inside 30 s pauses tess with `by: "budget"` and a reason that names the window, its app and the count; the 10th does not; a read-back, a user drag and an OS relocation do not count;
- three failed wakes in a row pause tess; two failures and a success do not;
- after a budget pause, `tess start` with the fault still present pauses again within one window;
- the existing `internal/commands` and `internal/daemon` suites pass on `native.Driver`.

### N9 — Slot interaction

Starts after Matt rules on Open Questions 1 and 2; under 1B the pane verbs act on leaves, and `merge` gains `merge all`. Interfaces: the verbs in § *Slot verbs* on `native.Driver`: `stack <dir>`; `split <dir>`, `split all [columns|rows]` and `merge` over the N6 pane edits, with `commands.SplitCommand{Dir wm.Direction; All bool; Axis wm.LayoutKind}` and `commands.MergeCommand{}`; `split all` calls `SplitAll` with the focused window and the slot's computed frame; `focus`, `swap` and `stack <dir>` all resolve their target with `Neighbour`; `move-mode` with `commands.MoveModeCommand{}` and actor state `MoveMode{Source int; Slot string; Pane int; Deadline time.Time}`, hit-tested with `PaneAt`; float and pin per § *Float and pin*, using `HomeSlot` with its pane index and the `PinLayer` capability; `resize` on `Override` or on `PaneWeights` per § *Temporary slot resize*; `layout save|reset` with `commands.LayoutCommand{Action string}`; `layout save` runs the symlink, hash, AST-edit, vet, rename and reload steps of Decision 11. Split effects come from one table, `splitPolicy(cmd) SplitEffect` with `SplitEffect{ClearOverride, MergeSplits, EqualPanes bool}`, one row per line of § *Splits within a slot*, *Lifetime*; the override table no longer decides splits. New CLI kinds `move-mode`, `stack`, `split`, `merge` and `layout` in `internal/cli`. The state stream gains `moveMode`, per-window `mode` and `pinned`, per-slot `axis` and `panes`, and per-workspace `modified`. Downstream: Matt's profile gains native-only bindings for `split east`, `split south`, `split all` and `merge`.

Gate: `synctest` tests on the fake platform: move mode commits on a click inside a pane, on a click on a window, and on the keyboard path, and cancels on Escape and on the 10 s timeout; the highlight moves between panes of one slot before it leaves the slot; the indicator shows and hides once each; Escape is registered only while move mode is on. A drag start floats the window, closes its pane and records the home slot and pane; `toggle-float` returns it to the same pane and index; a drag of the only window of a pane removes the pane, and `toggle-float` rebuilds it at the same pane index; a home slot that vanished sends it to the arrival target; a drag that ends on another display moves membership. With `PinLayer` off the window is raised and reports `pinned: false`. Splits on the fake platform: `split east` on a two-window stack gives two side-by-side frames inside the slot and leaves every other slot's frame unchanged; `split all` then `merge` returns the slot to one stack with the focused window in front; `split all` on one pane of a split slot keeps its sibling panes' frames; `stack east` from the last pane of a slot lands in the nearest pane of the next slot, not the pane focused there last; `resize` along the split axis changes only pane frames, across it only slot frames; an arrival into a split slot lands in the pane focused last. One test per row of § *Splits within a slot*, *Lifetime*: `apply` merges every split; `layout save` keeps every split and every pane frame, with no frame write; `layout reset` keeps every split and sets pane weights equal.

Override tests, one per row of § *Temporary slot resize*, *What clears the override*: each command in the left column clears it, each event in the right column keeps it. Save and reset: `resize` then `layout reset` restores profile frames; `resize` then `layout save` rewrites the source file through the symlink, reloads it, and the override clears with no frame change; a hash mismatch at save refuses and leaves the file and the override unchanged; a vet failure leaves the file unchanged; a missing symlink target errors and saves nothing; `layout reset` after a save keeps the saved weights as the profile weights.

### M1 — Profile set, switching and `tess mcp`

Needs N8 and N9 (the pause, the budget and the indicator) and `tess-app.md` B2 (daemon profile reload). Runs before N12, so Matt can try drafts in the N11 scratch run.

Interfaces, per § *Profiles and the MCP server*:

- `internal/config`: `ProfileSet{Dir, Live string}` with `List() ([]ProfileInfo, error)`, `Read(ref Ref) ([]byte, error)`, `Write(name string, src []byte, live *Profile) (Ref, error)` (runs `Vet`, then stores a new revision: temp file, `fsync`, rename, directory `fsync`; an existing revision file is never opened for writing), `Latest(name string) (Ref, error)`, `Delete(name string) error` (refuses `live`; keeps any revision a pointer names), `Snapshot(src []byte) (Ref, error)` (stores the live bytes as a `live` revision at each load), `GC(a Active) error`; `Ref{Name, Rev string}`; `ProfileInfo{Name string; Latest Ref; Valid bool; Err string}`; `ValidName(name string) error`; `Active{Active, Good, Prior Ref; Trial *Trial}` and `Trial{Ref Ref; Until time.Time; By string}`, read and written as `profile-active.json` with the same durable steps; `Vet(src []byte, live *Profile) error`, the one check for every path, which runs `Parse`, then the `backend` rule and the Open Question 4 rule (option B: `spawn` and `hotkeys` equal to live's). `LoadLive(path string) (*Profile, []byte, error)` reads the symlinked live file; the daemon and `tess mcp` both call it. `Load` reads `profile-active.json` first and falls back `active`, `good`, `live`.
- `commands.ProfileCommand{Action, Name string; For time.Duration}` for `list`, `show`, `vet`, `write`, `delete`, `switch`, `preview`, `keep`, `back`, `pick` and `promote`; `promote` reuses the Decision 11 save path from N9. CLI kind `profile` in `internal/cli`.
- The daemon socket protocol: `run 1 {"argv":[…],"profile":"<path>","stdin":"<text>"}`. `const MaxStdin = 262144` (decoded UTF-8 bytes) and `const MaxRequestLine = 1638400` (encoded bytes) in `internal/daemon`. `readLine` is replaced by `readRequest(conn net.Conn) ([]byte, error)`, which reads through `io.LimitReader(conn, MaxRequestLine+1)` and returns `ErrRequestTooLarge` without decoding when the line has no `\n` within the limit; the decoded `stdin` over `MaxStdin` and invalid UTF-8 are a `reject`. Both checks come before `accepted`. The CLI fills `stdin` for `profile vet -` and `profile write <name> -`.
- The trial timer lives in the actor; at a daemon start an expired or unfinished trial switches back to `good`.
- `native.Platform` gains `ShowPicker(display string, rows []string, active int) (<-chan PickerResult, error)` and `HidePicker() error`, with `PickerResult{Row int; Preview bool}`, and `ShowIndicator` gains buttons: `ShowIndicator(display, text string, buttons []string) (<-chan int, error)`.
- `internal/mcpserver` (portable, no cgo) on `github.com/modelcontextprotocol/go-sdk/mcp`: `func New(dial func(ctx context.Context) (net.Conn, error), loadLive func() (*config.Profile, []byte, error), version string) *mcp.Server`, one `mcp.AddTool` per row of the tool table with typed input and output structs and the listed annotations. `validate_profile` and `get_schema` call `loadLive` and `config.Vet` in process; every other tool dials. `tess mcp` passes a `loadLive` that calls `config.LoadLive` on `~/.config/tessera/profile.cue` and runs `New(...).Run(ctx, &mcp.StdioTransport{})`. The state stream gains `profiles` and `trial`; the app's tray lists profiles and runs the same verbs.
- Downstream: Matt's MCP client config gains a `tess` server entry with command `tess mcp`; his profile gains bindings for `profile pick`, `profile back`, `stop` and `start`.

Gate: `ProfileSet` tests on a temp dir: a bad name, a vet failure and a backend change write nothing; under Open Question 4 option B, a draft that changes a live `spawn`, sets `spawn` on a new key, or changes `hotkeys` fails `Vet`; a draft whose `spawn` argv would write into the repo checkout (`["sh", "-c", "echo x >> <repo>/profile.cue"]`) or run `["tess", "start"]` fails `Vet` and is never written, so no switch can run it; `Write` twice to the same name keeps both revisions and changes no pointer; `Delete` of the name `good` points at keeps that revision on disk, and `back` still loads it; `Delete` refuses `live`; an edit to the live source after `good` was taken leaves `good`'s bytes unchanged; a crash after each step of a pointer write (fake `FS`) leaves either the old or the new `profile-active.json`, never a mix; a missing `active` revision at start falls back to `good`, then `live`. Switch tests on the native fake: `switch` then `back` returns to the first profile's frames; `back` twice returns to the second; `write` to the active name during a trial does not reload, and `back` returns to the old `good` bytes; `preview` reverts at the deadline and at a daemon restart; `keep` sets `prior` to the old `good` and `good` to the trial; a switch whose apply trips the budget pauses tess and leaves `good` unchanged, and `back` then `start` restores the old profile; while paused, `switch` changes only `profile-active.json`. Socket tests: a `stdin` of exactly 262144 bytes is accepted; 262145 is a reject; 262144 bytes of `"`, newline and `\x1f` (each escaped) fit the line limit; 262144 bytes of multi-byte UTF-8 are accepted; invalid UTF-8 is a reject; a 2 MiB line with no newline is rejected after at most `MaxRequestLine+1` bytes are read; a request with no `stdin` is unchanged. MCP tests with `mcp.NewInMemoryTransports` against a fake daemon socket: the tool list matches the table with its annotations; each tool sends the expected argv and stdin; a non-zero exit is a tool error with the stderr text; `validate_profile` gives the same accept or reject for each `Vet` case above with the daemon up and with it down; with the live profile unreadable, `validate_profile` and `get_schema` return that error; every other tool reports `tess daemon not running` with no daemon; no tool runs `start`, `promote` or `layout save`. Live, on Matt's Mac in the N11 scratch run: an agent writes two drafts through `tess mcp`, previews one, and Matt switches between them with the picker and goes back with one keypress.

### N10 — Observe mode

Interfaces: `backend: "observe"` runs the yabai driver and, read-only, the native platform and Space service. The daemon logs each window whose native membership or frame differs from yabai's, including windows on hidden Spaces, and each event yabai saw that tess did not. tess's SA is not loaded in observe mode; yabai's is.

Gate: one week of Matt's use. Every difference is fixed. A difference may be accepted only when it does not change the membership the planners read. This proves enumeration and event coverage only, not writes or focus.

### N11 — Actuation gate

Interfaces: N1's live suite, extended, run by Matt on the desk against `native.Driver` on one scratch Space. The run starts with `tess sa load --test`, which writes `sa-restore.json` and boots out yabai's agent, and ends with `tess sa restore`, which brings yabai back with its own SA.

Gate: slot placement, swap, stack, split, `split all` and merge, resize inside a split slot, save and reset, focus (Zen across displays and on a hidden Space, 20 trials each, success rate recorded), move mode by click and by keyboard into a pane, a drag that floats and pins and its return to a split slot, `toggle-float` back to the slot, a window that refuses a frame, a terminal, an Electron app, `tess rescue`, a daemon `kill -9` mid-flush, and a cold restart with windows on hidden Spaces and windows created while the daemon was down. `tess stop` then a daemon restart moves nothing; `tess start` resumes. A test helper app that undoes every frame write trips the move budget, and the indicator names it; record the highest move count per window in the honest runs. Dock restart three ways: plain (record whether Space IDs change, which settles the [INFERENCE] in § *Lifecycle contract*); during a `FocusSpace`; and with the payload bundle removed so the reload fails. Then a `kill -9` of the daemon mid-run followed by `tess sa restore` alone, which must leave yabai tiling with its own SA. No drag misclassified, no write loop.

### N12 — Migration and switch

Interfaces: `tess migrate --to native`, `tess migrate --reverse` and `tess migrate --status` per § *Migration*; profile `backend: "yabai" | "observe" | "native"`; `migration.json` and `migration-snapshot.json`. Bindings per § *Bindings across the switch*: `backends?: [...("yabai" | "native")]` on `#Hotkey` and `config.Hotkey`; `wm.Driver.Supports(argv []string) bool`; `hotkey.Set(backend string, all []Binding) []Binding`; the switch is `Diff` from one set to the other, applied in one actor step. Downstream: home-manager deploys `~/.config/tessera/profile.cue` with `mkOutOfStoreSymlink` to the profile source in Matt's nix repo checkout (Decision 11), so `layout save` edits the live file; Matt's profile gains the native-only bindings and marks the yabai-only ones; sketchybar reads `tess query`; the yabai agent stays installed but the migration boots it out; the yabai sudoers entry stays until N14.

Gate: fake tests inject a crash after each item, forward and in reverse, and prove resume in the recorded direction; a profile that disagrees with the recorded state moves nothing; a reverse after Spaces were added and removed; a display change mid-reverse; a reverse item that fails three times ends in `reverse-blocked`. Binding tests: each set validates apart; a native-set binding that names a tree verb fails profile load; the switch registers exactly the native set with no moment where both sets or neither are registered; a crash between the binding item and activation resumes with the native set; the reverse registers exactly the S4 set. On Matt's Mac: one forward, one full reverse, then forward again; after each, `tess query` lists the registered combos and they match the expected set; then the live checklist: apply on desk, unplug to laptop, replug, focus-slot 1–9, Zen focus, Cmd-Tab to a hidden Space, native fullscreen and back, Dock restart, move mode on cmd+alt i, drag-float and return.

### N13 — Soak

Gate: two weeks on `native` with no reverse, with Space-service budgets and the move budget tuned from the recorded timings and counts. Every budget pause in the soak is logged with its reason; a pause on honest use raises the limit, and a pause on a fault gets a fix.

### N14 — Remove yabai and tree code

Interfaces: delete `internal/wm/yabai/` (driver, args, rules, signals, supervisor and test data), `internal/commands/staging.go`, `staged-windows.json` handling, `wm.RuleOps`, `wm.EventSource`, `wm.SettleOps`, `ArmInsert`, `ToggleSplit`, `SetSplitRatio`, `SplitType`, `LayoutBSP`, `WarpWindow`, `SetSpaceLayout`, `BalanceSpace`, `StackWindowCap`, `ErrSpaceFull`, the `observe` and `yabai` backend values, `tess migrate --to native` and its reverse, and the CLI verbs `insert`, `warp`, `reset-splits`, `balance`, `columns`, `space bsp`, `drop-action` and `rules`. `wm.Capabilities` loses `NeedsStaging`. The `backends` field of `#Hotkey` goes, with the yabai-only bindings. `sa.RestoreRecord` loses its yabai fields, and `tess sa restore` only uninstalls tess's payload. Rewrite `README.md`, which today calls tess "a config layer for yabai". Downstream: remove the yabai agent, yabairc, the yabai Homebrew formula and the yabai sudoers entry; `sudo yabai --uninstall-sa`. The tagged downstream commit from the first commit keeps them.

Gate, in two commits. First commit: `docs/runbooks/rollback-to-yabai.md` per § *Migration*, *After N14*, with the pinned release, its checksum and the tagged downstream commit; Matt runs it once on the desk, checks the § *Guarantee* list on yabai, then runs `tess migrate --to native` and checks the binding set again. Second commit, only after the first is exercised: the deletions above. Then a search for `yabai` in `*.go` files finds nothing outside comments that name the SA's source; `go build ./...` on darwin and `GOOS=linux go vet` on portable packages pass; `moon ci` green; Matt runs one full day with yabai uninstalled.

### V0 — Parking spike on Matt's geometry

Interfaces: live probes in the N1 suite, behind `//go:build darwin && live && virtual`, with a disposable AX test window and a scratch controller. Needs only S2 and N7. Does not block any N task.

Probes, from evidence § *Virtual workspaces*: for AW windows, park at both AW bottom corners and at the free laptop bottom-left and G9 bottom-right corners; record requested and read-back frames and screenshots of all three displays, and whether any part stays visible or clickable. Repeat with Zoom and other resistant apps, floating and slotted sizes, and several windows. Then rapid workspace changes; Cmd-Tab and Dock activation for single- and multi-window apps; Mission Control; native fullscreen enter and exit; tess termination and relaunch while windows are parked; unplug and reconnect, and sleep and wake, with parked AW windows.

Gate: pass only if AW windows stay out of the visible work area with no leak onto another display, every activation restores the intended workspace and window, and cold-start rescue restores every parked frame. Results go to evidence.md. A fail ends stage V for this geometry.

### V1 — Virtual workspace lifecycle (only after V0 passes)

Interfaces: parking and the ledger per § *Parking and recovery*; activation restore, stray Spaces and unplug per § *Lifecycle contract*; a model-backed `wm.SpaceService` with every SA capability off.

Gate: fake tests for every row of the lifecycle table under V; live: AW windows parked and restored, Cmd-Tab and Dock activation of a parked window, native fullscreen and back, unplug with inactive workspaces, lid close and reconnect, daemon `kill -9` with parked windows then restart and `tess rescue`.

### V2 — Consolidation migration (only after V0 passes)

Interfaces: `tess migrate --to virtual` and `--reverse` per § *Migration*.

Gate: a crash after each item resumes in the recorded direction on fakes; on Matt's Mac, one forward and one full reverse with parked windows before the daily switch.

### V3 — Retire the SA (only after V0 passes)

Interfaces: `tess sa uninstall` on Matt's Mac; the sudoers entry goes downstream. The `sa/` code stays, because a Mac with SIP partly off can still use native Spaces. Matt re-enables SIP when he chooses (Decision 2).

Gate: two weeks on `virtual` with no reverse.

## Tasks

- [ ] S0 — delete the Windows (komorebi) code, CI job and design record; add `THIRD_PARTY_NOTICES.md`
- [ ] S1 — every command through the daemon (#76 T5 protocol)
- [ ] S2 — darwin cgo build, `accessory` run loop, `otool` and bare-runner checks
- [ ] S3 — `hotkeys` in the profile; Carbon press and release; repeat on any binding; 50-combo live check
- [ ] S4 — nine raw bindings as tess verbs on yabai; behaviour test for all 50; downstream skhd removal
- [ ] N1 — feasibility spike: writes, focus, drags, SkyLight with SIP on, non-activating panel
- [ ] N2 — stable signed daemon bundle (self-signed now, Developer ID later); TCC gate
- [ ] N3 — `wm.Capabilities` and `wm.Transactional`; planner and staging behind them
- [ ] N4 — tess's scripting addition: fork, cut to six opcodes with the exact packed arguments (anchor sid for create), install, load only on native, version handshake, crash-safe `tess sa restore`, attribution
- [ ] N5 — `wm.SpaceService` with capabilities and postconditions; anchor resolution; nested focus budgets; SIP-on degradation
- [ ] N6 — after Matt rules on Open Questions 1 and 2: slot model with splits inside a slot (shape, cap and persistence per those rulings), override and pane weights, home slots with pane index, `Frames`, `PaneAt`, one `Neighbour` rule, `SplitAll` on the focused window's pane, `AdoptSlots`, working copy
- [ ] N7 — `native.Platform`, per-app AX workers and observers, main-thread AppKit dispatch, indicator and pick panels
- [ ] N8 — `native.Driver`, actor, fencing, focus postcondition and missing-focus path, Dock-restart recovery with its hold separate from the pause, `tess stop` and `tess start` as control messages with dispatch checks, fail-closed `pause.json`, per-window move budget and failed-wake budget, `tess query`, `tess rescue`
- [ ] N9 — after Matt rules on Open Questions 1 and 2: slot interaction with `split`, `split all` and `merge`, pane-aware focus, swap and stack, move mode into panes with top-right indicator, float and pin on drag with pane return, temporary resize of slots and panes, split lifetime table, save, reload and reset
- [ ] M1 — profile set with revisions that never change and `active`, `good`, `prior` and `trial` pointers, one `Vet` online and offline with the draft limits of Open Question 4, bounded socket `stdin`, `tess profile` verbs with preview, keep and one-step back, picker and trial panels, `tess mcp` on the official Go SDK
- [ ] N10 — observe mode for one week, including hidden Spaces
- [ ] N11 — actuation gate on a scratch Space; Dock restart three ways; crash then `tess sa restore`
- [ ] N12 — recorded migration from yabai to native with the binding-set switch, reverse, sketchybar on `tess query`
- [ ] N13 — two-week soak; tune Space-service budgets and the move budget from recorded counts; log every budget pause with its cause
- [ ] N14 — exercise the rollback recipe first; then delete the yabai driver, staging, insert arms, tree verbs and yabai-only bindings; uninstall yabai downstream
- [ ] V0 — parking spike on Matt's three-display geometry (optional, any time after N7)
- [ ] V1 — virtual workspace lifecycle (only after V0 passes)
- [ ] V2 — consolidation migration and reverse (only after V0 passes)
- [ ] V3 — retire the SA on Matt's Mac; SIP per Decision 2
