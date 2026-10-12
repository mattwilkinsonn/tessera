# Design — A Windows backend for `tess`

*Design record for running `tess` on Windows. It builds on
[`go-port/design.md`](../go-port/design.md) and
[`architecture.md`](../architecture.md), and it edits no frozen record. The
upstream evidence, the cross-compile probe and the rejected alternatives
are in [`evidence.md`](evidence.md).*

## Open Questions

The record follows each **Recommendation**. A **load-bearing** question
changes the plan if the answer differs.

1. **Which window manager.** *Load-bearing.*
   - (a) **komorebi.** It has stacks, run-time named workspaces, ratios, a
     state query and push events. No command takes a window handle (Open
     Question 2).
   - (b) **GlazeWM** (GPL-3.0). Every command takes `--id`, but it has no
     stack and only config-declared workspaces. The laptop home would
     become a monocle column, which changes the engine.
   - (c) **A native tiler.** `architecture.md` keeps tess a WM driver.

   A stack is required (`wm.TargetStack` in `DeskPlan`, `wm.LayoutStack`
   in `LaptopConvergeStep`). The Komorebi License 2.0.0 "does not permit
   any kind of commercial use (i.e. using `komorebi` at work)". The
   per-person Individual Commercial Use License costs $10 a month or $110
   a year for Windows, or $15 a month or $180 a year with macOS
   ([lgug2z.com/software/komorebi](https://lgug2z.com/software/komorebi/)).
   tess ships no komorebi code, so its MIT licence is unaffected
   *[INFERENCE: not legal advice]*.

   **Recommendation: (a), with a commercial licence for work use.**
   **Decision needed:** confirm komorebi, and confirm the licence covers
   your use. If W4 check 5 fails, reopen this with (b).

2. **Changing a window or workspace that has no focus** (depends on 1).
   *Load-bearing.* komorebi commands act on the focused window or take a
   monitor and workspace index. None reorders workspaces, and
   `CloseWorkspace` refuses a named one. `ApplyState` can do both, but it
   is a restore path. It fails silently, drops windows managed after the
   query, and hides and restores every workspace. komorebi also drops
   messages while busy or paused (`evidence.md` § ApplyState).
   - (a) **Raw `ApplyState` for all structure.** A silent failure repeats
     until `convergeIterationCap`.
   - (b) **Focus-then-act for everything.** It still needs `ApplyState` to
     destroy and reorder.
   - (c) **Commands first.** Index commands where they exist,
     focus-then-act for window moves, and (d) only for `DestroySpace` and
     `MoveSpaceToIndex`.
   - (d) **Guarded `ApplyState` for all structure** (§ Guarded state).

   **Recommendation: (c).** Most mutations are window moves, and (c) gives
   each a checkable command. The full-desktop `ApplyState` runs only for
   the two operations with no command: in the laptop reconcile and order
   phases, and in `teardownSpaces`. A check query ends every mutation, so
   a dropped message is an error, not a loop. The cost: focus moves
   visibly, and keys typed during a daemon converge can land in the wrong
   window. If W4 check 5 fails, use (d) and reopen Open Question 1. If
   check 2 fails, `DestroySpace` uses the retire pool, and reordering
   needs its own engine record.

3. **`wm.RuleOps`** (depends on 1). *Not load-bearing: no task depends on
   it.* (a) Skip it in v1, and copy `auto:` rules into `komorebi.json` by
   hand; (b) clear and re-add all rules, which wipes static ones; (c) rules
   scoped to tess workspaces. **Recommendation: (a) now, (c) once rules
   come from the profile.**
4. **Window `App` identity.** *Not load-bearing: no Windows profile exists
   yet.* (a) The exe stem (`Code`); (b) the full exe name; (c) the window
   class. **Recommendation: (a).**
5. **Display identity.** *Not load-bearing: (b) is additive.* (a) Keep
   width matching, since the profile is per machine; (b) an optional
   `deviceId` on `DisplaySpec`. **Recommendation: (a).**
6. **Supervision.** *Not load-bearing: it changes only W7's install files.*
   (a) komorebi autostarts, and a shipped Task Scheduler XML runs the
   daemon; (b) the daemon starts komorebi; (c) a Startup-folder shortcut,
   which never restarts. **Recommendation: (a).**
7. **Race detector in Windows CI.** *Not load-bearing: a CI setting.*
   (a) `-race` with cgo and the runner's gcc; (b) no `-race`, since the
   ubuntu job races the same code. **Recommendation: (b).**
8. **Distributing `tess.exe`.** *Not load-bearing: it can be added any
   time.* (a) `go install` plus a CI artifact; (b) goreleaser and Scoop
   now. **Recommendation: (a).**

## Problem / Intent

`tess` runs only on macOS. Its driver is yabai, and its daemon needs `nc`,
`launchctl` and an `O_NOFOLLOW` signal guard. This record adds a Windows
backend with the same profile JSON, engine and commands, driving komorebi
behind `wm.Driver`.

## Approach

Most of tess is portable. A Windows build fails only in
`SuppressSignals`, and tests fail only in three packages. AF_UNIX,
`gofrs/flock` and SIGTERM at logoff work on Windows, so the daemon socket,
locks and shutdown do not change (`evidence.md` § Cross-compile, § Go on
Windows). The work is a driver, a push-event seam and small splits.

### Platform splits

- **Signal guard.** `SuppressSignals` moves unchanged to `guard_unix.go`.
  `guard_windows.go` rejects a non-regular file and writes the stamp with
  `0o600`. The Windows temp dir is per user *[INFERENCE]*, and NTFS has no
  FIFOs, so the unix link and owner checks are not needed.
- **Guard path.** `signalGuardPath()` keeps `/tmp/yabai-apply.guard` on
  unix and uses `os.TempDir()\tessera-apply.guard` on Windows. No other
  path changes.
- **Composition root.** `cmd/tess/backend_other.go` holds yabai,
  sketchybar and launchd. `backend_windows.go` holds komorebi.
- **Tests.** FIFO and SIGTERM cases move to `//go:build unix` files.
  `cli_test.go` uses a per-platform temp root and an `.exe` stub
  *[INFERENCE: `CreateProcess` needs it; W2 proves it]*.

### The komorebi driver

`internal/wm/komorebi` is a pure-Go `wm.Driver`. Like `komorebi-client`,
it writes one JSON `SocketMessage` per connection to
`%LOCALAPPDATA%\komorebi\komorebi.sock`, and never runs `komorebic`.

**Normalization** maps one `State` query to a `wm.World`:

| tess | komorebi |
| --- | --- |
| `Display.Idx` | monitor ring index + 1, so `DisplaySelector.Index` stays 1-based |
| `Display.Frame` | `Monitor.size` |
| `Display.SpaceIDs` | the monitor's workspaces in ring order; `[0]` is the home space |
| `Space.Label` | `Workspace.name` |
| `Space.Layout` | `stack` for one container of more than one window; `float` when `tile` is false; else `bsp` |
| `Window.ID` | `hwnd` (an `isize`; fits Go's `int` on amd64 and arm64) |
| `Window.App` | the exe stem (Open Question 4) |
| `Window.Floating` | the window is in `floating_windows` |
| `Window.Visible` | its workspace is its monitor's focused workspace |
| `Window.SplitType` | always `none` |
| `Window.Sticky`, `Window.Minimized` | false *[INFERENCE: minimized windows leave the rings; W4 checks this]* |

**Space IDs.** The driver owns `SpaceID` (`architecture.md` D1): a named
workspace is `name:<label>`, an unnamed one `pos:<device_id>:<index>`.
`LabelSpace` records the old ID as an alias, so a create, label, layout
sequence survives the rename. A shifted positional ID can only make the
converger skip a stray (`ConvergeState.DestroyTried`). `DestroySpaceOp`
reads its ID from the current snapshot, so it never hits the wrong space.

**Operations.** Each mutating method takes one path (Open Question 2):
*index*; *workspace focus* (`FocusMonitorWorkspaceNumber`, act,
restore); *window focus* (§ Focus, act, restore); or *guarded state*. A
*focused* method returns `ErrNotFocused` unless `winID` has focus. Every
mutating path ends with a `State` check that returns `ErrNotApplied` if
the effect is missing.

| `wm.Driver` method | komorebi | Path |
| --- | --- | --- |
| `Query*` | `State` | — |
| `CreateSpace(displayIdx)` | `EnsureWorkspaces(m, n+1)`, then `WorkspaceName(m, n, "tess-new-<n>")` | index |
| `DestroySpace` | remove the workspace | guarded state |
| `LabelSpace` | `WorkspaceName(m, w, label)`, plus an alias | index |
| `SetSpaceLayout` | `bsp`: `WorkspaceLayout(m, w, BSP)` and `WorkspaceTiling(m, w, true)`. `float`: `WorkspaceTiling(m, w, false)`. `stack`: `StackAll`, then `ToggleWorkspaceWindowContainerBehaviour` when the workspace is not `Append` *[INFERENCE: W4 check 4]* | index; `stack` uses workspace focus |
| `MoveSpaceToIndex` | reorder within the monitor; a target on another monitor is an error | guarded state |
| `BalanceSpace` | `LayoutRatios([], [])`, because `null` keeps the old ratios *[INFERENCE: W4 check 3]* | workspace focus |
| `RealizeSpaceLayout` | `stack`: `StackAll`. `columns` and `rows`: `UnstackAll` and `WorkspaceLayout(m, w, Columns or Rows)`; `Promote` each track's first window in reverse track order; `StackWindow(Left or Up)` each later window of a track; then `LayoutRatios` with `w[i]/sum(w)` for the first `min(len-1, 5)` tracks, logging a warning when komorebi will clamp or drop a ratio | workspace focus, then window focus |
| `MoveWindowToSpace` | `UnstackWindow` when the window shares a container, then `SendContainerToMonitorWorkspaceNumber(m, w)` | window focus |
| `StackOnto` | `ErrUnsupported`; only drivers and their tests call it | — |
| `MoveWindowToDisplay` | `MoveContainerToMonitorNumber(idx-1)`, or `CycleMoveContainerToMonitor` | focused |
| `SetSplitRatio(win, 0.5)` | `LayoutRatios([], [])` on `win`'s workspace (the only call site is `ResetSplits`); any other ratio is `ErrUnsupported` | workspace focus |
| `ToggleSplit` | `ErrUnsupported`; never called, because every window reports `SplitNone` | — |
| `ToggleFloat` | `ToggleFloat` | focused |
| `ArmInsert` | `PreselectDirection(dir)` | focused |
| `ResizeWindow(edge, dx, dy)` | `ResizeWindowEdge(direction, Increase or Decrease)` by the sign of the delta | focused |
| `SwapWindows`, `WarpWindow` | `MoveWindow(dir)` | focused |
| `FocusWindow(id)` | § Focus | — |
| `FocusWindowDir` | `FocusWindow(dir)`; `CycleStack` for `stack.next` and `stack.prev`; `FocusStackWindow(0 or n-1)` for `stack.first` and `stack.last` | focused |
| `FocusDisplay` | `FocusMonitorNumber(idx-1)` or `CycleFocusMonitor` | index |
| `SpawnWindow` | `spawn.Run` (§ Shared spawn) | — |
| `SettleUnit` | 150 ms until W4 check 7 measures it | — |

**Focus.** Windows refuses the foreground to a process with no recent
input. Like komorebi's `raise_and_focus_window`, the driver sends an empty
`SendInput`, then `SetForegroundWindow`, via `NewLazySystemDLL`. It polls
`State` for one second, then returns `ErrFocusRefused`. The calls sit
behind an unexported `foregrounder`, which tests replace.

**Guarded state.** Under the driver lock, `DestroySpace` and
`MoveSpaceToIndex` query `State`, edit one workspace in an
`encoding/json/v2` value tree, send `ApplyState`, and query again. Unknown
members pass through unchanged. A missing change returns `ErrNotApplied`,
and the converge stops. The retire pool, if W4 check 2 fails, renames the
workspace to `tess-free-<n>`, which `Normalize` hides and `CreateSpace`
reuses.

**Feature probe.** komorebi reports no usable version. On first use the
driver checks that every `State` member it uses exists, and names a
missing one and the oldest tested release, v0.1.41 (2026-05-03).

### Push events

komorebi writes one notification per change to a subscriber socket, with
no newline, so the daemon's line protocol cannot read it. The driver owns
the transport behind a new optional interface:

```go
// EventStream is a WM that pushes events. The driver owns the transport, framing and reconnect.
type EventStream interface {
	// Events subscribes and returns the mapped events. The channel closes when ctx ends.
	// When the WM comes back after it stopped, the driver subscribes again and sends EventDisplayMoved.
	Events(ctx context.Context) (<-chan Event, error)
}
```

`daemon.Run` calls `Events` before `Serve` and forwards each event to
`Hub.Notify`. The driver binds `DATA_DIR\tessera.sock` and sends
`AddSubscriberSocketWithOptions("tessera.sock", {filter_state_changes:
true})`.

- **Drain at once.** komorebi writes while holding its lock, so a slow
  reader makes it drop tess's commands. The accept loop waits only on
  `Accept`, and each connection is read to EOF (16 MiB cap) in its own
  goroutine.
- **Stop.** `komorebic stop` sends an empty connection. The driver
  re-subscribes with a 1–30 s backoff, then sends `EventDisplayMoved`.
- **Crash.** A crash sends nothing, so the driver also re-subscribes every
  30 s, which komorebi treats as idempotent *[INFERENCE: a restart within
  30 s lays out nothing until the next event; W8 checks this]*.

| komorebi `event` | tess event | Hub channel |
| --- | --- | --- |
| `WindowManager(Manage)`, `WindowManager(Show)` | `window_created` | flex |
| `WindowManager(Unmanage)`, `WindowManager(Destroy)` | `window_destroyed` | flex |
| `Monitor(DisplayConnectionChange)` | `display_added` or `display_removed` by monitor count against the previous notification; `display_moved` when the count is unchanged | display |
| `Monitor(ResolutionScalingChanged)`, `Monitor(WorkAreaChanged)` | `display_moved` | display |
| anything else | none | — |

The signal guard covers events that tess itself causes.

### Shared spawn

`yabai.Driver.SpawnWindow` does not depend on the WM. It moves to
`internal/wm/spawn` as `Run`, and both drivers call it.

### Supervision

`Options.Bootstrap` is false on Windows, so `launchd.go` does not run.
The nudge is a no-op. A shipped task, `docs/windows/tess-daemon.xml`,
runs the daemon at logon, registered with
`schtasks /Create /TN "tessera daemon" /XML …` *[INFERENCE: for the
current user; W8 checks this]*. Its settings replace defaults that would
stop the daemon silently (`evidence.md` § Task Scheduler):

| Element | Value | Why |
| --- | --- | --- |
| `LogonTrigger` | the current user's logon | start only for this user |
| `Principal/LogonType` | `InteractiveToken` | run on the user's desktop, so spawned windows show |
| `MultipleInstancesPolicy` | `IgnoreNew` | one daemon; the socket lock also refuses a second |
| `DisallowStartIfOnBatteries`, `StopIfGoingOnBatteries` | `false` | the default stops a laptop's daemon |
| `ExecutionTimeLimit` | `PT0S` | the default stops it after 72 hours |
| `RestartOnFailure` | `Count` 10, `Interval` `PT1M` | both are required; one minute is the minimum |

A clean exit returns 0 and is not restarted. `tess daemon` frees its
console when it is alone there, and logs to
`os.UserCacheDir()\tessera\daemon.log` *[INFERENCE: a window can still
flash at logon; W8 checks this]*.

### Tests

A Go stub, `testdata/stubkomorebi`, serves `komorebi.sock` from synthetic
fixtures and logs each message. Goldens in `testdata/messages/` hold the
expected sequences, like the stub-yabai corpus. `TESS_KOMOREBI_DIR`
points the driver at the stub. Event tests use `net.Pipe` inside
`synctest.Test`.

## Global Constraints

- **Toolchain.** `go 1.27` (1.27.2 in CI), Windows build 10.0.17063+ for
  AF_UNIX, and komorebi v0.1.41+, enforced by the feature probe.
- **No cgo.** `CGO_ENABLED=0`; Win32 calls use `golang.org/x/sys/windows`
  or `NewLazySystemDLL`.
- **Import rules**, added to `go-port/design.md` § Module layout: only
  `cmd/tess` imports `wm/komorebi`; `wm/spawn` imports no internal
  package, and only drivers import it; `wm` still imports none.
- **Concurrency.** Driver methods are safe for concurrent use: the daemon
  runs display and flex work in parallel, and hotkeys start processes. One
  driver lock spans each mutating path from first query to last check,
  including each focus sequence through its restore. It is a `sync.Mutex`
  plus a `gofrs/flock` lock on `DATA_DIR\tessera-driver.lock`, taken with
  the call's context and a 2 s limit. Single-message calls take it too.
  It guards the alias map and the last monitor count.
- **Dependencies.** No new module; `golang.org/x/sys` becomes direct.
- **Licences.** No komorebi or GlazeWM source or schema is copied in.
- **Public repo.** No private repository names, paths or issue IDs, and no
  real titles, user paths or serials in fixtures.
- **House rules** (`go-port/design.md`): errors wrapped, never discarded;
  `os/exec` only at a `//nolint:depguard // <reason>` site; `exhaustive`
  over komorebi event switches; `gotest.tools/v3` and `synctest`, never
  `time.Sleep`.

## Plan

Each task is one commit with its tests. W1–W3 do not depend on the WM
choice. W4 and W8 are manual, on a Windows host.

### W1 — Portable build

Do § Platform splits.

Interfaces:

- `func SuppressSignals(path string) error` keeps its signature. Its
  bodies are in `internal/effects/guard_unix.go` and `guard_windows.go`.
- The unexported `func signalGuardPath() string`, in both guard files,
  feeds `DefaultPaths`.
- `effects_unix_test.go`, `yabai/args_unix_test.go` and
  `cli/cli_unix_test.go` carry `//go:build unix`.
- `cmd/tess`:

  ```go
  // backend is the per-OS composition; backend_other.go and backend_windows.go define newBackend.
  type backend struct {
  	Driver    wm.Driver
  	Nudge     effects.Nudger
  	Bootstrap bool // launchd bootstrap of the WM services; false on Windows
  }

  func newBackend() (backend, error)
  ```

  W1 adds only `backend_other.go`. `main.go` passes `b.Nudge` to
  `commands.Env.Nudge` and `daemon.Options.Nudge`, and sets
  `daemon.Options.Bootstrap = b.Bootstrap && !command.NoBootstrap`.

Gate: `GOOS=windows go build` and `go vet` pass for all but `cmd/tess`.
`go test -race ./...` passes unchanged on macOS and Linux. A Windows
guard test shows that `SuppressSignals` refuses a directory, and that
`SignalsSuppressed` reads its stamp as fresh.

### W2 — Windows CI

A `windows` job on `windows-latest` (`actions/setup-go@v6`, `1.27.2`) runs
build, vet and test without `-race`. The ubuntu job adds
`GOOS=windows go vet ./...` and `GOOS=windows golangci-lint run`. From W7
on, the Windows job uploads `tess.exe`.

Interfaces: `.github/workflows/ci.yml`. The Windows steps skip
`./cmd/tess` until W7.

Gate: both jobs are green on the W1 head.

### W3 — `internal/wm/spawn`

Interfaces: `func Run(ctx context.Context, argv []string, wait
time.Duration) error` takes today's `SpawnWindow` body and its helpers
(`spawnResult`, `discardSpawnStderr`). yabai's `SpawnWindow` becomes
`return spawn.Run(ctx, argv, d.spawnWait)`.

Gate: the moved spawn tests pass on both jobs. The stub-yabai corpus does
not change.

### W4 — komorebi spike (manual)

Run komorebi v0.1.41 or later with a throwaway program in the W5 client
shape. Record the answers in `evidence.md` § Spike results.

| # | Check | Expected |
| --- | --- | --- |
| 1 | `State` shape: `Ring<T>`, `Rect` as edges or size, physical pixels | sets `Normalize` and Open Question 5 |
| 2 | guarded `ApplyState` removes and reorders one workspace, and keeps unknown members | yes |
| 2a | a window closed between query and apply | no change; `ErrNotApplied` |
| 2b | a window opened between query and apply | record whether it is dropped, and whether a later `Show` brings it back |
| 2c | an apply while a monitor connects or disconnects | `ErrNotApplied` |
| 2d | where focus lands; how much the desktop flickers | record |
| 2e | a message while paused, or while the lock is held over 1 s | dropped; the check query catches it |
| 3 | `LayoutRatios([], [])`; `LayoutRatios(null, null)` | equal widths; old ratios kept |
| 4 | `Append` keeps new windows in the home stack | yes |
| 5 | focus from the daemon after a hotplug with no input; time until `State` shows it; `Promote` and `StackWindow(Left)`; where typed keys land | the foreground moves within 1 s |
| 6 | notification framing and size; `komorebic stop` against `taskkill /F` | one connection, no newline; an empty connection against none |
| 7 | settle time after `WorkspaceLayout` and after a focus sequence | sets `SettleUnit` |
| 8 | a GUI launcher spawned with no window flag | no console flash |

Interfaces: `evidence.md` § Spike results, and the scrubbed fixtures
`state-desk.json`, `state-laptop.json` and `notification-*.json` in
`internal/wm/komorebi/testdata/`.

Gate: every check has an observed answer. If check 5 fails, the
window-focus methods move to guarded state, and Open Question 1 reopens.
If check 2 fails, `DestroySpace` uses the retire pool, and
`MoveSpaceToIndex` waits for its own engine record.

### W5 — `internal/wm/komorebi`

Build the client, normalization, § Operations, the feature probe, the
driver lock, and the stub server and corpus.

Interfaces:

- `func New(dataDir string) *Driver`, which implements `wm.Driver`.
- `func DataDir() (string, error)` returns `TESS_KOMOREBI_DIR` if set.
  Otherwise it returns `os.UserCacheDir()` joined with `komorebi`, and
  passes that function's error through.
- The errors:

  ```go
  var (
  	ErrNotFocused   = errors.New("komorebi acts on the focused window")
  	ErrUnsupported  = errors.New("komorebi has no equivalent")
  	ErrNotApplied   = errors.New("komorebi did not apply the change")
  	ErrFocusRefused = errors.New("windows refused the foreground change")
  )
  ```

- The unexported `type foregrounder func(hwnd int) error`, in
  `foreground_windows.go` and `foreground_other.go`. Tests use
  `newDriver(dataDir, lockPath string, fg foregrounder) *Driver`.
- The pure
  `func Normalize(state jsontext.Value, aliases map[wm.SpaceID]wm.SpaceID) (wm.World, error)`.

Gate:

- Each method has a corpus golden, and each W4 fixture snapshots to the
  expected `wm.World`.
- `LaptopConvergeStep` and `DeskPlan`, through `RunConverge` and
  `RunPlan` on the stub, reach a fixed point within
  `convergeIterationCap`.
- An ignored message gives `ErrNotApplied`, and `RunConverge` stops at
  once.
- Under `-race`, concurrent mutating calls never interleave messages.
- An unknown `State` member survives a guarded edit byte for byte.
- A missing required member gives an error that names it.

### W6 — `wm.EventStream` and the daemon

Interfaces:

- `wm.EventStream` (§ Push events).
- `daemon.Options.Events wm.EventStream`. A nil value keeps today's
  behaviour.
- The unexported
  `func forwardEvents(ctx context.Context, events <-chan wm.Event, h *Hub)`.
  `Run` calls `Events` before `Serve`, runs `forwardEvents` beside it, and
  waits for it on return. An `Events` error ends `Run` at startup.
- The komorebi test seams
  `listen func(ctx context.Context) (net.Listener, error)` and
  `dial func(ctx context.Context) (net.Conn, error)`.

Gate, under `synctest.Test`:

- 100 notification writes complete while nobody reads the channel.
- `Manage` runs flex work once after `FlexQuiet`. One more monitor runs
  display work once after `DisplayQuiet`. An unmapped event runs nothing.
- An empty connection, then two refused dials and one accepted dial, give
  one subscribe message and one `EventDisplayMoved`.
- After 30 s, a second subscribe message appears.
- Cancelling ctx closes the listener and the channel, and `Run` returns.

### W7 — Windows composition and docs

Interfaces:

- `cmd/tess/backend_windows.go`: `newBackend` returns
  `komorebi.New(dataDir)`, a no-op nudge and `Bootstrap: false`.
- `main.go` sets `daemon.Options.Events` when the driver asserts to
  `wm.EventStream`.
- `cmd/tess/console_windows.go`: `func daemonLog() (*os.File, error)`
  frees the console and opens the log. `console_other.go` returns
  `os.Stderr`. `main.go` calls it before `daemon.Run`, points `slog` at
  it, and joins its `Close` error into the exit error.
- `docs/windows/tess-daemon.xml` (§ Supervision).
- A README section for Windows: komorebi v0.1.41 or later and
  `komorebic enable-autostart`; the profile path
  (`%USERPROFILE%\.config\tessera\profile.json` or `$TESSERA_PROFILE`);
  exe-stem `app` patterns and Windows `spawn` argv; registering the task;
  `tess rules` doing nothing; and the komorebi licence.

Gate: the W2 jobs pass with `./cmd/tess`. On Windows CI, `tess --version`
prints the version, and `tess wake` with no daemon exits 1 with
`tess: daemon not running`.

### W8 — Live smoke (manual)

Install `tess.exe`, register the task, and check that:

- `tess apply` on the desk matches the profile;
- unplugging runs the laptop converge through the daemon;
- opening and closing a window converges the grid again;
- after `komorebic stop` and `start`, tess re-subscribes and lays out the
  desktop without `tess wake`;
- after `taskkill /F` and a restart, tess re-subscribes within 30 s;
- a daemon killed with a non-zero exit restarts within a minute, and
  `schtasks /Query /TN "tessera daemon" /XML` shows the shipped settings;
- no console stays open after logon.

Interfaces: none. Gate: each check is recorded in `evidence.md` § Live
smoke.

## Tasks

- [ ] W1 — portable build and backend seam
- [ ] W2 — Windows CI job
- [ ] W3 — shared `internal/wm/spawn`
- [ ] W4 — komorebi spike and fixtures
- [ ] W5 — `internal/wm/komorebi` driver
- [ ] W6 — `wm.EventStream` and daemon forwarding
- [ ] W7 — Windows composition, console, task XML, README
- [ ] W8 — live smoke
