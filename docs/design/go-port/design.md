# Design — Rewrite `tess` in Go, with the resident daemon

*Design record for RIG-4487, revised by Matt's rulings on RIG-4526 and of
2026-10-10. It supersedes `effect4-cli.md` and builds `resident-daemon.md`
in Go. The profile becomes JSON, rendered from a TypeScript module that a
Zod schema in this repo checks.*

## Problem / Intent

Every skhd keybind spawns `tess`, and a `bun build --compile` binary spends
about 19 ms starting its runtime (the base row in `effect4-cli.md` §
"Startup-latency budget"). Matt chose Go. This record rewrites `tess` as
idiomatic Go and does not keep byte-level compatibility with the TS build.
In the same change it moves event handling into the resident daemon from
`resident-daemon.md`. It also changes the profile format: tess reads JSON
only, and the profile's types live in a Zod schema that tessera ships.

## Approach

**Re-realize, not port.** The TS source states the behaviour. The Go code
is native, and the behaviour tests are ported case for case. Anything that
exists only to mirror the TS shape is deleted: the two CLI front ends, the
Effect error classes, `RunOpts` injection, and the file-stamp waiter
protocol, which the daemon replaces. Where a well-adopted library does a
job, the port uses it instead of porting the TS code (§ Global
Constraints).

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

| TS source | New home |
| --- | --- |
| `src/config/types.ts` | `schema/profile.ts` (Zod), and the structs in `internal/config` |
| `src/config/validate.ts`, `chain.ts`, `loadProfile` | `internal/config` (embeds `default.json`) |
| `src/config/profile.ts` | `schema/default.ts`, rendered to `internal/config/default.json` |
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

The CLI is built on `github.com/spf13/cobra` v1.10.2. `internal/cli` holds
a 21-row table (name, one-line help, choices or an integer argument, flags)
and builds one `cobra.Command` per row from it. cobra supplies what the TS
build wrote by hand: argument checks, per-command help, "Did you mean"
suggestions, and shell completion. Linking it adds about 0.9 ms to process
start at p50. The comparison with urfave/cli and kong is in
[`profile-evidence.md`](profile-evidence.md) § "CLI library".

- **Kinds.** 21 in total:
  - the 19 manual and keybind kinds of today's `SUBCOMMANDS` (all of them
    except `display-event` and `flex-event`);
  - `daemon`;
  - `wake`.

  cobra adds `help` and `completion` beside them, and the hidden
  `__complete` command that the completion scripts call.
- **Arguments.** Each row sets `Args`:
  - a kind with choices sets `ValidArgs` to them and uses
    `cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs)`. Its `Use`
    names the choices, as in `snap {3col|50-50|columns}`;
  - `focus-slot` uses `cobra.ExactArgs(1)` plus an `Args` check that runs
    `strconv.Atoi`. So `2.0` and `2e0`, which the TS grammar accepted, are
    now usage errors;
  - every other kind uses `cobra.NoArgs`.

  The root sets no `Args`. cobra's default for a root with subcommands then
  rejects an unknown name with `unknown command "bogus" for "tess"` and a
  suggestion. The only flag is `daemon --no-bootstrap`.
- **Help and version.** Bare `tess` and `tess --help` list the kinds.
  `tess <cmd> --help` and `tess help <cmd>` print that command's usage,
  which names its choices. `tess --version` prints `tess v<version>` through
  `SetVersionTemplate`, linked in from `VERSION`. All of these exit 0.
- **Parse, then run.** A leaf's `RunE` only records its `commands.Command`.
  `cli.Parse` runs the tree with `ExecuteContextC(ctx)` and returns that
  command. It returns nil when cobra itself printed help, the version or a
  completion script. `cli.Main` then runs the command, so parsing is tested
  without a driver.
- **Errors.** The root sets `SilenceErrors` and `SilenceUsage`, so cobra
  prints nothing on a failure. `Parse` returns a `*cli.UsageError` that
  carries cobra's message and the failing command's use line. `cli.Main`
  prints `tess: <message>` and `usage: <use line>` on stderr and exits 1.
  Output goes to the injected writers (`SetOut`, `SetErr`). Diagnostics go
  through `log/slog`.
- **Completion.** `tess completion bash|zsh|fish|powershell` prints a
  script, and choices complete from `ValidArgs`. The flake installs the
  bash, zsh and fish scripts with `installShellCompletion`, as the nixpkgs
  `gh` package does.

### Profile format

The profile is JSON. The downstream writes it as a TypeScript module and
renders it to JSON at build time. tess reads only JSON and never runs TS.

**Schema.** The source of truth is a Zod schema in tessera. It ships as a
small TS package in `schema/`, named `tessera-wm` as the README already
documents, with `zod` 4 as a peer dependency. `schema/profile.ts` exports
the schema `Profile` and the type `Profile = z.infer<typeof Profile>`. The
downstream pins the package at the same tessera revision as the `tess`
binary, so the schema and `Validate` always match. Its shape follows
`src/config/types.ts`, with three changes:

- a regex becomes a pattern string;
- `laptopStackApps` becomes a list of app names;
- a `desk` entry is a `z.discriminatedUnion("kind", …)`. The `stack`
  member has exactly one track and no `weights`. The `columns` and `rows`
  members share one track shape.

Every object is a `z.strictObject`. `tsc` does not catch an extra, mistyped
key in an object literal that a callback returns, so the render must reject
unknown keys at run time. A strict object does that.

Zod checks shape: types, required keys, closed objects, enums and the
`desk` union. The cross-field rules live in Go `Validate` only, so there is
one copy of each, and tess runs it on every load.

**Downstream render.** The downstream's profile module imports `Profile`
from the package. `tsc` checks it and `bun test` tests it.
`@rigelbuild/config-render` renders it to JSON at build time (`defineConfig`,
deterministic JSON, `render --check`). Zod 4 keeps schema key order on
output, so a render is byte-stable.

**Go types.** `internal/config` mirrors the Zod schema in plain structs.
Go has no sum type, so `DeskLayout` stays one struct that holds the fields
of every union member, as it is one interface in `src/config/types.ts`.

```go
type Profile struct {
	Displays        Displays              `json:"displays"`
	Windows         map[string]WindowSpec `json:"windows"`
	Desk            []DeskLayout          `json:"desk"`
	Topologies      []Topology            `json:"topologies"`
	Weights         WeightDefaults        `json:"weights"`
	DeskSlots       []DeskSlot            `json:"deskSlots"`
	LaptopPinned    []string              `json:"laptopPinned"`
	LaptopStackApps []string              `json:"laptopStackApps"`
}

type Displays struct {
	G9     DisplaySpec `json:"g9"`
	Aw     DisplaySpec `json:"aw"`
	Laptop DisplaySpec `json:"laptop"`
}

type DisplaySpec struct {
	Width   int            `json:"width"`
	Weights WeightDefaults `json:"weights"`
}

// Keys are track counts, written as JSON strings ("3").
type WeightDefaults struct {
	Columns map[int][]float64 `json:"columns"`
	Rows    map[int][]float64 `json:"rows"`
}

type WindowSpec struct {
	App         string   `json:"app"`
	Title       string   `json:"title"`
	TitleInvert bool     `json:"titleInvert"`
	Spawn       []string `json:"spawn"`
	// Set by Validate. TitleRE is nil when Title is empty (any title).
	AppRE, TitleRE *regexp.Regexp `json:"-"`
}

// Both members of the Zod desk union; Validate enforces the kind rules.
type DeskLayout struct {
	Display DisplayName `json:"display"`
	Label   string      `json:"label"`
	Kind    LayoutKind  `json:"kind"` // "stack", "columns" or "rows"
	Tracks  [][]string  `json:"tracks"`
	Weights []float64   `json:"weights"`
}

type DeskSlot struct {
	Name      string      `json:"name"`
	OnDisplay DisplayName `json:"onDisplay"`
}

type Topology struct {
	Name     string        `json:"name"`
	Displays []DisplayName `json:"displays"`
	Desk     []DeskLayout  `json:"desk"`
}
```

`DisplayName` and `LayoutKind` are `string` types with one constant per
value, so `exhaustive` checks every switch on them.

**Decode.** `Parse` decodes with `encoding/json/v2`:

```go
err := json.Unmarshal(src, &p, json.RejectUnknownMembers(true))
```

A member that Go does not know fails the decode. A member that Go knows
but the JSON omits stays at its zero value and does not fail. So `Validate`
rejects the zero value of every field Go needs:

- a display's `width` (it must be greater than 0);
- a window's `app`;
- a layout's `display`, `label`, `kind` and `tracks`;
- a topology's `name` and `displays`;
- a desk slot's `name`.

A list or map that may be empty means the same thing when it is missing.

**Validate.** `config.Validate` then checks every rule and returns all
violations with `errors.Join`:

- each pattern compiles with `regexp` (RE2). A compile error fails the
  load. The result is stored in `AppRE` and `TitleRE`;
- `display` and `onDisplay` are `g9`, `aw` or `laptop`, and `kind` is
  `stack`, `columns` or `rows`;
- every rule of `validateProfile` (`src/config/validate.ts`): positive
  weights; chain ratios between 0.1 and 0.9 over every subsequence
  (`chainRatios`); weight-table keys of at least 2 that equal the vector
  length; one track and no weights for a stack; no weights for a one-track
  layout; weights as long as tracks; and each topology laying out exactly
  its declared displays, once each;
- every name in `tracks`, `deskSlots` and `laptopPinned` is a key of
  `windows`. A typo there silently claims nothing today.

**Drift test.** `schema/full.ts` is a profile that sets every optional
field to a non-zero value. The schema package renders it to
`internal/config/testdata/full.json`. A Go test decodes that file with
`RejectUnknownMembers(true)` and calls `Validate`. It then compares the
result with a Go literal of the same profile, using `assert.DeepEqual`
and ignoring `AppRE` and `TitleRE`. A key that Zod adds or renames fails
the decode. A required key that Zod drops fails `Validate`. An optional
key that Zod drops fails the comparison.

**Default.** `schema/default.ts` holds today's neutral default
(`src/config/profile.ts`). The package renders it to
`internal/config/default.json`, which `internal/config` embeds.
`schema/profile.test.ts` renders both files with
`JSON.stringify(Profile.parse(value), null, 2)` and fails when a committed
file differs. `UPDATE=1 bun test` rewrites them.

**Loader.**

```go
func Load(ctx context.Context, env func(string) string, home string) (*Profile, error)
```

1. `Load` resolves `$TESSERA_PROFILE`, then
   `${XDG_CONFIG_HOME:-~/.config}/tessera/profile.json`, then the embedded
   `default.json`. A missing well-known file falls through. A broken one is
   an error.
2. A `profile.ts` with no `profile.json` beside it fails loudly. So does a
   `$TESSERA_PROFILE` that ends in `.ts`. The error says to render the
   profile. skhd and the daemon run under launchd and never see
   `$TESSERA_PROFILE`, so falling back silently would hide the problem.
3. `Parse` decodes the file and runs `Validate`. Errors read
   `profile <path>:` followed by the decode or `Validate` error.

The loader uses only the standard library. The facts it rests on are in
[`profile-evidence.md`](profile-evidence.md) § "Profile as JSON".

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
sketchybar pair stays registered directly. The user's yabai startup config
runs `tess wake` when yabai starts.

**A lock file is the lock.** `daemon.Listen` takes
`flock.New(<socket>.lock).TryLock()` (`github.com/gofrs/flock` v0.13.1,
which calls `flock(LOCK_EX|LOCK_NB)`) and holds it for the daemon's whole
life. The kernel drops the lock when the process dies, even on SIGKILL, so
the lock never goes stale. The file holds no content and is never removed.
The apply and laptop locks use the same package (§ Runtime contract).

- If the lock is held, `Listen` returns `ErrRunning`, and `tess daemon`
  exits 0.
- If `Listen` gets the lock, no other starter can be past this point. It
  removes any leftover socket file without probing it, then binds.

Probe, unlink and bind therefore never race. The listener's `Close` unlinks
the socket first and releases the lock second. Two `*flock.Flock` handles
on one path conflict even inside one process (probed: the second
`TryLock` returns `false, nil`), so the race test can run in-process.

**Manual starts.** The launchd agent is the normal way to run the daemon.
Its `KeepAlive` must be `{ SuccessfulExit = false; }`, so launchd restarts
it after a crash or a wedge (exit 1) but not after `ErrRunning` or a clean
SIGTERM (exit 0). A manual `tess daemon` while the agent runs prints
`tess: daemon already running` and exits 0. To run one by hand, boot the
agent out first. If a manual daemon holds the lock when the agent starts,
the agent exits 0 and stays down until it is kickstarted.

**Protocol.** Each connection carries one line, `<event>\n` or `wake\n`.
The daemon reads the line with `bufio.Reader.ReadString('\n')` under a
one-second deadline and closes the connection at once, because BSD `nc`
stays open until the peer closes. It logs and drops any unknown line. The
standard `net` and `bufio` packages cover this exact operation, so the
protocol takes no framing library.

**In-memory debounce.** The debounce is hand-rolled, because no library
carries the H7 display-quiet gate or the `actedOn` capture (§ Alternatives
considered). `daemon.Hub` holds one stamp (`time.Time`) and one `running`
flag per channel:

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
again. The apply and laptop locks also stay on disk, because a manual
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

A failed wake routine is logged and is not fatal. At startup yabai may not
be up yet; when it starts, its `tess wake` runs the routine again.

**Wedge guard.** Each work call runs under
`context.WithTimeout(ctx, CascadeTimeout)` (60 s), derived from the serve
context. A deadline error ends `Serve` with that error, and `tess daemon`
exits 1. `KeepAlive` then restarts it, so a hidden wedge becomes a crash
that launchd can see.

**launchd bootstrap (D-T2).** At startup the daemon brings up yabai and skhd
under their upstream labels, `com.asmvik.yabai` and `com.asmvik.skhd`, from
`~/Library/LaunchAgents/<label>.plist`. These are the labels that each
tool's own `--install-service` uses. For each service the daemon follows
the upstream `service_start` (`src/misc/service.h` in yabai,
`src/service.h` in skhd):

1. Run `launchctl print gui/<uid>/<label>`.
2. If the service is not loaded:
   - run `launchctl enable gui/<uid>/<label>`, because a disabled service
     cannot be bootstrapped. A failure here is logged and the daemon goes
     on, as upstream ignores this result too;
   - run `launchctl bootstrap gui/<uid> <plist>`. On success the daemon
     records the service as **owned**.
3. If the service is already loaded, run `launchctl kickstart
   gui/<uid>/<label>`, which starts it only if it is not running. It is not
   owned.

A bootstrap failure is logged at error level, and the service is not owned.
The daemon still serves events, because its event job does not depend on
who started yabai.

On SIGTERM the daemon runs `launchctl bootout` for **owned services only**.
A service it found loaded stays loaded. Ownership does not survive a
daemon restart: after a crash, the next daemon finds both services loaded
and owns neither. Bootout runs under
`context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)`, because
the root context is already cancelled at that point.

`--no-bootstrap` skips all of this for the dev smoke. tess never runs
`sudo`; loading yabai's scripting addition stays the user's yabai config's job.

tess runs `launchctl` through `os/exec` behind the `Launchctl` interface.
It reads no plist, so it needs no plist library. The one Go service library
that meets the dependency bar drives launchd through the legacy `load` and
`unload` verbs, and it cannot do these steps (§ Alternatives considered).

### Runtime contract

This section holds only what callers depend on.

- **Exit codes.** These are a contract for the CLI and scripts:
  - 0: success, help, version, bare `tess`, and a daemon that is already
    running.
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
  - the deferred `Unlock` calls and the guard removal run;
  - `cli.Main` returns 130.

  Lock rules:
  - The apply and laptop locks are `gofrs/flock` locks on
    `os.TempDir()/tessera-apply.lock` and `os.TempDir()/tessera-laptop.lock`,
    in the same `$TMPDIR` as the socket. They replace the mkdir locks of
    `src/effects/locks.ts`.
  - `effects.AcquireLock` gives up on contention for both. A contended
    `apply` exits 0. A contended `laptop` exits 1 silently, or its flex
    work returns `Restamp`.
  - The kernel drops a `flock` lock when its holder dies, so neither lock
    goes stale. The PID file, the stale-PID reclaim and the EPERM rule are
    deleted.
  - The guard path stays as in `src/effects/constants.ts`.
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
  choice. Expected argv files live in `testdata/argv/` and are checked with
  `golden.Assert` from `gotest.tools/v3/golden`. `go test -update` rewrites
  them, and the diff is reviewed.
- **Assertions.** Tests use `gotest.tools/v3/assert`: `assert.NilError`,
  `assert.Equal`, and `assert.DeepEqual` with go-cmp options for structs.
- **Time.** No test calls `time.Sleep`. Every daemon and waiter test runs
  inside `synctest.Test` and uses event gates (channels, `synctest.Wait`).
  Socket I/O does not block durably inside a bubble, so `Serve` takes a
  `net.Listener`. Bubble tests feed it `net.Pipe` connections. Listener
  tests run outside the bubble on real sockets under
  `os.MkdirTemp("/tmp", …)`, which keeps the path under the length limit.
  Two starters released by one channel race on a path that holds a stale
  socket file: exactly one gets a listener, and the other gets
  `ErrRunning`.
- **launchctl** sits behind a `Launchctl` interface. Tests assert the argv.

### Build and the startup bar

`packages.default` becomes `buildGoModule` with `CGO_ENABLED=0`,
`-trimpath` and `disallowedReferences = [ go ]`. The bun attrs, `bun2nix`
and `checks.bun-lock` go.

**Version source.** A root `VERSION` file holds the bare version and
replaces `package.json`'s `version` field:

- the flake sets `version = pkgs.lib.fileContents ./VERSION` and passes it as
  `-X main.version=${version}`;
- `release.yml` reads it with `tr -d '[:space:]' < VERSION`.

`installCheckPhase` checks four things:

- a bogus subcommand exits 1;
- `--help` exits 0;
- `--version` prints `tess v$version`;
- a loader smoke loads the embedded `default.json` and exits 0.

The home-manager `profilePath` default becomes `.../tessera/profile.json`,
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
| `architecture.md` | Global Constraints "Runtime/tooling" and "Deploy"; Layer 1 "`profile.ts`, typed data"; Open Question 2 (now TS rendered to JSON); Open Question 3 (now the daemon); the Bun mechanics; § "Build + deploy"; T7 | Layers, layering law, D1, D2, driver contract, engine |
| `grid-layouts.md` | "Runtime/tooling"; `bun test` cycles; the `profile.ts` name | Tracks, weights, chain formula, resolution order |
| `laptop-flex-spaces.md` | "ports to TypeScript" | The converger model |
| `resident-daemon.md` | "Runtime/tooling"; `Bun.listen`; the TS `Interfaces:` of its Plan; remove-then-add; the 1 s poll; "Bun exposes no `launch_activate_socket`"; bind-as-lock (now a lock file); unconditional bootout; its T5; "one PR per task slice" | D-T1, D-T2, D-S1; H2/H7; the wedge default |

### Migration

One PR, with its slices as commits (§ Plan). Every commit builds and passes
its own tests, so the stack stays bisectable. The last commit switches the
flake and deletes `src/`. After that PR merges, the consumer makes one
change: its profile becomes a TS module that imports `tessera-wm`, its
build renders that module to `profile.json`, and its service wiring
follows. Reverting that change rolls back.

The consumer's old layout scripts stop being invoked at cutover. Nothing
else then takes the old mkdir locks, so the apply and laptop locks move to
new `flock` paths with no shared lock protocol to keep.

## Alternatives considered

One line each; sources are in [`profile-evidence.md`](profile-evidence.md).

- **A config language (CUE, Pkl, KCL).** Matt chose a TS profile rendered
  to JSON instead (Resolved decision 7).
- **Running the TS profile in tess (goja).** tess would embed a JS engine
  and run TS on every keybind. Matt rejected it (Resolved decision 7).
- **JSON Schema generated from the Go structs.** Go has no sum type, so the
  generated schema cannot state the `desk` union on `kind`. The Zod schema
  states it, and `z.toJSONSchema` can emit a JSON Schema from it if one is
  needed.
- **urfave/cli v3.14.0.** An unknown command exits 3 with "No help topic",
  and its completion offers `help` but not the choices.
- **kong v1.16.1.** It has no built-in completion (the `kongplete` add-on
  has 48 importers). Each command's arguments and choices are struct tags,
  even under `kong.DynamicCommand`, so the kind table cannot set them.
- **bep/debounce v1.2.1.** 277 importers. It is a trailing-edge timer, with
  no H7 gate and no restamp.
- **kardianos/service v1.3.0.** 1,591 importers, but it drives launchd with
  `load`, `unload` and `list`, and installs its own plist. tess bootstraps
  upstream's plists under upstream labels.
- **howett.net/plist v1.0.1.** 543 importers, and tess reads no plist.
- **adrg/xdg v0.5.3.** 1,197 importers, but on macOS it defaults to
  `~/Library/Application Support` and `~/Library/Caches`. tess keeps the
  `~/.config` and `~/.cache` defaults its users already have.
- **deckarep/golang-set v2.9.0.** 1,842 importers, but a
  `map[T]struct{}` with `maps.Clone` and `maps.Equal` covers every set
  operation the engine uses. The module also requires the MongoDB driver
  module for its BSON methods.
- **testify v1.12.1.** 20,797 importers for `require`. `gotest.tools/v3`
  is already required for golden files and ships `assert` with go-cmp
  options, so a second assertion module adds nothing.
- **launchd socket activation.** The only cgo-free binding is
  `bored-engineer/go-launchd`, with 7 stars and a last push on 2024-12-20.
  The `flock` lock file covers single-instance without it.
- **Thin-client fallback in `wake`.** It runs the cascade in two places;
  launchd's restart already covers that case.
- **Routing keybinds through the daemon.** It couples every keybind to a
  live daemon. `architecture.md` Q5 keeps skhd separate.

## Global Constraints

- **Toolchain.** `go.mod` declares `go 1.27`. `encoding/json/v2` is GA in
  Go 1.27; on Go 1.26 it needs `GOEXPERIMENT=jsonv2`. devenv supplies Go
  1.27 and a `golangci-lint` built with it. CI runs on `ubuntu-latest`.
  `nix build` runs on `macos-latest`.
- **Schema package.** `schema/` is TypeScript run by Bun, at the Bun
  version CI pins today. `zod` 4 is a peer dependency and a dev dependency.
  CI runs `tsc --noEmit` and `bun test` there. Nix does not build it.
- **Dependencies.** Prefer a well-adopted, maintained library to
  hand-rolled code (Resolved decision 5). Keep the standard library only
  where it already covers the exact operation. A module meets the bar
  when:
  - pkg.go.dev lists at least 1,000 known importers, and named projects
    pin it. pkg.go.dev counts only non-test imports, so a test-only module
    is measured by its most-imported package;
  - it has a commit or a release in the last year;
  - it needs no cgo.

  The modules, with sources and dates in
  [`profile-evidence.md`](profile-evidence.md) § "Library evidence":

  | Module | Version | Use | Importers |
  | --- | --- | --- | ---: |
  | `github.com/spf13/cobra` | v1.10.2 | the CLI (§ CLI) | 195,884 |
  | `github.com/gofrs/flock` | v0.13.1 | the daemon, apply and laptop locks | 1,499 |
  | `golang.org/x/sync` | v0.23.0 | `errgroup` | 30,110 |
  | `github.com/samber/lo` | v1.53.0 | transforms `slices` and `maps` lack: `Map`, `Filter`, `FlatMap`, `Reduce`, `Uniq`, `GroupBy` | 12,533 |
  | `github.com/samber/mo` | v1.17.0 | `Option` only; below the bar, kept for `ClaimSet` (C4) | 434 |
  | `github.com/samber/oops` | v1.23.2 | daemon errors that carry `slog` attributes and a stack; below the bar, kept by Matt's ruling (Resolved decision 6) | 332 |
  | `gotest.tools/v3` (tests) | v3.5.2 | `assert`, `golden` | 1,570 (`assert`) |
  | `github.com/google/go-cmp` (tests) | v0.7.0 | diff options for `assert.DeepEqual` | 5,704 |

  A module outside this table needs the same evidence in the PR body.
- **House slate.** The Go idioms record applies as written:
  - golangci-lint `default: all`, with `exhaustive` and `gochecksumtype` on;
  - no `panic` in library code;
  - errors are returned, built or wrapped with `samber/oops` where the
    daemon logs them (`.With` attributes, not formatted messages), and
    otherwise wrapped with `%w` or joined with `errors.Join`;
  - diagnostics go to `slog`, and output to an explicit `io.Writer`;
  - `ctx` is the first parameter and is threaded down.
    `context.Background()` appears only in `main` and in tests.
- **Time.** Code uses `time` directly. Tests use `testing/synctest` and
  event gates, never `time.Sleep`.
- **Public repo.** No private path, layout data or consumer wiring. orion
  is named only as the downstream consumer.

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

### C2 — `schema/` and `internal/config`

The Zod schema package, `default.json`, the JSON loader, `Validate`, the
drift test, and `configtest`. Ports `validate.test.ts` and
`profile.test.ts`. Adds a `schema` CI job that runs
`bun install --frozen-lockfile`, `tsc --noEmit` and `bun test` in
`schema/`.

Interfaces:

- `schema/package.json` names the package `tessera-wm`, with `zod` 4 as a
  peer dependency.
- `schema/profile.ts` exports a Zod schema and its `z.infer` type under
  each name `src/config/types.ts` exports: `DisplayName`, `TrackKind`,
  `Weights`, `WeightDefaults`, `WindowSpec`, `DeskLayout`, `DeskSlot`,
  `Topology` and `Profile`.
- `schema/default.ts` and `schema/full.ts` each export
  `const profile: Profile`.
- `schema/profile.test.ts` writes `internal/config/default.json` and
  `internal/config/testdata/full.json` when `UPDATE=1`, and otherwise
  compares them.
- `Load(ctx context.Context, env func(string) string, home string) (*Profile, error)`
- `Parse(path string, src []byte) (*Profile, error)`; `path` is used only
  in errors
- `Validate(p *Profile) error`; it also sets `AppRE` and `TitleRE`
- the structs in § Profile format, and `DisplayName` and `LayoutKind` with
  their constants.

Gate:

- `tsc --noEmit` and `bun test` pass in `schema/`;
- in `bun test`, `Profile.parse` rejects an unknown key in an object that a
  callback returns, a stack with two tracks, and a `kind` of `grid`;
- the drift test passes on `full.json`;
- JSON with an unknown key fails `Parse`, and JSON with no `app` or no
  `width` fails `Validate`;
- the pattern `(?<=x)y` fails `Validate` with the `regexp` error;
- each `validate.test.ts` case fails or passes through `Parse`, as in TS;
- an unknown name in `tracks` fails;
- a `profile.ts` with no `profile.json`, and a `$TESSERA_PROFILE` that
  ends in `.ts`, fail loudly;
- the embedded `default.json` loads.

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

- `AcquireLock(path string) (*flock.Flock, bool, error)`, which calls
  `TryLock`; the caller defers `Unlock`
- `IsQuiet(now, stamp time.Time, d time.Duration) bool`
- `IsFlexQuiet(now, own, display time.Time) bool`
- `SignalsSuppressed(path string, now time.Time) (bool, error)`

Gate:

- a held lock makes a second `AcquireLock` return `false, nil`, in-process;
- a SIGKILLed child's lock is free at once, with no reclaim step;
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
- `Listen(ctx context.Context, path string) (net.Listener, error)`; it
  holds the `<path>.lock` flock and returns `ErrRunning` if it is held
- `Serve(ctx context.Context, ln net.Listener, h *Hub, wake func(context.Context) error) error`
- `RegisterSignals(ctx context.Context, src wm.EventSource, sock string) error`
- `type Launchctl interface { Run(ctx context.Context, args ...string) error }`
- `Bootstrap(ctx context.Context, lc Launchctl, uid int, svcs []Service) (Owned, error)`
- `Bootout(ctx context.Context, lc Launchctl, uid int, owned Owned) error`

Gate, in synctest:

- a burst of N events gives one cascade;
- an event during work re-arms the waiter;
- H2: a held guard restamps;
- H7: flex waits for display-quiet;
- no action holds a tess path;
- a timeout ends `Serve`.

Outside the bubble:

- two concurrent starters on a stale socket file: one listener, one
  `ErrRunning`;
- a `kill -9`'d holder's lock frees, and the next `Listen` binds;
- enable runs before bootstrap; an already-loaded service is kickstarted,
  not owned, and never booted out; a failed bootstrap leaves it not owned;
- with one service already loaded and the other bootstrapped successfully,
  only the bootstrapped one is recorded as owned, and clean shutdown boots
  out exactly that one.

### C9 — `internal/cli` and `cmd/tess`

The cobra command tree built from the kind table, `daemon`, `wake`, `main`,
and `tools/benchstartup`.

Interfaces:

- `Parse(ctx context.Context, argv []string, version string, stdout, stderr io.Writer) (commands.Command, error)`;
  a nil command with a nil error means cobra printed help, the version or a
  completion script. `Main` passes `deps.Version`, which `cmd/tess` sets
  from `main.version`
- `type UsageError struct { Msg, UseLine string }`, returned by `Parse` for
  every cobra failure
- `Main(ctx context.Context, argv []string, stdout, stderr io.Writer, deps Deps) int`

`Parse` always passes a non-nil slice to `SetArgs`. With a nil slice, cobra
v1.10.2 reads `os.Args[1:]` (`Command.ExecuteC`), which would make the
tests read the test binary's flags.

Gate:

- each of the 21 kinds parses to its `commands.Command`;
- `bogus` gives a `*UsageError` that names `"bogus"`, and `Main` exits 1;
- a choice outside `ValidArgs`, an extra argument, and `focus-slot 2.0`
  give a `*UsageError`;
- bare `tess`, `--help`, `snap --help` and `--version` return a nil
  command and exit 0;
- `completion zsh` exits 0, and `__complete snap ""` lists the three
  choices;
- the stub-yabai corpus;
- SIGTERM during a stub-blocked `apply` exits 130 and leaves the lock free;
- `wake` with no daemon exits 1.

### C10 — Cutover

This commit:

- switches to `buildGoModule`;
- removes the bun attrs and the root Bun CI job; the `schema` job stays;
- adds `VERSION`, carrying over `package.json`'s current version, and
  points the flake (`pkgs.lib.fileContents ./VERSION`) and `release.yml`
  (`tr -d '[:space:]' < VERSION`) at it;
- sets `profilePath` to `profile.json`;
- updates the README, which now says to import `Profile` from `tessera-wm`
  and render the profile to JSON, and `devenv.nix`;
- deletes `src/`, the root `package.json` and `bun.lock`, the other root
  Bun manifests, and the TS scripts.

Gate:

- `nix flake check` and `nix build` pass on `macos-latest`;
- `--version` matches `VERSION`;
- no file in the repo reads the root `package.json`;
- the bar holds (bench table in the PR body);
- no `.ts` file remains outside `schema/`;
- a live smoke: plug and unplug, a window-churn burst, `kill -9` the daemon,
  and observe the reclaim.

## Tasks

- [ ] C1 — module, lint, CI `go` job
- [ ] C2 — Zod schema package, JSON loader, `Validate`, drift test, name references
- [ ] C3 — `wm` contract and fake, with labelled `AddSignal`
- [ ] C4 — engine, with sealed `Op` and `ConvergeAction`
- [ ] C5 — yabai driver and stub yabai
- [ ] C6 — effects and executor, with `gofrs/flock` locks and cancel-releases-lock
- [ ] C7 — commands, with `Wake`
- [ ] C8 — daemon: hub, socket, registration, launchd, synctest gates
- [ ] C9 — cobra CLI and `main`: completion, exit 130, argv corpus, `benchstartup`
- [ ] C10 — flake cutover, `src/` deleted, the bar holds, live smoke
- [ ] Downstream — the consumer's TS profile, its JSON render and its service wiring (§ Migration)

## Resolved decisions

Matt ruled on RIG-4526:

1. **Profile.** "want types." Matt also asked about typed config
   languages. Resolved decision 7 settles the format.
2. **Help and errors.** "ii, we don't care about parity, just build in the
   best way possible". The parity contract, the captured TS goldens and
   their CI step are gone. The help and errors are idiomatic Go (§ CLI).
3. **Landing.** "Just one super PR is fine". There is one PR with commit
   slices (§ Plan).
4. **Daemon.** "Keep. daemon is best pattern for this imo, yabai and skhd
   are already daemons/services". It is built here in Go and lands in the
   same PR (§ Resident daemon).

On 2026-10-05 Matt ruled on libraries:

5. **Libraries.** "i'd rather use libraries if they are well adopted." He
   added that "stdlibs tend to lag behind with features most of the time."
   The dependency bar is in § Global Constraints. Under it, the CLI moves to
   cobra, the locks move to `gofrs/flock`, slice transforms the standard
   library lacks use `samber/lo`, and the tests use `gotest.tools/v3`.
   `samber/oops` stays (Resolved decision 6). The debounce, the socket
   line protocol and the
   `launchctl` calls stay as written, for the reasons under § Alternatives
   considered and § Resident daemon.

On 2026-10-06 Matt ruled on errors:

6. **samber/oops.** "keep oops." It is below the importer bar (332).
   The daemon uses it so a logged error keeps its attributes and stack.

On 2026-10-10 Matt ruled on the profile format:

7. **TS rendered to JSON.** The profile is a TS module. `tsc` checks it,
   `bun test` tests it, and the downstream renders it to JSON at build time
   with `@rigelbuild/config-render`. tess reads only JSON. The schema
   source of truth is Zod, in tessera, shipped as a small TS package beside
   the Go structs and `Validate`. Patterns are strings that `Validate`
   compiles with `regexp`. Config languages and an embedded JS engine are
   rejected (§ Alternatives considered).
