# Design — Port the `tess` CLI to Effect 4 and `effect/cli`

*Design record for RIG-4048 ("Let's use effect 4"). Builds on the architecture*
*record (Layer 4 — the thin router); the engine, effects and driver keep their*
*behaviour. Lands before the resident daemon (`resident-daemon.md`).*

## Problem / Intent

`tess` has no `--help`: `parseArgs` in `src/index.ts` is a hand-rolled
`switch` over `argv[0]`, so `tess --help` prints the one-line `USAGE` string
and exits 2, and `tess apply --help` **runs the real `apply`** because the
router ignores every token after the ones it consumes. Matt decided on
RIG-4048 to replace the hand-rolled grammar with Effect 4's CLI module so
every subcommand gets generated `--help`, typed arguments with clear
rejections, and `--version`, while the skhd keybinds that invoke `tess`
dozens of times a day stay as fast as they are today.

## Approach

**Effect owns the parser, help and version. Both argv paths build the
existing `Command` union and call the existing `run()`.** The hand-rolled
`parseArgs` is replaced by an `effect/cli` command tree whose every handler
builds one `Command` value and hands it to `run(profile, command, driver,
opts)` exactly as `import.meta.main` does today. `run`, `RunOpts`, the
`Command` union, the exhaustive `switch`, `commands.ts`, `effects/` and
`driver/` are untouched, so every existing test keeps passing, and the
exhaustiveness test in `index.test.ts` stays the single guard that every
subcommand is dispatched. A thin argv pre-check keeps skhd keybinds off the
Effect graph (the latency fork, Open Question 1). The deeper port — a
`Driver` service seam with `Effect`-returning handlers — is recorded as
option (b) of that question, not designed here.

### What `effect@4.0.0` ships (verified from the installed package, 2026-10-02)

`npm view effect dist-tags`: `latest` is **4.0.0** (published 2026-10-01);
`@effect/platform-bun@4.0.0` peers on `effect ^4.0.0`. This supersedes the
RIG-4048 research (3.22.2 / rc.117). The surface this record uses, each
name read from `dist/*.d.ts`:

- Import path **`effect/cli`** — there is no `effect/unstable/cli` in 4.0.0.
  Every CLI module is still `@stability unstable` in its JSDoc.
- `Command.make(name, config, handler)`, `Command.withDescription`,
  `Command.withSubcommands([...])`, `Command.provide(layer)`,
  `Command.run(root, { version })`, `Command.runWith(root, { version,
  renderErrors })(argv)`. `Argument.Literals(name, choices)`,
  `Argument.Int(name)`, `Flag.String(name)`, `Flag.filter(pred, onFalse)`,
  `*.withDescription`.
- **Built-ins are removable:** `CliConfig.layer({ builtIns:
  [GlobalFlag.Help, GlobalFlag.Version] })` drops wizard, completions and
  log-level. Verified: help renders `--help, -h` and `--version, -v` only.
  This closes the Effect-TS/effect#6151 concern.
- Exit codes: `BunRuntime.runMain` → `Runtime.defaultTeardown`: 0 on
  success, 130 on interrupt-only, else the error's `[Runtime.errorExitCode]`
  (default 1). `CliError.ShowHelp` carries `errorExitCode = errors.length ?
  1 : 0` and `errorReported = false`. `runMain` also honours a
  `process.exitCode` set by a succeeding handler (verified: 3 → rc 3).
- `Command.Environment` = `FileSystem | Path | Terminal |
  ChildProcessSpawner | Stdio`; `BunServices.layer` provides all of them.
  Test layers: `Stdio.layerTest({})`, `Path.layer`,
  `FileSystem.layerNoop({})`, `Terminal.make({...})`,
  `ChildProcessSpawner.make(() => Effect.die("unused"))` — the shape
  `Command.d.ts`'s own examples use.

### The command surface

22 subcommands (`Command` union in `src/index.ts`): 15 take one positional
literal or integer (`snap`, `stack-cycle`, `resize`, `move-display`,
`cycle-display`, `focus`, `swap`, `warp`, `insert`, `space`, `focus-slot`
plus the no-arg `reset-splits`, `columns`, `toggle-float`, `balance`); six
composite no-arg commands (`apply`, `laptop`, `display-event`, `flex-event`,
`rules`, `display-setup`); and `init --self PATH`, the one flag. Callers:
**skhd binds every keybind command and also `apply` and `laptop`**
(`skhdrc`: `cmd + alt - a : tess apply`, `shift + cmd + alt - a : tess
laptop`, numpad 0 → `apply`); yabai signal actions registered by `init`
fire `display-event` / `flex-event`; yabairc runs `init --self`. Names,
literals and `--self` stay verbatim — skhdrc and the signal actions depend
on them. `--help`/`-h` and `--version`/`-v` become global flags; no caller
passes either today.

The full inventory with choice lists lives once, in T1's `SUBCOMMANDS`.

### Two front ends, one dispatcher

```text
argv ──► fastMatch(argv)  ──matched──► Command value ─┐
          │ (no "-" token, a fastPath subcommand,       ├─► run(profile, command, driver, opts)
          │  exact arity, literal ∈ choices,            │        (today's exhaustive switch)
          │  Number.isInteger for focus-slot)           │
          └──otherwise──► import("./cli/main.ts") ──► effect/cli parse ─┘
                            (help, version, errors, non-fastPath commands)
```

- **`fastMatch`** is pure and shares `SUBCOMMANDS` with the Effect tree. It
  settles only an argv the Effect parser would also accept for a
  `fastPath: true` subcommand; everything else — any `-` token, a bad
  literal, wrong arity, `apply`/`laptop`/`init`/events — falls through to
  the Effect CLI, which owns every message. Integer parity: the fast path
  accepts exactly what `Argument.Int` accepts on a flag-free token
  (verified on 4.0.0: `7`, `1e2`, `+1`, `1.0`, `007` accepted; `0x10`, `""`,
  `" 1"` rejected; `-1` is accepted by `Argument.Int` but has a `-` so it
  routes to Effect either way). `fastMatch` therefore uses the same
  predicate as today's `parseArgs` (`Number(arg)` + `Number.isInteger`)
  **and** rejects `0x`/empty/whitespace tokens so both paths agree; the
  `grammar.test.ts` parity table pins it.
- **The Effect tree** (`src/cli/commands.ts`) is generated from
  `SUBCOMMANDS` plus a hand-written `init`. Each handler maps its parsed
  input to a `Command` value (`{ kind: "snap", mode }`) and calls the one
  `dispatch(command)`; `dispatch` is `Effect.tryPromise` around
  `loadProfile()` + `run(profile, command, driver, opts)`, mapping a
  nonzero `run` result to `process.exitCode` (the `laptop` contended
  contract: exit 1, silent) and a throw to `DriverError`.
- **Driver construction is shared**: `src/cli/driver.ts` exports
  `liveDriver = () => new YabaiDriver({ yabaiPath: process.env.TESS_YABAI })`
  — `TESS_YABAI` is a real feature (tests and the startup bench point tess
  at a stub yabai; unset keeps `/opt/homebrew/bin/yabai`) and both front
  ends call it. The fast path's error line and the Effect path's
  `DriverError` line come from one `formatDriverError(cause)`.

`apply` and `laptop` are skhd-bound but stay on the Effect path: their
converge dominates their latency (seconds of settle sleeps), and `laptop`'s
contended exit must come from the one place that owns exit codes. The
field is named `fastPath`, not `keybind`, so a future keybind on a
lock-bearing command is not put on the no-Effect path by its name.

### Error model

Today: exit 2 on a parse failure, 1 on a contended `laptop`, 0 otherwise;
uncaught throws (yabai gone, broken profile) exit 1 with a stack trace.

| situation | value | stderr | exit |
| --- | --- | --- | --- |
| explicit `--help` / `--version` | none — the `GlobalFlag.Action` runs and the effect **succeeds** | help / version on stdout | **0** |
| bare `tess` | `CliError.ShowHelp`, 0 errors | root help on stdout | **0** |
| unknown subcommand / bad literal / missing `--self` / extra positional | `CliError.ShowHelp` with errors | help + `ERROR` block | **1** |
| `laptop` contended | `run` returns 1 → `process.exitCode = 1` | nothing | **1** |
| profile missing/broken | `ProfileLoadError` | one line (the `importProfile` message) | 1 |
| a command function threw (yabai gone — per `WmDriver` the only thrower; or a bug) | `DriverError` | one line (`Error.message`) | 1 |

Errors (`src/cli/errors.ts`) are `Data.TaggedError` classes whose markers
are **readonly class fields** — `Runtime.d.ts` declares both markers
`readonly` on `Error`, so assigning them later is a `TS2540` (verified):

```ts
export class ProfileLoadError extends Data.TaggedError("ProfileLoadError")<{ readonly cause: unknown }> {
	readonly [Runtime.errorExitCode] = 1;
	readonly [Runtime.errorReported] = false; // we print the one line ourselves
}
export class DriverError extends Data.TaggedError("DriverError")<{ readonly cause: unknown }> {
	readonly [Runtime.errorExitCode] = 1;
	readonly [Runtime.errorReported] = false;
}
```

A root `Effect.tapError` prints the one line and nothing else; `runMain`'s
reporter stays silent for these and still pretty-prints a genuine defect.

Two contract changes, both deliberate: parse failures exit **1** not 2, and
bare `tess` exits **0** with help not 2 with usage. No caller reads the 2
(skhd and signal actions ignore codes; the bash re-loop tests nonzero only
on `laptop`/`flex-event`). The flake smoke asserts both the code **and** a
parser-specific message (`Unknown subcommand`) so a handler failure cannot
masquerade as a rejection. Keeping 2 is Open Question 4.

### Lock handling

`src/effects/locks.ts` and the lock calls in `commands.ts` are unchanged.
`laptop` → `acquireLock(LAPTOP_LOCK)` with stale-pid reclaim: a LIVE holder
→ `"contended"` → exit 1 via `process.exitCode`, as today. `apply` →
`acquireLockOrSkip(APPLY_LOCK)`: a bare `mkdirSync`, no pid probe, so any
contention is a silent exit 0 — and a SIGKILLed `apply` leaves
`/tmp/yabai-apply-workspace.lock` behind until removed or reboot; that
stale-lock wedge is Open Question 2, not changed here. **Release on a
signal:** verified under `runMain` — SIGINT/SIGTERM interrupt the fiber and
exit 130; the wrapped Promise's `finally` does **not** run; the lock is
released by the `process.once("exit", release)` hook `claim()` installs,
and the signal guard self-expires by TTL. That is the contract the daemon
record inherits for ordinary shutdown.

### Startup-latency budget (the acceptance bar)

Measured with `scripts/bench-startup.ts`'s exact protocol (T6): **three
binaries built from this repo's real `src/` at `main`**, identical argv on
every row, the same `exit 1` yabai shim via `TESS_YABAI` on every row,
`HOME` at an empty dir, n=40, `Bun.spawnSync` wall clock, Bun 1.4.0 arm64,
no `--minify` (the flake build does not minify). "Base" is today's
`src/index.ts` plus the one-line `TESS_YABAI` change; "hybrid" is base +
the fast-match pre-check + `cli/main.ts` behind a dynamic import; "flat"
is `cli/main.ts` on every path.

| binary | `snap 3col` p50 / p90 | `focus east` p50 / p90 | `snap --help` p50 / p90 |
| --- | --- | --- | --- |
| base (today) | 18.8 / 22.4 ms | 18.4 / 20.2 ms | n/a |
| **hybrid** | **35.1 / 39.0 ms** | 34.3 / 37.5 ms | 43.2 / 46.2 ms |
| flat | 50.7 / 53.0 ms | 49.9 / 53.2 ms | 43.6 / 45.5 ms |

Binary size 64.67 MB vs 63.98 MB base (+0.7 MB). The hybrid's hot path is
**+16 ms (1.9×)** over today; the flat entry is **+32 ms (2.7×)**. The
hybrid's remaining delta is not the Effect graph (a trace line at the top
of `cli/main.ts` never fires on the hot path) — it is the larger bundle
that `bun build --compile` must map and the JSON-trailer scan it does
before `main`; `bun build --compile --bytecode` fails on this bundle
("Failed to generate bytecode"), so bytecode caching is not available.
The earlier 27 ms figure was a 5-command stub, not this source; it is
withdrawn.

**Bar (Global Constraints):** hot-path p50 ≤ 2.0× base on the same machine
and run, and p90 ≤ 45 ms absolute; help p50 ≤ 60 ms. The 2× bound
controls (36.8 ms here); the absolute p90 catches a pathological import
graph on a faster machine. The hybrid meets it (1.87×, p90 39 ms); the
flat entry does not (2.7×). The daemon record removes the per-event cold
start for signals, not for skhd keybinds, so this bar outlives it.

### Version pinning

Exact `"effect": "4.0.0"` and `"@effect/platform-bun": "4.0.0"` (no caret)
in `dependencies`; `bun.lock` committed; `bun install --frozen-lockfile` in
CI and in the nix build. `effect/cli` is `@stability unstable` by its own
JSDoc, so a 4.x minor may rename within it; an exact pin makes a bump a
deliberate PR that re-runs the smoke rows. `tsconfig.json` needs **no**
change: the package ships `.d.ts` under `exports`, and a JSON import with
`with { type: "json" }` is typed by TypeScript 6.0.3 without
`resolveJsonModule` (verified under `tsc --noEmit`, `moduleResolution:
bundler`). The house `effect-skills` corpus is 3.x; the PR body says so.

### Testing

- Untouched and green: `index.test.ts`'s `run` describes (exhaustiveness,
  simple dispatch, laptop contended, `init --self` registration),
  `commands.test.ts`, `exec.test.ts`, `driver/*.test.ts`, `engine/`,
  `effects/`. `RunOpts` keeps its `displayWaiter`/`flexWaiter` injections,
  so the event-command tests stay off the real `/tmp` waiter locks.
- `index.test.ts`'s `parseArgs` describes are **rewritten**, not re-pinned:
  the function is deleted. Their intent moves to (1) `fastMatch` table
  tests and (2) `src/cli/commands.test.ts` driving `Command.runWith` under
  the test layers with a `Layer.succeed(RunDeps, …)` carrying a
  `FakeDriver`, temp-dir `RunOpts`, and the fixture profile. Assertions:
  `Cause.squash(exit.cause)` is the error; `Runtime.getErrorExitCode(err)`
  is the code `runMain` would set (proven on 4.0.0 under `bun:test`: bad
  literal → `ShowHelp` whose `errors[0]` is `InvalidValue`, code 1; bare
  argv → `ShowHelp`, code 0; `init --self=-x` → `ShowHelp` carrying the
  `Flag.filter` message).
- **Production fast dispatch has a FakeDriver test**: `fastMatch` →
  `run(profile, command, fakeDriver, tmpOpts)` for every `fastPath`
  subcommand, asserting the same observable driver effect the Effect-path
  test asserts for the same argv (one `test.each` over both front ends).
- **`--help` never builds the production layer**: `commands.ts` exports
  `withDeps(layer)`; the test provides a counting `Layer.sync(RunDeps,
  ...)` and asserts `["snap","--help"]`, `["--help"]` succeed with the
  counter at 0 and `["snap","3col"]` reads 1 (proven on 4.0.0:
  `Command.provide` maps the layer over the handler and builds it lazily).
- `renderErrors: false` in tests; the production entry keeps the default.
- The startup bench is not a `bun test`; it is the T6 script, run by hand
  and pasted into the PR body.

### Release and flake impact

`flake.nix` compiles `src/index.ts` with no install step and asserts
`disallowedReferences = [ pkgs.bun ]`. Once `effect` is imported anywhere
— including behind a dynamic import, which `bun build` resolves at
bundle time — the nix sandbox needs `node_modules`, so **the flake change
lands in the same PR series as the dependency**, never after it (a PR
whose required `nix` job is knowingly red is not mergeable). Shape:
**`bun2nix`**, the house precedent in the `oh-my-pi` fork (`flake.nix`
input `github:nix-community/bun2nix` with `inputs.nixpkgs.follows`;
`nix/package.nix` uses `bunDeps = bun2nix.fetchBunDeps { bunNix =
./bun.nix; }`, `bun2nix.hook`, `bunInstallFlags = [ "--linker=isolated"
"--backend=copyfile" ]`, and a `checks.bun-lock` that regenerates and
`diff`s `bun.nix`). A hand-hashed fixed-output `node_modules` derivation
was rejected: the hash goes stale on every bump and Bun's install output
is not byte-stable across Bun versions. `nix build .#default` on
`macos-latest` stays the gate; `installCheckPhase` gains the new smokes.
The home-manager module, overlay and attr names do not move. Ships as
**0.2.0** (exit codes and `--help` are a CLI-contract change), bumped with
the flake change so the flake `version` and the release tag agree.

## Alternatives considered

- **Fix `--help` in the hand-rolled parser; no Effect.** Twenty lines, no
  dependency, 19 ms stays 19 ms. Rejected by Matt's RIG-4048 decision;
  it is the fallback if the latency bar fails and Matt reverses.
- **`node:util` `parseArgs`** — flags only; no subcommand tree, help or
  literal validation. Same gap with more ceremony.
- **Effect 3.x `@effect/cli`** — 3.x is no longer `latest`; porting to it
  now is porting twice.
- **Full service seam now** (`Driver`/`RunOpts` as `Context.Service`s,
  handlers calling `commands.ts` under `Effect.tryPromise`, `run` and the
  `Command` union deleted). It is Open Question 1 (b): idiomatic, but it
  rewrites the router's tests for no user-visible change and, with a fast
  path, leaves 15 Effect handlers unreached in production.

## Global Constraints

- **Runtime/tooling:** Bun 1.4.x; TypeScript 6.0.3 strict; Biome 2.5.8;
  `bun:test`; one `bun build --compile` binary; `disallowedReferences =
  [ pkgs.bun ]` stays asserted; no `--minify` in the flake build.
- **Dependencies:** exactly `effect@4.0.0` and `@effect/platform-bun@4.0.0`,
  pinned exact; `bun.lock` committed; `--frozen-lockfile` everywhere.
  Imports: `effect`, `effect/cli`, `effect/process`,
  `@effect/platform-bun`. Never `effect/unstable/*`, never 3.x `@effect/cli`.
- **Scope fence:** `src/engine/**`, `src/effects/**`, `src/driver/**`,
  `src/config/**`, `src/exec.ts`, `src/commands.ts` and their tests are not
  modified. In `src/index.ts`, `Command`, `run`, `RunOpts`, `loadProfile`
  keep their signatures; `parseArgs`, `ParseResult`, `USAGE` and the seven
  lookup tables are deleted.
- **Exit codes:** success 0; `laptop` contended 1, silent; parse failure /
  unknown subcommand 1 after help; explicit `--help`/`--version` 0; bare
  `tess` 0 with root help; profile or driver error 1 with one stderr line.
- **Latency bar:** hot-path p50 ≤ 2.0× the base binary measured in the
  same T6 run, p90 ≤ 45 ms; help p50 ≤ 60 ms; binary ≤ 70 MB. Measured by
  `scripts/bench-startup.ts` with the shim on both sides and pasted into
  the PR body; a miss is not merge-ready.
- **CLI built-ins:** `CliConfig.layer({ builtIns: [GlobalFlag.Help,
  GlobalFlag.Version] })` only.
- **Naming:** subcommand names, literals and `--self` verbatim from today's
  `parseArgs`. New modules under `src/cli/`: `grammar.ts`, `errors.ts`,
  `driver.ts`, `commands.ts`, `main.ts`. `fastPath`, not `keybind`, is the
  grammar field name.
- **House rules:** the one `await import("./cli/main.ts")` carries the
  `ts-no-dynamic-import` exception comment; no `as any`; help strings one
  short sentence each; the init/event handlers carry a `// Temporary until
  resident-daemon.md lands` comment.
- **Sequencing:** this record lands **before** `resident-daemon.md`. The
  `init --self`, `display-event` and `flex-event` handlers (and
  `Flag.String("self")`, the only flag) are temporary: that record deletes
  them with the daemon. Effort on them is kept to the mapping line each.
- **PR shape:** one series, no PR with a knowingly red required job;
  Conventional Commits; `Co-authored-by: Matt Wilkinson` trailer.

## Plan

Sources under `src/`, tests colocated. **One PR, five commits**, each
typechecking and `bun test`-green: T1 adds the dependency **and** the flake
change together, so the `nix` job is green at every commit; T2–T4 build on
it. T5 (bench) and T6 (README) ride the same PR. Written against Open
Question 1 (a). *If Matt picks (b): T2's handlers call `commands.ts`
functions under a `Driver` service instead of building `Command` values,
T3's fast path dispatches through the same name-to-function table, and
`run`/`RunOpts`/the exhaustiveness test are deleted in T4.*

### T1 — Dependencies + `bun2nix` flake + grammar table + errors

- `package.json`: `"dependencies": { "effect": "4.0.0",
  "@effect/platform-bun": "4.0.0" }`, `version` → `0.2.0`; `bun install`;
  commit `bun.lock`.
- `flake.nix`: input `bun2nix = { url = "github:nix-community/bun2nix";
  inputs.nixpkgs.follows = "nixpkgs"; }`; `pkgs` imported with
  `bun2nix.overlays.default`; the `tess` derivation gains `bunDeps =
  pkgs.bun2nix.fetchBunDeps { bunNix = ./nix/bun.nix; }`,
  `nativeBuildInputs` += `pkgs.bun2nix.hook`, `bunInstallFlags =
  [ "--linker=isolated" "--backend=copyfile" ]`, `dontUseBunBuild = true`,
  `dontRunLifecycleScripts = true`; `buildPhase` otherwise unchanged.
  `nix/bun.nix` generated by `bun2nix -l bun.lock -o nix/bun.nix` and
  committed; `checks.bun-lock` regenerates and `diff`s it (oh-my-pi's
  check, paths adjusted); `bun2nix` added to `devenv.nix` packages.
  `installCheckPhase` at this commit still asserts `bogus-subcommand` →
  rc 2 (T3 flips it).
- `src/cli/grammar.ts`:

  ```ts
  /** One `tess` subcommand. `choices` null = no positional; a list = one literal; `int` = one integer. */
  export interface SubcommandSpec {
  	readonly description: string;
  	readonly choices: ReadonlyArray<string> | null;
  	readonly int: boolean;
  	/** True = settled by `fastMatch` without the Effect graph. False = always the Effect CLI. */
  	readonly fastPath: boolean;
  }
  export const SUBCOMMANDS: Readonly<Record<string, SubcommandSpec>>;
  // fastPath true : focus-slot(int) snap[3col,50-50,columns] stack-cycle[next,prev] resize[grow,shrink]
  //   move-display[g9,aw,laptop] cycle-display[next,prev] focus/swap/warp[west,south,north,east]
  //   insert[east,west,north,south,stack] space[bsp,stack] reset-splits columns toggle-float balance
  // fastPath false: apply laptop display-event flex-event rules display-setup
  // init is hand-written in T2 (the one flag); it is not in the table.
  /** Shared integer predicate: `Number.isInteger(Number(s))` and `s` matches /^[+-]?\d+(\.0+)?([eE]\d+)?$/ — what Argument.Int accepts on a flag-free token. */
  export function isIntToken(s: string): boolean;
  ```

- `src/cli/errors.ts`: `ProfileLoadError`, `DriverError` as in the
  Approach; `formatDriverError(cause: unknown): string` (the `Error.message`
  or `String(cause)`).
- `src/cli/driver.ts`: `export const liveDriver = (): YabaiDriver => new
  YabaiDriver({ yabaiPath: process.env.TESS_YABAI });` and `src/index.ts`'s
  entry guard switches to it (the only index.ts change in T1).
- Tests: `grammar.test.ts` — every `choices` list equals today's
  `parseArgs` acceptance set (oracle at this commit; rewritten in T4);
  `isIntToken` table: accepts `7 007 +1 1.0 1e2`, rejects `0x10 "" " 1"
  1.5 abc`.

Interfaces:

- consumes: `src/driver/yabai.ts` `YabaiDriver` (`{ yabaiPath?: string }`
  option); `src/index.ts` `parseArgs` (test oracle only).
- produces: `SUBCOMMANDS`, `SubcommandSpec`, `isIntToken`;
  `ProfileLoadError`, `DriverError`, `formatDriverError`; `liveDriver`;
  `nix/bun.nix`, `checks.bun-lock`.

### T2 — The Effect command tree + `main()`

- `src/cli/commands.ts`:

  ```ts
  /** What a handler needs to call `run`: the driver, the profile loader, the opts. One Context.Service so tests swap all three. */
  export interface RunDepsShape {
  	readonly driver: WmDriver;
  	readonly loadProfile: () => Promise<Profile>;
  	readonly opts: RunOpts;
  }
  export class RunDeps extends Context.Service<RunDeps, RunDepsShape>()("tess/RunDeps") {}
  /** The tree without services; tests provide a FakeDriver-backed RunDeps. */
  export const rootCommand: Command.Command<"tess", {}, {}, ProfileLoadError | DriverError, RunDeps>;
  /** The tree with a RunDeps layer applied — `main()` passes the live one; the no-build-on-help test passes a counting one. */
  export const withDeps: <E>(layer: Layer.Layer<RunDeps, E>) => Command.Command<"tess", {}, {}, ProfileLoadError | DriverError | E, never>;
  /** The one dispatcher every handler calls. */
  export const dispatch: (command: Command) => Effect.Effect<void, ProfileLoadError | DriverError, RunDeps>;
  ```

  `dispatch`: yield `RunDeps`; `Effect.tryPromise({ try: deps.loadProfile,
  catch: ProfileLoadError })`; `Effect.tryPromise({ try: () =>
  run(profile, command, deps.driver, deps.opts), catch: DriverError })`;
  nonzero result → `process.exitCode = code`. Handlers: each
  `SUBCOMMANDS` key → `Command.make(name, {value: Argument.Literals(…)} |
  {n: Argument.Int("n")} | {}, (input) => dispatch({ kind: name, …input }))`
  built in one loop over the table (the literal → union cast is sound by
  `grammar.test.ts`); `init` by hand with `Flag.String("self").pipe(
  Flag.filter((s) => s !== "" && !s.startsWith("-"), () => "init --self
  needs a path"))` → `dispatch({ kind: "init", self })`, marked temporary.
  Root: `Command.make("tess").pipe(Command.withDescription("Tessera
  window-manager driver"), Command.withSubcommands([...]))`.
- `src/cli/main.ts`: `export const main = (): void =>
  withDeps(Layer.sync(RunDeps, () => ({ driver: liveDriver(),
  loadProfile, opts: {} }))).pipe(Command.run({ version: pkg.version }),
  Effect.tapError(printOneLine), Effect.provide(Layer.mergeAll(
  BunServices.layer, CliConfig.layer({ builtIns: [GlobalFlag.Help,
  GlobalFlag.Version] }))), BunRuntime.runMain)`, with `import pkg from
  "../../package.json" with { type: "json" }`.
- Tests `src/cli/commands.test.ts` under the test layers + `Layer.succeed(
  RunDeps, { driver: new FakeDriver(seed), loadProfile: async () =>
  profile, opts: tmpOpts })`: (a) every `SUBCOMMANDS` key with its first
  choice succeeds and the FakeDriver shows the effect `index.test.ts`
  "simple dispatch" asserts for the same `Command`; (b) every key `--help`
  succeeds with a throw-on-call driver and zero calls; (c) bad literal →
  `ShowHelp` + `InvalidValue`, code 1; (d) `init` alone / `--self ""` /
  `--self --apply` → `ShowHelp`, code 1; (e) **`withDeps(counting
  Layer.sync)`: `["snap","--help"]` and `["--help"]` leave the counter at
  0, `["snap","3col"]` reads 1**; (f) pre-held laptop lock (mkdir with our
  pid, as `index.test.ts` does) → success with `process.exitCode === 1`,
  reset in `afterEach`.

Interfaces:

- consumes: `Command`, `run`, `RunOpts`, `loadProfile` from
  `src/index.ts`; `SUBCOMMANDS`, `isIntToken`, errors, `liveDriver` (T1);
  `Command`, `Argument`, `Flag`, `CliConfig`, `GlobalFlag` from
  `effect/cli`; `BunRuntime`, `BunServices` from `@effect/platform-bun`.
- produces: `RunDeps`, `RunDepsShape`, `rootCommand`, `withDeps`,
  `dispatch`, `main`.

### T3 — `fastMatch` + the hybrid entry; flip the flake smoke

*(Open Question 1 (b) does not change this task's shape: `fastMatch`
produces whatever value the shared dispatcher consumes.)*

- `src/index.ts`:

  ```ts
  /** Settle a keybind argv without the Effect graph; null defers to the CLI. Pure. */
  export function fastMatch(argv: ReadonlyArray<string>): Command | null;
  // null when: argv empty; argv[0] not a fastPath key; any token starts with "-";
  //   argc !== 1 + (choices||int ? 1 : 0); literal ∉ choices; int && !isIntToken(argv[1]).
  ```

  Entry guard: `const fast = fastMatch(argv); if (fast) { try { const
  profile = await loadProfile(); process.exit(await run(profile, fast,
  liveDriver())); } catch (cause) { stderr(formatDriverError(cause));
  process.exit(1); } } const { main } = await import("./cli/main.ts");
  main();` — the import carries the `ts-no-dynamic-import` exception
  comment citing this record.
- `flake.nix` `installCheckPhase`: `bogus-subcommand` → rc **1** and
  stderr contains `Unknown subcommand`; `--help` → rc 0 and stdout contains
  `USAGE`; `--version` → stdout `tess v$version`; `snap --help` → stdout
  contains `tess snap`.
- Tests `index.test.ts` (replacing the `parseArgs` describes): `fastMatch`
  table — every `fastPath` key × each choice → the `Command`; `[]`,
  `--help`, `snap --help`, `snap bogus`, `snap`, `snap 3col extra`,
  `apply`, `laptop`, `init --self x`, `focus-slot x`, `focus-slot -1`,
  `focus-slot 0x10` → `null`. **Fast-dispatch FakeDriver test**: for each
  `fastPath` key, `run(profile, fastMatch(argv)!, fakeDriver, tmpOpts)`
  asserts the same driver effect T2 (a) asserts. Fallback spawn test:
  `Bun.spawnSync(["bun", "src/index.ts", "snap", "--help"], { env: {
  HOME: tmp, TESS_YABAI: shim } })` → rc 0, stdout contains `tess snap`.

Interfaces:

- consumes: `SUBCOMMANDS`, `isIntToken`, `liveDriver`, `formatDriverError`
  (T1); `main` (T2, dynamic); `run`, `loadProfile`, `Command`.
- produces: `fastMatch`; the entry guard; the flake smokes.

### T4 — Delete `parseArgs`, `ParseResult`, `USAGE`, the lookup tables

`src/index.ts` keeps `Command`, `RunOpts`, `run`, `loadProfile`,
`importProfile`, `fastMatch`, the entry guard. `grammar.test.ts` drops its
oracle and pins the literal lists directly. Whole suite green with every
non-router test file byte-identical to `main`.

Interfaces: consumes T1–T3; produces nothing new.

### T5 — `scripts/bench-startup.ts` + the measurement in the PR body

```ts
// usage: bun scripts/bench-startup.ts <base-binary> <new-binary> [n=40]
// Writes an `exit 1` yabai shim to a temp dir; HOME=<empty tmp>, TESS_YABAI=<shim> on EVERY row and BOTH binaries.
// Rows: `snap 3col`, `focus east` (hot; the shim's exit 1 → #jsonOrNull null → no-op), `snap --help` (help; new binary only).
// Prints p50/p90/min per binary×row as a Markdown table plus the hot-path ratio new/base; exits 1 on any nonzero rc or a missed bar.
```

The base binary is `main` plus T1's `liveDriver` one-liner cherry-picked
(so `TESS_YABAI` works on both sides); the script refuses to run a base
binary that ignores the shim (it checks `snap 3col` under a shim that
writes a marker file). Paste the table and pass/fail into the PR body.

Interfaces: consumes two compiled `tess` paths, `Bun.spawnSync`,
`TESS_YABAI`; produces the table and a nonzero exit on a miss.

### T6 — README

Replace "Run `tess` with no arguments for the full subcommand list" with
the `--help`/`--version` contract; add an "Exit codes" subsection (Global
Constraints); add "after changing deps, `bun2nix -l bun.lock -o
nix/bun.nix`" under Developing.

Interfaces: consumes Global Constraints; produces README text.

## Tasks

One PR, five commits:

- [ ] T1 — deps pinned exact + `0.2.0`; `bun2nix` input/overlay,
  `nix/bun.nix`, `bunDeps` + hook, `checks.bun-lock`, `devenv.nix`;
  `src/cli/grammar.ts` (`SUBCOMMANDS`, `isIntToken`), `errors.ts`,
  `driver.ts` (`liveDriver`, `TESS_YABAI`); `nix build .#default` green.
- [ ] T2 — `src/cli/commands.ts` (`RunDeps`, `rootCommand`, `withDeps`,
  `dispatch`), `src/cli/main.ts`; `commands.test.ts` (a)–(f) green incl.
  the counting-layer no-build-on-help test.
- [ ] T3 — `fastMatch` + hybrid entry guard; flake smokes flipped to
  rc 1 with `Unknown subcommand`, plus `--help`, `--version`, `snap --help`;
  `index.test.ts` `fastMatch` table + fast-dispatch FakeDriver test +
  fallback spawn test.
- [ ] T4 — delete `parseArgs`, `ParseResult`, `USAGE`, the seven tables;
  whole suite green; non-router tests byte-identical.
- [ ] T5 — `scripts/bench-startup.ts`; base-vs-new table in the PR body;
  p50 ≤ 2.0×, p90 ≤ 45 ms, help ≤ 60 ms, size ≤ 70 MB, each stated.
- [ ] T6 — README.

## Open Questions

1. **How deep the Effect port goes** *(load-bearing; decides T2–T4)*.
   - (a) **Effect for the parser, help and version only.** Both argv paths
     build the existing `Command` union and call the existing `run()`;
     `RunOpts` (with its waiter injections) and the exhaustiveness test
     stay; `commands.ts`, `effects/`, `driver/` untouched. One dispatcher,
     two front ends, one FakeDriver test per front end. Cost: the handlers
     are mapping lines, not "Effect code"; the typed-error surface is the
     two wrappers in `dispatch`.
   - (b) **Full service seam.** `Driver` and the effect paths become
     `Context.Service`s; each handler calls its `commands.ts` function
     under `Effect.tryPromise` with tagged errors; `run`, the `Command`
     union, `RunOpts` and the exhaustiveness test are deleted; the fast
     path dispatches through one shared name-to-function table so handler
     bodies are not duplicated. Cost: the router's tests are rewritten for
     no user-visible change, and with a fast path 15 of the Effect handlers
     are unreached in production (only help/version/errors and the six
     non-fast commands run under Effect); `EffectPaths` must carry the
     waiter injections or event tests hit the real `/tmp` locks.
   **Recommendation: (a).** It delivers every item in the Problem with the
   smallest diff, keeps one production dispatcher, and leaves (b) as a
   later record under the same `RunDeps` key. **The hot-path shape** is
   folded in here, not a second question: the measured flat entry is 2.7×
   today (51 ms p50) and the hybrid 1.9× (35 ms); the Plan is written for
   the hybrid and the bar is set so the flat entry fails it. If Matt
   prefers one code path, the bar relaxes to ≤ 3.0× and T3's `fastMatch`
   is dropped.
2. **`apply`'s stale-lock wedge** *(non-load-bearing, deferred)*: keep
   `acquireLockOrSkip` verbatim here (the port changes no lock semantics);
   switch `apply` to `acquireLock` with reclaim in its own follow-up PR with
   a dead-pid test. Recommendation: that order.
3. **`move-display` names `g9|aw|laptop`** *(non-load-bearing, deferred)*:
   keep the literal list (today's `DISPLAY_NAMES`); widening to the loaded
   profile's keys belongs to the `tessera-oss-repoint` profile-portability
   work. Recommendation: keep.
4. **Exit 1 vs 2 for parse failures** *(non-load-bearing; the Plan assumes
   1)*. Exit 2 would preserve a parser-vs-runtime distinction in the flake
   smoke and the yabai log at the cost of an eight-line custom `teardown`
   (`BunRuntime.runMain(effect, { teardown })`: `CliError.ShowHelp` with
   errors → `onExit(2)`, else `Runtime.defaultTeardown`). With 1 the smoke
   distinguishes them by asserting the `Unknown subcommand` text instead.
   **Recommendation: 1** — the library convention, no caller reads 2, and
   bare `tess` exiting 0 with help is what the README already promises.
