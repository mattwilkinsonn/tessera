# Design: the tess app — a live space grid and layout editor

*Design record for a desktop app that replaces GridLion. It builds on
[`resident-daemon.md`](resident-daemon.md), [`go-port/design.md`](go-port/design.md),
[`windows-backend/design.md`](windows-backend/design.md) and the in-flight
`release-binaries.md` (tessera PR #69). It edits no frozen record.*

## Open Questions

Each question stands alone. Q3 depends on Q1. The others are independent.

1. **UI framework, and the adoption rule.** The app needs one overlay window
   per display that shows on every Space, a menu-bar or tray icon, and a
   path to Windows and Linux. § *Framework* has the evidence.
   - (a) **Wails v3.1.0.** It has every feature the overlay needs, including
     `CollectionBehavior: CanJoinAllSpaces` and `WindowLevel` on macOS. It
     fails the adoption bar: 189 pkg.go.dev importers and 642 GitHub
     dependents. v3.0.0 shipped on 2026-10-26, after nearly four years of
     alpha and beta.
   - (b) **Wails v2.17.1.** It meets the bar (6,123 GitHub dependents) but
     has one window per app, no tray, no global shortcuts and no macOS
     collection behaviour. A per-display overlay is not possible.
   - (c) **Fyne v2.8.2.** It meets the bar (3,707 importers) and is pure Go
     widgets. It has a tray and `RequestAlwaysOnTop`, but no Spaces
     behaviour, no transparency and no global shortcuts.

   **Recommendation: (a), with a recorded waiver of the importer count.**
   The same maintainers have shipped Wails since 2018 (v2 is the adopted
   evidence), v3 has a weekly release cadence, and it is the only option
   whose window API covers the overlay. The app is a separate module
   (§ *Module boundary*), so a later framework swap touches only `app/`.

2. **Grid shape for hotkey navigation.** GridLion lays Spaces out as a 2D
   grid. tess must pick one shape for both the app and `tess space-focus`.
   - (a) **One row per display.** East and west move along a display's
     Spaces. North and south move to the display above or below, by frame
     position. Simple, but the laptop can hold 10 or more `lap-*` Spaces,
     so a row gets long.
   - (b) **A per-display grid that wraps at `grid.columns`.** East and west
     move within a row. North and south move between rows. At an edge, the
     move crosses to the physically adjacent display and lands on its
     visible Space. Without `grid.columns`, a display uses one row, so (b)
     degrades to (a).
   - (c) **A grid declared cell by cell in the profile.** Full control, but
     every new flex Space needs a profile edit, which defeats flex.

   **Recommendation: (b).** It keeps desk displays as one short row and
   wraps the long laptop row, with one optional profile field.

3. **Where the editor writes** (depends on Q1 only for the UI). Matt's
   `~/.config/tessera/profile.cue` is a home-manager symlink into the
   read-only nix store, so the app cannot write it.
   - (a) **Make the deployed file a live link to the repo copy.** Matt's
     home-manager config uses `mkOutOfStoreSymlink` to point the profile at
     the working-copy file. The app edits that file in place, tess reloads
     it at once, and Matt commits the diff. One downstream line.
   - (b) **The app writes to a separate source path** set in the app's
     settings. tess still reads the store copy, so an edit takes effect
     only after a commit and `nix-switch`.
   - (c) **The app exports a patch** to the clipboard. No write path at all,
     and no live preview.

   **Recommendation: (a).** It is the only option where "edit, see the
   layout change, commit" works without a rebuild.

4. **Show the grid briefly on every hotkey move.** GridLion flashes the
   grid when you switch. (a) Yes: `tess space-focus` asks the app to show a
   0.6 s HUD on the focused display. (b) No: the grid shows only on its own
   hotkey or tray click. **Recommendation: (a).** It shows where you landed
   in the grid, which is the point of grid navigation. It is one line in
   the command and a setting in the app.

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
  binds, and the profile link from Q3. These are downstream only.
- **Prior art.**
  - [GridLion](https://gridlion.hopefullyuseful.com/): a menu-bar app with
    a hotkey grid over Spaces. Borrow: the 2D grid, arrow-key moves and the
    overlay. Avoid: a second, scraped source of Space state.
  - [glzr-io/zebar](https://github.com/glzr-io/zebar): "a tool for creating
    customizable and cross-platform taskbars, desktop widgets, and popups"
    for GlazeWM and komorebi. It is a Tauri 2 app
    (`packages/desktop/Cargo.toml`: `tauri = { version = "2" …`). Borrow: a
    web UI that the WM feeds, not a UI that queries the WM.
  - sketchybar in Matt's setup refreshes on `yabai_spaces_changed`
    triggers (`RegisterSignals` in `internal/daemon/wiring.go`). Borrow:
    refresh on WM events, never on a timer.
- **Lifetime.** Long. The app outlives the yabai backend and must move to
  komorebi on Windows and a Linux WM later. Effort goes into the daemon
  protocol and the pure grid logic, which every platform shares. The UI
  stays thin.
- **Simpler option.** `tess space-focus north|south|east|west` bound in
  skhd gives grid navigation with no app at all. It is task A2, so it ships
  first and stands alone. The app adds the visual grid and the editor.

## Problem / Intent

Show a live grid of Spaces per display, with each Space's windows as app
icons, and switch Space on click or hotkey. Later, edit desk tracks,
`laptopPinned` and `deskSlots` in the app and write them back to the CUE
profile. The grid must never go stale, and the UI must run on macOS,
Windows and Linux.

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
  until either side closes. Every other line keeps today's
  close-at-once behaviour, so `nc` notifiers never linger.
- **Full snapshots, not diffs.** Each `state` frame carries the whole
  world: displays, their Spaces in order, each Space's windows, the focused
  Space, grid positions and a busy flag. The app replaces its model on every
  frame. A missed frame cannot leave a dead cell, because the next frame
  is complete. Matt's world is about 20 Spaces and 100 windows, a few KB.
- **Refresh on events.** `RegisterSignals` adds labelled yabai signals for
  `space_created`, `space_destroyed`, `space_changed`, `display_changed`,
  `window_focused`, `window_moved`, `window_minimized`,
  `window_deminimized` and `window_destroyed`. All of these names exist in
  yabai's `signal_type_str` (`src/event_signal.h`). Each sends the line
  `state` to the socket. Labels are `tessera-state-<event>`, so they sit
  next to the placement and sketchybar signals for the same event. On
  komorebi, the driver's `EventStream` already sees every state change; the
  daemon maps events it does not place on to the same refresh.
- **Coalesce, then snapshot.** A `StatePublisher` waits 75 ms after the
  last refresh request, runs `wm.Snapshot` once, builds `ipc.State`, and
  sends it if it differs from the last frame. It also refreshes when any
  `Hub` work or wake ends, because tess's own cascades change Spaces. While
  `TopologyState.SerializeCascade` runs, frames carry `busy: true`, so the
  app dims instead of showing half-built layouts as final.
- **Slow subscribers cannot stall the daemon.** Each subscriber has a
  one-slot, latest-wins buffer and a 1 s write deadline. A subscriber that
  misses its deadline is dropped. The daemon caps subscribers at 8.
- **No subscribers, no snapshots.** The publisher does nothing while no
  client is connected, so the daemon's yabai load does not change for users
  without the app.

Polling was rejected: the app would need its own driver, it would lag
by the poll interval, and it would add constant yabai queries.

### Commands go through the CLI

The app runs `tess space-focus <space-id>` for a click, and other `tess`
subcommands later. It does not import `commands` or a driver. Every action
then has one code path, shared by skhd, whkd and the app, and the app stays
platform-neutral. A `tess` call costs a Go process start plus one yabai
call [INFERENCE: tens of ms; A4 measures it].

The app finds `tess` next to its own executable first, then on `PATH`.

### Grid navigation lives in the engine and the CLI

Grid layout is pure: `engine.SpaceGrid(profile, world) Grid` assigns each
Space a display, row and column (Q2 (b)). `engine.GridNeighbor(grid,
from, dir)` returns the target Space. The daemon puts the same grid
positions into each `state` frame, so the app draws exactly the grid the
hotkeys move through.

`tess space-focus north|south|east|west|<space-id>` resolves the target
with these functions and calls a new driver method,
`FocusSpace(ctx, wm.SpaceID) (bool, error)`. On yabai this is
`yabai -m space --focus <index>`, which needs the scripting addition that
Matt's yabairc already loads. On komorebi it is
`FocusMonitorWorkspaceNumber`. Hotkeys stay in skhd (macOS) and whkd
(Windows), as `go-port/design.md` keeps keybinds out of the daemon.

The app has no global hotkey of its own. `tess grid` sends `ui toggle` to
the daemon, which relays a `ui` frame to subscribers. All keybinds stay in
one skhd file, and the trigger costs a `tess` start, not an app start.
While the overlay has focus, arrow keys move a selection and Return
focuses that Space, through window-local key bindings.

### Framework

Evidence gathered 2026-11-08. "Importers" is the pkg.go.dev count;
"dependents" is GitHub's dependency graph.

| Option | Latest release | Adoption | Tray / menu bar | Global hotkeys | Overlay, all Spaces, transparency | Multi-monitor |
| --- | --- | --- | --- | --- | --- | --- |
| Wails v3 | v3.1.0, 2026-11-01 | `v3/pkg/application` 189 importers; 642 dependents | yes | yes; Carbon on macOS, no Accessibility | `AlwaysOnTop`, `WindowLevel`, `CollectionBehavior` (CanJoinAllSpaces); transparent webview needs `-tags private_mac_apis` | `Screens` API, many windows |
| Wails v2 | v2.17.1, 2026-11-01 | 571 importers; 6,123 dependents; used by tiny-rdm | no | no | `AlwaysOnTop` only | one window per app |
| Fyne | v2.8.2, 2026-10-07 | 3,707 importers; 1,814 dependents; used by fyne-io/terminal, rymdport | `SetSystemTrayMenu` | none built in; `golang.design/x/hotkey` has 49 importers | `RequestAlwaysOnTop` (since 2.8), "subject to support"; no Spaces control or transparency | `RequestPosition`, ignored on Wayland |
| Gio | v0.10.3, 2026-09-28 | `gioui.org/app` 909, `gioui.org/layout` 1,652 importers; 1,900 dependents; used by gotraceui | no | no | `TopMost` option only | limited |
| Tauri 2 + Go sidecar | tauri 2.12.5 (crates.io) | 793 reverse deps, 42.9 M downloads; used by zebar | yes | plugin | yes | yes |
| Native per OS | — | — | yes | yes | yes | yes |

Sources: [Wails v3.0.0 release](https://github.com/wailsapp/wails/releases/tag/v3.0.0),
[Wails v3.1.0 post](https://wails.io/blog/wails-v3-1-released/),
[global shortcuts](https://wails.io/features/keyboard/global-shortcuts/),
[window options](https://wails.io/features/windows/options/),
[private macOS APIs](https://wails.io/guides/build/private-macos-apis/),
[Fyne `desktop.Window`](https://github.com/fyne-io/fyne/blob/develop/driver/desktop/window.go),
[Gio `app`](https://pkg.go.dev/gioui.org/app), and the module proxy for
release dates.

- **Wails v3 (recommended, Q1).** The release notes list "multiple
  windows", "System tray", "global shortcuts" and "Windows … macOS … and
  Linux". The macOS window options give the exact overlay recipe:
  `WindowLevel` plus `CollectionBehavior: CanJoinAllSpaces |
  FullScreenAuxiliary`. Go services bind to JS, and Go-to-JS events carry
  the `state` frames. Default builds use cgo; v3.1.0 adds an experimental
  purego backend ("CGo remains the default").
- **Wails v2.** Adopted, but it cannot draw one window per display or a
  tray icon. Its maintainers say it "continues to receive critical
  updates", so it is a maintenance branch.
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

Linux risk, for all webview options: on Wayland an app cannot place its
own window, so per-display overlays need layer-shell support. This is
unsolved for Wails and out of scope until a Linux backend exists.

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
- **One overlay window per display**, frameless, at
  `MacWindowLevelPopUpMenu` with `CanJoinAllSpaces | FullScreenAuxiliary
  | Stationary`, hidden until a `ui` frame or tray click. Each window
  draws its display's grid. A cell shows the Space label, up to 8 app icons
  and a `+N` overflow count, with the focused Space outlined.
  Transparency is off by default, because it needs `private_mac_apis`.
- **Icons.** `state` frames carry each window's `pid`. The app resolves
  an icon per pid once, through `NSRunningApplication` on macOS
  (`processIdentifier`, then `icon`), cached by app name. A miss draws a
  letter tile. Windows uses the exe's shell icon later.
- **Disconnected state.** When the socket closes, the app greys the grid,
  shows "tess daemon not running", and reconnects with 0.25–5 s backoff.
  It never shows a cached grid as live.

### Profile editing (P2)

`internal/config/edit` edits the CUE source and keeps comments:

1. Parse with `parser.ParseFile(path, src, parser.ParseComments)`.
2. Evaluate the file as `config.Parse` does, and map each editable path
   (`desk[i].tracks`, `deskSlots`, `laptopPinned`) to its source node with
   `cue.Value.Source()`.
3. Apply edits as moves of existing AST nodes: reorder, insert and delete
   list elements. A moved element keeps its attached comments, such as the
   `// 1 — the G9 centre wave` line on each `deskSlots` entry. Only new
   elements are built fresh.
4. When a path's source is a shared definition, such as `#laptopStack` used
   by `desk` and two topologies, the edit goes to the definition. The UI
   lists every use that will change. A path whose source is not a list
   literal is read-only, with a "defined at" hint.
5. Format with `format.Node`, then validate the bytes with `config.Parse`
   (schema plus `Validate`) before writing. A failure is shown and nothing
   is written.
6. Write atomically next to the symlink's resolved target (temp file plus
   rename), and refuse if the file's sha256 changed since it was read.

The app then runs `tess wake`. The daemon re-reads the profile at the start
of each wake and swaps it into an `atomic.Pointer[config.Profile]` that
display, flex and arrival work read. A profile that fails to load keeps
the old one and sends an `error` frame.

### Packaging

- **Nix.** A new output `packages.aarch64-darwin.app` builds `app/` with
  `buildGoModule`, `CGO_ENABLED=1`, `-tags production`, and the darwin SDK.
  `postInstall` assembles `Applications/Tessera.app` (Info.plist with
  `LSUIElement`), bundles `tess` from `packages.default` into
  `Contents/MacOS/`, and ad-hoc signs the bundle with `codesign --force -s
  -`. `dontStrip` keeps the signature intact. The home-manager module gains
  `programs.tessera.app.enable`, which installs the package and a
  `launchd.agents.tessera-app` agent with `RunAtLoad` and `KeepAlive`.
- **Releases.** After PR #69 lands, the release job also packs
  `Tessera-darwin-arm64.zip` with `ditto -c -k --keepParent`, which keeps
  the bundle's structure and signature. The `tessera` cask gains
  `app "Tessera.app"`, and its quarantine `postflight` covers the app.
  Signing stays ad-hoc (release-binaries Open Question 1). The app needs no
  TCC permission, so ad-hoc signing loses nothing.
- **Windows.** Later. The W7 GoReleaser step adds `tess-app.exe` once a
  komorebi `FocusSpace` and the state stream run there.

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
- **State is always complete.** No diff frames. The app never shows a frame
  older than its connection as live.
- **Commands.** The app runs `tess` subcommands. It never mutates the WM
  any other way.
- **Platform.** P1 ships `aarch64-darwin` only. Platform code sits behind
  `//go:build darwin` files with a letter-tile fallback elsewhere.
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
ship value with no app. A4 needs A3. P2 needs P1.

### A1 — wm: focus and visibility fields, `FocusSpace`

Interfaces:

- `wm.Space` gains `Focused bool` and `Visible bool`. `wm.Window` gains
  `PID int`.
- `wm.FocusOps` gains `FocusSpace(ctx context.Context, id SpaceID) (bool,
  error)`.
- yabai: `RawYabaiSpace` reads `has-focus` and `is-visible`;
  `RawYabaiWindow` reads `pid`. `FocusSpaceArgs(index int) []string`
  returns `{"-m", "space", "--focus", strconv.Itoa(index)}`, and
  `(*Driver).FocusSpace` resolves the index with `resolveIndex`.
- `fake.Driver.FocusSpace` sets the focused Space and its display's
  visible Space.

Gate: argv corpus golden updated; normalize tests over
`spaces.live.json` read the new fields; fake tests cover focus moves.

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
  Space ID. It snapshots, computes the grid, calls `FocusSpace`, and then
  sends `ui flash` to the daemon when Q4 is (a), ignoring a missing
  daemon.

Gate: table tests over the three-display fixture
(`profile.three-displays.cue`, `displays.three-displays.json`) cover every
direction, wrap, display crossing and outer edge; CLI corpus golden updated.

### A3 — `internal/ipc` and the daemon state stream

Interfaces:

- `internal/ipc`:

  ```go
  type Frame struct {
      V       int     `json:"v"`
      Kind    string  `json:"kind"` // "state" | "ui" | "error"
      Seq     uint64  `json:"seq,omitempty"`
      State   *State  `json:"state,omitempty"`
      UI      string  `json:"ui,omitempty"` // "toggle" | "flash"
      Message string  `json:"message,omitempty"`
  }
  type State struct {
      Displays []Display `json:"displays"`
      Focused  wm.SpaceID `json:"focused"`
      Busy     bool       `json:"busy"`
  }
  type Display struct {
      Idx    int      `json:"idx"`
      Name   string   `json:"name,omitempty"` // logical g9/aw/laptop
      Frame  wm.Frame `json:"frame"`
      Spaces []Space  `json:"spaces"`
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
      ID    int    `json:"id"`
      App   string `json:"app"`
      Title string `json:"title"`
      PID   int    `json:"pid"`
      Floating  bool `json:"floating"`
      Minimized bool `json:"minimized"`
  }
  func BuildState(profile *config.Profile, world wm.World, busy bool) State
  func Subscribe(ctx context.Context, socket string) (<-chan Frame, error)
  ```

  `ipc` imports `engine` for `SpaceGrid`, so `BuildState` and the
  `space-focus` command use the same grid.
- `daemon.StatePublisher` with `Request()`, `Subscribe(conn net.Conn)` and
  `Run(ctx)`; 75 ms coalescing; latest-wins buffer; 1 s write deadline;
  at most 8 subscribers.
- `handle` routes `subscribe 1` to the publisher before closing, `state`
  to `Request`, and `ui toggle` or `ui flash` to a broadcast.
- `RegisterSignals` adds the `tessera-state-<event>` signals listed in
  § *One source of truth*. `TopologyState` exposes `Busy() bool`.
- CLI kind `grid` sends `ui toggle`.

Gate: `synctest` tests cover coalescing, slow-subscriber drop, frame after
each cascade, `busy` during `SerializeCascade`, and that non-subscribe
lines still close at once. `net.Pipe` round-trips `Frame`.

### A4 — the app (`app/`)

Interfaces:

- `app/main.go`: Wails v3 `application.New` with accessory policy, a tray
  menu, and one overlay window per screen from `app.Screen`.
- `app/state.go`: a Go service that runs `ipc.Subscribe` and emits each
  frame as the Wails event `tess:frame`.
- `app/actions.go`: service methods `FocusSpace(id string) error` and
  `Wake() error`, which run `tess` (located next to the executable, then
  on `PATH`) at one `//nolint:depguard // run the tess CLI` site.
- `app/icons_darwin.go`: `IconPNG(pid int) ([]byte, error)` through
  `NSRunningApplication`, cached by app name.
- `app/frontend/`: `index.html`, `grid.js`, `style.css`, `runtime.js`.

Gate: Go tests cover the reconnect backoff and tess lookup. A manual check
on the MacBook: create, destroy and move Spaces with `tess laptop`; every
change shows within one frame with no dead cell; clicks switch Spaces;
arrows and Return work in the overlay; the overlay appears on every Space.
The PR records the click-to-switch latency.

### A5 — packaging

Interfaces: `packages.aarch64-darwin.app`, `programs.tessera.app.enable`
and the launchd agent as in § *Packaging*. After PR #69: the
`Tessera-darwin-arm64.zip` asset and `app "Tessera.app"` in the cask.

Gate: `nix build .#app` yields a bundle that passes
`codesign --verify --deep --strict`; `nix flake check` passes; a
`--dry-run` of Matt's darwin config builds the app.

### B1 — `internal/config/edit`

Interfaces:

```go
type Document struct { /* path, source bytes, sha256, *ast.File, value */ }
func Open(path string) (*Document, error)
func (d *Document) Lists() []EditableList // desk tracks, deskSlots, laptopPinned
type EditableList struct {
    Path     string   // "desk[0].tracks[1]", "deskSlots", "laptopPinned"
    Items    []string // rendered elements
    Shared   []string // other paths that use the same source node
    ReadOnly bool
}
func (d *Document) Move(path string, from, to int) error
func (d *Document) Insert(path string, at int, cueExpr string) error
func (d *Document) Delete(path string, at int) error
func (d *Document) Render() ([]byte, error)      // format.Node, then config.Parse
func (d *Document) Save() error                  // atomic, refuses on hash change
```

Gate: golden tests on `internal/config/testdata/full.cue` and a fixture
shaped like Matt's profile (definitions, topologies, trailing comments):
every comment survives a move; an edit through `#laptopStack` changes
`desk` and both topologies; an invalid edit writes nothing; a concurrent
change is refused.

### B2 — daemon profile reload

Interfaces: `daemon.Options` gains `Profile *atomic.Pointer[config.Profile]`
and `LoadProfile func(context.Context) (*config.Profile, error)`.
`WakeRoutine` reloads first; `DisplayWork`, `FlexWork` and `WindowCreated`
read the pointer. A failed load keeps the old profile and sends an `error`
frame.

Gate: tests show a wake picks up a changed profile and a broken profile
leaves the old one in place.

### B3 — editor UI

Interfaces: an `Edit layout` window with one column per display listing
desk tracks, plus lists for `deskSlots` and `laptopPinned`; drag to
reorder; a window-name picker from `profile.windows`. Save calls
`Document.Save`, then `Wake`.

Gate: a manual edit, save and wake on the MacBook changes the layout, and
`jj diff` of the profile shows only the edited lines.

## Tasks

- [ ] A1 — wm: `Space.Focused`/`Visible`, `Window.PID`, `FocusSpace` (yabai, fake)
- [ ] A2 — `engine.SpaceGrid`/`GridNeighbor`, `grid.columns`, `tess space-focus`
- [ ] A3 — `internal/ipc`, daemon state stream, state signals, `tess grid`
- [ ] A4 — Wails v3 app: tray, per-display overlay, click and keyboard switch, icons
- [ ] A5 — nix `app` output, home-manager option and agent; release asset and cask after PR #69
- [ ] B1 — `internal/config/edit`: comment-preserving AST edits, validation, atomic save
- [ ] B2 — daemon reloads the profile on wake
- [ ] B3 — layout editor UI
- [ ] Downstream (Matt's config) — skhd binds for `space-focus` and `grid`, the app agent, the profile link per Q3, and removing GridLion
