# Design: the tess app — a live space grid and layout editor

*Design record for a desktop app that replaces GridLion. It builds on
[`resident-daemon.md`](resident-daemon.md), [`go-port/design.md`](go-port/design.md),
[`release-binaries.md`](release-binaries.md). It edits no frozen record.*

## Decisions

Matt ruled on PR #71 on 2026-11-08 and took the recommended option for
each question. The plan below states the chosen path.

- **UI framework: Wails v3.1.0, with a recorded waiver of the
  1,000-importer rule.** Matt, 2026-11-08, PR #71. Fyne is the only option
  that passes the rule, and it cannot show a window on every Space.
- **Grid shape: a per-display grid that wraps at optional `grid.columns`.**
  Matt, 2026-11-08, PR #71. Desk displays stay one short row, and the long
  laptop row wraps. Without `grid.columns`, a display uses one row.
- **Profile writes: `mkOutOfStoreSymlink` the deployed profile to the repo
  working copy.** Matt, 2026-11-08, PR #71. It is the only option where
  "edit, see the layout change, commit" works without a rebuild.
- **Hotkey HUD: every `tess space-focus` shows a 0.6 s passive HUD on the
  target display.** Matt, 2026-11-08, PR #71. It shows where you landed in
  the grid, which is the point of grid navigation.
- **Platform overlay: portable core, per-OS overlay.** Matt, 2026-11-08,
  PR #71. Navigation already works with no app, so the overlay is a
  convenience. No Linux feasibility gate is needed; Linux gets the
  normal-window fallback.
- **Editor arrangement: a selector over every arrangement, defaulting to
  the active one.** Matt, 2026-11-08, PR #71. Matt edits the desk layout
  away from the desk, and the default keeps the common case one click.

## Direction

- **Product fit.** tess owns the Space layout, so it is the only process
  that knows when a Space is created, moved or destroyed. GridLion reads
  Mission Control and goes stale whenever tess changes Spaces; it shows
  dead cells. Its docs require Accessibility permission "for global
  shortcuts, Mission Control automation, and reliable Space switching"
  ([GridLion docs](https://gridlion.hopefullyuseful.com/docs/)). A tess app
  reads the daemon's world and switches through yabai, so it needs neither
  permission.
- **Scope across repos.** tessera: a new `app/` module, a state stream in
  the daemon, a `space-focus` command, a profile editor package, and a
  flake output. Matt's nix config: the app package, a launchd agent, skhd
  binds, and the `mkOutOfStoreSymlink` profile link. These are downstream only.
- **Prior art.**
  - [GridLion](https://gridlion.hopefullyuseful.com/): a menu-bar app with
    a hotkey grid over Spaces. Borrow: the 2D grid, arrow-key moves, and
    its split between a keyboard picker and a passive overlay. Avoid: a
    second, scraped source of Space state.
  - [glzr-io/zebar](https://github.com/glzr-io/zebar): a Tauri 2 app for
    GlazeWM. Borrow: a web UI that the WM feeds, not a UI that queries the
    WM; and its widget config, which keeps `focused` separate from `zOrder`
    ([`widget-config.ts`](https://github.com/glzr-io/zebar/blob/main/packages/client-api/src/config/widget-config.ts)).
  - sketchybar in Matt's setup refreshes on `yabai_spaces_changed`
    triggers (`RegisterSignals` in `internal/daemon/wiring.go`). Borrow:
    refresh on WM events, never on a timer.
- **Lifetime.** Long. The app outlives the yabai backend and must move to
  a Linux WM later. Effort goes into the daemon protocol and the pure grid
  logic, which every platform shares. The UI stays thin.
- **Simpler option.** `tess space-focus north|south|east|west` bound in
  skhd gives grid navigation with no app at all. It is task A2, so it ships
  first and stands alone. The app adds the visual grid and the editor.

## Problem / Intent

Show a live grid of Spaces per display, with each Space's windows as app
icons, and switch Space on click or hotkey. Later, edit desk tracks,
`laptopPinned` and `deskSlots` in the app and write them back to the CUE
profile. The grid must reflect every change that tess or the WM reports
(§ *Freshness*), and the grid, switching and editor must run on macOS
and Linux (overlay behaviour per platform, § *Platform capabilities*).

## Approach

### One source of truth: the daemon streams full state

The daemon already owns the WM connection, event intake and every cascade
that changes Spaces (`daemon.Run`, `Hub`, `WakeRoutine`). It becomes the
only producer of grid state. The app holds no WM driver and runs no query.

- **Subscribe on the existing socket.** Today `handle` in
  `internal/daemon/serve.go` reads one line and closes the connection:
  "handle reads one line under a deadline, closes the connection at once,
  then dispatches it." A new first line, `subscribe 1`, keeps the
  connection open. The daemon writes newline-delimited JSON frames to it
  until either side closes or the daemon's context ends; `Serve` waits for
  subscriber goroutines like any other. Every other line keeps today's
  close-at-once behaviour, so `nc` notifiers never linger.
- **Hello, then an initial frame, always.** On subscribe the daemon writes
  a `hello` frame (protocol version and tess version), then a complete
  `state` frame for that connection, even when the world has not changed
  since another client saw it. A reconnect is a new connection, so it also
  gets a full frame. Change suppression is per subscriber (the hash of the
  last frame sent to it), never global.
- **Full snapshots, not diffs.** Each `state` frame carries the whole
  world: displays, their Spaces in order, each Space's windows, the focused
  Space and grid positions. The app replaces its model on every frame.
  Matt's world is about 20 Spaces and 100 windows, a few KB.
- **Bounded freshness.** A `StatePublisher` keeps a dirty flag. A refresh
  request sets it. The publisher snapshots 75 ms after the last request,
  but never later than 250 ms after the first pending one, so a steady
  event stream cannot postpone a frame forever.
- **Failure is visible.** If `wm.Snapshot` fails, the publisher sends a
  `status` frame with `fresh: false` and the error. The app dims the grid
  and keeps the connection. The next successful snapshot sends `state`
  with `fresh: true`. A snapshot failure never disconnects a subscriber.
- **State and control use separate queues.** Each subscriber has a
  one-slot, latest-wins slot for `state` and `status`, and an ordered queue
  of up to 16 control frames (`ui`, `error`). A state frame can replace an
  older state frame but never a control frame. The writer sends queued
  control frames before the pending state frame. A full control queue or a
  missed 1 s write deadline drops the subscriber; the app reconnects. The
  daemon caps subscribers at 8.
- **No subscribers, no snapshots.** The publisher does nothing while no
  client is connected, so the daemon's yabai load does not change for users
  without the app.

Polling was rejected: the app would need its own driver, it would lag
by the poll interval, and it would add constant yabai queries.

### Freshness: what refreshes each field

A field is only as fresh as the events that invalidate it. Every published
field has a trigger below. Events already wired to the hub
(`DisplayEvents`, `FlexEvents` in `internal/daemon/hub.go`) also call
`StatePublisher.Request` from `Hub.Notify`. The rest get new labelled
yabai signals, `tessera-state-<event>`, whose action sends the line `state`.
All names exist in yabai's `signal_type_str` (`src/event_signal.h`).

| Field | yabai events that refresh it | Source |
| --- | --- | --- |
| Display set, frames | `display_added`, `display_removed`, `display_moved` | hub (existing) |
| Display frames, scale | `display_resized`, `display_changed` | new signal |
| Space list, order | `space_created`, `space_destroyed` | new signal |
| Focused and visible Space | `space_changed`, `display_changed` | new signal |
| Space labels, layout | end of every tess rebuild (§ *Snapshot consistency*) | busy-off |
| Windows per Space | `window_created`, `window_destroyed`, `application_launched`, `application_terminated` | hub (existing) |
| Windows per Space | `window_moved`, `window_minimized`, `window_deminimized` | new signal |
| Window title | `window_title_changed` | new signal |
| Focused window | `window_focused` | new signal |

**Not covered:** a Space relabel or reorder done by hand with `yabai -m
space` fires no yabai signal. The grid shows it at the next event of any
kind. tess itself never mutates Spaces outside a busy scope.

### Snapshot consistency

`wm.Snapshot` runs three separate queries under errgroup, so a rebuild
can change Spaces between them, and a mixed world must never be shown as
final.

- **One busy signal for every rebuild.** `commands.PreserveFocus` already
  wraps every rebuild — Apply, Laptop, the display cascade and Snap — in a
  scope that sends `tess_busy STATE=on|off` through `env.Nudge`. The
  composition root's `Nudger` gains a second sink: it also sends
  `busy on <pid>` and `busy off <pid>` to the daemon socket. This covers
  standalone `tess apply` and `tess laptop` processes, which hold only
  their own file locks. Inside the daemon, `TopologyState` arrivals and
  cascades report the same way in-process. The daemon keeps a busy set
  keyed by pid, each entry expiring after `CascadeTimeout` so a killed
  process cannot pin it.
- **Generation check.** The publisher keeps a generation counter that
  every busy-on, busy-off and refresh request bumps. It reads the
  generation and busy set, snapshots, and reads them again. It publishes
  only if the set was empty and the generation unchanged across the
  snapshot. Otherwise it discards the world and retries after the next
  busy-off or request.
- **While busy** the publisher sends a `status` frame with `busy: true`
  and keeps the last committed `state`. The app dims and disables clicks.
  Busy-off triggers a fresh snapshot.
- **One profile per snapshot.** `BuildState` reads the profile pointer
  once (B2), so grid positions and labels come from one profile
  generation.
- **A hotkey during a rebuild.** `tess space-focus` does not wait for
  busy. A rebuild's `PreserveFocus` restores the window that had focus when
  it started, which can move focus back. This race is accepted and
  documented: the hotkey is retried by hand, and the app blocks clicks
  while busy.

### Commands go through the CLI

The app runs `tess space-focus <space-id>` for a click, and other `tess`
subcommands later. It does not import `commands` or a driver. Every action
then has one code path, shared by skhd and the app, and the app stays
platform-neutral. A click costs a Go process start, a profile load, one
Spaces query, the focus call and a verifying query [INFERENCE: tens of ms;
A4 measures the whole path].

The app runs the `tess` that the daemon runs. It reads the path from the
`TESS_PATH` environment variable that its launchd agent sets, then falls
back to `PATH`. It does not bundle its own `tess`. The `hello` frame
carries the daemon's protocol version; an unsupported version shows "tess
daemon too old" and disables actions.

### Grid navigation lives in the engine and the CLI

Grid layout is pure: `engine.SpaceGrid(profile, world) Grid` assigns each
Space a display, row and column. `engine.GridNeighbor(world,
grid, from, dir)` returns the target Space. The daemon puts the same grid
positions into each `state` frame, so the app draws exactly the grid the
hotkeys move through.

`tess space-focus north|south|east|west|<space-id>` resolves the target
with these functions and calls a new driver method,
`FocusSpace(ctx, wm.SpaceID) (wm.FocusResult, error)`:

| Result | When | CLI exit | HUD flash |
| --- | --- | --- | --- |
| `FocusChanged` | the target is now focused, checked by query | 0 | yes |
| `FocusAlready` | the target was focused before the call; no WM call is made | 0 | yes |
| `FocusMissing` | the ID is not in the current snapshot, or the move has no neighbour | 1, "no such space" | no |
| `FocusRejected` | the WM refused, or the check query shows another Space | 1, the WM's message | no |

A backend error (yabai gone, timeout) is an `error`, exit 1, distinct from
`FocusRejected`. On yabai, `FocusSpace` resolves the stable ID to an index,
runs `yabai -m space --focus <index>`, then checks `QueryFocusedSpace`. If
the index moved between resolve and focus, the check sees another Space,
so it re-resolves and tries once more before returning `FocusRejected`.
The already-focused pre-check means tess never depends on yabai's
"cannot focus an already focused space" error. That message and the
"display is animating" rejection were read in upstream source, not
against the installed yabai. `yabai -m space --focus` needs the scripting
addition, which Matt's yabairc already loads. Hotkeys stay in skhd (macOS),
as `go-port/design.md` keeps keybinds out of the daemon.

The app has no global hotkey of its own. `tess grid` sends `ui toggle` to
the daemon, which relays a `ui` frame to subscribers. All keybinds stay in
one skhd file, and the trigger costs a `tess` start, not an app start. A
`ui` frame names its target: `toggle` carries the focused Space, and
`flash` carries the Space that `space-focus` landed on, so the app does not
depend on which arrives first, the `ui` frame or the next `state` frame.

### Framework

Evidence gathered 2026-11-08. "Importers" is pkg.go.dev "known importing
packages" for the named package, which is the metric the dependency rule
uses. "Dependents" is GitHub's dependency graph for the module; it is
context only and does not count toward the rule. Only Fyne passes the rule.

| Option | Latest release | pkg.go.dev importers (rule: 1,000) | Other adoption | Tray / menu bar | Global hotkeys | Overlay, all Spaces, transparency | Multi-monitor |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Wails v3 | v3.1.0, 2026-11-01 | `v3/pkg/application`: 189 (fails) | 642 GitHub dependents | yes | yes; Carbon on macOS, no Accessibility | `AlwaysOnTop`, `WindowLevel`, `CollectionBehavior` (CanJoinAllSpaces), non-activating `NSPanel`; transparent webview needs `-tags private_mac_apis` | `Screens` API, many windows |
| Wails v2 | v2.17.1, 2026-11-01 | module root: 571 (fails) | 6,123 GitHub dependents; used by tiny-rdm | no | no | `AlwaysOnTop` only | one window per app |
| Fyne | v2.8.2, 2026-10-07 | `fyne.io/fyne/v2`: 3,707 (passes) | used by fyne-io/terminal, rymdport | `SetSystemTrayMenu` | none built in; `golang.design/x/hotkey` has 49 importers | `RequestAlwaysOnTop` (since 2.8), "subject to support"; no Spaces control or transparency | `RequestPosition`, ignored on Wayland |
| Gio | v0.10.3, 2026-09-28 | `gioui.org/app`: 909 (fails); `gioui.org/layout`: 1,652 | used by gotraceui | no | no | `TopMost` option only | limited |
| Tauri 2 + Go sidecar | tauri 2.12.5 (crates.io) | not a Go module | 793 crates.io reverse deps; used by zebar | yes | plugin | yes | yes |
| Native per OS | — | — | — | yes | yes | yes | yes |

Sources: [Wails v3.0.0 release](https://github.com/wailsapp/wails/releases/tag/v3.0.0),
[Wails v3.1.0 post](https://wails.io/blog/wails-v3-1-released/),
[global shortcuts](https://wails.io/features/keyboard/global-shortcuts/),
[window options](https://wails.io/features/windows/options/),
[private macOS APIs](https://wails.io/guides/build/private-macos-apis/),
[Fyne `desktop.Window`](https://github.com/fyne-io/fyne/blob/develop/driver/desktop/window.go),
[Gio `app`](https://pkg.go.dev/gioui.org/app), and the module proxy for
release dates.

- **Wails v3 (chosen).** The release notes list "multiple windows", "System tray" and "global shortcuts".
  On macOS the window options cover the overlay: `WindowLevel`,
  `CollectionBehavior: CanJoinAllSpaces | FullScreenAuxiliary`, and
  `WindowClass: MacWindowClassPanel` with `PanelPreferences.NonActivating`
  ("Showing or focusing the panel then leaves the currently active
  application active", `webview_window_options.go`). These are macOS
  options; § *Platform capabilities* says what other platforms get. Go
  services bind to JS, and Go-to-JS events carry the frames. Default builds
  use cgo; v3.1.0 adds an experimental purego backend ("CGo remains the
  default").
- **Wails v2.** Fails the rule too, and it cannot draw one window per
  display or a tray icon. Its maintainers say it "continues to receive
  critical updates", so it is a maintenance branch.
- **Fyne.** Pure Go and well adopted. Its maintainers resisted always-on-top
  for years (fyne#3429, closed as a duplicate of #1129), and the 2.8 API is
  a request the WM may ignore. No Spaces control means the overlay would
  appear on only one Space.
- **Gio.** Immediate-mode and fast, but no tray and no hotkeys. Every
  desktop feature would be hand-written per OS.
- **Tauri 2 + Go sidecar.** Strong on every feature and well adopted, and
  zebar proves the WM-widget use case. It adds a Rust toolchain, a second
  language in the shell and a sidecar process to supervise. tess is Go, and
  a second language must buy a capability Go lacks; Wails v3 covers these.
- **Native per OS.** The best overlay, three times over: Swift, Win32 and
  GTK UIs for one feature. Rejected on cost.

### Platform capabilities

A portable window API does not make overlay behaviour portable. P1 ships
macOS only. The Linux columns are what the per-OS overlay promises.

| Capability | macOS (yabai) | Linux X11 | Linux Wayland |
| --- | --- | --- | --- |
| Grid window, click to switch, editor | yes | yes | yes |
| Window per display, placed on it | `Screens`, window position | yes | no: the compositor places windows |
| Visible on every WM workspace | `CanJoinAllSpaces` | WM-dependent (`_NET_WM_STATE_STICKY`) | layer-shell only, not in Wails |
| Above fullscreen apps | `FullScreenAuxiliary` | WM-dependent | layer-shell only |
| Passive HUD that takes no focus | non-activating `NSPanel` | WM-dependent | no |
| Fallback when missing | — | normal grid window | normal grid window |

### Module boundary

The app is a nested Go module at `app/`, module
`github.com/mattwilkinsonn/tessera/app`, with
`replace github.com/mattwilkinsonn/tessera => ../`. The `tess` module keeps
`CGO_ENABLED=0` and gains no dependency. Because the app's import path is
under the tessera root, Go's `internal` rule lets it import
`internal/ipc` and `internal/config`. gopls uses the same layout: its
`go.mod` has `replace golang.org/x/tools => ..`.

The app imports only `internal/ipc` (the wire types), `internal/config`
and `internal/config/edit` (P2). Never `wm`, a driver, `commands` or
`daemon`.

The frontend is plain HTML, CSS and ES modules, with the Wails runtime as
a pre-built `runtime.js` ("Some projects will not use a Javascript bundler
and may prefer to use a pre-built bundled version of the runtime",
[frontend runtime](https://v3.wails.io/reference/frontend-runtime)). There
is no npm, so nix needs no node dependency hashes. P2 can add a bundler if
the editor needs one.

### App shape

- **Menu-bar app.** Accessory activation policy, so no Dock icon. The tray
  menu has Show grid, Edit layout (P2), Reconnect and Quit.
- **One grid window per display.** On macOS each is an `NSPanel`
  (`WindowClass: MacWindowClassPanel`), frameless, at
  `MacWindowLevelPopUpMenu` with `CanJoinAllSpaces | FullScreenAuxiliary |
  Stationary`. A cell shows the Space label, up to 8 app icons and a `+N`
  overflow count, with the focused Space outlined. Transparency is off by
  default, because it needs `private_mac_apis`.
- **Two modes, one window.** Placement and focus are separate settings, as
  zebar keeps `focused` apart from `zOrder`:
  - **Flash** (from `ui flash`): the panel is non-activating
    (`PanelPreferences.NonActivating: true`). Wails shows such a panel with
    `orderFrontRegardless`, not `makeKeyAndOrderFront`
    (`windowShow` in `webview_window_darwin.go`), so it never takes the key
    window. Only the target display's panel shows, for 0.6 s. Typing goes on
    to the app on the new Space.
  - **Picker** (from `ui toggle` or a tray click): all panels show; only
    the panel on the display of the `ui` frame's Space takes key status, by
    calling `Focus()` after show. Arrows move the selection, crossing
    displays the same way `GridNeighbor` does; Return hides every panel,
    then runs `tess space-focus <id>`; Escape or a second toggle hides every
    panel and leaves focus where it was. A click on a cell does the same as
    Return.
  - A flash while the picker is open is ignored.
- **Display mapping.** A `state` frame's `Display` carries `deviceId`
  (yabai's display UUID, from `NormalizeDisplay`) and its frame in points.
  Wails `Screen.ID` on macOS is the `CGDirectDisplayID`
  (`processScreen` in `screen_darwin.go`), a different identifier. The app
  maps each display by `Bounds` equal to the tess frame, both in points with
  a top-left origin [INFERENCE: A4 checks yabai and Wails share the
  origin]. Two displays with the same bounds cannot exist, since frames do
  not overlap. A display with no matching screen gets no panel and a tray
  warning.
- **Hotplug.** The app reconciles its panels on every `state` frame and on
  Wails `ApplicationDidChangeScreenParameters`: it creates a panel for a
  new display, moves and resizes one whose bounds or scale changed, and
  closes one whose display left. Panels are keyed by `deviceId`, so a
  reorder moves panels instead of recreating them.
- **Icons.** `state` frames carry each window's `pid`. The app resolves
  an icon per pid once, through `NSRunningApplication` on macOS
  (`processIdentifier`, then `icon`), cached by app name. A miss draws a
  letter tile.
- **Disconnected or stale.** When the socket closes, the app greys the
  grid, shows "tess daemon not running", and reconnects with 0.25–5 s
  backoff. A `status` frame with `fresh: false` or `busy: true` dims the
  grid and disables clicks. The app never shows a cached grid as live.

### Profile editing (P2)

`internal/config/edit` edits the CUE source and keeps comments.

1. **One AST.** Parse with `parser.ParseFile(path, src,
   parser.ParseComments)`, then evaluate that same `*ast.File` with
   `cue.Context.BuildFile(f, cue.Scope(schema))`. Edits and evaluation
   share node identity; there is no second compile of the bytes.
2. **Ownership is resolved in the AST, not from `Value.Source`.** CUE's
   `Value.Source` "returns nil for computed nodes" and does not name a
   unique owner or list other uses (`cue/types.go`). The editor walks the
   AST with `astutil.Resolve` and follows each editable path —
   `desk[i].tracks`, `topologies[t].desk[i].tracks`, `deskSlots`,
   `laptopPinned` — through these forms only:
   - a list literal: editable in place;
   - an identifier or selector that resolves to a definition or field
     whose value is one of these forms, followed through any chain:
     editable at the final literal;
   - anything else (unification `&`, disjunction, comprehension, a call,
     a list built by `+`): read-only, with a "defined at file:line" hint.
3. **Reverse uses.** Before an edit, the editor finds every editable path
   that resolves to the same literal node, by walking all paths above. That
   set is `EditableList.Shared`, and the UI lists it. An edit to
   `#laptopStack` names the root desk and both laptop topologies.
4. **Edits move existing nodes:** reorder, insert and delete list elements.
   A moved element keeps its attached comments, such as the
   `// 1 — the G9 centre wave` line on each `deskSlots` entry. Only new
   elements are built fresh. The editor never regenerates the file from
   the decoded struct.
5. **Validation is separate from ownership.** Format with `format.Node`,
   then validate the bytes with `config.Parse` (schema plus `Validate`).
   A failure is shown and nothing is written.
6. Write atomically next to the symlink's resolved target (temp file plus
   rename), and refuse if the file's sha256 changed since it was read.

The editor shows which arrangement is on screen now (from the state
frame's `arrangement` field, B2) and labels an edit to any other one "saved;
not on screen now". The app then runs `tess wake`. The daemon re-reads the
profile at the start of each wake and swaps it into an
`atomic.Pointer[config.Profile]` that display, flex, arrival work and the
state publisher read. A profile that fails to load keeps the old one and
sends an `error` frame.

### Packaging

The app follows `release-binaries.md` (PR #69): bytes shipped are bytes
tested, no release job compiles, and signing stays ad-hoc.

- **Build once, in the PR.** The `nix` CI job also runs
  `nix build .#app` and packs `Tessera.app` with `ditto -c -k
  --keepParent` into `Tessera-darwin-arm64.zip`. It uploads the zip as
  `tessera-app-darwin-arm64-<tree>`, with the same tree key, retention and
  same-repo rule as the CLI artifact. Main reuses it on a tree hit, exactly
  like the CLI.
- **Prove it runs outside nix.** A second CI job on `macos-latest`, with no
  Install Nix step, downloads the zip, unpacks it, runs
  `codesign --verify --deep --strict`, checks `otool -L` lists no
  `/nix/store` path, and runs `Tessera.app/Contents/MacOS/tessera-app
  --self-test`, which loads the frontend headless and exits 0.
- **Release.** The release job uploads `Tessera-darwin-arm64.zip` next to
  `tess-darwin-arm64.tar.gz`. The CLI tarball contract does not change.
  `nix/prebuilt.json` gains an `app` object (`url`, `hash`), written by
  `releasepin pin -app-zip PATH`.
- **Nix outputs.** `packages.aarch64-darwin.app` is the source build:
  `buildGoModule` on `app/` with `CGO_ENABLED=1`, `-tags production`, the
  darwin SDK, `postInstall` assembling `Applications/Tessera.app` with
  `LSUIElement` in its Info.plist, `codesign --force -s -`, and
  `dontStrip`. `packages.aarch64-darwin.app-prebuilt` fetches the pinned
  zip, as `prebuilt` does for the CLI. The home-manager option
  `programs.tessera.app.enable` defaults to `app-prebuilt` when the pin has
  an `app` entry, so enabling the app does not bring back local compiles.
  The option also installs `launchd.agents.tessera-app` with `RunAtLoad`,
  `KeepAlive`, and `TESS_PATH` set to `${programs.tessera.package}/bin/tess`.
- **Homebrew.** A separate cask, `Casks/tessera-app.rb`, installs
  `app "Tessera.app"` from the zip, with `depends_on cask: "tessera"` and
  the same quarantine `postflight` as the CLI cask. The `tessera` cask is
  unchanged.
- **CLI and daemon pairing.** The app does not bundle `tess`; it runs the
  one the daemon runs (`TESS_PATH`). The `hello` frame's protocol version
  gates the app, so an app newer than the daemon says so instead of
  failing.

### Alternatives considered

- **App queries yabai directly.** It would duplicate the driver in a cgo
  binary, race tess's own cascades, and need a second implementation per
  platform. Rejected for the daemon stream.
- **A second socket for subscribers.** It would separate concerns, but it
  adds a second path, lock and lifecycle. The first line already tells the
  two connection kinds apart.
- **Hotkeys registered by the app.** Wails v3 can do it, but keybinds would
  then live in two places, and navigation would fail whenever the app is
  not running.
- **Regenerate the profile from the decoded struct.** It loses every
  comment, every definition and the profile's structure. Rejected for AST
  edits.

## Global Constraints

- **Toolchain.** Go 1.27.3, as `go.mod` pins. Wails pinned to exactly
  `github.com/wailsapp/wails/v3 v3.1.0` in `app/go.mod`; bumps are explicit.
  `cuelang.org/go v0.17.1`, already a dependency.
- **The `tess` module stays cgo-free** and gains no new module. Only
  `app/go.mod` lists Wails.
- **Import rules**, added to `go-port/design.md` § Module layout:
  `internal/ipc` imports only `wm`, `engine` and `config`.
  `internal/config/edit` imports only `config` and CUE. `app/...` imports
  only `internal/ipc`, `internal/config` and `internal/config/edit`.
- **Wire protocol.** NDJSON over the existing daemon socket. Every frame
  carries `"v": 1`. A client sends `subscribe 1`. The daemon closes a
  subscriber whose version it does not support. Field names are
  lowerCamelCase.
- **State is always complete.** No diff frames. Every connection gets a
  full frame first. The app never shows a frame older than its connection,
  a stale frame, or a busy frame as live.
- **Commands.** The app runs `tess` subcommands. It never mutates the WM
  any other way.
- **Platform.** P1 ships `aarch64-darwin` only. Overlay behaviour is per
  platform (§ *Platform capabilities*). Platform code sits behind
  `//go:build darwin` files with a letter-tile icon and a normal-window
  fallback elsewhere.
- **No TCC prompts.** The app needs no Accessibility, Screen Recording or
  Input Monitoring permission.
- **House rules** (`go-port/design.md`): errors wrapped, never discarded;
  `os/exec` only at a `//nolint:depguard // <reason>` site; tests use
  `gotest.tools/v3` and `synctest`, never `time.Sleep`. Code comments are
  1–2 lines and carry no issue IDs.
- **Public repo.** Name Matt's private config only as the downstream
  consumer. No private paths or hosts.
- **Cost.** No usage-billed resource is added.

## Plan

Each task is one commit with its tests. A1–A3 are pure tess changes and
ship value with no app. A4 needs A3. A5 needs A4 and PR #69. P2 (B1–B3)
needs P1.

### A1 — wm: focus and visibility fields, `FocusSpace`

Interfaces:

- `wm.Space` gains `Focused bool` and `Visible bool`. `wm.Window` gains
  `PID int`.
- `wm.FocusResult` is a string type with `FocusChanged`, `FocusAlready`,
  `FocusMissing` and `FocusRejected` (§ *Grid navigation*).
- `wm.FocusOps` gains `FocusSpace(ctx context.Context, id SpaceID)
  (FocusResult, error)`.
- yabai: `RawYabaiSpace` reads `has-focus` and `is-visible`;
  `RawYabaiWindow` reads `pid`. `FocusSpaceArgs(index int) []string`
  returns `{"-m", "space", "--focus", strconv.Itoa(index)}`.
  `(*Driver).FocusSpace` checks `QueryFocusedSpace` first, resolves the
  index with `resolveIndex`, focuses, checks again, and re-resolves once on
  a mismatch.
- `fake.Driver.FocusSpace` sets the focused Space and its display's
  visible Space, and can be seeded to reject or to reorder Spaces between
  resolve and focus.

Gate: argv corpus golden updated; normalize tests over
`spaces.live.json` read the new fields; fake tests cover each
`FocusResult`, including a reorder between resolve and focus and an
already-focused target that makes no WM call.

### A2 — engine grid and `tess space-focus`

Interfaces:

- `config.Profile` gains `Grid *GridSpec \`json:"grid,omitempty"\``, with
  `type GridSpec struct { Columns int \`json:"columns,omitempty"\` }`.
  `schema.cue` adds `grid?: close({columns?: int & >=1 & <=12})` to
  `#Profile`.
- `engine.Cell struct { Space wm.SpaceID; DisplayIdx, Row, Col int }`.
- `engine.Grid struct { Cells []Cell }` with
  `func (g Grid) At(id wm.SpaceID) (Cell, bool)`.
- `func SpaceGrid(profile *config.Profile, world wm.World) Grid`: per
  display in `Display.SpaceIDs` order; columns are `profile.Grid.Columns`
  or the Space count when unset.
- `func GridNeighbor(world wm.World, grid Grid, from wm.SpaceID, dir
  wm.Direction) (wm.SpaceID, bool)`: within a display by row and column;
  at an edge, to the display adjacent by frame centre in that direction,
  landing on its visible Space; `false` at the outer edge.
- `commands.SpaceFocusCommand{ Target string }`; `FromCLI("space-focus",
  …)`; the CLI kind `space-focus` with choices `north|south|east|west` or a
  Space ID. It snapshots, computes the grid, calls `FocusSpace`, maps the
  result to the exit codes in § *Grid navigation*, and on `FocusChanged`
  or `FocusAlready` sends `ui flash <space-id>` to the daemon, ignoring a missing daemon.

Gate: table tests over the three-display fixture
(`profile.three-displays.cue`, `displays.three-displays.json`) cover every
direction, wrap, display crossing and outer edge; CLI tests cover each
exit code; CLI corpus golden updated.

### A3 — `internal/ipc` and the daemon state stream

Interfaces:

- `internal/ipc`:

  ```go
  type Frame struct {
      V       int     `json:"v"`
      Kind    string  `json:"kind"` // "hello" | "state" | "status" | "ui" | "error"
      Seq     uint64  `json:"seq,omitempty"`
      Hello   *Hello  `json:"hello,omitempty"`
      State   *State  `json:"state,omitempty"`
      Status  *Status `json:"status,omitempty"`
      UI      *UI     `json:"ui,omitempty"`
      Message string  `json:"message,omitempty"`
  }
  type Hello struct {
      Protocol int    `json:"protocol"`
      Version  string `json:"version"`
  }
  type Status struct {
      Fresh bool   `json:"fresh"`
      Busy  bool   `json:"busy"`
      Error string `json:"error,omitempty"`
  }
  type UI struct {
      Action string     `json:"action"` // "toggle" | "flash"
      Space  wm.SpaceID `json:"space"`
  }
  type State struct {
      Displays    []Display  `json:"displays"`
      Focused     wm.SpaceID `json:"focused"`
      Arrangement string     `json:"arrangement"` // topology name, or "" for the root desk
  }
  type Display struct {
      Idx      int      `json:"idx"`
      DeviceID string   `json:"deviceId"`
      Name     string   `json:"name,omitempty"` // logical g9/aw/laptop
      Frame    wm.Frame `json:"frame"`          // points, top-left origin
      Spaces   []Space  `json:"spaces"`
  }
  type Space struct {
      ID      wm.SpaceID `json:"id"`
      Label   string     `json:"label"`
      Row     int        `json:"row"`
      Col     int        `json:"col"`
      Visible bool       `json:"visible"`
      Windows []Window   `json:"windows"`
  }
  type Window struct {
      ID        int    `json:"id"`
      App       string `json:"app"`
      Title     string `json:"title"`
      PID       int    `json:"pid"`
      Floating  bool   `json:"floating"`
      Minimized bool   `json:"minimized"`
  }
  func BuildState(profile *config.Profile, world wm.World) State
  func Subscribe(ctx context.Context, socket string) (<-chan Frame, error)
  ```

  `ipc` imports `engine` for `SpaceGrid` and `ResolveDesk`, so
  `BuildState`, the `arrangement` name and `space-focus` use the same
  logic. `engine` gains `ResolveArrangement(profile, displays) string`,
  which returns the matched topology name that `ResolveDesk` uses.
- `daemon.StatePublisher` with `Request()`, `Busy(pid int, on bool)`,
  `Subscribe(conn net.Conn)` and `Run(ctx)`: dirty flag, 75 ms quiet,
  250 ms maximum delay, generation check, per-subscriber hash, state slot
  plus 16-frame control queue, 1 s write deadline, at most 8 subscribers.
- `handle` routes `subscribe 1` to the publisher; `state` to `Request`;
  `busy on|off <pid>` to `Busy`; `ui toggle|flash <space-id>` to a control
  broadcast. `Hub.Notify` also calls `Request`.
- The composition root's `Nudger` sends `tess_busy` to sketchybar and to
  the daemon socket, so standalone `tess apply`, `tess laptop`, `tess snap`
  and in-daemon cascades all report busy through `PreserveFocus`.
- `RegisterSignals` adds the `tessera-state-<event>` signals in
  § *Freshness*.
- CLI kind `grid` sends `ui toggle <focused-space-id>`.

Gate: `synctest` tests cover: an idle first subscriber gets `hello` and a
full `state`; a reconnect with an unchanged world gets a full `state`;
sustained requests every 50 ms still publish within 250 ms; a snapshot
failure sends `status fresh=false` and keeps the connection; a busy-on
during a snapshot discards it; a standalone busy pid that never sends off
expires after `CascadeTimeout`; `ui` frames interleaved with `state`
frames are all delivered in order; a slow subscriber is dropped; non-
subscribe lines still close at once. A fake-driver test fires a
`window_title_changed` for a non-focused window and a `display_resized`,
with no focus event after, and sees both in the next frame. `net.Pipe`
round-trips every frame kind.

### A4 — the app (`app/`)

Interfaces:

- `app/main.go`: Wails v3 `application.New` with accessory policy and a
  tray menu.
- `app/panels.go`: `Reconcile(state ipc.State, screens []*application.Screen)`
  creates, moves and closes panels keyed by `deviceId`, mapped to screens
  by bounds; it runs on every `state` frame and on
  `ApplicationDidChangeScreenParameters`.
- `app/modes.go`: `Flash(space string)`, `TogglePicker(space string)`,
  `Select(dir string)`, `Confirm()`, `Cancel()`, with the focus rules in
  § *App shape*. The app uses the `ipc` types and never imports `wm`.
- `app/state.go`: a Go service that runs `ipc.Subscribe` and emits each
  frame as the Wails event `tess:frame`.
- `app/actions.go`: service methods `FocusSpace(id string) error` and
  `Wake() error`, which run `tess` from `TESS_PATH`, then `PATH`, at one
  `//nolint:depguard // run the tess CLI` site.
- `app/icons_darwin.go`: `IconPNG(pid int) ([]byte, error)` through
  `NSRunningApplication`, cached by app name.
- `app/frontend/`: `index.html`, `grid.js`, `style.css`, `runtime.js`.
- `--self-test` flag: load the frontend without showing a window and exit 0.

Gate: Go tests cover reconnect backoff, tess lookup, `Reconcile` (add,
remove, reorder, scale change, two identical monitors at different
bounds) and the mode transitions. Manual checks on the MacBook, recorded
in the PR:

- create, destroy and move Spaces with `tess laptop`; each change shows
  after the busy dim clears, with no dead cell;
- after a `space-focus` hotkey flash, typing reaches the app on the new
  Space;
- the picker takes keys only on one display; Escape restores the previous
  focus; Return switches;
- the grid appears on every Space and over a fullscreen app;
- undock and redock, reorder monitors in System Settings, and change a
  display's scale: panels follow;
- yabai and Wails display origins agree;
- the click-to-switch latency, measured end to end.

### A5 — packaging

Interfaces: § *Packaging*. In `ci.yml`, the `nix` job builds and uploads
`tessera-app-darwin-arm64-<tree>`; a new `app-smoke` job without nix runs
the checks. `releasepin pin` gains `-app-zip PATH`; `nix/prebuilt.json`
gains `app.url` and `app.hash`. `flake.nix` adds `packages.app` and
`packages.app-prebuilt`; the home-manager module adds
`programs.tessera.app.enable` and the agent. `releasepin cask` renders
`Casks/tessera-app.rb` too.

Gate: on a PR, both jobs pass and `otool -L` shows no `/nix/store` path;
after merge, main reuses the PR's app artifact; the first stable release
has both assets and both casks; `brew install --cask
mattwilkinsonn/tap/tessera-app` launches the app on a Mac without nix; a
`--dry-run` of Matt's darwin config with the app enabled builds nothing
from source.

### B1 — `internal/config/edit`

Interfaces:

```go
type Document struct { /* path, source bytes, sha256, *ast.File, cue.Value built from that file */ }
func Open(path string) (*Document, error) // ParseFile + BuildFile on one AST
func (d *Document) Arrangements() []Arrangement
type Arrangement struct {
    Name   string // topology name, or "" for the root desk
    Prefix string // "desk" or "topologies[2].desk"
}
func (d *Document) Lists(arrangement string) []EditableList // its desk tracks, deskSlots, laptopPinned
type EditableList struct {
    Path     string   // "topologies[0].desk[0].tracks[1]", "deskSlots", "laptopPinned"
    Items    []string // rendered elements
    Owner    string   // where the literal lives: "#laptopStack.tracks[0]"
    Shared   []string // every other editable path resolving to the same literal
    ReadOnly bool     // unsupported source form
    DefinedAt string  // file:line of the owning node
}
func (d *Document) Move(path string, from, to int) error
func (d *Document) Insert(path string, at int, cueExpr string) error
func (d *Document) Delete(path string, at int) error
func (d *Document) Render() ([]byte, error) // format.Node, then config.Parse
func (d *Document) Save() error             // atomic, refuses on hash change
```

Gate: golden tests on generic fixtures (no private names or titles) with a
list literal, a shared definition used by the root desk and two
topologies, a two-step reference chain, a unification, and a computed
list. Each supported form is editable at its owner with every comment kept
after a move; unification and computed lists are read-only with a
`DefinedAt`; `Shared` lists every use; an invalid edit writes nothing; a
concurrent change is refused. `internal/config/testdata/full.cue` round-
trips byte-for-byte with no edit.

### B2 — daemon profile reload

Interfaces: `daemon.Options` gains `Profile *atomic.Pointer[config.Profile]`
and `LoadProfile func(context.Context) (*config.Profile, error)`.
`WakeRoutine` reloads first; `DisplayWork`, `FlexWork`, `WindowCreated`
and `StatePublisher` read the pointer once per work item or snapshot. A
failed load keeps the old profile and sends an `error` frame.

Gate: tests show a wake picks up a changed profile, a broken profile
leaves the old one in place, and an edit to a matched topology's desk
changes the layout that the next wake applies (fake driver).

### B3 — editor UI

Interfaces: an `Edit layout` window with an arrangement selector
covering every arrangement, defaulting to `State.Arrangement`; one column per display
listing that arrangement's desk tracks, plus lists for `deskSlots` and
`laptopPinned`; drag to reorder; a window-name picker from
`profile.windows`; an "also changes: …" note from `Shared`; read-only
lists with their `DefinedAt`. Save calls `Document.Save`, then `Wake`.

Gate: on the MacBook, an edit to the active topology changes the layout
after save; an edit to an inactive arrangement shows "saved; not on screen
now"; `jj diff` of the profile shows only the edited lines.

## Not taken

Every critic finding was applied. Matt ruled on the two open forks, overlay scope and editor arrangement (§ *Decisions*). One part of F3 is accepted as a known race instead of
fixed: a hotkey `space-focus` during a rebuild can be undone by the
rebuild's focus restore (§ *Snapshot consistency*). Blocking the hotkey on
busy would make a hotkey wait up to `CascadeTimeout`.

## Tasks

- [ ] A1 — wm: `Space.Focused`/`Visible`, `Window.PID`, `FocusSpace` with `FocusResult` (yabai, fake)
- [ ] A2 — `engine.SpaceGrid`/`GridNeighbor`, `grid.columns`, `tess space-focus` with exit codes
- [ ] A3 — `internal/ipc`, state publisher (initial frame, bounded delay, status, split queues, busy generations), freshness signals, `tess grid`
- [ ] A4 — Wails v3 app: tray, per-display panels with hotplug, flash and picker modes, click and keyboard switch, icons
- [ ] A5 — tested app artifact, no-nix smoke job, release zip, pin, `app` and `app-prebuilt` outputs, `tessera-app` cask
- [ ] B1 — `internal/config/edit`: one AST, AST ownership and reverse uses, comment-preserving edits, validation, atomic save
- [ ] B2 — daemon reloads the profile on wake
- [ ] B3 — layout editor UI with arrangement selector over every arrangement
- [ ] Downstream (Matt's config) — skhd binds for `space-focus` and `grid`, the app agent, the `mkOutOfStoreSymlink` profile link, and removing GridLion
