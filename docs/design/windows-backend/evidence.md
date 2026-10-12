# Windows backend — upstream evidence

*Supporting file for [`design.md`](design.md). komorebi paths are in
`LGUG2Z/komorebi` and GlazeWM paths in `glzr-io/glazewm`, on each default
branch, read on 2026-10-12. The Go probes ran on macOS arm64 with Go
1.27.2. W4 appends § Spike results here, and W8 appends § Live smoke.*

## Cross-compile

With `SuppressSignals` stubbed, `GOOS=windows go build ./...` succeeds.
Without the stub, it fails only in `SuppressSignals` in
`internal/effects/effects.go`:

```text
undefined: syscall.O_NOFOLLOW
undefined: syscall.Stat_t
undefined: syscall.Fstat
```

Test compilation for Windows fails in three packages and nowhere else:

| Package | Cause |
| --- | --- |
| `internal/cli` | `syscall.Kill`, a `#!/bin/sh` stub, `os.MkdirTemp("/tmp", …)` |
| `internal/effects` | `syscall.Mkfifo` |
| `internal/wm/yabai` | `syscall.Mkfifo` in `args_test.go` |

The engine, executor, commands, config, daemon, `wm` and `wm/fake` packages
compile their tests for Windows unchanged.

## Go on Windows

- **AF_UNIX.** `net/unixsock_posix.go` builds for
  `unix || js || wasip1 || windows`. Windows supports AF_UNIX from build
  10.0.17063 (`SupportUnixSocket` in
  `internal/syscall/windows/version_windows.go`), so `daemon.listen` and
  `commands.Wake` work unchanged.
- **Locks.** `gofrs/flock` v0.13.1 has `flock_windows.go`, which calls
  `LockFileEx` with `LOCKFILE_EXCLUSIVE_LOCK`. `TryLock` works there.
- **Shutdown.** `os/signal` delivers `CTRL_CLOSE_EVENT`,
  `CTRL_LOGOFF_EVENT` and `CTRL_SHUTDOWN_EVENT` as `syscall.SIGTERM`
  ([`os/signal` § Windows](https://pkg.go.dev/os/signal#hdr-Windows)), so
  `main` needs no change.
- **Race detector.** `-race` needs cgo on every OS except darwin
  (`cmd/go/internal/work/init.go`). On Windows it therefore depends on the
  runner image's gcc.
- **`golang.org/x/sys/windows` v0.47.0** wraps `GetForegroundWindow`. It
  has no wrapper for `SetForegroundWindow`, `FreeConsole` or
  `GetConsoleProcessList`. `NewLazySystemDLL` (`dll_windows.go`) loads
  those without cgo.
- **Runner.** `windows-latest` is Windows Server 2025
  (`images/windows/Windows2025-Readme.md` in `actions/runner-images`).

## komorebi IPC

`send_message` in `komorebi-client/src/lib.rs`:

```rust
let socket = DATA_DIR.join(KOMOREBI);
let mut stream = UnixStream::connect(socket)?;
stream.set_write_timeout(Some(Duration::from_secs(1)))?;
stream.write_all(serde_json::to_string(message)?.as_bytes())
```

- `SocketMessage` is serialized with
  `#[serde(tag = "type", content = "content")]`
  (`komorebi/src/core/mod.rs`).
- `DATA_DIR` is `dirs::data_local_dir()` joined with `komorebi`
  (`komorebi/src/lib.rs`), which is `%LOCALAPPDATA%\komorebi`.
- `read_commands_uds` (`process_command.rs`) waits one second for the
  window manager lock. It then logs "could not acquire window manager
  lock, not processing message" and drops the message. A paused komorebi
  ignores all but a few messages ("ignoring while paused").
- `StateQuery::Version` returns `build::RUST_VERSION`, the rustc version
  (`process_command.rs`). `komorebic --version` prints komorebic's own
  `build::CLAP_LONG_VERSION`, not the running komorebi's, and would need
  `os/exec`. This is why the driver uses a feature probe.

## komorebi commands

- Only `DebugWindow(isize)` takes a window handle (`SocketMessage`).
- Index-addressed commands include
  `SendContainerToMonitorWorkspaceNumber`,
  `MoveContainerToMonitorWorkspaceNumber`, `FocusMonitorWorkspaceNumber`,
  `EnsureWorkspaces`, `WorkspaceName`, `WorkspaceLayout`,
  `WorkspaceTiling`, `WorkspaceLayoutCustom`, `WorkspaceRule` and
  `ClearWorkspaceRules`. The `Send…` form keeps focus where it is. The
  `Move…` form follows the window.
- No command reorders workspaces. The nearest are
  `MoveWorkspaceToMonitorNumber` and `SwapWorkspacesToMonitorNumber`.
- `CloseWorkspace` closes only the focused workspace, and only when it is
  empty and unnamed. `set_workspace_name` cannot clear a name.
  `ensure_workspace_count` only grows the ring. `EnsureNamedWorkspaces`
  renames every workspace by position
  (`ensure_named_workspaces_for_monitor`).
- `LayoutRatios(Option<Vec<f32>>, Option<Vec<f32>>)` acts on the focused
  workspace, and a `None` side keeps its old ratios. `validate_ratios`
  (`komorebi-layouts/src/default_layout.rs`) clamps each ratio to
  `MIN_RATIO..MAX_RATIO`, keeps at most `MAX_RATIOS`, and stops when the
  running sum would reach 1.0.
- `ToggleWorkspaceWindowContainerBehaviour` toggles the value. There is
  no setter.
- `Promote` moves the focused container to the front
  (`promote_container_to_front`). `StackWindow(dir)` joins the neighbour
  in `dir`.
- `WindowsApi::raise_and_focus_window` (`komorebi/src/windows_api.rs`)
  sends an empty mouse `SendInput` "so that we pass the foreground lock
  check", calls `SetWindowPos(HWND_TOP, …)`, then `SetForegroundWindow`.
- `EnableAutostart` (`komorebic/src/main.rs`) backs
  `komorebic enable-autostart`.

## ApplyState

`main.rs` calls it once, to load `komorebi.state.json` after a quick
restart. `WindowManager::apply_state` (`komorebi/src/window_manager.rs`):

- returns with only a log line when the monitor count differs ("cannot
  apply state from {}; state file has {state_monitors_len} monitors, but
  only {current_monitors_len} are currently connected");
- does nothing when any window in the sent state has closed
  (`window.exe().is_err()` sets `can_apply = false`);
- replaces each workspace with the sent copy
  (`*workspace = state_workspace.clone()`), so a window that komorebi
  starts to manage after tess's query leaves the ring *[INFERENCE: whether
  a later `Show` manages it again is unverified; W4 check 2]*;
- focuses the sent focused workspace, then hides and restores every
  workspace on every monitor (`Monitor::load_focused_workspace`);
- writes no reply.

## Subscriptions

`notify_subscribers` in `komorebi/src/lib.rs`:

```rust
match UnixStream::connect(path) {
    Ok(mut stream) => {
        tracing::debug!("pushed notification to subscriber: {socket}");
        stream.write_all(notification.as_bytes())?;
    }
    Err(_) => {
        stale_sockets.push(socket.clone());
    }
}
```

- Each notification is one connection with no trailing newline. A failed
  connect drops the subscriber for good.
- It runs at the end of `process_command`, under the lock that
  `read_commands_uds` holds, and `write_all` blocks.
- `AddSubscriberSocket` inserts into the `SUBSCRIPTION_SOCKETS` map, so a
  repeat is harmless. `AddSubscriberSocketWithOptions` adds
  `filter_state_changes`.
- `WindowManager::stop` (`window_manager.rs`) connects to each subscriber,
  calls `shutdown(Shutdown::Both)`, removes `komorebi.sock` and exits. The
  Ctrl-C path in `main.rs` dumps state and does the same.
- `WindowManagerEvent` (`window_manager_event.rs`) has Destroy,
  FocusChange, Hide, Cloak, Minimize, Show, Uncloak, MoveResizeStart,
  MoveResizeEnd, MouseCapture, Manage, Unmanage, Raise and TitleUpdate.
  `MonitorNotification` (`monitor_reconciliator/mod.rs`) includes
  `DisplayConnectionChange`, `ResolutionScalingChanged` and
  `WorkAreaChanged`.

## Rules, windows and monitors

- `GlobalState.workspace_rules` (`komorebi/src/state.rs`) lists the
  workspace rules. `ClearNamedWorkspaceRules`, `NamedWorkspaceRule` and
  `InitialNamedWorkspaceRule` exist. A rule sent over the socket uses
  `MatchingStrategy::Legacy` (`process_command.rs`), which is
  `title.starts_with(&identifier.id) || title.ends_with(&identifier.id)`
  (`window.rs`), not a regex. `ClearAllWorkspaceRules` also clears the
  static config's rules until komorebi reloads it.
- `commands.Rules` installs macOS rules: a `^.*$` catch-all to the laptop
  space, `Code` and `Linear` to displays, and `Finder` and an `AXDialog`
  subrole as unmanaged. The catch-all, `Finder` and `AXDialog` have no
  Windows meaning.
- `impl Serialize for Window` (`window.rs`) emits `exe` (for example
  `Code.exe`), `class` and `title`. `Monitor` (`monitor.rs`) has
  `size: Rect`, `device_id` and `serial_number_id`.
  `engine.ResolveDisplay` matches when
  `display.Frame.W == float64(spec.Width)` (`internal/engine/matcher.go`).
- `komorebi-layouts/src/custom_layout.rs` gives a width only to
  `Column::Primary` (`ColumnWidth::WidthPercentage`).

## Licence

The Komorebi License 2.0.0 "does not permit any kind of commercial use
(i.e. using `komorebi` at work)". The Individual Commercial Use License is
a subscription for one named person: $10 a month or $110 a year for
Windows, or $15 a month or $180 a year for Windows and macOS
([lgug2z.com/software/komorebi](https://lgug2z.com/software/komorebi/)).

## GlazeWM

- IPC is a WebSocket on `127.0.0.1:6123` (`packages/wm/src/ipc_server.rs`).
  Commands target any container with `--id` (`AppCommand::Command` in
  `packages/wm-common/src/app_command.rs`), and `sub -e` streams typed
  events (`packages/wm-common/src/wm_event.rs`).
- `InvokeCommand` has no stack or tab container.
- Only workspaces declared in the config can be activated: "Workspace with
  name '{workspace_name}' doesn't exist or is already active."
  (`packages/wm/src/commands/workspace/activate_workspace.rs`). So
  `CreateSpace` cannot make `lap-*` spaces. `UpdateWorkspaceConfig` can
  relabel a declared one.
- Window rules live only in the config file. The licence is GPL-3.0.

## Task Scheduler

From Microsoft Learn:

- [`DisallowStartIfOnBatteries`](https://learn.microsoft.com/en-us/windows/win32/taskschd/taskschedulerschema-disallowstartifonbatteries-settingstype-element):
  "the default setting for this element is True".
- [`TaskSettings`](https://learn.microsoft.com/en-us/windows/win32/taskschd/tasksettings):
  by default a task "will be stopped 72 hours after it starts to run".
- [`RestartOnFailure`](https://learn.microsoft.com/en-us/windows/win32/taskschd/taskschedulerschema-restartonfailure-settingstype-element)
  needs both children, `Count` and `Interval`.
- [`TaskSettings.RestartInterval`](https://learn.microsoft.com/en-us/windows/win32/taskschd/tasksettings-restartinterval)
  is at least one minute.

So a one-line `schtasks /Create /SC ONLOGON` task would stop the daemon on
battery and after 72 hours, and would never restart it.

## Alternatives considered

- **GlazeWM.** It has the safer command model and the better licence for
  users, but no stack container, config-only workspaces and config-only
  rules (§ GlazeWM, design Open Question 1).
- **Running `komorebic` once per call.** It spends a process start on each
  call to send the same message `komorebi-client` sends.
- **Named-pipe subscriptions** (`komorebic subscribe-pipe`,
  `AddSubscriberPipe`). The standard library cannot serve a named pipe, so
  it needs another module, and AF_UNIX already works.
- **Reusing the daemon socket for pushes.** It would change the one-line
  protocol that `tess wake` and the tests depend on, only to accept a
  newline-less, multi-KB body.
- **A shell action through `wm.EventSource`.** komorebi has no per-event
  command hook, and Windows has no `nc`.
- **`WorkspaceLayoutCustom` for column widths.** Only the primary column
  has a width, so it cannot carry one weight per track.
- **`EagerFocus(exe)` to focus a window.** It focuses the first window of
  that exe, which is ambiguous when an app has two windows.
- **A Go struct for `State`.** It would silently drop the members that
  later komorebi versions add, and `ApplyState` would send them back
  missing.
- **Linking with `-H windowsgui`.** It removes the daemon's console, but
  it also cuts the CLI's output in a terminal.
