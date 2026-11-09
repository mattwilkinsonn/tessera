# Event-driven tess on macOS (RIG-8692)

## Open Questions

1. **Which commands run in the daemon, and which ones wait for the cascade
   lock?** RIG-8692 asks for daemon-run hotkey commands.
   `docs/design/go-port/design.md` decided against daemon keybinds.
   `docs/design/tess-app.md` § *Snapshot consistency* accepts that
   "`tess space-focus` does not wait for busy". Running a command in the
   daemon and putting it behind the lock are separate choices. A queued
   relative command (focus, swap, toggle-float) acts on whatever window has
   focus when it finally runs, which can be up to `CascadeTimeout` (60 s)
   later.
   - (a) Route every command except `daemon` and `wake`. Only rebuild
     commands are serialized. A rebuild command is one whose code calls
     `PreserveFocus` or `runPlacementCommand` today: apply, laptop, rules,
     display-setup and snap. All other commands run at once in the daemon
     and never queue. This keeps tess-app's accepted focus-restore race.
     It supersedes the go-port decision.
   - (b) Route and serialize every command. No interleaving, but
     navigation can stall for a whole cascade and then target a different
     window. This supersedes both earlier records.
   - (c) Route only the rebuild commands. Navigation stays in-process.
     That leaves two execution paths and two routing tables.

   **Recommendation: (a).** It serializes exactly what is serialized
   today: these commands already take file locks. It does not change when
   navigation happens. T5 depends on this answer.
2. **What happens to a daemon-run command when its client disconnects?**
   *Depends on 1.* skhd can kill a client. A client can also stop reading.
   - (a) Cancel on disconnect at every stage, including mid-run. Nothing
     runs unobserved, but a cancelled rebuild can leave floating windows
     part-way through, which is the RIG-8691 failure.
   - (b) Cancel a request that is still queued. Once a request holds the
     lock, it runs to completion under `bounded` (`CascadeTimeout`), and
     its output is dropped after a missed 1 s write deadline. An abandoned
     hotkey never runs late. A rebuild that has started is never cut short.
     The client cannot learn the outcome of a run whose stream broke.
   - (c) Never cancel. Queued requests run even when nobody waits for
     them.

   **Recommendation: (b).** A started rebuild must finish to a consistent
   layout, and a queued one has no user left to serve.
3. **Which profile does a daemon-run command use?** *Depends on 1.* Today
   each process runs `config.Load`, which honours `TESSERA_PROFILE` and
   then `profile.cue`. The daemon loads its profile once. tess-app B2
   reloads the profile only on wake.
   - (a) The daemon's profile is authoritative. Before each request, the
     daemon reloads the profile if the file changed. A client's
     `TESSERA_PROFILE` is ignored, with a warning.
   - (b) The client sends the profile path it resolved. The daemon loads
     that profile for this request, cached by path and mtime. Faithful,
     but a request can then run a profile other than the one the daemon's
     topology state was built with.
   - (c) As in (a), and a client whose resolved path differs from the
     daemon's runs in-process.

   **Recommendation: (c).** Normal hotkeys share one fresh profile, and an
   explicit override keeps today's meaning. T5 includes B2's reload
   interface; it does not wait for B2's editor work.
4. **How long may each wait last, and what happens when it times out?**
   A fixed sleep never failed. A checked wait can time out, which is a new
   outcome.
   - (a) Each budget equals today's sleep or poll bound. On timeout the
     step continues as it does today, except focus. Both focus phases
     share today's 2 s and fail with `wm.ErrFocusTimeout`. The worst case
     never gets longer. The startup uptime hold stays a separate bound.
   - (b) Longer budgets, chosen for convergence: move 1 s, toggle 500 ms,
     focus 2 s per phase. A timeout returns an error. Steps become more
     reliable, but the worst case grows past today's.
   - (c) Today's budgets, and every timeout returns an error. Fails fast,
     but it adds failures where today's sleeps silently carried on.

   **Recommendation: (a).** It makes the change strictly no worse than
   today. The budgets are in the sleep table. The rest of this record
   assumes (a).
5. **How should tess fix the Zen focus failures?** *Depends on 4 for the
   focus budget.*
   - (a) Space first: when the target Space is not active, call the shared
     awaited `FocusSpace` (§ *A shared FocusSpace contract*), then focus
     the window. This covers the first window on an empty Space.
   - (b) Focus-free stacking: when the target Space has a managed anchor,
     move the window, confirm the move, then run `StackOnto`. No focus is
     needed. This does not cover the first window.
   - (c) Keep focus-then-poll and only raise the timeout.

   **Recommendation: (a), plus (b) where an anchor exists.** (a) is the
   only option that covers the first window. (b) removes the focus race
   for every later window. T6 is the live test.
6. **Delete `SettleUnit`?** It has no callers outside the `wm.Driver`
   interface and its two implementations. The Windows-backend record
   expects a future settle check.
   - (a) Delete it now. The Windows backend adds an `AwaitState`-based
     wait when it needs one.
   - (b) Keep it until the Windows backend lands.

   **Recommendation: (a).** Each new backend would otherwise copy a dead
   method.

## Direction

**Fit.** RIG-8692 asks tess to wait on what yabai reports instead of fixed
sleeps, and to run hotkey commands in the daemon. The resident-daemon
record (`docs/design/resident-daemon.md`) made tess a long-lived event
subscriber. This record adds subject-keyed waits and a request path to that
daemon. The profile model, layout engine and rule set do not change.

**Scope.** tessera only. skhd still runs `tess <cmd>`, so the orion nix
module does not change. Two sibling records are touched. tess-app A1
(`FocusSpace`) and A3 (state signals) adopt the shared contracts in
§ *A shared FocusSpace contract* and § *One signal inventory*. B2's profile
reload interface is reused in T5.

**Prior art.**

- [i3 IPC](https://i3wm.org/docs/ipc.html): one Unix socket carries
  commands (`RUN_COMMAND`) and event subscriptions (`SUBSCRIBE`), with a
  typed message header. Borrow: one socket, a first-line message type, and
  a version on every frame.
- [AeroSpace](https://github.com/nikitabobko/AeroSpace): the `aerospace`
  CLI is a thin client to the running server, which holds the live state.
  Borrow: the CLI is a client, and the server runs the command.
- [komorebi](https://lgug2z.github.io/komorebi/): `komorebic
  subscribe-socket` streams every change with full state attached. Borrow:
  events are hints that are confirmed against state, which is the model
  `AwaitState` uses.

**Lifetime.** Years. The waits run on every desk apply and hotkey. Most
design effort goes to failure modes. A lost event costs at most one
backstop poll. A wait never hangs. A request never runs twice.

**Simpler option.** Keep polling at a shorter interval and drop the sleeps
that guard yabai's synchronous state. This gives most of the latency win
with no bus. T3 ships those removals with or without the bus. T0 measures
whether the bus also cuts wait time or yabai queries. If it does not, the
bus is removed and the other tasks stand.

**Read-only measurements on this MacBook, 50 runs each:** `tess --version`
1.72 s, `/usr/bin/true` 1.51 s, `getconf DARWIN_USER_TEMP_DIR` 1.02 s,
`yabai -m query --spaces --space` 1.26 s. Process start costs about 4 ms
more than `true`. Socket-path resolution costs about 20 ms. A focused-Space
query costs about 25 ms. These are rough figures from one machine.

## Problem / Intent

The yabai driver sleeps for a fixed time after each step and polls focus
for up to 2 s before an unfloat. Fixed sleeps waste time when yabai is
fast and are too short when it is slow. On 2026-11-08 the focus wait timed
out three times and aborted desk applies. tess should wait for the state it
needs, wake on yabai's events, and fall back to polling when an event is
lost. Hotkey commands should run in the daemon and share its live state.

## Approach

### What yabai provides (source, v7.3.2)

The installed yabai is the Homebrew fork at commit `313bcd16`, read at
`/tmp/yabai-v7.3.2-src`.

- **Commands and macOS events share one loop.** A command is posted as
  `DAEMON_MESSAGE` (`event_loop_post(&g_event_loop, DAEMON_MESSAGE, NULL,
  sockfd);`, `src/message.c`). `EVENT_HANDLER(DAEMON_MESSAGE)` in
  `src/event_loop.c` runs `handle_message(rsp, message); fflush(rsp);
  fclose(rsp);`. When the CLI returns, yabai's in-memory model already
  reflects the command. macOS-side effects, such as a Space switch, may
  still be pending.
- **The only ordering guarantee is weak.** `event_loop_run` calls
  `event_signal_flush();` after each handler. The flush forks
  (`pid_t pid = fork(); if (pid) { ... return; }`), then forks once per
  subscriber to run `"/usr/bin/env", "sh", "-c", signal->command`
  (`src/event_signal.c`). So the command's response is closed before its
  own queued signals are forked. After that, the CLI's read loop (`while
  ((bytes_read = read(sockfd, rsp, sizeof(rsp)-1)) > 0)`,
  `client_send_message` in `src/yabai.c`) races the notifier processes. A
  notice can reach tess before or after the yabai CLI exits. Signals
  caused later by macOS notifications arrive on later loop turns. A waiter
  must therefore subscribe before it sends the command.
- **Signals can be lost or reordered.** Neither `fork()` result is checked
  for `-1`. The notifier is `nc -U -w 1`, which drops the line when the
  socket is missing or busy. `handle` in `internal/daemon/serve.go` drops a
  line it cannot read within `readTimeout = time.Second`. Each subscriber
  runs in its own process, so arrival order is not fixed. Only mouse events
  are coalesced (`if (type == MOUSE_DRAGGED || type == MOUSE_MOVED)`).
- **`space_changed` is driven by an OS notification.**
  `activeSpaceDidChange:` posts `SPACE_CHANGED` (`src/workspace.m`). A
  scripted switch therefore produces the signal only if macOS posts
  `NSWorkspaceActiveSpaceDidChangeNotification` [INFERENCE: expected,
  unverified]. `EVENT_HANDLER(SPACE_CHANGED)` returns without a signal in
  two cases: during Mission Control, and within 400 ms of a Mission Control
  cancellation (`space_manager_focus_space(prev_sid); return;`). The
  handler can also focus a different window (`"front process %s has no
  windows on visible spaces, focusing %s %d"`).
- **A cross-display switch takes another path.** `activeDisplayDidChange:`
  posts `DISPLAY_CHANGED`. That handler sets
  `g_space_manager.current_space_id` and pushes `SIGNAL_DISPLAY_CHANGED`.
  It returns early when the display is unchanged. `space_manager_focus_space`
  calls `display_manager_focus_display(new_did, sid)` when the target is on
  another display.
- **`window_focused` is skipped in several cases.** It is skipped for an
  app that is not frontmost, a minimized window, during Mission Control,
  and for a window on an invisible Space (`"%s %d is on invisible space
  %lld, skipping focus assignment"`, `EVENT_HANDLER(WINDOW_FOCUSED)`).
- **Moves to a Space have three paths and no completion signal.**
  `space_manager_move_window_to_space` uses
  `SLSPerformAsynchronousBridgedWindowManagementOperation` when it exists.
  Otherwise it uses `SLSMoveWindowsToManagedSpace`, or a scripting-addition
  fallback with a compat-ID swap. No signal confirms Space membership.
- **Subjects are in the environment.** `window_created`, `window_focused`
  and `window_deminimized` set `YABAI_WINDOW_ID`. `space_created` sets
  `YABAI_SPACE_ID`. `space_changed` sets `YABAI_SPACE_ID` from
  `current_space_id`. `display_changed` sets `YABAI_DISPLAY_ID`
  (`event_signal_push`).
- **Labels replace in place.** `event_signal_add` does `if
  (signal->label) event_signal_remove(signal->label);`. `signal --remove
  <label>` fails with `signal with label '%s' not found.` when the label is
  absent.

Rule: **events are hints; a query confirms the state.**

### `AwaitState`: confirm, wake on notice, poll as backstop

`wm.AwaitState` is the one wait loop for every driver step.

- The caller subscribes with `wm.Prepare` **before** sending the yabai
  command. A notice that arrives before `Wait` starts is kept.
- `Wait` runs the confirming query once at once. After that, it re-runs the
  query on each matching notice and on a timer. The first query that
  confirms ends the wait.
- With no notices (an in-process command, notices disabled, or tests), the
  timer runs at `Poll`, which is today's interval. With notices it runs at
  `Backstop` (100 ms), so a slow Space switch costs about 20 queries in 2 s
  instead of 40. A lost notice costs at most one backstop.
- `Prepare` returns an `*Await` whose `Close` unsubscribes. `Wait` closes
  it. A caller that fails between `Prepare` and `Wait` must call `Close`;
  callers `defer a.Close()`.
- The daemon's bus never blocks the socket handler. Each subscription has a
  one-slot channel, and a send to a full channel is dropped. This is safe
  because the waiter queries again.

### Sleep sites, wakes and budgets

Budgets follow Open Question 4's recommendation. The **On timeout**
column is the outcome when the budget runs out.

| Site | Today | Confirming query | Wake | Budget | On timeout |
| --- | --- | --- | --- | --- | --- |
| `LabelSpace` | 50 ms sleep | none (synchronous) | none | 0 | n/a |
| `MoveWindowToSpace` | 100 ms sleep | raw window `space` == target index | poll 25 ms | 100 ms | continue, as today |
| `realizeStack` move | 350 ms sleep | same | poll 25 ms | 350 ms | continue |
| `BSPSteps` anchor move | `Settle: 400` | same | poll 25 ms | 400 ms | continue |
| `BSPSteps` extra-window move | `Settle: 350` | same | poll 25 ms | 350 ms | continue |
| `BSPSteps` anchor insert | `Settle: 150` | none (synchronous) | none | 0 | n/a |
| `BSPSteps` stack insert before extra | `Settle: 150` | none (synchronous) | none | 0 | n/a |
| `BSPSteps` ratio | `Settle: 150` | none (synchronous) | none | 0 | n/a |
| `unfloatOne` focus (both phases) | 50 ms poll, 2 s | active Space == target | `space_changed`, `display_changed`, `window_focused`; backstop | 2 s shared | `wm.ErrFocusTimeout` |
| `unfloatOne` after toggle | 150 ms sleep | raw window `is-floating` false | poll 25 ms | 150 ms | continue |
| `CreateSpace` | 50 ms poll, 10 tries | new Space ID present | `space_created`; backstop | 500 ms | not found, as today |
| `SettleUnit` | 150 ms, no callers | n/a | n/a | n/a | deleted (Open Question 6) |

The move checks use one raw `query --windows --window <id>` and compare
its `space` index to the index the move command used. They do not use
`QueryWindow`, which runs two yabai queries. Insertion, ratio and label
commands change yabai's tree inside the command handler, so they need no
wait. T6 gates the removal of those waits on a live desk apply. The `Hub`
debounce, the lock polls and the spawn polls in
`internal/commands/composite.go` keep their timers. They wait for quiet or
for other processes, not for one state change.

### A shared `FocusSpace` contract

tess-app A1 adds `FocusSpace(ctx, id) (FocusResult, error)`, which checks
`QueryFocusedSpace`, focuses, and checks again once. This record makes
that method the awaited contract that both records use.

- **Active, not just visible.** Each display has a visible Space. yabai
  tiles an unfloated window on `space_manager_active_space()`: the focused
  window's visible user Space, or else the active display's current Space.
  `query --spaces --space` resolves the same function
  (`handle_domain_query`, `acting_sid = space_manager_active_space()`).
  The predicate is therefore "the focused-Space query returns the target",
  not `is-visible`.
- **Already active:** return `FocusAlready` with no WM call.
  `space_manager_focus_space` returns `SPACE_OP_ERROR_SAME_SPACE` for
  that case.
- **Visible on another display:** yabai focuses the display. The wake is
  `display_changed`, not `space_changed`. The wait listens for both.
- **Index drift:** resolve the index, focus, and await. On a mismatch,
  re-resolve once, as A1 specifies.
- **Without the scripting addition** yabai falls back to
  `space_manager_focus_space_using_gesture`. That path rate-limits and can
  return without switching (`"rate-limiting gesture to space"`). This host
  loads the scripting addition. Elsewhere the await times out cleanly.

`unfloatOne` then focuses the window and awaits the same predicate, within
the 2 s shared budget.

### The Zen focus failure

The daemon log at `/tmp/tessera-daemon.err.log` has three failures, all
from `unfloatOne`:

```text
ERROR wake routine failed error="applying engine.RealizeLayoutOp: window 262 did not activate target space 48 before unfloat"
ERROR daemon work failed error="applying engine.RealizeLayoutOp: window 217 did not activate target space 104 before unfloat"
ERROR wake routine failed error="applying engine.RealizeLayoutOp: window 217 did not activate target space 161 before unfloat"
```

The lines just before two of them are `holding spawns: yabai uptime 11s is
below 30s` and `... uptime 0s ...`. [INFERENCE] At least two failures ran
within seconds of a yabai restart, while yabai was still rebuilding its
window tables. The log does not name the app. The Zen attribution comes
from the RIG-8691 report.

The source shows three ways a focus-then-poll can miss. `window_focused`
is skipped for a window on an invisible Space. The Space switch depends on
macOS activating the app first [INFERENCE: an app that is already
frontmost with windows on several Spaces may not switch; not reproduced].
`space_changed` can hand focus to another window.

The remedy (Open Question 5):

- Space first through `FocusSpace`, then window focus, both within one 2 s
  budget.
- With an anchor: run `MoveWindowToSpace`, confirm membership (move row),
  then run `StackOnto`. `window_manager_stack_window` clears the float
  flag and attaches the window to the anchor's view, but moves no Space
  (`view_stack_window_node(a_node, b); window_manager_add_managed_window(wm,
  b, a_view);`). Its correctness depends on the window already being on the
  target Space.
- On timeout: one diagnostic line with the focused Space, focused window,
  the window's Space and its app, then `wm.ErrFocusTimeout`.
- Wake routines that start within `wm.SpawnReadyUptime` of a yabai start
  wait out the rest of that window before realizing, using the existing
  `spawnHold` check (`internal/commands/spawn_health.go`). This is a
  separate bound of at most 30 s.

### One signal inventory

All daemon signals come from one table, `daemon.SignalInventory`, and
yabai holds exactly one socket notifier per event.

| Event | Line subject | Routes |
| --- | --- | --- |
| `display_added`, `display_removed`, `display_moved` | none | Hub display, publisher |
| `display_changed` | `display=<id>` | bus, publisher |
| `display_resized` | none | publisher (A3) |
| `space_created` | `space=<id>` | bus, publisher (A3) |
| `space_destroyed` | none | publisher (A3) |
| `space_changed` | `space=<id>` | bus, publisher |
| `window_created` | `window=<id>` | Hub flex, arrivals, bus, publisher |
| `window_destroyed` | none | Hub flex, publisher |
| `window_focused` | `window=<id>` | bus, publisher |
| `window_moved`, `window_minimized`, `window_deminimized`, `window_title_changed` | none | publisher (A3) |
| `application_launched`, `application_terminated` | none | Hub flex, publisher |

- Labels are `tessera-notify-<event>`. The action is `printf '<event>
  <key>=%s\n' "$YABAI_..." | /usr/bin/nc -U -w 1 '<sock>'`. Events with no
  subject send `<event>` alone.
- T2 registers only the rows that have a consumer when it lands. Rows
  marked "(A3)" are registered by tess-app A3, from this table. A3 drops
  its separate `tessera-state-<event>` labels and its `state` line, and
  calls `StatePublisher.Request` from the line router.
- Before adding notifiers, registration removes the old labels. A missing
  label is not an error. The old labels are `tessera-application_launched`,
  `tessera-application_terminated`, `tessera-window_created`,
  `tessera-window_destroyed`, `tessera-display_added`,
  `tessera-display_removed` and `tessera-display_moved` (live `yabai -m
  signal --list` shows all seven), plus any `tessera-state-<event>`. A
  yabai that survives a tess upgrade therefore never runs two notifiers for
  one event. Removing old labels first means a gap can lose an event,
  never double it. The startup wake reconciles any loss.
- The sketchybar labels `tessera-space_changed` and
  `tessera-display_changed` keep their names and actions.
- The old `window_created <id>` line form is retired. The daemon always
  registers the notifiers whose lines it parses.

### Hotkey commands as daemon requests

skhd still runs `tess <cmd>`. The CLI parses argv once with `cli.Parse`.
The daemon re-parses the same argv with the same function. Routing follows
Open Question 1(a).

**Package seam.** `cli` imports `daemon` today (`internal/cli/runtime.go`),
so `daemon` must not import `cli`. The composition root, `run` in
`cmd/tess/main.go`, builds `daemon.Options.PrepareRequest`. That function
calls `cli.Parse` and returns a `daemon.Job` that wraps `commands.Run`.
The daemon only sees the `Job`. The client side lives in `daemon`
(`daemon.Dispatch`), and `cli.Main` reaches it through a `Deps` hook.

**Protocol.** NDJSON on the existing socket, sharing tess-app's `"v": 1`
framing.

1. The client writes one line: `run 1 <json argv>`.
2. The daemon checks the version and runs `PrepareRequest`. On failure it
   writes `{"v":1,"reject":"<reason>"}` and closes.
3. Otherwise the daemon writes `{"v":1,"accepted":true}` **before**
   anything that can touch the WM. Then it writes `stdout` and `stderr`
   frames, and finally `{"v":1,"exit":<code>}`.
4. The client falls back to in-process when the dial fails, when a
   `reject` frame arrives, or when the stream ends before any frame. An
   older daemon drops the unknown line and closes with no frame. Bytes
   written to a Unix socket are still readable after the writer exits, so
   end-of-stream before `accepted` proves nothing ran. After `accepted`
   the client never falls back. A broken stream exits 1 with `tess: daemon
   request outcome unknown`.

**Lifetime** follows Open Question 2(b). A goroutine reads the
connection. The client never writes after its request, so any read return
means disconnect. Disconnect cancels a request still waiting for
`SerializeCascade`. A running request finishes under `bounded`
(`CascadeTimeout`; a wedged run ends the daemon and launchd restarts it,
as for Hub work). Each frame write has a 1 s deadline. After a missed
deadline, output is dropped and the run continues.

**Profile** follows Open Question 3(c). It reuses B2's
`Profile *atomic.Pointer[config.Profile]` and `LoadProfile`. Before each
request, the daemon reloads the profile if its file's size or mtime
changed. A failed load keeps the old profile and writes a stderr frame.
The client sends its resolved profile path in the request. A mismatch is a
`reject`, so the client runs in-process.

**Savings.** Process start stays (about 4 ms over `true`). The command
skips the profile load, driver set-up and a cold `TopologyState`, and it
gets the bus and the cascade lock. Socket-path resolution (`getconf`,
about 20 ms) now runs on every command. T0 records the net effect.

### Windows and komorebi parity

`wm.Notice`, `wm.Notices`, `wm.AwaitState` and the request protocol are
backend-neutral. The komorebi driver publishes notices from its event
stream, with the window or workspace as the subject. Its confirming
queries are its own state reads. This record designs no komorebi code.

### Alternatives considered

- **Trust the event, skip the confirming query.** Rejected: signals are
  lossy and unordered, and `window_focused` is skipped exactly when it
  matters.
- **A second socket for notices and requests.** Rejected for tess-app's
  reason: the first line already tells connection kinds apart.
- **A `yabai -m signal` subscriber per wait.** Rejected: signals are global
  yabai state, and a crash would leave stale signals.
- **Keep the old placement labels and change only their actions.**
  Rejected: the sketchybar labels already own the `tessera-<event>` names
  for `space_changed` and `display_changed`, so one naming scheme cannot
  cover every event.

## Global Constraints

- Go 1.27, toolchain 1.27.3. No new module dependencies.
- Public repo: no references to private repositories.
- Package graph: `daemon` never imports `cli`. `cmd/tess` is the only
  place that joins them.
- Tests use `testing/synctest` for every timer and `internal/wm/fake` for
  drivers. No test talks to a live yabai.
- A nil `wm.Notices` gives today's polling behaviour at today's
  intervals.
- Budgets and timeout outcomes are exactly those in the sleep table. No
  wait is unbounded.
- The client never falls back after it reads `accepted`. The daemon writes
  `accepted` before any WM call.
- Every socket line except `run 1` and tess-app's `subscribe 1` is read and
  closed at once.
- Signal labels come only from `daemon.SignalInventory` and the sketchybar
  pair. The sketchybar labels never change.

## Plan

### T0 — Bus value gate (owner-run, after T1–T3)

Interfaces: the driver counts yabai calls. `AwaitState` logs at debug
level per wait: site, elapsed, yabai queries run, and outcome.
`TESS_DAEMON_NOTICES=0` in the daemon's environment makes `run` in
`cmd/tess/main.go` skip `SetNotices`. It doubles as a runtime kill switch
for the bus.

Procedure: from the same starting desk state, run `tess wake` 10 times
with notices on and 10 times with them off. Record, per mode, the median
and maximum total wait time per wake, the yabai queries per wake, and the
focus timeouts.

Decision: keep the bus if it adds no failures and either cuts the median
wait time per wake by 20% or more, or cuts yabai queries per wake by 30%
or more. Otherwise remove T2's subscriptions and the notice routes in the
inventory, and run `AwaitState` poll-only. T1, T3, T4 and T5 stand either
way. T5 is a RIG-8692 requirement, not a bus outcome.

### T1 — `wm.Notice` and `wm.AwaitState`

Interfaces, new file `internal/wm/notice.go`:

```go
type Notice struct {
    Event   Event
    Window  int     // 0 when the event names no window
    Space   SpaceID // "" when the event names no Space
    Display string  // "" when the event names no display
}

// Notices delivers notices. A nil Notices means poll only.
type Notices interface {
    // Subscribe returns a channel that receives after each notice match
    // accepts, and a cancel func. Sends never block.
    Subscribe(match func(Notice) bool) (<-chan struct{}, func())
}

type AwaitOptions struct {
    Timeout  time.Duration
    Poll     time.Duration // interval with no notices
    Backstop time.Duration // interval with notices
}

type Await struct{ /* unexported */ }

// Prepare subscribes before the caller sends its command.
func Prepare(n Notices, match func(Notice) bool) *Await
// Close unsubscribes; safe to call twice.
func (a *Await) Close()
// Wait checks at once, on each notice and on each tick, then Closes.
// It returns false with a nil error when Timeout passes.
func (a *Await) Wait(ctx context.Context,
    check func(context.Context) (bool, error), opts AwaitOptions) (bool, error)

func AwaitState(ctx context.Context, n Notices, match func(Notice) bool,
    check func(context.Context) (bool, error), opts AwaitOptions) (bool, error)

var ErrFocusTimeout = errors.New("focus did not reach target space")
```

Gate: `synctest` tests cover confirm-at-once, confirm-on-notice, a lost
notice confirmed by the backstop, a timeout returning false, a nil
`Notices` polling at `Poll`, a notice between `Prepare` and `Wait`, and
`Close` before `Wait` leaving no subscription.

### T2 — Daemon bus, line router, signal inventory

Interfaces:

- `internal/daemon/bus.go`: `type Bus struct`, `func NewBus() *Bus`,
  `func (b *Bus) Publish(wm.Notice)`. `*Bus` implements `wm.Notices`.
- `func ParseNotice(line string) (wm.Notice, bool)` parses `<event>` and
  `<event> <key>=<value>` with keys `window`, `space` and `display`.
- `handle` in `internal/daemon/serve.go` parses each line once. It
  publishes to the bus, then routes to `Hub.Notify` or
  `Hub.NotifyWindowCreated` by event.
- `internal/daemon/signals.go`: `var SignalInventory []SignalSpec`, with
  `type SignalSpec struct { Event wm.Event; Subject string; Owner string }`.
  `RegisterSignals` removes the old labels, then adds the T2-owned rows.
- `wm.EventSource` gains `RemoveSignal(ctx context.Context, label string)
  error`. yabai's not-found failure maps to nil.
- `(*yabai.Driver).SetNotices(n wm.Notices)`. `daemon.Run` builds one
  `Bus` and passes it in before serving, unless `TESS_DAEMON_NOTICES=0`.

Gate: tests for each line form and malformed lines; a full subscriber
never blocks `Publish`; old labels removed before new ones are added; the
sketchybar labels unchanged; one `window_created window=7` line gives
exactly one arrival and one bus notice.

### T3 — Driver waits replace fixed sleeps

Interfaces: `BspStep.Settle int` becomes `BspStep.Await AwaitKind`, with
`AwaitNone` and `AwaitMove`. Move steps carry their table budget in
`BspStep.Budget time.Duration`. A new
`func (d *Driver) awaitOnSpace(ctx context.Context, windowID, index int,
budget time.Duration) error` serves every move row. `SettleUnit` is
removed from `wm.Driver`, `yabai.Driver` and `fake.Driver` (Open Question
6).

Every row of the sleep table is implemented with its budget and timeout
outcome. Gate: `args_test.go` cases updated to `Await`/`Budget`; new
`synctest` cases show each site returns as soon as the fake reports the
state, continues on timeout where the table says so, and polls at today's
interval with a nil bus.

### T4 — Shared `FocusSpace`, Space-first unfloat, anchored placement

Interfaces:

- `(*yabai.Driver).FocusSpace(ctx context.Context, id wm.SpaceID)
  (wm.FocusResult, error)` per A1's signature, now awaited on
  `space_changed` and `display_changed` with the active-Space predicate.
  If A1 has not landed, T4 adds A1's `wm.FocusResult` values and the fake
  method as A1 specifies.
- `unfloatOne` calls `FocusSpace` when the target is not active, then
  focuses the window, within one 2 s budget. On timeout it returns a
  wrapped `wm.ErrFocusTimeout` and logs one diagnostic line.
- `placeWindowOutcome` and `placeArrivalWindowOutcome` in
  `internal/commands/window_placement.go` use the same awaited predicate in
  place of their single focused-Space query. When an anchor exists, they
  move, confirm membership, and then call `StackOnto`.
- Wake routines run the `spawnHold` uptime check before realizing and wait
  out the remainder.

Gate: fake-driver tests for an invisible target, a target visible on
another display but not active, an already-active target (no WM call),
index drift between resolve and focus, cross-Space anchored placement
(the move is confirmed before the stack), a timeout (`errors.Is(err,
wm.ErrFocusTimeout)` plus one log line), and a wake inside the uptime
window.

### T5 — Commands as daemon requests

Depends on Open Questions 1, 2 and 3.

Interfaces:

- `internal/daemon/request.go`:

  ```go
  type Job struct {
      Serialized bool
      Run        func(ctx context.Context, stdout, stderr io.Writer) (int, error)
  }
  // PrepareRequest parses argv; it must not touch the WM.
  type PrepareRequest func(ctx context.Context, argv []string, profilePath string) (Job, error)
  ```

  `Options` gains `PrepareRequest`, plus B2's `Profile
  *atomic.Pointer[config.Profile]` and `LoadProfile func(context.Context)
  (*config.Profile, error)`.
- `handle` routes `run 1 <json>` to `serveRequest`, which owns the
  version check, the `reject`/`accepted`/`exit` frames, disconnect
  detection, queue cancellation, `bounded` and the 1 s write deadline.
- `internal/daemon/client.go`: `func Dispatch(ctx context.Context, sock
  string, argv []string, profilePath string, stdout, stderr io.Writer)
  (code int, accepted bool, err error)`.
- `internal/config`: `func ResolvePath(env func(string) string, home
  string) (string, error)` is split out of `Load`. It returns `""` for the
  embedded default. `Load` calls it.
- `cli.Deps` gains `Remote func(ctx context.Context, argv []string,
  command commands.Command, stdout, stderr io.Writer) (code int, accepted
  bool, err error)`. `cli.Main` calls it after `Parse` for every command
  except `DaemonCommand` and `WakeCommand`, and calls `Run` when
  `accepted` is false and `err` is nil.
- `run` in `cmd/tess/main.go` wires `Remote` to `daemon.Dispatch` and
  `PrepareRequest` to `cli.Parse` plus `commands.Run`. Rebuild commands
  (Open Question 1) get `Serialized: true`.

Gate: tests for dial-failure fallback; fallback on `reject` and on a
frameless close; no fallback after `accepted`; exit-code and output round
trip; a queued request cancelled by disconnect never runs; a running
request survives disconnect; profile reload on change and keep-old on a
broken file; profile-path mismatch rejected; a test table assigning every
`commands.Command` kind to serialized, immediate or not routed; `go test
-race` over concurrent immediate requests and a cascade.

### T6 — Live gate (owner-run)

On the MacBook with yabai and the scripting addition, with the AW and G9:
one desk apply from a cold yabai start; one with a Zen window on an
invisible Space; one with the target Space visible on another display; ten
hotkey runs with the daemon up and ten with it stopped. Pass: no focus
timeout, no stranded floating window, no change in layout from removed
insert, ratio or label waits, and T0's figures recorded in the PR.

## Tasks

- [ ] T0 — Owner-run bus value gate (notices on vs off, 10 wakes each)
- [ ] T1 — `wm.Notice`, `Notices`, `Prepare`/`Close`/`Wait`, `AwaitState`, `ErrFocusTimeout`
- [ ] T2 — `Bus`, `ParseNotice`, `SignalInventory`, old-label removal, `RemoveSignal`, `SetNotices`
- [ ] T3 — Every sleep-table row with its budget; delete `SettleUnit`
- [ ] T4 — Awaited `FocusSpace`, Space-first unfloat, confirmed-move stacking, uptime hold
- [ ] T5 — `run 1` protocol, `Dispatch`, `PrepareRequest`, `ResolvePath`, profile reload
- [ ] T6 — Owner-run live gate on the desk
