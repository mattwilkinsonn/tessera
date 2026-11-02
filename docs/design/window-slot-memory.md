# Design: remember which window holds each slot

## Resolved decisions

- **Tessera remembers windows; the browser is not configured to name them** (Matt, 2026-11-02). Naming windows with a browser add-on was rejected: it depends on a third-party add-on whose names do not survive restarts.

## Problem / Intent

A slot name that several windows can fill, such as `browser`, gets a random window on each layout. Switching desktop → laptop → desktop should put each window back where it was on each display. Memory may reset when the app restarts.

The cause is claim order. `ClaimSet.Claim` (`internal/engine/claim.go`) takes the first unclaimed candidate in `world.Windows` order. That is yabai query order, which follows focus recency. Browser specs match on app only, so any Zen window can fill any `browser` slot. `stepPinned` (`internal/engine/laptop.go`) sorts by window id first, so the laptop is steadier, but its order still shifts when a lower id closes.

## Approach

Remember which window last filled each **display slot**. A display slot key is `<display>/<name>#<k>`, where `<display>` is the profile display name and k counts occurrences of `name` within that display's layout. So `g9/browser#1` is the first browser slot on the G9, and `laptop/browser#3` is the third on the laptop.

- **Why keys are per display.** The topologies give one display different desks. For example, the full desk has two browser slots on the AW, but `aw-laptop` uses `#awSolo` with four. Topology selection is `ResolveDesk` in `internal/engine/matcher.go`. A count across the whole plan would move the AW's first slot from occurrence 2 to occurrence 1 when the topology changes. Counting within a display keeps `aw/browser#1` the same slot in every topology that lays out the AW.
- **Laptop.** The laptop-only pinned list (`laptopPinned`) and the desk `#laptopStack` both use the `laptop/` prefix. Their k-th browsers share a key, so the same window leads on the laptop in both modes.
- **Claiming.** A plan first marks every window that one of its own keys remembers as reserved for that key. Then, for each key in plan order:
  1. Take the remembered window if it is a live, unclaimed candidate (not minimized or floating). Memory beats `preferDisplay`, because honouring the remembered slot is the point of the feature.
  2. Otherwise take an unclaimed candidate that no key in this plan reserves, keeping today's display preference.
  3. Otherwise leave the slot empty for this run. A window reserved by a later key is never taken. So minimizing the window in slot 1 empties slot 1 and leaves slot 2 alone. Today a minimized window is not laid out either.
- **Recording.** After a plan's ops succeed, the command records key → window id for every slot that was filled. A window that fills slots on two displays (desk, then laptop) is remembered under both keys.
- **Pruning.** An entry is dropped when its window id no longer exists, or exists under a different app. The app check guards against yabai reusing an id for another app after a restart. Minimized and floating windows still exist, so they keep their keys.
- **Storage and concurrency.** A JSON map `key → {id, app}` at `$XDG_CACHE_HOME/yabai/window-slots.json` (`effects.Paths.WindowSlots`). A file is needed because CLI commands (`Apply`, `Laptop` in `internal/commands/composite.go`) plan outside the daemon. `Apply` and `laptop` hold different locks, so every read-modify-write takes a dedicated short flock (`window-slots.lock`, next to the file). Under that lock the command:
  1. re-reads the file,
  2. writes only the keys its plan filled,
  3. prunes against a fresh `QueryWindows`,
  4. writes to a temp file and renames it.

  A concurrent run therefore cannot overwrite the other's keys, or prune a window the other just spawned.
- **Unchanged.** `ResolveSlot` (numpad focus, `deskSlots`) still resolves a browser slot by display. `SpawnsNeeded` still counts missing windows and ignores memory. Flex order is untouched.

### Alternatives considered

- **One ordinal per window per spec**, shared by every layout (`browser#k` everywhere). It gives one identity per window, but occurrence counts differ between topologies (see above), so the same physical slot would map to different ordinals.
- **Memory in the daemon only.** CLI `tess apply` and `tess laptop` plan outside the daemon, so they would ignore it.

## Global Constraints

- Go 1.27 (`go.mod`). The engine stays pure: no I/O in `internal/engine`. Memory comes in through `World`, and filled slots come back as values.
- Thread `ctx` from the caller; no `context.Background()` outside `main` or tests.
- Comments are 1–2 lines and explain why. No issue ids in source.
- Tests use `gotest.tools/v3/assert` and the `internal/wm/fake` driver. Subtest names are lowercase phrases.
- Gates: `gofmt`, `go vet`, `go test -race ./...`, `golangci-lint run --config .golangci.yml ./...`, `nilaway`.

## Plan

### Task 1: slot-aware claims (engine)

- `Interfaces:`
  - `type SlotKey string`
  - `type WindowRef struct { ID int; App string }`
  - `type SlotMemory map[SlotKey]WindowRef`
  - `World` gains `Slots SlotMemory`.
  - `func NewClaimSet(profile *config.Profile, memory SlotMemory, reserved map[int]SlotKey) *ClaimSet`
  - `func (c *ClaimSet) ClaimSlot(windows []wm.Window, key SlotKey, name string, preferDisplay mo.Option[int]) (int, bool)`
  - `func Reserve(memory SlotMemory, keys []SlotKey) map[int]SlotKey`
  - `func SlotKeys(display config.DisplayName, names []string) []SlotKey`: k counts per name within the list.
  - `Claim` with nil memory keeps today's behaviour for `SpawnsNeeded`.
- Tests:
  - A remembered window wins its key whatever the query order.
  - Memory beats `preferDisplay`.
  - With slot 1's window minimized and slot 2's live, slot 1 is empty and slot 2 keeps its window.
  - A reserved window is never taken by an earlier key.
  - With empty memory, claims match today's output (existing `TestDeskPlan` stays green).

### Task 2: plans return filled slots (engine)

- `Interfaces:`
  - `func DeskPlan(profile *config.Profile, world World) ([]Op, SlotMemory)`
    - `buildDesks` and `deskTracks` (`desk.go`) build keys per display with `SlotKeys(layout.Display, flatten(layout.Tracks))`.
  - `ConvergeState` gains `Filled SlotMemory`.
    - `stepPinned` (`laptop.go`) claims `laptopPinned` with `SlotKeys(config.DisplayLaptop, profile.LaptopPinned)` and records into `Filled`.
  - `func PruneSlots(memory SlotMemory, windows []wm.Window) SlotMemory`
  - `func MergeSlots(base, filled SlotMemory) SlotMemory`
- Tests:
  - Full desk → `aw-laptop` → full desk with the same windows and reshuffled query order. Each AW and G9 browser returns to its slot.
  - Desk → laptop-only → desk keeps desk slots.
  - Prune drops dead ids and app-changed ids, and keeps minimized and floating windows.
  - Merge overwrites only filled keys.

### Task 3: persistence and wiring (effects, commands)

- `Interfaces:`
  - `effects.Paths.WindowSlots string`, defaulting to `filepath.Join(cache, "yabai", "window-slots.json")`.
  - `func ReadSlots(path string) (engine.SlotMemory, error)`: a missing file returns empty with no error. A corrupt file is logged and returns empty, so a bad file never blocks layout.
  - `func UpdateSlots(ctx context.Context, path string, filled engine.SlotMemory, live func(context.Context) ([]wm.Window, error)) error`: under the flock it reads, merges, prunes against `live`, then writes to a temp file and renames it.
  - `Apply` sets `World.Slots` from `ReadSlots` on the post-spawn snapshot and runs the plan, then calls `UpdateSlots` with the plan's filled slots.
  - `laptop` does the same with `final.Filled` after `RunConverge`.
  - README gets one line on where memory lives and that it resets on app restart.
- Tests:
  - Read/write round trip.
  - A missing file and a corrupt file both return empty.
  - Two `UpdateSlots` calls with disjoint keys and different live sets keep both writers' keys, in either order.
  - A fake-driver `Apply` persists slots. A second `Apply` with reshuffled query order places the same windows.
  - A fake-driver `Laptop` run starts from persisted slots, reshuffles query order during convergence, places pinned windows by memory, and persists the result.

## Tasks

- [ ] Task 1: `SlotKey`, `SlotMemory`, `ClaimSlot`, `Reserve`, `SlotKeys` with tests
- [ ] Task 2: `DeskPlan` and `stepPinned` return filled slots; `PruneSlots`, `MergeSlots`; topology and mode-switch tests
- [ ] Task 3: `Paths.WindowSlots`, `ReadSlots`, `UpdateSlots`, `Apply`/`laptop` wiring, README line, command tests
