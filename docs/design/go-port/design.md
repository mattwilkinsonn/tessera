# Design — Port `tess` from Bun/TypeScript to Go

*Design record for RIG-4487. Adds to the frozen records in `docs/design/` and
supersedes `effect4-cli.md`. Only the language, the CLI front end, the profile
format and the build change. The parity contract and the superseded text of
each frozen record are in [`parity-contract.md`](parity-contract.md).*

## Problem / Intent

Every skhd keybind spawns `tess`. A `bun build --compile` binary spends about
19 ms starting the runtime before `main` runs (the base row in
`effect4-cli.md` § "Startup-latency budget"). Matt chose to port `tess` to Go,
so the keybind path becomes a static binary that starts in a few
milliseconds. Rust is rejected, and Effect is dropped. This record says how
to port with no change a caller can see.

## Approach

**Re-realize, not port.** The TS source is the spec, not the template. Tests
are transcribed exactly; the code is native Go. Constructs that exist only to
mirror the TS shape are deleted: the two-front-end CLI (`fastMatch` plus the
Effect tree), the per-waiter injection objects in `RunOpts`, and the
`bun build --compile` packaging. The engine, effects, executor and driver keep
their behaviour and their layering.

### Module layout

The module path is `github.com/mattwilkinsonn/tessera`. The layering law in
`architecture.md` § "Global Constraints" becomes the import rules below:

- `config`, `wm` and `effects` import no internal package.
- `engine` imports `config` and `wm`.
- `executor` imports `engine` and `wm`.
- `commands` imports all of the above.
- `cli` imports `commands`.
- Only `cmd/tess` imports `wm/yabai`.

| TS source | Go package |
| --- | --- |
| `src/config/*.ts`; `loadProfile` in `src/index.ts` | `internal/config` (`default.yaml` embedded) |
| `src/config/profile.fixture.ts` | `internal/config/configtest` |
| `src/driver/types.ts`, `src/engine/world.ts` | `internal/wm` |
| `src/driver/yabai.ts`, `src/driver/fake.ts` | `internal/wm/yabai`, `internal/wm/fake` |
| `src/engine/*.ts` except `world.ts` (13 modules) | `internal/engine`, one file each |
| `src/exec.ts` | `internal/executor` |
| `src/effects/*.ts` | `internal/effects` |
| `src/commands.ts`; `Command`, `RunOpts`, `run` in `src/index.ts` | `internal/commands` |
| `src/cli/*.ts`; the entry guard in `src/index.ts` | `internal/cli`, `cmd/tess` |

`internal/engine` stays one package. Its modules call each other freely, and
splitting it would add import edges the TS tree does not have.

Every TS union becomes a sealed interface. Each one is marked
`//sumtype:decl`, has an unexported marker method, and is checked by
`gochecksumtype`:

- `engine.Op` covers the `PlanOp` variants (`src/engine/plan.ts`).
- `engine.ConvergeAction` is nested inside `Op` (`interface{ Op;
  convergeAction() }`) and covers the `ConvergeAction` variants
  (`src/engine/laptop.ts`). The converger can therefore only emit converge
  actions.
- `commands.Command` has one typed struct for each of the 22 kinds in the TS
  `Command` union (the 21 `SUBCOMMANDS` plus `init`). Its payload types
  (`DirSel`, `CycleDir`, `InsertDir`, `ResizeDir`, `SnapMode`, `SpaceLayout`)
  stay distinct.

### Driver contract in Go

`wm.Driver` matches `WmDriver` (`src/driver/types.ts`) method for method,
with three changes:

- **Context and errors.** Every method takes `ctx` first and returns an
  error, so a `Promise<boolean>` becomes `(bool, error)`. A method fails in
  exactly the cases where the TS method throws. The two focus queries keep
  the `#jsonOrNull` rule: a nonzero exit or empty output means "none".
- **Optional capabilities.** The optional `rules` and `events` capabilities
  become the interfaces `wm.RuleOps` and `wm.EventSource`. Callers find them
  by type assertion.
- **Settle unit.** `settleMs` becomes `SettleUnit() time.Duration`.

`wm.Snapshot` runs its three queries under errgroup, as `Promise.all` does in
`worldSnapshot`. The yabai runner is `exec.CommandContext`, so cancelling the
root context stops a running yabai child.

### CLI front

`internal/cli` holds one table: `SUBCOMMANDS` from `src/cli/grammar.ts`
without the `fastPath` field, plus `init`. One parser handles every argv;
there is no fast path because there is no runtime graph to skip. The parser
keeps today's grammar:

- **Help and version.** `--help`/`-h` and `--version`/`-v` may appear
  anywhere in argv. Help is scoped to the named subcommand.
- **Positionals.** Each subcommand takes zero or one positional: a literal
  from its choice list, or an `isIntToken` integer (`-1` is accepted).
- **`init` flag.** `init` takes `--self PATH` or `--self=PATH`.
- **Parse failure.** Any other argv prints help on stdout and `\nERROR\n
  <message>` on stderr, then exits 1.

The output bytes depend on Open Question 2. In every option, the CLI corpus
covers these edge argv: `--help snap`, a repeated `--self`, `--`, `--foo`,
`focus-slot -1`, and every parse failure in `src/cli/commands.test.ts`.

### Parity contract

[`parity-contract.md`](parity-contract.md) holds the full contract. Two
items in it are new to the port:

- **Signals.** SIGINT and SIGTERM release locks and exit 130, as under the
  Effect `runMain` today. `cmd/tess` uses `signal.NotifyContext`, so
  deferred releases run. Without this, a signalled `apply` would leave its
  no-reclaim lock behind forever.
- **JS formatting.** `toFixed` tie rounding is reproduced in `jsFixed` and
  `jsString`, and the `parseInt` prefix rule is reproduced for stamp and PID
  files.

Everything else in it is unchanged: exit codes, stderr and argv text, the
`/tmp` protocol, `init --self` and RE2 matching. Output order never depends
on map iteration, and the display slots stay fixed on purpose.

### Profile format

The profile is YAML, and the Go loader is its only validator (Open Question
1).

- **Shape.** Fields keep their TS names. A `RegExp` becomes a pattern
  string, `laptopStackApps` becomes a list, and a TS const that a track list
  reuses becomes a YAML anchor.
- **Resolution.** The order is the same as `loadProfile`'s; only the file
  name changes, to `tessera/profile.yaml`.
- **Switch-over guard.** If the well-known `profile.ts` exists and
  `profile.yaml` does not, the load fails with `profile.ts is no longer
  read; convert it to profile.yaml`. skhd is started by launchd and never
  sees `$TESSERA_PROFILE`. Without this guard it would silently fall back to
  the placeholder default.
- **Decoding.** `go.yaml.in/yaml/v3` decodes with `KnownFields(true)`. A
  missing field decodes to its zero value, so `config.Validate` checks
  presence. The rule: every field that is required (has no `?`) in
  `src/config/types.ts` must be present, at every depth. That means the six
  top-level fields and all three display slots, plus `displays.*.width`,
  `windows.*.app`, the desk layout fields (`display`, `label`, `kind`,
  `tracks`), `deskSlots[*].name`, and topology `name`, `displays` and `desk`.
  A nil required regexp is a load error, never a nil that reaches
  `matchesSpec`. This replaces `satisfies Profile`. The `validateProfile`
  rules then run unchanged. Consumers check a profile by running the real
  loader.
- **Schema.** The schema exists for the editor only. It is generated with
  `invopop/jsonschema`, using `Reflector{FieldNameTag: "yaml"}`, so schema
  keys and required-ness follow the yaml tags: a field without `omitempty`
  is required. A `Reflector.Mapper` matches `regexp.Regexp` (the reflector
  dereferences pointers first) and maps it to `{type: string, format: regex}`. The schema is checked in and installed at
  `share/tessera/profile.schema.json`. The bundled default and every
  fixture profile must pass it, and the negative corpus must fail both it
  and the loader.

### Parity proof

1. **Tests ported case for case.** Each of the 268 `test(` calls, plus the
   loop-generated cases, becomes a Go test named after its TS title.
2. **Goldens captured from the shipping TS.** `scripts/capture-goldens.ts`
   writes each golden family in the slice that consumes it:
   - `profile` (T2): the TS profiles as data.
   - `trace` (T7): for each scenario, the driver calls, the final world, the
     exit code and stderr. Arguments are recorded with `JSON.stringify`.
     Mutators compare in order; runs of consecutive queries compare as
     multisets.
   - `cli` (T8): stdout, stderr and the exit code from the `nix build` TS
     binary, for the argv corpus.
   - `proc` (T8): the yabai argv each binary sends when `TESS_YABAI` points
     at a recording stub serving `windows.live.json` queries. One case per
     keybind subcommand and choice. This checks the real process boundary.

   While `src/` exists, a CI step re-runs the capture and fails on any diff,
   so no golden goes stale.
3. **Reused fixtures.** `windows.live.json` and the argv tables in
   `src/driver/yabai.test.ts` move over unchanged.

### Build and the startup bar

`packages.default` becomes `buildGoModule`; the builder sets `-trimpath`
and `disallowedReferences = [ go ]`. The bun attrs, `bun2nix` and
`checks.bun-lock` are removed. `installCheckPhase` adds two checks: `init`
without `--self` exits 1, and a loader smoke exits 0. The startup bar is
`effect4-cli.md` § "Startup-latency budget", unchanged. Its protocol moves
to `scripts/benchstartup`, and the base is the TS build at T9's parent.

### Frozen records

`effect4-cli.md` is superseded in full. In the other four records, this
record supersedes the Runtime/tooling constraints, the Bun build text and
the "config is a TS module" line. Everything else stands. The per-record
list is in [`parity-contract.md`](parity-contract.md). The daemon is Open
Question 4.

### Migration

Slices T1 to T9 form one linear stack. The flake switches once, in T9, which
also deletes the TS tree. Open Question 3 decides how the stack lands.

After T9, one atomic orion PR bumps the input, converts the profile, and
replaces the consumer typecheck with a loader check. To roll back, revert
that PR, not just the pin.

## Alternatives considered

One line per rejected option. The four open forks are under § Open
Questions.

- **Profile (b): keep `profile.ts`, evaluated to JSON by bun at build time.**
  Bun and the TS type package would stay in the consumer's build after the
  TS tree is gone, and the runtime format would be JSON anyway.
- **Profile (c): a Go profile compiled into the binary.** A private layout
  must load at runtime and never enter the public build. A layout edit would
  also need a rebuild.
- **Profile as TOML.** `desk` and `topologies` are nested arrays of tables,
  and TOML has no anchors.
- **Profile as JSON only.** The bundled default is a teaching file, and JSON
  has no comments. YAML loads JSON anyway.
- **Ordered `displays` list instead of fixed slots.** It would change the
  `move-display` choices, which is a behaviour change.
- **stdlib `flag`.** It has no subcommands and stops at the first
  non-flag. It also accepts `-self` and prints its own usage.
- **cobra, urfave/cli, kong.** Each brings its own help layout, error
  wording and exit codes. One positional and one flag across 22 kinds does
  not earn a dependency.
- **Keep a fast path.** The fast path existed only to skip Effect's import
  graph. Go has no such graph.
- **Flat `Command` struct.** One `Dir string` would carry four TS types.
- **Ship Go beside TS and switch per subcommand.** This is a long-lived dual
  implementation.
- **One unsliced PR.** About 5,800 lines of TS and 268 tests would have no
  per-slice review gate.

## Global Constraints

Every task inherits these constraints.

- **Toolchain.** `go.mod` declares `go 1.26`, the pinned nixpkgs Go (1.26.7).
  The devenv shell uses that nixpkgs Go and its `golangci-lint` (2.13.2).
  The CI `go` job also uses that Go and runs on `ubuntu-latest`.
- **Dependencies.** The allow-list is:
  - runtime: `go.yaml.in/yaml/v3` v3.0.5, `golang.org/x/sync`,
    `github.com/samber/oops`, and `github.com/samber/mo` (`Option` only);
  - generator: `github.com/invopop/jsonschema` v0.14.0;
  - tests only: `github.com/santhosh-tekuri/jsonschema/v6` v6.0.3 and
    `github.com/google/go-cmp` v0.7.0.

  Anything else needs the reviewer's sign-off.
- **House Go slate.** Matt's Go idioms record ("re-realize, not port") and
  its five write-time rules apply as written. Lint is golangci-lint with
  `default: all` and a reasoned disable-list, and `exhaustive` and
  `gochecksumtype` are on.
- **Time is injected.** `effects.Clock` replaces `WaiterDeps` and the
  `Sleep` parameters. Tests use a fake clock or `testing/synctest`.
- **Parity contract binds the port.** Every item in `parity-contract.md` is
  a constant or a golden, and stays fixed from T1 to T9. After T9 the goldens
  are ordinary regression tests: a deliberate change edits its golden in its
  own PR.
- **Ported-test ledger.** Each PR body maps every TS test title in its scope
  to the Go test that ports it. Only two kinds of TS test may be dropped:
  - tests of deleted machinery (`fastMatch`, `RunDeps`, the Effect error
    classes);
  - tests of a TS type ("app matcher is a RegExp").

  The ledger names each dropped test and the reason.
- **Tessera is public.** Code, comments, fixtures and records name no
  private-repo path and hold no private layout data.

## Plan

The slices form one linear jj-vine stack, T1 → T9. Each slice is one PR
carrying its ported tests. Open Question 3 decides when they merge.

**Signature rule.** Each exported TS function becomes an exported Go
function:

- it has the same parameters, in the same order;
- `Profile` becomes `*config.Profile`;
- `ctx context.Context` comes first, but only if the function does I/O;
- a function that can fail returns `(T, error)`;
- `T | null` becomes `(T, bool)`.

The tasks below list only the signatures this rule cannot derive.

### T1 — Skeleton and CI

Add `go.mod`, `.golangci.yml`, the devenv Go toolchain and a `go` job in
`ci.yml`. The job runs `go vet`, `golangci-lint run` and `go test -race
./...` on `ubuntu-latest`. Add `scripts/capture-goldens.ts` with a
`--check` mode, and a CI step that runs it while `src/` exists.

Interfaces:

- Produces: the module, the lint config, the CI job and the capture script.
- Gate: CI is green, and `capture-goldens --check` passes on an empty
  corpus.

### T2 — `internal/config`

Port the types, `ChainRatios`, `Validate` (with the required-field and slot
checks), the YAML loader, the switch-over rule, the embedded default,
`configtest`, the schema generator and `profile` goldens. Port
`validate.test.ts`, `profile.test.ts` and `loader.test.ts`.

Interfaces:

- Produces:
  - `type Profile struct`. Its `Displays` field is
    `struct{ G9, Aw, Laptop *DisplaySpec }`, and its `LaptopStackApps` field
    is `[]string`.
  - `type WindowSpec struct { App, Title *regexp.Regexp; TitleInvert bool; Spawn []string }`
  - `func Validate(p *Profile) error`. Violations are joined with `; `, in
    TS order.
  - `func Load(env func(string) string, home string) (*Profile, error)`
  - `func Parse(path string, data []byte) (*Profile, error)`
  - `configtest.Fixture(tb testing.TB) *Profile`
  - `schema/profile.schema.json`, built by `go generate`
- Gate:
  - Ported tests are green.
  - Both YAML files decode to their `profile` goldens.
  - The bundled default and every fixture profile pass the generated
    schema. The negative corpus fails both the loader and the schema.
  - Loader regression cases fail with a named-field error, not a nil:
    `windows: { arc: {} }` (missing `app`), and a desk layout missing each
    of `display`, `label`, `kind` and `tracks`.
  - A well-known `profile.ts` with no `profile.yaml` fails loudly.
  - `go generate` leaves no diff.

### T3 — `internal/wm` and `internal/wm/fake`

Port the driver contract, `WorldSnapshot` and `FakeDriver`, plus a call
recorder. Port `fake.test.ts`.

Interfaces:

- Produces:
  - `type Driver interface`: every `WmDriver` method, with `ctx` first and
    an error, plus `SettleUnit() time.Duration`.
  - `type RuleOps interface { ListRules(ctx) ([]string, error); RemoveRule(ctx, label string) error; AddRule(ctx, Rule) error; ApplyRules(ctx) error }`
  - `type EventSource interface { RegisterSignal(ctx, Event, []string) error }`
  - `func Snapshot(ctx context.Context, d Driver) (World, error)`
  - `fake.New(seed fake.Seed) *fake.Driver`
  - `fake.Record(d wm.Driver) (wm.Driver, *fake.Calls)`. It records
    `name(args)` with each argument JSON-encoded, which matches the capture
    script.
- Gate: green under `-race`.

### T4 — `internal/engine`

Port the 13 modules and their tests.

Interfaces:

- Produces:
  - `type Op interface` (sealed). Its variants are `DestroySpace`,
    `RealizeLayout` and `BalanceSpace`, plus every `ConvergeAction` variant.
  - `type ConvergeAction interface{ Op; convergeAction() }`. Its variants
    are `RelabelHome`, `CreateSpace`, `MoveWindow`, `RehomeAndDestroy`,
    `MoveSpace`, `SetLayout` and `DestroySpace`.
  - `func LaptopConvergeStep(p *config.Profile, w wm.World, s ConvergeState) (ConvergeAction, ConvergeState, bool)`.
    The bool is false when the converger is done.
  - `func WeightsFor(p *config.Profile, kind config.TrackKind, count int, display mo.Option[config.DisplayName], override []float64) []float64`
  - `func DisplayOfSpace(p *config.Profile, w wm.World, space wm.SpaceID) (config.DisplayName, bool)`
  - `func NewClaimSet(p *config.Profile) *ClaimSet`, with these methods:
    - `Claim(ws []wm.Window, name string, preferDisplay mo.Option[int]) (int, bool)`
    - `ClaimMany(ws []wm.Window, names []string, preferDisplay mo.Option[int]) []int`
    - `Reset()`
- Gate: every engine test is green, including the `windows.live.json`
  matcher cases.

### T5 — `internal/effects` and `internal/executor`

Port the locks, stamps, guard, flex-order state, nudge, paths, the waiter,
`runPlan` and `runConverge`, with their tests.

Interfaces:

- Produces:
  - `func AcquireLock(dir string) (*Lock, error)`. It returns nil on live
    contention, reclaims a stale PID and treats EPERM as alive.
  - `func AcquireLockOrSkip(dir string) (*Lock, error)`. It does no reclaim.
  - `func (l *Lock) Release() error`. Release is idempotent.
  - `type Clock interface { Now() int64; Sleep(ctx context.Context, d time.Duration) error }`
  - `func RunWaiter(ctx context.Context, cfg WaiterConfig, clock Clock) (bool, error)`
  - `type Paths struct` holding the eight lock, stamp, guard and flex paths.
    `DefaultPaths(env func(string) string, home string) Paths` returns the
    defaults.
  - `type Nudger func(ctx context.Context, event string) error`
  - `func RunConverge[S any](ctx context.Context, d wm.Driver, step StepFunc[S], initial S) (S, error)`
- Gate:
  - Ported tests are green, and they wait on events, not sleeps.
  - A child process killed with SIGKILL leaves a lock that `AcquireLock`
    reclaims.
  - A cancelled context makes `RunWaiter` release its lock.

### T6 — `internal/wm/yabai`

Port the argv builders, the normalizers, `bspSteps`, `ratioArg`, the runner
and `Driver`. Port `yabai.test.ts`.

Interfaces:

- Produces:
  - `func New(yabaiPath string) *Driver`. It implements `wm.Driver`,
    `wm.RuleOps` and `wm.EventSource`.
  - `jsFixed(x float64, digits int) string` and `jsString(x float64) string`
- Gate:
  - The argv tables are green.
  - `jsFixed(5.0/32, 4)` returns `0.1563`.
  - The `0.9473684210526315` clamp warning matches byte for byte.
  - A missing binary gives `yabai query failed (exit 1): …`.

### T7 — `internal/commands` and trace goldens

Port the command functions and `Run`, `commands.test.ts`, and
`index.test.ts` except `fastMatch`. Capture the `trace` goldens.

Interfaces:

- Produces:
  - `type Command interface` (sealed). It has one struct per kind:
    `Apply{}`, `Snap{Mode engine.SnapMode}`, `Focus{Dir wm.DirSel}`,
    `Init{Self string}`, … (22 in all).
  - `type Env struct { Paths effects.Paths; Nudge effects.Nudger; Clock effects.Clock; Stderr io.Writer }`
  - `func Run(ctx context.Context, p *config.Profile, c Command, d wm.Driver, env Env) (int, error)`
- Gate: every trace golden matches. The `init` registration test is green.

### T8 — CLI, `cmd/tess`, process goldens

Port the parser, help, version, error block and `main`. Port
`scripts/benchstartup`. Capture the `cli` and `proc` goldens from the
`nix build` TS binary.

Interfaces:

- Produces:
  - `func Parse(argv []string) (Result, error)`
  - `func Main(ctx context.Context, argv []string, stdout, stderr io.Writer, deps Deps) int`.
    It returns 130 when `ctx` was cancelled by a signal.
  - `cmd/tess/main.go`. It sets up `signal.NotifyContext(…, os.Interrupt,
    syscall.SIGTERM)` and exits through `os.Exit(cli.Main(…))`.
- Gate:
  - Every `cli` golden passes, at the level Open Question 2 picks.
  - Every `proc` golden passes against the stub yabai.
  - A SIGTERM sent during a stub-blocked `apply` exits 130 and leaves no
    lock dir.

### T9 — Cutover

This slice does six things:

- moves `packages.default` to `buildGoModule`;
- removes the bun attrs, `bun2nix` and `checks.bun-lock`;
- changes the home-manager default `profilePath` to `profile.yaml`;
- moves the version file and has `release.yml` read it;
- rewrites the README and `devenv.nix`;
- deletes `src/`, the Bun manifests and configs, the TS scripts, and the Bun
  and capture CI steps.

Interfaces:

- Produces: the Go flake package with the installed schema, and the bench
  table in the PR body.
- Gate:
  - `nix flake check` and `nix build .#default` pass on `macos-latest`.
  - The bar holds against the TS build at T9's parent.
  - No `.ts` file remains.

### Downstream (orion, not designed here)

One atomic PR follows T9. It bumps the input, converts the profile, points
`profilePath` at `profile.yaml`, and replaces the consumer typecheck with a
loader check (`TESSERA_PROFILE=… tess focus east` against a stub yabai).

## Tasks

- [ ] T1 — module, lint, the CI `go` job on ubuntu, and the capture script
  with its `--check` step
- [ ] T2 — `internal/config`: required-field and slot checks, the switch-over
  rule, the generated schema, the negative corpus, and the `profile` goldens
- [ ] T3 — `internal/wm` and `internal/wm/fake`, with the JSON-argument
  recorder
- [ ] T4 — `internal/engine`, including the sealed `Op` and the nested
  `ConvergeAction`
- [ ] T5 — `internal/effects` and `internal/executor`, with
  cancel-releases-lock
- [ ] T6 — `internal/wm/yabai`, with `jsFixed` and `jsString`
- [ ] T7 — `internal/commands`, with the sealed `Command` and the `trace`
  goldens
- [ ] T8 — `internal/cli` and `cmd/tess`: `NotifyContext` and exit 130, the
  `cli` and `proc` goldens, and `benchstartup`
- [ ] T9 — `buildGoModule` cutover and TS tree deleted; the bar holds
- [ ] Downstream — one atomic orion PR: input bump, profile conversion and a
  loader check

## Open Questions

1. **Profile authoring surface.** There are three options:
   - **(a)** YAML with a hand-written schema.
   - **(a′)** YAML, with the Go loader as the only validator and a schema
     generated from the Go types for the editor.
   - **(d)** A typed home-manager option (`programs.tessera.profile = { … }`),
     rendered to YAML by the module, with Nix eval type-checking it.
     Non-Nix users would still write YAML.

   **Recommend (a′).** It is the shape this record is written for. (a) lets
   the schema and the decoder drift with no failing test. (d) is worth it
   only if Matt wants to edit his layout in Nix, and it adds a second
   authoring surface the Go loader must still validate. The decision needed
   is (a′) or (d).
2. **Help-text parity.** There are three options:
   - **(i)** Help and errors byte-identical to Effect, including the row
     caps in `renderTable` and the tie rules in `suggest`.
   - **(ii)** The same exit codes and message lines, with help re-laid out
     in Go.
   - **(iii)** Byte-identical exit codes, stream split, `ERROR` block,
     message lines and `Did you mean this?`, plus containment goldens for
     help: the usage line and every subcommand name and description.

   **Recommend (iii).** No caller reads the help layout; the flake smokes
   grep only four strings. Option (i) re-implements a deleted dependency's
   rendering for no reader. The edge-argv corpus applies under every option.
3. **Landing shape.** There are two options:
   - **Land T1 to T8 on `main` one at a time.** The Go tree is tested but
     ships nothing for the length of the port. Every TS fix in that window
     is also needed in Go. A reviewer may read the unshipped slices as
     inert under `rule://no-inert-gating`.
   - **Review each slice as its own PR, then merge the whole stack in order
     after T9 is approved.** The merge is by hand; tessera has no merge
     queue.

   **Recommend merging the stack together.** `main` never holds unshipped
   Go code, the dual-fix window shrinks to rebases, and the no-inert-gating
   question goes away. The cost: the stack stays open for the whole port,
   and each rebase re-runs `capture-goldens --check`.
4. **Resident daemon.** The port changes the three forces in
   `resident-daemon.md` § "Problem / Intent" as follows:
   - **Force 1, the self-path, is unchanged.** `init --self` stays. Using
     `os.Executable` instead could resolve the nix-darwin symlink to a
     `/nix/store` path, which garbage collection deletes.
   - **Force 2, the cold start of a ~50 MB Bun binary per signal, goes
     away.**
   - **Force 3, simpler concurrency, is unchanged.** T5 ports the
     locks, stamps and waiter in full.

   There are four options:
   - **(i)** Keep the daemon, with a Go record after T9.
   - **(ii)** Shelve it until a measurable trigger fires.
   - **(iii)** Close it.
   - **(iv)** Build the daemon in Go instead of porting T5's waiter, stamp
     and lock code. This conflicts with the no-behaviour-change framing.

   **Recommend (ii).** Reopen when either of these happens:
   - the T9 bench's Go `focus east` p90 (the per-signal process cost)
     exceeds 10 ms;
   - one of `architecture.md`'s other conditions appears: a second event
     consumer, or a stale lock wedging a run.

   Note that (ii) partly reverses the pace D-T1 and D-T2 set.
