# Design: `tess daemon` — resident event subscriber + supervisor

## Problem / Intent

Make tessera a resident process: one long-lived `tess daemon` subscribes to
yabai events once and debounces in-process, replacing today's
spawn-per-signal + file-stamp/mkdir-lock/waiter machinery. Optionally, the
daemon also supervises yabai and skhd as child processes (Matt's ask:
"tess can just start yabai itself as a child process basically and run that
way. can also start skhd"). These are two separable decisions and this record
treats them separately.

Three forces motivate doing this now:

1. **The self-path bug class.** The compiled binary registered its own path
   with yabai via `process.argv[1]`, which for a `bun build --compile`
   standalone is the internal VFS path `/$bunfs/root/tess` — unusable under
   yabai's `sh -c` (undefined `$bunfs` collapses it to `//root/tess`), so
   every event-driven layout silently failed after boot. The interim fix
   (PR #1) threads an explicit path: `tess init` now requires `--self <path>`
   (`src/index.ts:64` — `{ kind: "init"; self: string }`), and `init()`
   registers `[wmPath, "display-event"]` / `[wmPath, "flex-event"]` actions
   (`src/commands.ts:579,588`). A resident subscriber removes per-event
   re-exec entirely: yabai never invokes the tess binary per event, so there
   is no self-path to register and nothing to get wrong.
2. **Q3's trigger conditions are in play.** The frozen architecture record
   deferred the daemon (`docs/design/architecture.md:431-432`: "The event
   model stays spawn-per-signal + background waiter (no resident daemon in
   v1 — Open Questions Q3)") and named the re-open conditions at
   `architecture.md:871-875`: "each signal spawn of a ~50MB bun-compiled
   binary carries real startup I/O the bash+jq scripts don't — if window
   churn ever makes that per-event latency felt, that (alongside 'a second
   event consumer appears') is when the resident daemon earns its supervision
   cost." The compiled binary now exists and every window/display event pays
   that cold-start; the self-path bug is the concrete evidence that
   per-event re-exec of a compiled binary is a fragile model, not just a slow
   one.
3. **Simpler concurrency, already acknowledged.** `architecture.md:545-553`
   concedes the daemon is a "genuinely simpler concurrency model — the
   mkdir-locks, stale-PID reclaim, and stamp files exist only because every
   event spawns a fresh shell."

This record is the "revisit the daemon as its own design" that Q3
(`architecture.md:863-875`) called for. Per the freeze convention,
`architecture.md` is not rewritten; this record supersedes Q3's deferral by
citation. It also partially revisits Q5 (`architecture.md:882-889`, keep skhd
separate) — see Open Questions.

Scope: tessera-side design only — the `tess daemon` command, the
signal→daemon transport, the in-memory debounce, and the supervision model as
tess implements it. The orion-side deployment (launchd plist, retiring the
brew-services model, yabairc rewiring) is a named downstream dependency, not
designed here (it lands with/after the RIG-3082 repoint).

## Approach

### The transport (decision 1): yabai signals notify a daemon-owned unix socket

yabai has no persistent event stream. Its only event surface is
`signal --add event=<e> action=<shell command>` — the driver confirms this:
`WmEventSource.register(event, command)` is documented as "Signal
subscription (`yabai -m signal --add`)" (`src/driver/types.ts:86-89`), and
`yabaiArgs.signalAdd` builds exactly
`["-m", "signal", "--add", "event=<e>", "action=<cmd join ' '>"]`
(`src/driver/yabai.ts:340-349`). So a per-event shell spawn is unavoidable;
the design goal is to make that spawn a near-zero-cost notifier instead of a
~50MB binary cold-start running the full cascade.

**Transport: socket-notify (decided, D-T1).** Matt chose the socket-notify
design over the stamp-write-and-poll alternative for its sub-second latency;
see Resolved decisions. This section specifies it in full. (The rejected
alternative is retained under Alternatives considered for the record.)

The daemon owns a unix stream socket (via `Bun.listen({ unix })`) at
`$TMPDIR/tessera-daemon.sock` — `$TMPDIR` is per-user mode-0700, so an
arbitrary local process cannot connect and spam `wake` (each `wake` is a full
reclaim cascade); `/tmp` would be world-writable. At daemon startup (and on
yabai restart — see wake below) the daemon registers each placement signal
with a thin shell one-liner action:

```text
echo <event> | /usr/bin/nc -U -w 1 $TMPDIR/tessera-daemon.sock
```

- `/usr/bin/nc` ships with macOS (incl. Tahoe 26 — unlike `telnet`, it was
  never removed) and supports `-U` (unix sockets); `-w 1` bounds the idle so
  a dead daemon never wedges yabai's action spawn. Note BSD `nc` does **not**
  exit on stdin EOF — it lingers until the peer closes or `-w 1` expires — so
  the daemon MUST read-then-close each connection promptly (T2), or every
  action spawn lingers ~1s.
- The action contains **no tess binary path at all** — the `--self` concern
  is eliminated for the event path, not just patched (see below).
- Message protocol is one event name per connection, newline-terminated.
  No framing, no JSON: the daemon only needs to know *which debounce channel*
  to stamp, exactly as `recordEvent` only writes an epoch second today
  (`src/effects/debounce.ts:70-75`). Ordering/loss under a display storm is
  consequence-free: the daemon only stamps "now" per channel, so interleaved
  or even dropped messages mid-burst are absorbed by the debounce (a later
  event in the burst re-stamps).
- Socket lifecycle: the daemon unlinks the socket on clean shutdown; the
  stale-file unlink-and-rebind path (single-instance, below) relies on the
  launchd label to single-instance the agent so a manual `tess daemon` and
  the launchd agent do not race the unlink/rebind. **launchd socket
  activation** (launchd owns the socket, queues connections while the daemon
  is down) would remove the stale-file dance and most of #5's dropped-event
  window, but is rejected: Bun exposes no `launch_activate_socket` API, so
  inheriting the FD needs FFI or a fragile fd-number convention.
- Registration uses yabai signal labels (`label=tessera-<event>`), with
  remove-then-add so re-registration is idempotent across daemon restarts.
  `signalAdd` grows an optional label (see Plan T3).

Event flow, end to end:

```mermaid
flowchart LR
    Y[yabai event] -->|sh -c: echo e ! nc -U sock| S[unix socket]
    S --> D[tess daemon: stamp channel in memory]
    D --> W[in-process waiter: isQuiet / isFlexQuiet]
    W -->|quiet| C[cascade / converge via YabaiDriver]
```

The `space_changed`/`display_changed` → sketchybar signals
(`src/commands.ts:591-597`) stay registered directly to
`SKETCHYBAR_PATH --trigger yabai_spaces_changed` — they never involved tess
at runtime and there is no reason to proxy them through the daemon. Likewise
`dock_did_restart` → `sudo yabai --load-sa` stays in yabairc (it is yabai's
own scripting-addition reload, root-adjacent, out of tess's lane).

**Yabai-restart wake:** signals live in yabai's runtime state and are lost
when yabai restarts, which is why yabairc runs `tess init` today. Under the
daemon, yabairc's tessera line becomes `tess wake` — a thin CLI that connects
to the daemon socket and sends `wake`. On `wake` (and on its own startup) the
daemon re-registers the signals and runs the startup reclaim cascade
(displaySetup → rules → laptop-or-apply, the same body as today's `init`,
`src/commands.ts:600-610`). yabairc invoking `tess` by name is safe — it is a
real shell script resolving via PATH/absolute path, not a
`process.argv[1]`-self-registration; the `$bunfs` bug cannot recur there.

**`--self` is eliminated.** With no tess re-exec in any signal action, the
`--self <path>` plumbing from the interim fix (`init --self PATH` in
`src/index.ts:64,274-277`, `wmPath` in `src/commands.ts:561-598`) is deleted
along with the `init`, `display-event`, and `flex-event` subcommands the
daemon replaces. This is a clean cutover, not a deprecation.

### In-memory debounce (preserving H2 + H7)

The pure predicates survive verbatim. `isQuiet(now, stamp, quietSecs)`
(`src/effects/debounce.ts:43-49`) and `isFlexQuiet(now, ownStamp,
displayStamp)` (`debounce.ts:59-68`, the H7 gate: "the flex rebuild fires
only once BOTH its own stamp is quiet for `FLEX_QUIET_SECS` AND the display
stamp is quiet for `DISPLAY_QUIET_SECS`") are pure over `(now, stamp)` and
are untouched.

What changes is where stamps live. Today `runWaiter` reads the stamp file
directly in its poll (`readStamp(config.stampPath) ?? 0`,
`debounce.ts:144-147,162`) and takes a cross-process mkdir waiter-lock
(`acquireLock(config.waiterLock)`, `debounce.ts:134`). In the daemon:

- **Stamps become in-memory epoch seconds** behind a small `StampStore`
  interface (`read/write`), with the current file implementation kept for the
  cross-process paths and a trivial `MemoryStamps` for the daemon. The
  load-bearing invariant is preserved structurally: `runWaiter` already
  captures `actedOn` INSIDE the quiet poll and never re-reads it after the
  break (`debounce.ts:9-17,140-148` — "capturing here, before `work`, is the
  load-bearing invariant"); swapping the store does not touch that ordering.
- **Waiter locks disappear.** `DISPLAY_WAITER_LOCK`/`FLEX_WAITER_LOCK` exist
  only to elect one waiter among concurrent event spawns
  (`src/effects/locks.ts:1-11`). The daemon is a single process with a single
  event loop: "one waiter per channel" is a boolean per channel, not a
  filesystem lock. Stale-PID reclaim for waiters is deleted wholesale.
- **H2 stays cross-process and file-based.** The TTL signal guard
  (`src/effects/guard.ts:1-6`: "a manual `tess apply` … holds the guard …
  the auto flex waiter reads it and re-waits rather than converge on top of
  the in-flight rearrange (H2). It self-expires after GUARD_TTL seconds")
  guards against *manual* skhd-invoked `tess apply`/`tess laptop`, which
  remain separate short-lived processes. The daemon's flex waiter therefore
  keeps consulting `signalsSuppressed(SIGNAL_GUARD)` on the real `/tmp` path,
  and the `APPLY_LOCK`/`LAPTOP_LOCK` mkdir locks stay for the same reason —
  they serialize the daemon's cascade against manual CLI runs. Only the
  *debounce* machinery (event stamps + waiter locks) moves in-memory; the
  *cross-process coordination* machinery (guard, apply/laptop locks) stays on
  disk unchanged.
- **H2's re-stamp-on-contention** (`debounce.ts:252-263`: guard held →
  `"restamp"`, contended converge → `"restamp"`) maps to writing "now" into
  the in-memory stamp — same `WaiterStep` protocol, same semantics.
- **H7 keeps one clock and one constant.** Both channels' stamps live in the
  same process sharing `DISPLAY_QUIET_SECS`/`FLEX_QUIET_SECS` from
  `src/effects/constants.ts:56-59`, so the "flex gates on the display window
  (3s), not the shorter flex window (2s)" rule (`debounce.ts:55-57`) holds by
  construction.

A daemon crash mid-cascade is already survivable by design: the guard
self-expires (`GUARD_TTL_SECS = 8`, `constants.ts:50`), the mkdir locks have
stale-PID reclaim (`locks.ts:7-11`), and on restart the daemon runs the
startup reclaim cascade, reconverging whatever events were lost while it was
down.

### Supervision (decision 2): launchd-bootstrap — tess starts them, launchd owns them

Chosen model (Matt, D-T2): **`tess daemon` is a launchd-managed user agent
with `KeepAlive` that `launchctl bootstrap`s the yabai + skhd launchd
definitions at its own startup** (and `bootout`s them on clean shutdown). So
"tess starts yabai and skhd" holds as observable behavior — tess is the one
thing launched — while launchd still owns each process's restart/`KeepAlive`
and `sudo yabai --load-sa` stays in yabairc. tess bootstraps; launchd
supervises. Rationale, addressing the frozen record's "long-lived process to
die silently" worry (`architecture.md:551`) head-on:

- **Restart-on-crash stays launchd's core competency.** A hand-rolled
  supervisor inside tess would re-implement KeepAlive, throttling, and log
  capture, worse. Bootstrapping hands launchd the definitions but keeps it
  the supervisor: silent death of any of the three → launchd restarts it. The
  daemon's own restart re-runs the startup reclaim cascade and re-bootstraps
  (idempotent: bootstrap of an already-loaded label is a no-op / handled).
- **No `Bun.spawn` parenting.** tess does not become the parent process of
  yabai/skhd. Full parenting was rejected (Resolved decisions): reparent-on-
  crash means a restarted tess would spawn a *second* yabai unless it grew
  pidfile-adoption/kill-on-start logic (get it wrong → the WM double-manages
  every window), and it would pull `sudo --load-sa` into tess. Bootstrapping
  gives the "one thing to launch" UX without any of that: launchd, not tess,
  is each process's parent.
- **`sudo yabai --load-sa` stays in yabairc.** The scripting-addition load
  needs root; bootstrapping the yabai *service* does not change where the SA
  load lives. tess never runs sudo.
- **Separability.** Decision 1 (resident events) delivers the entire bug-kill
  and latency win independently; the bootstrap step is additive and can be
  dropped back to plain subscribe-only without reopening the transport design.

**Crash vs wedge.** `KeepAlive` covers the *crash* half of "die silently"
well. It does not cover a *wedge*: a daemon whose process is alive but whose
event loop is stuck (a hung `await` in a cascade against a wedged yabai, a
driver call that never resolves) keeps the socket bound and converts every
future event into a silent no-op indefinitely, while launchd reports a
healthy service. This is strictly worse than today, where a wedged waiter
self-heals — its mkdir lock has stale-PID reclaim (`src/effects/locks.ts:7-11`)
and the guard has an 8s TTL, so the *next* per-event spawn elects a fresh
waiter. The daemon deletes that per-event re-election, so the wedge must be
handled explicitly. Chosen defense: run each cascade under a generous timeout
(the driver already models yabai-gone as a throwing query, so a stuck driver
call becomes a caught error → the daemon exits → `KeepAlive` restarts it,
converting an invisible wedge into a visible crash it *can* see). A
`launchctl`-scheduled `tess wake --probe` liveness check (expect a pong,
`kickstart -k` on timeout) is a cheap belt-and-braces alternative if the
in-loop timeout proves insufficient (Open Questions #3).

**Single-instance enforcement:** binding the unix socket is the lock. On
`EADDRINUSE` the starting daemon probes the socket with a `wake` connect: a
live daemon answers (exit 0, "already running"); a dead one leaves a stale
file, which is unlinked and rebound. No pidfile.

If tess crashes, yabai and skhd are unaffected (they are siblings); events
fired while the daemon is down are dropped at the `nc -w 1` timeout, and the
restarted daemon's reclaim cascade converges the end state — the same
eventual-consistency story the stamp files gave a SIGKILLed waiter.

## Alternatives considered

- **Status quo: spawn-per-signal + file stamps/locks.** The v1 decision
  (`architecture.md:549-553`): "battle-tested … crash-free-by-construction
  (no long-lived process to die silently)." Rejected *now* because both Q3
  triggers have fired in spirit: the compiled binary made per-event spawn
  expensive (`architecture.md:871-875`) and outright broken once
  (`/$bunfs/root/tess`). The `--self` fix patches the symptom; the daemon
  removes the mechanism that produced it. The crash-free-by-construction
  property is traded for launchd `KeepAlive` plus a reclaim-on-start
  cascade — a recovery story the stamp protocol already needed anyway for
  SIGKILLed waiters.
- **Transport: keep `tess notify <event>` as the signal action** (a thin
  tess subcommand writing to the socket). Rejected: it re-introduces a tess
  binary path inside a yabai signal action, so the `--self` plumbing and the
  whole self-path bug class survive. The only argument for it — not
  depending on `nc` — is weak: `/usr/bin/nc` is part of macOS, and if it
  ever vanished the fallback is reinstating a tiny notifier binary or, better,
  switching to the stamp-write-and-poll transport (#1(b)), whose action is a
  plain `date +%s > <stamp>` needing no `nc` at all. (There is no `sh`/`bash`
  unix-socket redirect analogous to `/dev/tcp` — a `bash` socket fallback
  does not exist for unix-domain sockets.)
- **Transport: named pipe (FIFO) instead of unix socket.** `mkfifo` +
  `echo event > pipe` needs no `nc`. Rejected: a FIFO writer blocks forever
  when no reader is attached (a dead daemon wedges every yabai action spawn
  until cleanup), open/close semantics around multiple concurrent writers
  are subtler than accept-per-connection, and Bun has first-class unix
  socket support (`Bun.listen({ unix })`) but no FIFO primitive beyond raw
  `node:fs` reads. The socket's connect-refused failure mode (bounded by
  `-w 1`) is strictly better.
- **Transport: `fs.watch` on the existing stamp files.** Signals keep
  writing stamps; the daemon `fs.watch`es `/tmp`. Rejected specifically as a
  *watch*: macOS FSEvents adds coalescing/latency semantics into the debounce
  path. But its close cousin — the daemon *polling* the stamp files at the 1s
  cadence `runWaiter` already uses — is not dismissed; it is a live transport
  contender (Open Questions #1, option (b)) that costs a stamp-write shell
  action equal to the `nc` notifier while deleting the socket machinery.
- **Transport: poll yabai state.** No events at all; the daemon polls
  `yabai -m query` on an interval. Rejected: trades event latency for
  constant background query load, and diffing world snapshots to infer
  "window_created" re-implements the event surface badly. (Distinct from
  #1(b), which polls cheap stamp files an event handler writes — not yabai.)
- **A richer yabai event API (SA/socket).** Verified against the driver:
  the only event surface tessera models is `WmEventSource.register` →
  `signal --add` (`src/driver/types.ts:86-89`, `src/driver/yabai.ts:340-349`).
  yabai's message socket is command/query, not a subscription stream; the
  scripting addition adds window-manipulation capability, not events. No
  such API exists to build on.
- **Full parent supervision (Matt's literal ask): `tess daemon` spawns
  yabai + skhd via `Bun.spawn`.** Genuinely attractive properties: one
  process tree, tess controls yabai's config/restart ordering, `yabairc`
  could shrink toward nothing. Rejected (as the default; see Open
  Questions): it re-implements launchd restart/throttle logic; a tess crash
  orphans or (with kill-on-exit) takes down the whole WM where today it
  costs nothing; orphan adoption on restart is a hard correctness problem
  (double-yabai = double-managed windows); and the `sudo --load-sa` root
  interaction lands in tess's lap. The frozen record's daemon worry was
  silent death (`architecture.md:551`) — parenting *amplifies* the blast
  radius of exactly that worry. Subscribe-only captures the full event-model
  win; parenting can be a later record if config-ownership consolidation
  (post-RIG-3082) makes it earn its keep.
- **skhd as a tess child.** Q5 (`architecture.md:882-889`) recommended
  keeping skhd "a separate concern dispatching to `tess`". Nothing in the
  daemon changes skhd's relationship to tess — skhd invokes `tess`
  subcommands as short-lived CLI processes either way (those commands do not
  route through the daemon). Parenting skhd would be supervision for its own
  sake. Keep Q5's answer.

## Global Constraints

- **Runtime/tooling:** Bun; TypeScript strict; Biome; `bun:test`. Zero npm
  dependencies (Bun builtins + `node:` core only). Packaging stays a plain
  `bun build --compile` single binary — the daemon is a subcommand of the
  same `tess` binary, not a second artifact.
- **Layering is law** (inherited from `architecture.md:565-568`): `engine/`
  stays pure; the daemon lives in `effects/`/CLI layers; all yabai contact
  goes through `WmDriver`.
- **H2 + H7 semantics are preserved, not approximated.** The pure predicates
  `isQuiet`/`isFlexQuiet` (`src/effects/debounce.ts:43-68`) survive
  unchanged; the `actedOn`-captured-inside-the-poll invariant
  (`debounce.ts:9-17`) survives structurally in `runWaiter`; the TTL signal
  guard and the apply/laptop mkdir locks stay file-based on the same `/tmp`
  paths (`src/effects/constants.ts:32-45`) because manual CLI runs remain
  separate processes.
- **No behavior change to manual commands.** `tess apply`, `tess laptop`,
  and every skhd-dispatched subcommand keep their current process model and
  exit-code contract (`src/index.ts:12-15`).
- **Clean cutover:** `init`, `display-event`, `flex-event`, and the
  `--self` plumbing are deleted in the same series that lands the daemon —
  no dual event paths left running.
- **VCS:** jj + jj-vine, one PR per task slice; this record frozen on merge.

## Plan

Sources under `src/`, tests colocated `*.test.ts`, `src/index.ts` the CLI
entry — same tree as the architecture record.

### T1 — Stamp store abstraction (in-memory debounce substrate)

**Contingent on Open Questions #1.** This task exists only under the
socket-notify transport (a), where event stamps move in-memory. If #1
resolves to stamp-write-and-poll (b), stamps stay file-based, `debounce.ts`
is nearly untouched, and T1 collapses to nothing (the daemon reads the same
`fileStamps()` the signal actions write).

Extract the stamp read/write behind an interface so `runWaiter` and the flex
H7 gate can run against memory. The file implementation keeps byte-identical
behavior for any remaining file-stamp callers during the series; the memory
implementation is the daemon's.

- `src/effects/stamp.ts` grows the interface + memory impl;
  `src/effects/debounce.ts`'s `RunWaiterConfig` takes a `stamps: StampStore`
  (replacing direct `readStamp(config.stampPath)` at `debounce.ts:144-147,162`)
  and `WaiterDeps.stamp` routes through the same store.
- The `actedOn`-inside-the-poll capture (`debounce.ts:140-148`) is untouched;
  a test asserts the capture ordering against a scripted `MemoryStamps` that
  mutates between poll and work.

Interfaces:

```typescript
/** Epoch-second stamp channel; null = never stamped. */
export interface StampStore {
  read(key: string): number | null;
  write(key: string, epochSecs?: number): void;
}

/** File-backed store: key IS the path (today's /tmp protocol, verbatim). */
export function fileStamps(): StampStore;

/** In-process store for the daemon; a plain Map<string, number>. */
export function memoryStamps(): StampStore;
```

Test cycle: `bun test src/effects/stamp.test.ts src/effects/debounce.test.ts`
— existing debounce tests pass unmodified against `fileStamps()`; new tests
cover `memoryStamps()` and the capture-ordering invariant.

### T2 — Daemon core: socket listener + in-process event channels

The resident loop: own the unix socket, parse one-line event messages, stamp
the matching in-memory channel, and ensure exactly one waiter per channel is
live (the in-process replacement for `DISPLAY_WAITER_LOCK`/
`FLEX_WAITER_LOCK`). Pure daemon plumbing — no yabai registration yet
(that is T3), so this slice is fully testable against a fake socket dir and
stub waiters.

- New `src/daemon/core.ts`. Socket path constant
  (`/tmp/tessera-daemon.sock`) joins `src/effects/constants.ts`.
- Message grammar: `<event>\n` where `<event>` is a `WmEvent`
  (`src/driver/types.ts:91-101`) or `wake`; anything else is logged and
  dropped.
- Display trio → display channel; the four flex events → flex channel
  (the same routing `init()` wires today, `src/commands.ts:574-589`).
- Waiter election: per channel, if no waiter is running, start
  `runDisplayWaiter`/`runFlexWaiter` (with `stamps: memoryStamps`, no
  waiter-lock) and clear the flag when it settles; if one is running, the
  fresh stamp is enough — it will be observed, mirroring today's
  "live waiter holds it; it will observe our stamp" (`debounce.ts:136`).
- Single-instance: bind; on `EADDRINUSE`, probe-connect; live daemon →
  exit 0 with a message, stale socket file → unlink and rebind.
- H2 unchanged: the flex waiter keeps `guardPath: SIGNAL_GUARD` (file) and
  the converge keeps `LAPTOP_LOCK`.

Interfaces:

```typescript
/** One debounce channel's wiring. */
export interface ChannelSpec {
  events: ReadonlyArray<WmEvent>;
  /** Starts the channel's waiter against the shared store; resolves when
   *  settled. */
  runWaiter(stamps: StampStore): Promise<void>;
}

export interface DaemonCoreOptions {
  socketPath: string;
  channels: ReadonlyArray<ChannelSpec>;
  /** Called on "wake" messages and at startup (T3 wires registration +
   *  reclaim here). */
  onWake(): Promise<void>;
}

/** Bind, serve, and dispatch until stop() — resolves on clean shutdown.
 *  Returns "already-running" without binding when a live daemon answers. */
export function startDaemonCore(
  options: DaemonCoreOptions,
): Promise<{ kind: "started"; stop(): Promise<void> } | { kind: "already-running" }>;
```

Test cycle: `bun test src/daemon/core.test.ts` — fake channels + tmpdir
socket: routing, waiter election (burst of N events → one waiter run),
wake dispatch, single-instance (second start → `already-running`), stale
socket reclaim.

### T3 — Signal registration with labels + the notifier action

Teach the driver labeled, idempotent signal registration and build the
`nc` one-liner actions.

- `yabaiArgs.signalAdd` (`src/driver/yabai.ts:340-349`) grows an optional
  `label`; add `signalRemove(label)` (`signal --remove <label>`).
  `WmEventSource` (`src/driver/types.ts:87-89`) grows the same. **This is
  also where the argv pretense dies:** `yabai.ts:349` already joins the
  `command: string[]` into a single `action=<cmd join ' '>` shell string, and
  the notifier action is genuinely shell text (a pipe, `echo … | nc …`), not
  argv. So `register` takes `action: string`, not `command: string[]` — a
  joined array and a string are equal today, but the array shape falsely
  invites a caller to assume quoting/escaping is handled. The fake driver
  mirrors the new shape.
- New `src/daemon/register.ts`: for each placement event, remove-then-add
  `label=tessera-<event>`, `action=echo <event> | /usr/bin/nc -U -w 1
  <socketPath>`. The sketchybar pair keeps its current direct registration
  (`src/commands.ts:591-597`), moved here from `init()`.

Interfaces:

```typescript
// driver/types.ts
export interface WmEventSource {
  register(event: WmEvent, action: string, label?: string): Promise<void>;
  unregister(label: string): Promise<void>;
}

// daemon/register.ts
/** Idempotently (re)wire all tessera + sketchybar signals to notify
 *  `socketPath`. */
export function registerSignals(
  driver: WmDriver,
  socketPath: string,
): Promise<void>;
```

Test cycle: `bun test src/driver/yabai.test.ts src/driver/fake.test.ts
src/daemon/register.test.ts` — argv goldens for labeled add/remove; register
against the FakeDriver asserts the exact action strings (including that no
action contains a tess binary path).

### T4 — `tess daemon` + `tess wake` CLI; delete the spawn-per-event surface

Wire the daemon into the router and cut over.

- `parseArgs`/`run` (`src/index.ts:127,321`) gain
  `{ kind: "daemon" }` and `{ kind: "wake" }`. `daemon` composes T2 core +
  T3 registration + the startup reclaim cascade (the `init()` body minus
  signal wiring: displaySetup → rules → laptop-or-apply,
  `src/commands.ts:600-610`) as `onWake`. `wake` connects to the socket,
  sends `wake\n`, exits 0 (or nonzero with "daemon not running" on
  connect-refused). This is only as "loud" as anyone reading yabai's log:
  yabairc has no `set -e`, so a failed `tess wake` line does not halt startup
  and the user-visible symptom is nothing — but the window self-heals
  (launchd restarts the daemon, whose own startup re-registers signals and
  runs the reclaim cascade). Signal registrations also survive a *daemon*
  restart within one yabai lifetime (they point at the socket path, not a
  process), so startup re-registration is idempotent belt-and-braces — which
  is why remove-then-add matters.
- Delete `{ kind: "init"; self }`, `{ kind: "display-event" }`,
  `{ kind: "flex-event" }` (`src/index.ts:60-64,361-382`), `init()`
  (`src/commands.ts:561-610`), `recordEvent`'s file-path call sites, the
  waiter-lock constants, and the `--self` usage text
  (`src/index.ts:274-277`). `runDisplayCascade`/`runFlexConverge` stay —
  they are the daemon's work callbacks.
- Manual commands (`apply`, `laptop`, all skhd dispatch) untouched.
- **launchd-bootstrap (D-T2):** on startup the daemon `launchctl bootstrap`s
  the yabai + skhd launchd labels (idempotent — an already-loaded label is a
  no-op), and `bootout`s them on clean shutdown (SIGTERM). This is the
  "tess starts yabai and skhd" behavior; launchd remains their supervisor.
  The label set + plist paths come from constants; the actual plist authoring
  is orion's (T5). A `--no-bootstrap` flag keeps the dev-machine `bun run
  src/index.ts daemon` smoke path from touching the live launchd domain.

Interfaces:

```typescript
export type Command =
  | { kind: "daemon" }
  | { kind: "wake" }
  /* … existing manual/keybind kinds; init/display-event/flex-event removed */;

/** The daemon subcommand body: single-instance bind, signal registration,
 *  reclaim cascade, then serve until SIGTERM/SIGINT. Returns the exit code. */
export function runDaemon(driver: WmDriver, profile: Profile): Promise<number>;
```

Test cycle: `bun test src/index.test.ts src/commands.test.ts` — parse
goldens for `daemon`/`wake`, removal asserted (old kinds now parse errors);
smoke: `bun run src/index.ts daemon` on the dev machine, plug/unplug +
window-churn bursts, observe single debounced cascade per burst; kill -9 the
daemon, relaunch, observe reclaim.

### T5 — Downstream (orion, not designed here)

Named dependency only. Under the launchd-bootstrap decision (D-T2), orion:

- Ships the `tess daemon` launchd plist (`KeepAlive`, user agent), the one
  agent enabled at login.
- Authors the yabai + skhd launchd definitions that `tess daemon`
  `bootstrap`s — the labels/plist paths tess references as constants (T4).
  These replace the brew-services invocations; brew still installs the
  binaries, launchd (via tess) runs them.
- Rewires `yabairc`'s tessera line from `tess init --self …` to `tess wake`,
  and drops the brew-services model.

Coordination: this series lands after the repoint PR (#2029) merges, so there
is one yabairc to rewire, in orion, against tessera-as-source-of-truth.

## Tasks

- [ ] T1 — `StampStore` (+ memory impl); `runWaiter` on the store; capture
  invariant test
- [ ] T2 — daemon core: socket, channels, in-process waiter election,
  single-instance
- [ ] T3 — labeled signal add/remove in driver; `registerSignals` with `nc`
  actions
- [ ] T4 — `tess daemon`/`tess wake` CLI; delete
  `init`/`display-event`/`flex-event`/`--self`; live smoke
- [ ] T5 — (downstream, orion) `tess daemon` launchd plist + yabai/skhd
  launchd definitions tess bootstraps + yabairc rewire + brew-services retire

## Resolved decisions

The two load-bearing forks were ruled by Matt at design-review time and are
folded here as decisions; the Approach above reflects them.

1. **Transport → socket-notify (D-T1).** The signal action is
   `echo <event> | /usr/bin/nc -U -w 1 $TMPDIR/tessera-daemon.sock`; the
   daemon owns a unix socket and stamps in memory (the Approach's transport
   section). Chosen over stamp-write-and-poll for its sub-second latency (no
   1s poll floor). This keeps the socket lifecycle, the `/usr/bin/nc`
   dependency, the one-line message grammar, and T1's `StampStore`
   abstraction (all in the Plan). The `tess notify`-subcommand variant is
   rejected — it re-introduces a tess path in the action and resurrects the
   self-path bug. `--self` still dies either way (D-S1 below).
2. **Supervision → launchd-bootstrap (D-T2).** `tess daemon` runs
   `launchctl bootstrap` on the yabai + skhd launchd definitions at its own
   startup, so "tess starts yabai and skhd" holds as observable behavior
   (tess is the one thing launched) — but launchd still owns each process's
   restart/`KeepAlive`, and `sudo yabai --load-sa` stays in yabairc. Chosen
   over subscribe-only (which does not literally start the peers) and over
   full `Bun.spawn` parenting (rejected: a tess crash orphans/kills the WM,
   it re-implements launchd restart/throttle, and it pulls the `--load-sa`
   root step into tess — parenting amplifies the silent-death worry at
   `architecture.md:551`). tess bootstraps; launchd supervises. This adds a
   bootstrap/bootout step to the `tess daemon` startup/shutdown (Plan T4) and
   an orion-side task to author the yabai+skhd launchd definitions tess
   bootstraps (Plan T5). skhd is bootstrapped, not parented — Q5's
   sibling-service relationship (`architecture.md:882-889`) is otherwise
   unchanged; the daemon still routes no skhd keybind through itself.
3. **`--self` is eliminated (D-S1, follows from D-T1).** No signal action
   references a tess binary path; `init --self` is deleted with the `init`
   subcommand; yabairc calls `tess wake` by PATH/absolute path as a normal
   shell command, where `$bunfs` cannot occur.

## Open Questions

Only non-load-bearing deferrals remain — the record is correct without them;
each is an optional later refinement, ratified as deferred by the merge.

1. **Dropped events while the daemon is down** *(non-load-bearing)*.
   Accept the loss: `nc -w 1` bounds the cost, launchd restarts the daemon
   promptly, and the reclaim-on-start cascade converges the end state — the
   same recovery the stamp protocol offered a SIGKILLed waiter. The
   alternative (spooling events to disk) rebuilds the stamp files.
2. **Landing order vs RIG-3082 repoint** *(non-load-bearing, coordination)*.
   Land after the repoint merges so yabairc is rewired once, in orion,
   against tessera-as-source-of-truth.
3. **Wedge watchdog** *(non-load-bearing — recovery detail)*. `KeepAlive`
   restarts a *crashed* daemon but cannot see a *wedged* one (alive process,
   stuck event loop). Default to the in-loop cascade timeout (a stuck driver
   call → caught error → exit → `KeepAlive` restart, turning an invisible
   wedge into a visible crash). A scheduled `tess wake --probe` +
   `launchctl kickstart -k` is the alternative if the in-loop timeout proves
   insufficient; either is ~20 lines. Revisit only if wedges are observed in
   practice.
