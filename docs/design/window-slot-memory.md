# Design: remember which window holds each slot

## Resolved decisions

- **Tessera remembers windows; the browser is not configured to name them** (Matt, 2026-11-02). Naming windows with a browser add-on was rejected: it depends on a third-party add-on whose names do not survive restarts.

## Problem / Intent

A profile slot that several windows can fill, such as the four `browser` slots, gets a random window on each layout. Switching desktop → laptop → desktop should put each window back where it was. Memory may reset when the app restarts.

The cause is claim order. `ClaimSet.Claim` (`internal/engine/claim.go`) takes the first unclaimed candidate in `world.Windows` order. That is yabai query order, which follows focus recency. Browser slots match on app only (`#browser` in the profile), so any Zen window can fill any `browser` slot. The laptop path sorts by window id first (`stepPinned`, `internal/engine/laptop.go`), so it is steadier, but its order still shifts when a lower id closes.

## Approach

Give every window that a spec matches a stable **ordinal** within that spec: `browser#1`, `browser#2`, and so on. The k-th occurrence of a spec name in a plan claims the window that holds ordinal k. Desk and laptop already list occurrences in a fixed order (`desk[].tracks`, `laptopPinned`), so ordinal k always lands in the same slot of a given layout. A mode switch and back returns every window to its slot.

- **Assignment.** `AssignOrdinals` is a pure engine function. It runs once per command before planning, and its result is written back to disk:
  - It keeps a prior entry while a live window has that id and the same app. This guards against yabai reusing an id for another app after a restart.
  - It drops every other entry, which prunes closed windows and stale ids after a reboot.
  - It gives each unassigned matching window the lowest free ordinal, in window-id order, so two runs that see the same windows assign the same way.
  - Minimized and floating windows keep their ordinal, so minimizing a window does not reshuffle the rest.
- **Claiming.** For occurrence k of a name, `Claim` takes the ordinal-k window when it is a live, unclaimed candidate (not minimized or floating). Memory beats `preferDisplay`: honouring the remembered slot is the point of the feature. When that window is unavailable, the fallback first tries the spec's spare windows, meaning ordinals above the plan's demand for that name. Then it tries any unclaimed candidate, keeping today's display preference. This stops a fallback from taking a window that a later occurrence remembers.
- **Storage.** A JSON file next to the flex order: `$XDG_CACHE_HOME/yabai/window-ordinals.json` (`effects.Paths.WindowOrdinals`). The file is needed because CLI commands (`Apply`, `Laptop` in `internal/commands/composite.go`) run outside the daemon. Writes go to a temp file that is then renamed, so a reader never sees a partial file. `Apply` and `laptop` hold different locks. If both run at once, both compute the same assignment from the same windows, so the last write wins and nothing is lost.
- **Unchanged.** `ResolveSlot` (numpad focus, `deskSlots`) still resolves a browser slot by display. `SpawnsNeeded` still counts missing windows. Flex order is untouched.

### Alternatives considered

- **Per-layout slot map** (layout id + track + position → window id). This would remember positions per topology, but it needs a stable layout id and many keys, and the desk and laptop maps would drift apart. Ordinals give one identity per window that every layout reads.
- **Memory in the daemon only.** CLI `tess apply` and `tess laptop` plan outside the daemon, so they would ignore it.

## Global Constraints

- Go 1.27 (`go.mod`). The engine stays pure: no I/O and no clock in `internal/engine`. Memory goes into `World`, and the new assignment comes back as a value.
- Thread `ctx` from the caller; no `context.Background()` outside `main` or tests.
- Comments are 1–2 lines and explain why. No issue ids in source.
- Tests use the repo's `gotest.tools/v3/assert` and the `internal/wm/fake` driver. Subtest names are lowercase phrases.
- Gates: `gofmt`, `go vet`, `go test -race ./...`, `golangci-lint run --config .golangci.yml ./...`, `nilaway`.

## Plan

### Task 1: ordinal model and assignment (engine)

- `Interfaces:`
  - `type WindowRef struct { ID int; App string }`
  - `type Ordinals map[string][]WindowRef`: spec name → index k-1 holds ordinal k; `ID == 0` marks a free ordinal.
  - `func AssignOrdinals(profile *config.Profile, windows []wm.Window, prior Ordinals) Ordinals`
  - A window counts for a spec when `matchesSpec` holds. Minimized and floating windows count too.
- Tests:
  - It keeps prior ordinals for live windows with the same app.
  - It prunes dead ids and ids whose app changed.
  - It fills the lowest free ordinal, in id order.
  - It is deterministic for the same input.

### Task 2: claims honour ordinals (engine)

- `Interfaces:`
  - `World` gains `Ordinals Ordinals`.
  - `func NewClaimSet(profile *config.Profile, ordinals Ordinals, demand map[string]int) *ClaimSet`
  - `func Demand(names []string) map[string]int`
  - `ClaimSet` counts occurrences per name.
  - `buildDesks` (`desk.go`) passes the demand of all present desk tracks. `stepPinned` (`laptop.go`) passes the demand of `LaptopPinned`. `SpawnsNeeded` passes nil ordinals, since it only counts.
- Tests:
  - Occurrence k takes ordinal k across query orders.
  - Memory beats `preferDisplay`.
  - A minimized ordinal-1 window makes occurrence 1 take a spare, not ordinal 2.
  - With no ordinals, behaviour is unchanged.
  - Desk → laptop → desk keeps each Zen window in its desk slot. Use a fake world whose window order is reshuffled between steps.

### Task 3: persistence and wiring (effects, commands)

- `Interfaces:`
  - `effects.Paths.WindowOrdinals string`, defaulting to `filepath.Join(cache, "yabai", "window-ordinals.json")`
  - `func ReadOrdinals(path string) (engine.Ordinals, error)`: a missing file returns empty with no error. A corrupt file is logged and returns empty, so a bad file never blocks layout.
  - `func WriteOrdinals(path string, ordinals engine.Ordinals) error`: temp file + rename; creates the parent directory.
  - `Apply` and `laptop` (`composite.go`) read the ordinals, call `AssignOrdinals` on the post-spawn snapshot, write the result, then plan with `World.Ordinals` set.
- Tests:
  - A write/read round trip.
  - A missing file and a corrupt file both return empty.
  - An `Apply` run on the fake driver persists ordinals, and a second `Apply` with a reshuffled window order produces the same placements.

## Tasks

- [ ] Task 1: `Ordinals`, `WindowRef`, `AssignOrdinals` with tests
- [ ] Task 2: ordinal-aware `ClaimSet`, `Demand`, desk/laptop wiring, mode-switch test
- [ ] Task 3: `Paths.WindowOrdinals`, read/write, `Apply`/`laptop` wiring, README line
