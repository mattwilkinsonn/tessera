# Go port — parity contract and superseded text

*Supporting file for [`design.md`](design.md). Each item below has a ported
test or a golden in the Plan. The contract binds the port, T1 to T9. After T9
the goldens become ordinary regression tests: a deliberate change edits its
golden in its own PR.*

## Exit codes and signals

- 0 for success, help, version and bare `tess`. 1 for a parse failure, a
  profile error, a driver error, or a contended `laptop` (silent). This is the
  table in `effect4-cli.md` § "Error model".
- **SIGINT and SIGTERM exit 130 after releasing locks.** Today every
  `fastPath: false` subcommand (`apply`, `laptop`, `display-event`,
  `flex-event`, `rules`, `display-setup`) and `init` run under `runMain`. A
  signal interrupts the fiber and exits 130, and the `process.once("exit",
  release)` hook in `claim` (`src/effects/locks.ts`) frees the lock
  (`effect4-cli.md` § "Lock handling"). Go must keep this. The apply lock is
  taken by `acquireLockOrSkip`, which has no stale reclaim, so a lock left
  behind would block every later `apply`. `cmd/tess` therefore mints its root
  context with `signal.NotifyContext(…, os.Interrupt, syscall.SIGTERM)`. The
  cancelled context stops the running yabai child (`exec.CommandContext`) and
  the next `Clock.Sleep`. The command then returns, its deferred releases run,
  and `cli.Main` maps a cancelled root context to 130. In Go, a SIGTERM with
  no handler skips defers (probed), so the handler is required.

## Stderr

- **On error, one line**: the `err.Error()` of the failing load or command,
  with no prefix (`formatDriverError`, `src/cli/driver.ts`).
- **These lines stay word for word**:
  - from `spawnMissingWindows` (`src/commands.ts`): `tess: spawn failed for
    <name>: <msg>` and `tess: spawned windows not found for <sources>`;
  - from `src/driver/yabai.ts`: `spawnWindow failed (exit N): <argv>[:
    <stderr>]` and `yabai query failed (exit N): <args>`. If the yabai binary
    is missing, Go reports exit 1 with empty output, as `Bun.$` does;
  - from `ratioArg` and `bspSteps`: `space <idx>: ratio <r> clamped to <x>`;
  - every `validateProfile` message, joined by `; `, in TS order.
- All user-facing output goes to an injected `io.Writer`.

## Numbers and integer files

- **JS number formatting.** `ratioArg` prints `clamped.toFixed(4)` and
  `String(r)`. Go's `strconv.FormatFloat(x, 'f', 4, 64)` rounds exact ties to
  even: `5/32` gives `0.1562` and `1/32` gives `0.0312`. JS `toFixed` gives
  `0.1563` and `0.0313`. These results were probed on go 1.26.5 and 1.27.1
  (identical) and on bun 1.4.2. `internal/wm/yabai` therefore carries two
  helpers. `jsFixed` rounds ties away from zero on the exact binary value.
  `jsString` uses `'f', -1` inside `[1e-6, 1e21)` and JS exponent form outside
  that range. Both feed the argv (`abs:0.3000`) and the warning.
- **Integer files.** `readStamp` (`src/effects/stamp.ts`) and `readPid`
  (`src/effects/locks.ts`) use `Number.parseInt(raw.trim(), 10)`. That
  accepts a leading-digit prefix (`"12abc"` → 12). Go uses one
  `parseLeadingInt` with the same rule. Using `strconv.Atoi` would change
  which locks count as stale.

## Locks, stamps, guard

- The `/tmp` paths, the flex-order path, the quiet windows (3 s display, 2 s
  flex), the guard TTL (8 s), the 1 s poll and the sketchybar path all stay
  as in `src/effects/constants.ts`. An old and a new binary therefore share
  one protocol during the switch.
- `acquireLock` keeps stale-PID reclaim, and treats EPERM as alive.
  `acquireLockOrSkip` keeps surrender-on-contention with no reclaim.
- The waiter captures `actedOn` inside the quiet poll (`runWaiter`,
  `src/effects/debounce.ts`). It keeps the H2 re-stamp and the H7
  display-quiet gate.

## `init --self PATH`

`init` registers `[PATH, "display-event"]` for the three display events and
`[PATH, "flex-event"]` for the four flex events. It registers the sketchybar
trigger for `space_changed` and `display_changed`. It then runs the startup
cascade (`init`, `src/commands.ts`). `--self` stays required, and `tess` never
works out its own path (design § Open Questions 4).

## Matching and order

- **Regex.** Matchers compile with Go `regexp` (RE2) and match unanchored,
  which is the `RegExp.test` rule in `matchesSpec`. Every matcher in the
  default, fixture and consumer profiles is a literal or `^…$`, and both
  engines agree on those. A pattern RE2 rejects fails the load; it never
  changes meaning.
- **Order.** No output, argv or error order depends on Go map iteration.
  `slugForWindow` sorts names by byte order, which equals JS order for ASCII.
  `displayOfSpace`, `resolveDesk` and `validateProfile` iterate
  `profile.displays` in insertion order; Go iterates the fixed `G9`, `Aw`,
  `Laptop` struct fields. Weight-default keys iterate in ascending numeric
  order, as JS iterates integer keys.
- **Display slots stay fixed on purpose.** `Profile.Displays` is a struct
  with three named fields, as the TS `DisplayName` union is. Generalizing the
  slots would change the `move-display` choices, so it is out of scope.

## Deliberate differences no caller sees

No skhd, yabai or CI caller reads any of these:

- help carries no ANSI colour on a TTY;
- profile errors read `profile <path>: …` and carry YAML parse errors instead
  of TS ones;
- malformed yabai JSON reports the Go decoder's text;
- a signalled keybind command (the old fast path) exits 130, not Bun's 143.

## Superseded text in the frozen records

No frozen record is edited.

| Record | Superseded | Still stands |
| --- | --- | --- |
| `effect4-cli.md` | All of it: `effect/cli`, the `fastMatch` hybrid, the Effect error classes, `bun2nix`, version pinning, its Open Questions. | Its exit-code table, its signal contract, and its startup protocol and bar, carried here. |
| `architecture.md` | Global Constraints "Runtime/tooling" and "Deploy"; Layer 1 "`profile.ts`, typed data" and Open Question 2; the `Bun.$`, `Bun.sleep` and `JSON.parse` mechanics in Layer 3 and the executor; § "Build + deploy"; T7. | Layers, layering law, D1, D2, driver contract, engine, event model. |
| `grid-layouts.md` | Global Constraints "Runtime/tooling"; its `bun test` test cycles; the `profile.ts` file name in its deploy note. | Tracks and weights, chain formula, validation, resolution order. |
| `laptop-flex-spaces.md` | The preamble's "ports to TypeScript". | The converger model. |
| `resident-daemon.md` | Global Constraints "Runtime/tooling"; `Bun.listen`, `Bun.spawn` and the TS `Interfaces:` of T1–T4; the "Bun exposes no `launch_activate_socket` API" reason. | D-T1 and D-T2 as Matt's rulings. |
