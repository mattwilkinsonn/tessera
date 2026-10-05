# Design — Rewrite `tess` in Go, with the resident daemon

*Design record for RIG-4487, revised by Matt's rulings on RIG-4526. It
supersedes `effect4-cli.md`, builds `resident-daemon.md` in Go, and replaces
the TS profile with a typed CUE profile.*

## Problem / Intent

Every skhd keybind spawns `tess`, and a `bun build --compile` binary spends
about 19 ms starting its runtime (the base row in `effect4-cli.md` §
"Startup-latency budget"). Matt chose Go. This record rewrites `tess` as
idiomatic Go and does not keep byte-level compatibility with the TS build.
In the same change it moves event handling into the resident daemon from
`resident-daemon.md`, and gives the profile a typed language.

## Approach

**Re-realize, not port.** The TS source states the behaviour. The Go code
is native, and the behaviour tests are ported case for case. Anything that
exists only to mirror the TS shape is deleted: the two CLI front ends, the
Effect error classes, `RunOpts` injection, and the file-stamp waiter
protocol, which the daemon replaces.

### Module layout

The module path is `github.com/mattwilkinsonn/tessera`. The layering law in
`architecture.md` § "Global Constraints" becomes these import rules:

- `config`, `wm` and `effects` import no internal package.
- `engine` imports `config` and `wm`.
- `executor` imports `engine` and `wm`.
- `commands` imports all of the above.
- `daemon` imports `commands`, `effects` and `wm`.
- `cli` imports `commands` and `daemon`.
- Only `cmd/tess` imports `wm/yabai`.

| TS source | Go package |
| --- | --- |
| `src/config/*.ts`, `loadProfile` | `internal/config` (embeds `schema.cue`, `default.cue`) |
| `src/config/profile.fixture.ts` | `internal/config/configtest` |
| `src/driver/types.ts`, `src/engine/world.ts` | `internal/wm` |
| `src/driver/yabai.ts`, `src/driver/fake.ts` | `internal/wm/yabai`, `internal/wm/fake` |
| `src/engine/*.ts` except `world.ts` | `internal/engine` |
| `src/exec.ts` | `internal/executor` |
| `src/effects/*.ts` | `internal/effects` |
| `src/commands.ts`, `run` in `src/index.ts` | `internal/commands` |
| new | `internal/daemon` |
| `src/cli/*.ts`, the entry guard | `internal/cli`, `cmd/tess` |

Every TS union becomes a sealed interface marked `//sumtype:decl` and
checked by `gochecksumtype`. `engine.Op` covers the `PlanOp` variants.
`engine.ConvergeAction` nests inside it (`interface{ Op; convergeAction() }`)
so the converger can only emit converge actions. `commands.Command` has one
struct per kind.

### Driver contract in Go

`wm.Driver` matches `WmDriver` (`src/driver/types.ts`) method for method:

- **Context and errors.** Each method takes `ctx` first and returns an
  error, so `Promise<boolean>` becomes `(bool, error)`.
- **Capabilities.** The optional `rules` and `events` become `wm.RuleOps`
  and `wm.EventSource`, found by type assertion.
- **Settle.** `settleMs` becomes `SettleUnit() time.Duration`.

`wm.Snapshot` runs its three queries under errgroup. The yabai runner uses
`exec.CommandContext`, so a cancelled context stops a running yabai child.

**Labelled signals.** `EventSource.AddSignal` takes a label and a shell
action string. yabai already replaces a signal that has the same label:
`event_signal_add` in yabai's `src/event_signal.c` runs
`if (signal->label) event_signal_remove(signal->label);` before
`buf_push`. One labelled add is therefore idempotent. The frozen record's
remove-then-add step is not needed.

### CLI

The CLI uses only the standard library. It is a table-driven dispatcher,
with one `flag.FlagSet` per subcommand. **Why no library:** after `--self`
goes, the only flag left is `daemon --no-bootstrap`, and each subcommand
takes at most one positional. cobra, urfave/cli and kong would buy no
abstraction this table lacks, which the house "stdlib-first" rule forbids.

- **Kinds.** 21 in total:
  - the 19 manual and keybind kinds of today's `SUBCOMMANDS` (all of them
    except `display-event` and `flex-event`);
  - `daemon`;
  - `wake`.
- **Help.** `tess --help` lists the table. `tess <cmd> --help` prints that
  command's usage and choices. Both exit 0.
- **Errors.** A parse failure prints `tess: <message>` and one usage line
  on stderr. The program writes its output to an injected `io.Writer`.
  Diagnostics go through `log/slog`.

### Profile format

The profile is **CUE**, loaded in-process by `cuelang.org/go` v0.17.1. It
gives the types Matt asked for: required fields, closed structs, enums, RE2
regex validity from the engine Go matches with, and most cross-field rules.
It needs no runtime binary and no cgo.

**Schema.** `internal/config/schema.cue` is `package tessera`. It defines
`#Profile` and embeds it at file level, so any `package tessera` file that is
vetted beside it is checked as a profile. Its shape follows
`src/config/types.ts`:

- `displays!: {g9!, aw!, laptop!: #Display}`, where `#Display` is
  `{width!: int & >0, weights?: #WeightDefaults}`.
- `windows!: [string]: #WindowSpec`. A `#WindowSpec` is:
  - `app!: #Pattern`
  - `title?: #Pattern`
  - `titleInvert?: bool`
  - `spawn?: [string, ...string]`

  Here `#Pattern` is `string & regexp.Valid`.
- `desk!: [...#DeskLayout]` and `topologies?: [...#Topology]`.
- `weights?: #WeightDefaults`, plus `deskSlots!`, `laptopPinned!` and
  `laptopStackApps!` (a list).
- The cross-field rules move into the schema, in the form probed (below):
  - `weights?: #Weights & list.MinItems(len(tracks)) & list.MaxItems(len(tracks))`;
  - `if kind == "stack" { tracks!: [_]; weights?: _|_ }`;
  - weight-table keys must match `^([2-9]|[1-9][0-9]+)$`, and the vector
    length must equal the key;
  - a topology's `desk` displays must be unique, and must equal its
    `displays` set.

**What stays in Go.** `config.Validate` keeps the one rule CUE expresses
badly: chain ratios between 0.1 and 0.9, over every subsequence
(`chainRatios`, `src/config/validate.ts`). It adds one typed-reference rule:
every name in `tracks`, `deskSlots` and `laptopPinned` must be a key of
`windows`. A typo there silently claims nothing today.

**Loader.**

```go
func Load(ctx context.Context, env func(string) string, home string) (*Profile, error)
```

1. `Load` resolves `$TESSERA_PROFILE`, then
   `${XDG_CONFIG_HOME:-~/.config}/tessera/profile.cue`, then the embedded
   `default.cue`. A missing well-known file falls through. A broken one is
   an error. If `profile.ts` exists there and `profile.cue` does not, `Load`
   fails loudly. skhd and the daemon run under launchd and never see
   `$TESSERA_PROFILE`, so falling back silently would hide the problem.
2. It compiles the user file with `cue.Scope(schema)`, so a file with or
   without `package tessera` loads.
3. It unifies the file with `#Profile` and calls `Validate(cue.Concrete(true))`.
4. It decodes into Go types. `Decode` fills `*regexp.Regexp` and
   `map[int][]float64` directly (probed).
5. It runs `config.Validate`. Errors read `profile <path>:` followed by
   `errors.Details`, which gives the CUE position.

**Editor and consumer checks.** The schema installs at
`share/tessera/schema.cue`. A user runs `cue vet -c profile.cue <schema>`;
no `cue.mod` is needed (probed). `cue lsp` gives completion. JSON is valid
CUE input, so a Nix-rendered profile also loads.

**Evidence** is in [`profile-evidence.md`](profile-evidence.md): 12 of 12
schema cases correct, the loader and `cue vet` outputs, and the start-time
bench. CUE adds about 5.5 ms of package init (p50 8.79 ms against 2.65 ms
for an empty `main`), still about twice as fast as the 18.8 ms Bun base.

### Resident daemon

This section builds `resident-daemon.md` (D-T1, D-T2, D-S1) in Go. Manual
and skhd commands stay short-lived processes, and none of them goes through
the daemon.

**Transport.** `tess daemon` binds a unix socket at
`os.TempDir()/tessera-daemon.sock`. A shell and a launchd agent see the
same `$TMPDIR` (probed), and the path is under the 104-byte limit. Each
placement event is registered with:

```text
label=tessera-<event> action=echo <event> | /usr/bin/nc -U -w 1 <abs socket path>
```

yabai runs the action as `/usr/bin/env sh -c <command>` (`event_signal.c`).
The daemon writes the resolved absolute path into the action, so no
environment variable is read at event time. No action holds a tess path.
`init`, `display-event`, `flex-event` and `--self` are deleted. The
sketchybar pair stays registered directly. yabairc's tessera line becomes
`tess wake`.

**Bind is the lock.** `daemon.Listen` binds the socket. On `EADDRINUSE` it
dials the socket and sends `wake`:

- If a daemon answers, `Listen` returns `ErrRunning`, and `tess daemon`
  exits 0.
- If the dial is refused, the file is stale. `Listen` unlinks it and binds
  again.

There is no pidfile. A clean shutdown unlinks the socket.

**Protocol.** Each connection carries one line, `<event>\n` or `wake\n`.
The daemon reads the line under a one-second deadline and closes the
connection at once, because BSD `nc` stays open until the peer closes. It
logs and drops any unknown line.

**In-memory debounce.** `daemon.Hub` holds one stamp (`time.Time`) and one
`running` flag per channel:

- the display channel: `display_added`, `display_removed`, `display_moved`;
- the flex channel: the four application and window events.

`Notify` stamps the channel with the current time and starts its waiter if
none is running. That flag replaces `DISPLAY_WAITER_LOCK` and
`FLEX_WAITER_LOCK`.

The waiter:

1. Sleeps until the computed quiet deadline. There is no fixed 1 s poll.
2. Re-checks `effects.IsQuiet` or `effects.IsFlexQuiet`. These are the H7
   predicates, over `time.Time` and `time.Duration`; the 3 s display and 2 s
   flex windows are kept.
3. **Captures `actedOn` inside that check**, then runs its work.

The waiter exits only when, under the hub mutex, the stamp still equals
`actedOn`. It clears `running` under that same lock, so an event that
arrives during the work always re-arms the channel.

**H2 stays on disk.** The flex work reads the 8 s TTL guard
(`effects.SignalsSuppressed`) at its `/tmp` path. A held guard, or a
contended converge, returns `Restamp`, and the waiter writes "now" and waits
again. The apply and laptop mkdir locks also stay on disk, because a manual
`tess apply` is still a separate process. The display work is
`commands.RunDisplayCascade`. sketchybar is nudged once, after the channel
settles.

**Wake.** The daemon runs the wake routine at startup and on each `wake`
message:

1. Register every signal with its label.
2. Run the startup reclaim cascade (`displaySetup`, then `rules`, then
   `laptop` or `apply`), which is today's `init` body without the signal
   wiring.

`tess wake` dials the socket, sends `wake`, and exits 0. If the daemon is
down, it prints `tess: daemon not running` and exits 1. There is no
fallback that runs the cascade in-process. The frozen record has none:
launchd restarts the daemon, and its startup does the same work. Events
fired while the daemon is down are dropped after `nc -w 1` (the frozen
record's Open Question 1, accepted).

**Wedge guard.** Each work call runs under
`context.WithTimeout(ctx, CascadeTimeout)` (60 s), derived from the serve
context. A deadline error ends `Serve` with that error, and `tess daemon`
exits 1. `KeepAlive` then restarts it, so a hidden wedge becomes a crash
that launchd can see.

**launchd bootstrap (D-T2).** At startup the daemon brings up yabai and skhd
under their upstream labels, `com.asmvik.yabai` and `com.asmvik.skhd`, from
`~/Library/LaunchAgents/<label>.plist`. These are the labels that each
tool's own `--install-service` uses (`src/misc/service.h` in yabai,
`src/service.h` in skhd). The bring-up follows those tools' own logic:

- run `launchctl print gui/<uid>/<label>`;
- if the service is not loaded, run `launchctl bootstrap gui/<uid> <plist>`;
- if it is already loaded, leave it alone.

On SIGTERM the daemon runs `launchctl bootout` for both. `--no-bootstrap`
skips both steps for the dev smoke. tess never runs `sudo`; the
`--load-sa` line stays in yabairc.

### Runtime contract

This section holds only what callers depend on.

- **Exit codes.** These are a contract for the CLI and scripts:
  - 0: success, help, bare `tess`, and a daemon that is already running.
  - 1: a parse failure, a profile error, a driver error, a contended
    `laptop` (silent), `wake` with no daemon, or a daemon wedge or serve
    error.
  - 130: SIGINT or SIGTERM on a short-lived command, after its locks are
    released. A clean daemon shutdown exits 0.

  skhd and yabai never read these codes. skhd sets
  `signal(SIGCHLD, SIG_IGN)` (`src/skhd.c`). yabai forks per action and
  calls `exit(execvp(...))` in the child, with no wait (`event_signal.c`).
- **Locks and SIGTERM.** `cmd/tess` builds the process root context with
  `signal.NotifyContext(context.Background(), os.Interrupt,
  syscall.SIGTERM)`. That is the one `Background()` call, at the top of
  `main`. A Go process that has no handler skips its defers on SIGTERM
  (probed), so the handler is required.

  On cancellation:
  - the yabai child stops;
  - the deferred `Release` calls and the guard removal run;
  - `cli.Main` returns 130.

  Lock rules:
  - `AcquireLockOrSkip` (apply) never reclaims, so a lock left behind
    would block every later `apply`.
  - `AcquireLock` (laptop) reclaims a stale PID and treats EPERM as alive.
    A PID file that is missing or does not parse counts as stale.
  - The lock and guard paths stay as in `src/effects/constants.ts`.
- **yabai argv.** The argv each command sends is the contract with yabai.
  It is pinned two ways:
  - the argv tables ported from `src/driver/yabai.test.ts`;
  - the stub-yabai corpus (§ Tests).

  Ratios print with `strconv.FormatFloat(x, 'f', 4, 64)`. No argv or error
  order depends on map iteration.

### Tests

- **Ported behaviour tests.** Each TS `test(` becomes a Go test named after
  its TS title. The PR body carries a ledger mapping every TS title to a Go
  test or a drop reason. A test may be dropped only for one of two
  reasons:
  - it tests deleted machinery: `fastMatch`, `RunDeps`, the Effect errors,
    `init --self`, file event stamps, or waiter locks;
  - it tests a TS type.
- **Stub-yabai argv corpus.** `TESS_YABAI` points at a recording stub,
  built in `TestMain` from `internal/wm/yabai/testdata/stubyabai`, which
  serves `windows.live.json`. There is one case per keybind subcommand and
  choice. Expected argv files live in `testdata/argv/` and are refreshed
  with `go test -update`, then reviewed.
- **Time.** No test calls `time.Sleep`. Every daemon and waiter test runs
  inside `synctest.Test` and uses event gates (channels, `synctest.Wait`).
  Socket I/O does not block durably inside a bubble, so `Serve` takes a
  `net.Listener`. Bubble tests feed it `net.Pipe` connections. One test
  outside the bubble binds a real socket under `os.MkdirTemp("/tmp", …)`, to
  stay under the path limit, and covers `ErrRunning` and stale reclaim
  through channel gates.
- **launchctl** sits behind a `Launchctl` interface. Tests assert the argv.

### Build and the startup bar

`packages.default` becomes `buildGoModule` with `CGO_ENABLED=0`,
`-trimpath` and `disallowedReferences = [ go ]`. The bun attrs, `bun2nix`
and `checks.bun-lock` go. `installCheckPhase` checks four things:

- a bogus subcommand exits 1;
- `--help` exits 0;
- a loader smoke exits 0;
- the installed schema vets `default.cue`.

The home-manager `profilePath` default becomes `.../tessera/profile.cue`,
and the `TESSERA_PROFILE` absolute-path check follows it. The startup bar is
`effect4-cli.md`'s, unchanged:

- hot-path p50 at most 2.0× base;
- hot-path p90 at most 45 ms;
- help p50 at most 60 ms.

`tools/benchstartup` runs it against the TS build at the PR's parent.
`effect4-cli.md` notes that the daemon removes the cold start for signals
but not for keybinds, so the bar still applies.

### Frozen records

No frozen record is edited. This record supersedes:

| Record | Superseded | Still stands |
| --- | --- | --- |
| `effect4-cli.md` | All of it | Its startup bar |
| `architecture.md` | Global Constraints "Runtime/tooling" and "Deploy"; Layer 1 "`profile.ts`, typed data"; Open Question 2 (now CUE); Open Question 3 (now the daemon); the Bun mechanics; § "Build + deploy"; T7 | Layers, layering law, D1, D2, driver contract, engine |
| `grid-layouts.md` | "Runtime/tooling"; `bun test` cycles; the `profile.ts` name | Tracks, weights, chain formula, resolution order |
| `laptop-flex-spaces.md` | "ports to TypeScript" | The converger model |
| `resident-daemon.md` | "Runtime/tooling"; `Bun.listen`; the TS `Interfaces:` of its Plan; remove-then-add; the 1 s poll; "Bun exposes no `launch_activate_socket`"; "one PR per task slice" | D-T1, D-T2, D-S1; H2/H7; bind-as-lock; the wedge default; its downstream T5 |

### Migration

One PR, with its slices as commits (§ Plan). Every commit builds and passes
its own tests, so the stack stays bisectable. The last commit switches the
flake and deletes `src/`. After that PR merges, one atomic orion PR follows.
It:

- bumps the input;
- converts the profile to `profile.cue`;
- points `profilePath` at it;
- changes yabairc's `tess init --self …` to `tess wake`;
- adds the `tess daemon` launchd agent and the yabai and skhd definitions;
- retires brew services;
- swaps the consumer typecheck for `cue vet` plus a loader smoke.

To roll back, revert that PR.

## Alternatives considered

One line each; sources are in [`profile-evidence.md`](profile-evidence.md).

- **Pkl.** pkl-go runs an external JVM `pkl` (355 MiB closure, ~861 ms per eval).
- **KCL.** kcl-go needs a local `replace` and a native `libkcl` at runtime.
- **Nickel.** go-nickel has no release and needs cgo.
- **Dhall.** dhall-golang has not released since 2021-10-09.
- **YAML plus JSON Schema.** The types sit outside the loader and can drift.
- **Typed home-manager option.** It types Nix users only. A later
  `builtins.toJSON` render needs no new code, because CUE loads JSON.
- **cobra, urfave/cli, kong.** No abstraction over a 21-row table (§ CLI).
- **launchd socket activation.** The only cgo-free binding is
  `bored-engineer/go-launchd`, with 7 stars and a last push on 2024-12-20.
  Bind-as-lock covers single-instance without it.
- **Thin-client fallback in `wake`.** It runs the cascade in two places;
  launchd's restart already covers that case.
- **Routing keybinds through the daemon.** It couples every keybind to a
  live daemon. `architecture.md` Q5 keeps skhd separate.

## Global Constraints

- **Toolchain.** `go.mod` declares `go 1.26` (nixpkgs Go 1.26.7). devenv
  supplies that Go and `golangci-lint` 2.13.2. CI runs on `ubuntu-latest`.
  `nix build` runs on `macos-latest`.
- **Dependencies.** Runtime: `cuelang.org/go` v0.17.1, `golang.org/x/sync`,
  `github.com/samber/oops`, and `github.com/samber/mo` (`Option` only).
  Tests: `github.com/google/go-cmp` v0.7.0. Anything else needs reviewer
  sign-off.
- **House slate.** The Go idioms record applies as written:
  - golangci-lint `default: all`, with `exhaustive` and `gochecksumtype` on;
  - no `panic` in library code;
  - errors are returned, wrapped with `%w`, or joined with `errors.Join`;
  - diagnostics go to `slog`, and output to an explicit `io.Writer`;
  - `ctx` is the first parameter and is threaded down.
    `context.Background()` appears only in `main` and in tests.
- **Time.** Code uses `time` directly. Tests use `testing/synctest` and
  event gates, never `time.Sleep`.
- **Public repo.** No private path or layout data. orion is named only as
  the downstream consumer.

## Plan

There is one PR. Each item below is one commit with its ported tests, and is
reviewed in order.

**Signature rule.** Each exported TS function becomes an exported Go
function:

- the parameters stay the same and in the same order;
- `Profile` becomes `*config.Profile`;
- `ctx` comes first when the function does I/O;
- a function that can fail returns `(T, error)`;
- `T | null` becomes `(T, bool)`.

The `Interfaces:` lines below list only what this rule cannot derive.

### C1 — Skeleton and CI

`go.mod`, `.golangci.yml`, the devenv Go, and a `go` CI job running
`go vet`, `golangci-lint run` and `go test -race ./...`.

Interfaces: produces the module. Gate: CI is green.

### C2 — `internal/config`

The schema, `default.cue`, the loader, `Validate`, and `configtest`. Ports
`validate.test.ts` and `profile.test.ts`.

Interfaces:

- `Load(ctx context.Context, env func(string) string, home string) (*Profile, error)`
- `Parse(ctx context.Context, path string, src []byte) (*Profile, error)`
- `Validate(p *Profile) error`
- `type WindowSpec struct { App, Title *regexp.Regexp; TitleInvert bool; Spawn []string }`
- `Profile.Displays` is `struct{ G9, Aw, Laptop DisplaySpec }`.

Gate:

- the probe cases above pass as Go tests, through `Parse`;
- an unknown name in `tracks` fails;
- a `profile.ts` with no `profile.cue` fails loudly;
- `cue vet` accepts `default.cue`.

### C3 — `internal/wm` and `internal/wm/fake`

Interfaces:

- `Driver`
- `RuleOps`
- `EventSource { AddSignal(ctx, ev Event, label, action string) error }`
- `Snapshot(ctx, d Driver) (World, error)`
- `fake.New(fake.Seed) *fake.Driver`

Gate: `fake.test.ts` ported, green under `-race`.

### C4 — `internal/engine`

The 13 modules.

Interfaces:

- the sealed `Op` and `ConvergeAction`;
- `LaptopConvergeStep(p, w, s) (ConvergeAction, ConvergeState, bool)`;
- `NewClaimSet(p) *ClaimSet`, whose methods take
  `preferDisplay mo.Option[int]`.

Gate: the engine tests, including the `windows.live.json` cases.

### C5 — `internal/wm/yabai`

The argv builders, the normalizers, `bspSteps`, labelled `AddSignal`, the
runner, and the stub yabai.

Interfaces: `New(yabaiPath string) *Driver`, which implements all three
interfaces.

Gate:

- the argv tables;
- a labelled add renders `label=tessera-display_added`;
- the stub records argv.

### C6 — `internal/effects` and `internal/executor`

The locks, guard, flex-order state, nudge, paths and predicates, plus
`RunPlan` and `RunConverge`.

Interfaces:

- `AcquireLock(dir string) (*Lock, bool, error)`
- `AcquireLockOrSkip(dir string) (*Lock, bool, error)`
- `(*Lock).Release() error`
- `IsQuiet(now, stamp time.Time, d time.Duration) bool`
- `IsFlexQuiet(now, own, display time.Time) bool`
- `SignalsSuppressed(path string, now time.Time) (bool, error)`

Gate:

- a SIGKILLed child's lock is reclaimed;
- the predicate edge cases are green.

### C7 — `internal/commands`

The commands, `RunDisplayCascade`, `RunFlexConverge`, `Wake` and `Run`.

Interfaces:

- the sealed `Command`, 21 kinds;
- `type Env struct { Paths effects.Paths; Nudge effects.Nudger; Stderr io.Writer }`;
- `Run(ctx, p, c Command, d wm.Driver, env Env) (int, error)`;
- `Wake(ctx, p, d, env, sock string) error`.

Gate: `commands.test.ts` is green.

### C8 — `internal/daemon`

The hub, the waiters, serving, listening, registration and launchd.

Interfaces:

- `type Step int` (`Settled`, `Restamp`)
- `type Work func(ctx context.Context) (Step, error)`
- `NewHub(display, flex Work) *Hub`
- `(*Hub).Notify(ctx context.Context, ev string)`
- `Listen(ctx context.Context, path string) (net.Listener, error)` (`ErrRunning`)
- `Serve(ctx context.Context, ln net.Listener, h *Hub, wake func(context.Context) error) error`
- `RegisterSignals(ctx context.Context, src wm.EventSource, sock string) error`
- `type Launchctl interface { Run(ctx context.Context, args ...string) error }`
- `Bootstrap(ctx, lc Launchctl, uid int, svcs []Service) error`
- `Bootout(ctx, lc Launchctl, uid int, svcs []Service) error`

Gate, in synctest:

- a burst of N events gives one cascade;
- an event during work re-arms the waiter;
- H2: a held guard restamps;
- H7: flex waits for display-quiet;
- no action holds a tess path;
- a timeout ends `Serve`.

Outside the bubble: `ErrRunning` and stale reclaim.

### C9 — `internal/cli` and `cmd/tess`

The dispatcher, help, `daemon`, `wake`, `main`, and `tools/benchstartup`.

Interfaces:

- `Parse(argv []string) (commands.Command, error)`
- `Main(ctx context.Context, argv []string, stdout, stderr io.Writer, deps Deps) int`

Gate:

- the stub-yabai corpus;
- SIGTERM during a stub-blocked `apply` exits 130 and leaves no lock;
- `wake` with no daemon exits 1.

### C10 — Cutover

This commit:

- switches to `buildGoModule`;
- removes the bun attrs;
- sets `profilePath` to `profile.cue`;
- installs the schema;
- updates the README and `devenv.nix`;
- deletes `src/`, the Bun manifests, and the TS scripts.

Gate:

- `nix flake check` and `nix build` pass on `macos-latest`;
- the bar holds (bench table in the PR body);
- no `.ts` file remains;
- a live smoke: plug and unplug, a window-churn burst, `kill -9` the daemon,
  and observe the reclaim.

## Tasks

- [ ] C1 — module, lint, CI `go` job
- [ ] C2 — CUE schema, loader, `Validate`, name references
- [ ] C3 — `wm` contract and fake, with labelled `AddSignal`
- [ ] C4 — engine, with sealed `Op` and `ConvergeAction`
- [ ] C5 — yabai driver and stub yabai
- [ ] C6 — effects and executor, with cancel-releases-lock
- [ ] C7 — commands, with `Wake`
- [ ] C8 — daemon: hub, socket, registration, launchd, synctest gates
- [ ] C9 — CLI and `main`: exit 130, argv corpus, `benchstartup`
- [ ] C10 — flake cutover, `src/` deleted, the bar holds, live smoke
- [ ] Downstream — one atomic orion PR (§ Migration)

## Resolved decisions

Matt ruled on RIG-4526:

1. **Profile.** "want types. could think about CUE or pkl here potentially?
   or another similar language?" The profile is CUE (§ Profile format), and
   the other options are under § Alternatives considered.
2. **Help and errors.** "ii, we don't care about parity, just build in the
   best way possible". The parity contract, the captured TS goldens and
   their CI step are gone. The help and errors are idiomatic Go (§ CLI).
3. **Landing.** "Just one super PR is fine". There is one PR with commit
   slices (§ Plan).
4. **Daemon.** "Keep. daemon is best pattern for this imo, yabai and skhd
   are already daemons/services". It is built here in Go and lands in the
   same PR (§ Resident daemon).
