# Event-driven tess on macOS (RIG-8692)

## Open Questions

1. **Do hotkey commands run in the daemon?** `docs/design/go-port/design.md`
   recorded a decision not to route keybinds through a daemon. RIG-8692 asks
   for it. Measured on this MacBook, a fresh `tess --version` costs about
   34 ms and `/usr/bin/true` about 30 ms, so process start is not the cost.
   The gain is shared state: one driver, one event bus, and cascade
   serialization in one process.
   - (a) Route every WM command through the daemon with a thin client and
     in-process fallback when the daemon is not reachable. Hotkeys get event
     waits and serialize with cascades.
   - (b) Route only commands that mutate layout (`apply`, `laptop`, `flex`,
     `space-focus`). Read-only and trivial commands stay in-process.
   - (c) Keep commands in-process. They keep the polling fallback of
     `AwaitState` and gain nothing from the bus.

   **Recommendation: (a),** because a split routing table is a second
   convention to keep in sync, and (a) is reversible by one flag. Task T5 is
   the only task that depends on this answer.
2. **One notifier label per event, shared with the tess app.**
   `docs/design/tess-app.md` § *One source of truth* adds
   `tessera-state-<event>` signals that send the bare line `state`. This
   record needs subject-bearing lines for the same events.
   - (a) One label per event, `tessera-notify-<event>`, sends
     `<event> <subject>`. The daemon fans each line out to the bus, the
     `Hub` and the state publisher. tess-app A3 drops its own labels.
   - (b) Two labels per event: tess-app's `state` line and this record's
     subject line. Two forks per event, and the two can arrive in any order.

   **Recommendation: (a),** because yabai forks one shell per subscriber per
   event (`event_signal_flush`), so (b) doubles the fork cost of every
   focus change. Depends on nothing; tess-app A3 must adopt it if accepted.
3. **Zen focus remedy.** The three focus timeouts in today's daemon log need
   a remedy that does not depend on the macOS focus path alone.
   - (a) Space first: when the target Space is not visible, run
     `space --focus <index>`, wait for it to be active, then focus the
     window. Needs the scripting addition, which is loaded on this host.
   - (b) Focus-free stacking: when the target Space already has a managed
     anchor, use `window <anchor> --stack <id>`, which clears float and
     attaches to the anchor's view without using the active Space. Does not
     help the first window on an empty Space.
   - (c) Keep focus-then-poll and only raise the timeout.

   **Recommendation: (a) with (b) where an anchor exists,** because (a) is
   the only option that covers the first window, and (b) removes the race for
   all later windows. Gated by the live test in T6.
4. **Delete `SettleUnit`?** It has no callers outside the interface and its
   two implementations. `docs/design/windows-backend/design.md` expects a
   future Windows settle check.
   - (a) Delete it now; the Windows backend adds a waiter-based method when
     it needs one.
   - (b) Keep it until the Windows backend lands.

   **Recommendation: (a),** because dead interface methods are copied into
   every new backend. Depends on nothing.

## Direction

**Fit.** RIG-8692 asks tess to wait on what yabai reports instead of fixed
sleeps. The resident daemon record (`docs/design/resident-daemon.md`) made
tess a long-lived event subscriber; this record extends that daemon with
subject-keyed waits and a request path for commands. It does not change the
profile model, the layout engine or the rule set.

**Scope.** tessera only. The orion nix module that installs the daemon and
skhd needs no change unless Open Question 1 changes the skhd command line,
and it does not: skhd still runs `tess <cmd>`.

**Prior art.**

- [i3 IPC](https://i3wm.org/docs/ipc.html): one Unix socket carries both
  commands (`RUN_COMMAND`) and event subscriptions (`SUBSCRIBE`). Borrow:
  one socket, a first-line message type, and event payloads that name their
  subject.
- [AeroSpace](https://github.com/nikitabobko/AeroSpace): the `aerospace`
  CLI is a thin client to the running server, which holds the live state.
  Borrow: the CLI is a client; the server runs the command.
- [komorebi](https://lgug2z.github.io/komorebi/): `komorebic
  subscribe-socket` streams every state change with the full state attached.
  Borrow: events are hints and every frame carries confirmable state, which
  is the model `AwaitState` uses.

**Lifetime.** Years. The waits run on every desk apply and every hotkey.
Design effort goes to the failure mode: a lost event must cost at most one
poll interval, never a hang.

**Simpler option.** Keep polling but at a shorter interval and drop the
fixed sleeps that guard synchronous yabai state. This gets most of the
latency win with no bus. The bus is still worth it for two reasons: the
daemon already receives these events, and a 2 s focus timeout that polls
40 times wakes yabai's event loop 40 times while macOS is switching Spaces.
T3 ships the sleep removals first, so the simple win lands even if the bus
is cut.

## Problem / Intent

The yabai driver sleeps a fixed time after each step and polls focus for up
to 2 s before an unfloat. Fixed sleeps are slow when yabai is fast and
wrong when it is slow; on 2026-11-08 the focus wait timed out three times
and aborted desk applies. tess should wait for the state it needs, wake on
yabai's events, and fall back to polling when an event is lost, so a lost
event costs a poll interval and never a hang.

## Approach

### What yabai guarantees (source, v7.3.2)

The installed yabai is the Homebrew fork at commit `313bcd16`, read at
`/tmp/yabai-v7.3.2-src`. Each claim below is quoted from that tree.

- **Commands and events share one thread.** A command arrives as a
  `DAEMON_MESSAGE` event on the same loop as macOS notifications
  (`event_loop_post(&g_event_loop, DAEMON_MESSAGE, NULL, sockfd);`,
  `src/message.c`). `EVENT_HANDLER(DAEMON_MESSAGE)` in `src/event_loop.c`
  runs `handle_message(rsp, message); fflush(rsp); fclose(rsp);`, so a
  command's state changes inside yabai's model are done when the CLI
  returns.
- **Signals are sent after the handler, in a forked process.**
  `event_loop_run` calls `event_signal_flush();` after every event. The
  flush does `pid_t pid = fork(); if (pid) { g_signal_storage.used = 0;
  return; }`, then forks once per subscriber and runs
  `"/usr/bin/env", "sh", "-c", signal->command` (`src/event_signal.c`).
  So a signal caused by a command reaches tess after the command returns,
  and a waiter must subscribe before it sends the command.
- **Signals can be lost.** Neither `fork()` return value is checked for
  `-1`. Today's notifier is `echo <ev> | nc -U -w 1 <sock>`
  (`RegisterSignals`, `internal/daemon/wiring.go`); a missing or busy socket
  drops the line, and `handle` drops a line it cannot read within
  `readTimeout = time.Second` (`internal/daemon/serve.go`).
- **Signals are not ordered.** Each subscriber runs in its own process, so
  two signals from one flush, or from two flushes, can reach the socket in
  either order. Only mouse events are coalesced (`if (type == MOUSE_DRAGGED
  || type == MOUSE_MOVED)` in `event_loop_run`); signals are not.
- **`window_focused` does not fire for every programmatic focus.**
  `EVENT_HANDLER(WINDOW_FOCUSED)` returns before
  `event_signal_push(SIGNAL_WINDOW_FOCUSED, window);` when the app is not
  frontmost (`if (!application_is_frontmost(window->application)) return;`),
  during Mission Control, when the window is minimized, and when the window
  is on an invisible Space (`"%s %d is on invisible space %lld, skipping
  focus assignment"`). Focusing a window on another Space can therefore
  produce no `window_focused` signal at all until macOS has switched.
- **`space_changed` fires for every Space switch, including scripted ones,**
  except during Mission Control (`EVENT_HANDLER(SPACE_CHANGED)` ends with
  `event_signal_push(SIGNAL_SPACE_CHANGED, NULL);`). Its environment carries
  `YABAI_SPACE_ID` set from `g_space_manager.current_space_id`
  (`event_signal_push`, case `SIGNAL_SPACE_CHANGED`).
- **`space_changed` can move focus itself.** When the front app has no
  window on a visible Space, the handler focuses the top window of the new
  Space (`"front process %s has no windows on visible spaces, focusing %s
  %d"`). A focus wait that keys on a window ID can see focus land on a
  different window.
- **`window_created`, `window_focused` and `window_deminimized` carry
  `YABAI_WINDOW_ID`; `space_created` carries `YABAI_SPACE_ID` and
  `YABAI_SPACE_INDEX`** (`event_signal_push` cases in
  `src/event_signal.c`).
- **Latency is not knowable from source.** It is one `fork` of yabai, one
  `fork` per subscriber, an `sh` start and an `nc` start. It is measured in
  T0, which needs desktop writes and so is owner-run.

The rule that follows: **events are hints; state is confirmed by a query.**

### The event bus and `AwaitState`

Add `wm.Notice`, a subject-bearing event, and `wm.AwaitState`, one wait
loop used by every driver step.

- The waiter subscribes to matching notices **before** it runs the yabai
  command, because the signal arrives after the command returns.
- It runs the confirming query once at once, after every matching notice,
  and on a poll timer. The first query that confirms ends the wait.
- With no bus (in-process command, daemon down, or tests), the poll timer
  is the only wake, which is today's behaviour.
- A lost or reordered notice costs one poll interval. The timeout is the
  same per site as today, so the worst case never gets longer.
- The daemon's bus is a fan-out of parsed socket lines. It never blocks the
  socket handler: each subscriber has a one-slot channel and a send that
  finds it full is dropped, which is safe because the waiter re-queries.

### Which sleep each event replaces

| Site | Today | Becomes | Wake |
| --- | --- | --- | --- |
| `LabelSpace` | 50 ms sleep | no wait | none; the label is set in the command handler |
| `MoveWindowToSpace` | 100 ms sleep | `AwaitState` window space == target | poll only, 25 ms |
| `realizeStack` move | 350 ms sleep | same as above | poll only, 25 ms |
| `BSPSteps` move | `Settle: 400` | same as above | poll only, 25 ms |
| `BSPSteps` insert, ratio | `Settle: 150` | no wait | none; tree state is synchronous |
| `unfloatOne` focus | 50 ms poll, 2 s | `AwaitState` focused Space == target | `space_changed` with that Space ID |
| `unfloatOne` after toggle | 150 ms sleep | `AwaitState` window not floating | poll only, 25 ms |
| `CreateSpace` | 50 ms poll, 10 tries | `AwaitState` new Space ID present | `space_created` |
| `SettleUnit` | 150 ms, no callers | deleted (Open Question 4) | none |

Space membership after `window --space` has no confirming signal: yabai
moves the window through an asynchronous scripting-addition call, and the
only related signal, `window_moved`, is about frames and fires often. Those
sites poll the window query at 25 ms. The sleeps on synchronous yabai state
are removed outright; T6 gates that removal on a live desk apply.

The `Hub` debounce, the lock polls and the spawn polls in
`internal/commands/composite.go` keep their timers. They wait for quiet or
for other processes, not for one state change.

### The Zen focus failure

The daemon log at `/tmp/tessera-daemon.err.log` has three failures, all
from `unfloatOne`:

```text
ERROR wake routine failed error="applying engine.RealizeLayoutOp: window 262 did not activate target space 48 before unfloat"
ERROR daemon work failed error="applying engine.RealizeLayoutOp: window 217 did not activate target space 104 before unfloat"
ERROR wake routine failed error="applying engine.RealizeLayoutOp: window 217 did not activate target space 161 before unfloat"
```

Two ran in wake routines, and the lines just before two of them are
`holding spawns: yabai uptime 11s is below 30s` and `... uptime 0s ...`.
[INFERENCE] at least two failures ran within seconds of a yabai restart,
when yabai's window and application tables are still being rebuilt. The log
does not name the app; the Zen attribution comes from the RIG-8691 report.

The source gives three ways a focus-then-poll can miss:

1. `window_focused` is skipped for a window on an invisible Space, and the
   space switch depends on macOS activating the app first.
2. If the app is already frontmost with a visible window, macOS can treat
   the activation as done and not switch Spaces. [INFERENCE: Zen keeps
   several windows across Spaces; not reproduced.]
3. `space_changed` can hand focus to another window on the new Space.

The remedy (Open Question 3):

- When the target Space is not visible, focus the Space first with
  `space --focus <index>` and await it with `space_changed`. Then focus the
  window and await `query --spaces --space` reporting the target. A Space
  that is already visible skips the first step, because yabai rejects
  focusing the focused Space.
- When the target Space already has a managed anchor, use `StackOnto`
  instead of unfloat; it does not depend on the active Space.
- On timeout, log one diagnostic line: focused Space, focused window, the
  window's Space and its app. Return `wm.ErrFocusTimeout` so the RIG-8691
  recovery path can tell a retryable focus miss from a hard failure.
- Wake routines that start within `wm.SpawnReadyUptime` of a yabai start
  wait out the rest of that window before realizing, as spawns already do
  (`spawnHold` in `internal/commands/spawn_health.go`).

### Hotkey commands as daemon requests

skhd still runs `tess <cmd>`. The CLI becomes a thin client:

- It dials the daemon socket. On `ENOENT` or `ECONNREFUSED` it runs the
  command in-process, exactly as today.
- If the dial succeeds, it sends one line, `run 1 <json argv>`, and reads
  NDJSON frames: `stdout`, `stderr` and a final `exit`. Once the request
  line is written, the client never falls back, because the daemon may
  already have mutated the WM; a broken stream exits non-zero.
- The daemon runs the command with its own driver, bus and profile, under
  `TopologyState.SerializeCascade`, so a hotkey and a display cascade cannot
  interleave. Profile freshness follows tess-app B2 (daemon profile reload).

This does not remove the process start; the measured cost of that is near
`/usr/bin/true`. It removes the per-process profile load and driver set-up,
and it gives hotkey commands the bus and the cascade lock.

### Windows and komorebi parity

`wm.Notice` and `wm.AwaitState` are backend-neutral. The komorebi driver
publishes notices from its event stream (Prior art), with the window or
workspace it names as the subject. Its confirming queries are its own state
reads. The daemon bus, request protocol and wait sites are shared code. No
komorebi code is designed here.

### Alternatives considered

- **Trust the event, no confirming query.** Rejected: signals are lossy and
  unordered, and `window_focused` is skipped in the cases that matter.
- **A second socket for notices and requests.** Rejected for the same reason
  tess-app gives: the first line already tells connection kinds apart.
- **Spawn a `yabai -m signal` subscriber per wait.** Rejected: signal
  registration is global yabai state, and a crash would leave stale signals.

## Global Constraints

- Go 1.27, toolchain 1.27.3. No new module dependencies.
- Public repo: no references to private repositories.
- Tests use `testing/synctest` for every timer, and `internal/wm/fake` for
  drivers. No test talks to a live yabai.
- Signal labels use the `tessera-` prefix and never reuse
  `tessera-space_changed` or `tessera-display_changed`, which belong to the
  sketchybar triggers.
- A nil `wm.Notices` must give exactly today's polling behaviour.
- Every wait keeps or shortens its current timeout; none is unbounded.
- The client never falls back to in-process after the request line is sent.
- Socket lines stay one line per connection except `subscribe 1` and
  `run 1`, which keep the connection open.

## Plan

### T0 — Latency gate (owner-run)

Interfaces: none new. T2 logs each notice at debug level with its receive
time; T1 logs each `AwaitState` with the wake that confirmed it (`notice`,
`poll` or `immediate`) and elapsed time.

Gate: on the MacBook with the AW and G9, the owner runs one desk apply with
debug logging and records p50 and max elapsed per site, and how many waits
were confirmed by a notice. If notices confirm fewer than half the focus
waits, the bus is not worth its cost; ship T3's sleep removals and stop.

### T1 — `wm.Notice` and `wm.AwaitState`

Interfaces, new file `internal/wm/notice.go`:

```go
type Notice struct {
    Event  Event
    Window int     // 0 when the event names no window
    Space  SpaceID // "" when the event names no Space
}

// Notices delivers notices to subscribers. A nil Notices means poll only.
type Notices interface {
    // Subscribe returns a channel that receives a value after each notice
    // that match accepts, and a cancel func. Sends never block.
    Subscribe(match func(Notice) bool) (<-chan struct{}, func())
}

type AwaitOptions struct {
    Timeout time.Duration
    Poll    time.Duration
}

// AwaitState subscribes, then calls check at once, on each matching notice
// and on each poll tick until check reports true, ctx ends or Timeout passes.
// It returns false with a nil error on timeout.
func AwaitState(ctx context.Context, n Notices, match func(Notice) bool,
    check func(context.Context) (bool, error), opts AwaitOptions) (bool, error)
```

Callers that send a command use a two-step form so they can subscribe first:

```go
type Await struct{ /* unexported */ }
func Prepare(n Notices, match func(Notice) bool) *Await
func (a *Await) Wait(ctx context.Context,
    check func(context.Context) (bool, error), opts AwaitOptions) (bool, error)
```

`AwaitState` is `Prepare` then `Wait`. Gate: `synctest` tests cover
confirm-at-once, confirm-on-notice, lost notice confirmed by poll, timeout
returns false, nil `Notices`, and a notice sent between `Prepare` and the
command.

### T2 — Daemon bus and subject-bearing signals

Interfaces:

- `internal/daemon/bus.go`: `type Bus struct`, `func NewBus() *Bus`,
  `func (b *Bus) Publish(wm.Notice)`; `*Bus` implements `wm.Notices`.
- `ParseNotice(line string) (wm.Notice, bool)` parses `<event>`,
  `<event> window=<id>` and `<event> space=<id>`.
- `handle` in `internal/daemon/serve.go` publishes every parsed line to the
  bus, then keeps today's `Hub.Notify` and `NotifyWindowCreated` routing.
- `RegisterSignals` registers `tessera-notify-<event>` for `space_changed`,
  `space_created`, `window_focused` and `window_created`, with actions such
  as `printf 'space_changed space=%s\n' "$YABAI_SPACE_ID" | nc -U -w 1 ...`.
  The existing `window_created <id>` line stays readable until T2 lands the
  new form for every caller.
- `daemon.Run` builds one `Bus` and hands it to the driver (T3).

Gate: tests parse each line form, drop malformed lines, publish without
blocking on a full subscriber, and assert the sketchybar labels are
unchanged.

### T3 — Driver waits replace fixed sleeps

Interfaces: `yabai.New(yabaiPath string)` keeps its signature; add
`func (d *Driver) SetNotices(n wm.Notices)`, called once by `daemon.Run`
before serving, and the driver passes it to every wait. `BspStep.Settle` is replaced
by `BspStep.Await AwaitKind` with values `AwaitNone` and `AwaitSpace`.
`SettleUnit` is deleted from `wm.Driver`, the yabai driver and the fake
(Open Question 4).

Every row of the sleep table in Approach is implemented. Gate: existing
`args_test.go` cases updated to `Await`; new `synctest` cases show each
site returns as soon as the fake reports the state, and falls back to poll
with a nil bus.

### T4 — Zen focus remedy and typed timeout

Interfaces:

- `var ErrFocusTimeout = errors.New("focus did not reach target space")`
  in `internal/wm`; `unfloatOne` wraps it with the window and Space.
- `func (d *Driver) focusOnSpace(ctx context.Context, windowID int,
  space wm.SpaceID, index int) error` does Space-first focus, then window
  focus, each through `AwaitState`.
- Wake routines call `spawnHold`'s uptime check before realizing and wait
  out the remainder.

Gate: fake-driver tests for an invisible target Space (Space focused first),
a visible target Space (no Space focus), a timeout (`errors.Is(err,
wm.ErrFocusTimeout)` and one diagnostic log line), and a wake within the
uptime window.

### T5 — Commands as daemon requests

Depends on Open Question 1.

Interfaces:

- `internal/daemon/request.go`: `handleRun(ctx, conn, argv []string)` runs
  `cli.Run` with the daemon's driver and profile, under
  `SerializeCascade`, writing `{"v":1,"stdout":...}`,
  `{"v":1,"stderr":...}` and `{"v":1,"exit":<code>}` frames.
- `internal/cli/client.go`: `func Dispatch(ctx context.Context,
  argv []string, sock string) (code int, ran bool, err error)`; `ran` is
  false only when the dial failed, and `cmd/tess/main.go` then runs
  in-process.
- `tess daemon` itself and `tess wake` are never routed.

Gate: tests for dial failure fallback, no fallback after a written request,
exit-code round trip, and a request serialized behind a running cascade.

### T6 — Live gate (owner-run)

On the MacBook with yabai and the scripting addition: one desk apply from a
cold yabai start, one with a Zen window on an invisible Space, and ten
hotkey runs with the daemon up and ten with it stopped. Pass: no focus
timeout, no stranded floating window, and T0's numbers recorded in the PR.

## Tasks

- [ ] T0 — Owner-run latency gate using T1/T2 debug logs
- [ ] T1 — `wm.Notice`, `wm.Notices`, `wm.AwaitState`, `Prepare`/`Wait`
- [ ] T2 — Daemon `Bus`, `ParseNotice`, `tessera-notify-<event>` signals
- [ ] T3 — Replace every fixed sleep per the table; delete `SettleUnit`
- [ ] T4 — Space-first focus, `ErrFocusTimeout`, wake uptime hold
- [ ] T5 — `run 1` request protocol and thin client (Open Question 1)
- [ ] T6 — Owner-run live gate on the desk
