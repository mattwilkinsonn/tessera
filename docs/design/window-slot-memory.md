# Design: remember which window holds each slot

## Resolved decisions

- **Tessera remembers windows; the browser is not configured to name them** (Matt, 2026-11-02). Naming windows with a browser add-on was rejected: it depends on a third-party add-on whose names do not survive restarts.

## Problem / Intent

A slot name that several windows can fill, such as `browser`, gets a random window on each layout. Switching desktop → laptop → desktop should put each window back where it was on each display. Memory may reset when the app restarts.

The cause is claim order. `ClaimSet.Claim` (`internal/engine/claim.go`) takes the first unclaimed candidate in `world.Windows` order. That is yabai query order, which follows focus recency. Browser specs match on app only, so any Zen window can fill any `browser` slot. `stepPinned` (`internal/engine/laptop.go`) sorts by window id first, so the laptop is steadier, but its order still shifts when a lower id closes.

## Approach

Remember which window last filled each **display slot**. A slot key is `<mode>:<display>/<name>#<k>`:

- `<mode>` is `desk` (`Apply`) or `laptop` (the laptop-only `laptop` command).
- `<display>` is the profile display name.
- k counts occurrences of `name` within that display's layout.

So `desk:g9/browser#1` is the first browser slot on the G9 at the desk.

- **Why keys are per display.** The topologies give one display different desks. For example, the full desk has two browser slots on the AW, but `aw-laptop` uses `#awSolo` with four. Topology selection is `ResolveDesk` in `internal/engine/matcher.go`. A count across the whole plan would move the AW's first slot from occurrence 2 to occurrence 1 when the topology changes. Counting within a display keeps `desk:aw/browser#1` the same slot in every topology that lays out the AW. Validation allows the base desk to hold two layouts for one display. In that case k counts across them in desk order, so every key in a plan names one slot.
- **Why keys carry the mode.** The desk's `#laptopStack` and the laptop-only `laptopPinned` both lay out the laptop display. If they shared keys, a window that led the laptop in laptop mode would be pulled into the desk's laptop stack on the way back. With separate namespaces, laptop mode never changes desk memory, so desktop → laptop → desktop restores the desk exactly.
- **Claiming.** A plan reserves the windows its own keys remember. A window is reserved only while it still matches the spec its key names (`matchesSpec`, `internal/engine/model.go`), so a title-specific window whose title drifts is not held. Sometimes two active keys remember the same window, for example when an AW-only topology moved a G9 window into an AW slot that is also active on the full desk. Then the entry recorded under the plan's own display set wins, and the higher sequence number breaks a remaining tie. The losing key counts as unremembered for this plan. Returning to a display set therefore restores that set's own layout. Then, for each key in plan order:
  1. Take the remembered window if it is a live, unclaimed candidate (not minimized or floating). Memory beats `preferDisplay`, because honouring the remembered slot is the point of the feature.
  2. Otherwise take an unclaimed candidate that no key in this plan reserves, keeping today's display preference.
  3. Otherwise leave the slot empty for this run. A reserved window is never taken by another key, on any display, so minimizing the window in slot 1 empties slot 1 and leaves slot 2 alone. Today a minimized window is not laid out either.

  So within one display set, two entries never share a window. A conflict needs entries recorded under two different display sets, which is the case the display-set rule settles.
- **Recording.** After a plan's ops succeed, the command records key → window id for every slot that was filled. Each entry is stamped with the plan's display set (sorted profile display names, e.g. `aw,laptop`) and a sequence number the file increments. A window can hold one key per topology, for example `desk:g9/browser#1` and `desk:aw/browser#3`.
- **Pruning.** An entry is dropped when:
  - its window id no longer exists,
  - the id now belongs to a different app, which happens when yabai reuses an id after a restart, or
  - the window no longer matches the spec its key names.

  Minimized and floating windows still exist, so they keep their keys.
- **Storage and concurrency.** A JSON file at `$XDG_CACHE_HOME/yabai/window-slots.json` (`effects.Paths.WindowSlots`) holds `{seq, slots: key → {id, app, displays, seq}}`. A file is needed because CLI commands (`Apply`, `Laptop` in `internal/commands/composite.go`) plan outside the daemon. `Apply` and `laptop` hold different locks, so both also take a dedicated flock (`window-slots.lock`). They wait for it, bounded by ctx, and hold it from reading memory through planning to writing. This orders the two plans as well as their writes, so an older plan cannot overwrite a newer one. Writes merge only the keys the plan filled, prune against a fresh `QueryWindows`, then write a temp file and rename it.
- **Unchanged.** `SpawnsNeeded` still counts missing windows and ignores memory. Flex order is untouched.
- **Non-goal: numpad focus.** `ResolveSlot` (`deskSlots`, `internal/engine/focus.go`) is a separate ordered list. It picks a window by display at focus time and does not read memory. Memory makes the window on each display stable, so `onDisplay` slots find the same window when a display has one candidate. Picking between two stacked browsers on one display stays as it is today. Binding `deskSlots` to slot keys would need its own mapping and is deferred.

### Alternatives considered

- **One ordinal per window per spec**, shared by every layout (`browser#k` everywhere). It gives one identity per window, but occurrence counts differ between topologies (see above), so the same physical slot would map to different ordinals.
- **Shared laptop keys across modes.** These would let the same window lead the laptop in both modes, but returning to the desk would then pull the laptop-mode leader into the desk's laptop stack, which defeats the goal.
- **A lock around the file write only.** This is shorter, but an older plan could still overwrite a newer plan's key.
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
  - `type WindowRef struct { ID int; App string; Displays string; Seq uint64 }`
  - `type SlotMemory struct { Seq uint64; Slots map[SlotKey]WindowRef }`
  - `World` gains `Slots SlotMemory`.
  - `func SlotKeys(mode string, display config.DisplayName, names []string) []SlotKey`: k counts per name within the list.
  - `func Reserve(profile *config.Profile, memory SlotMemory, keys []SlotKey, displays string, windows []wm.Window) map[int]SlotKey`
    - It skips an entry whose live window has a different `App` than the stored one, or no longer matches its key's spec. The pruning rules therefore apply before planning as well as after.
    - When an id is remembered by two of `keys`, the entry whose `Displays` equals `displays` wins, then the higher `Seq`.
  - `func NewClaimSet(profile *config.Profile, memory SlotMemory, reserved map[int]SlotKey) *ClaimSet`
  - `func (c *ClaimSet) ClaimSlot(windows []wm.Window, key SlotKey, name string, preferDisplay mo.Option[int]) (int, bool)`
  - `Claim` with empty memory keeps today's behaviour for `SpawnsNeeded`.
- Tests:
  - A remembered window wins its key whatever the query order.
  - Memory beats `preferDisplay`.
  - With slot 1's window minimized and slot 2's live, slot 1 is empty and slot 2 keeps its window.
  - A reserved window is never taken by another key, including a fallback on another display (an empty AW slot cannot take a window a G9 key reserves).
  - With two active keys remembering one window, the entry recorded under the current display set gets it, even when the other has a higher `Seq`. With neither or both matching, the higher `Seq` gets it. The losing key claims normally.
  - A remembered Ghostty window whose title now matches another spec is not reserved and can fill that other spec's slot.
  - A reused id whose app changed, but still matches the spec's app regex, is not reserved.
  - With empty memory, claims match today's output (existing `TestDeskPlan` stays green).

### Task 2: plans return filled slots (engine)

- `Interfaces:`
  - `func DeskPlan(profile *config.Profile, world World) ([]Op, map[SlotKey]int)`
    - `buildDesks` (`desk.go`) builds keys per display once, with `SlotKeys("desk", display, names)`, where `names` concatenates the tracks of every present layout for that display in desk order. `deskTracks` consumes the keys in the same order.
  - `ConvergeState` gains `Filled map[SlotKey]int`.
    - `stepPinned` (`laptop.go`) claims `laptopPinned` with `SlotKeys("laptop", config.DisplayLaptop, profile.LaptopPinned)` and records into `Filled`.
  - `func PruneSlots(profile *config.Profile, memory SlotMemory, windows []wm.Window) SlotMemory`
  - `func RecordSlots(memory SlotMemory, filled map[SlotKey]int, displays string, windows []wm.Window) SlotMemory`: bumps `Seq`, stamps each filled key with it and `displays`, and leaves other keys alone.
- Tests:
  - Full desk → `aw-laptop` → full desk with the same windows and reshuffled query order. The AW's first two browser slots keep their windows across all three steps. The browsers the full desk had on the G9 return to their G9 slots in step 3, even though `aw-laptop` laid them out on the AW in step 2.
  - The same round trip where an AW browser closes during step 2, so a G9 window fills `desk:aw/browser#2` under `aw,laptop`. In step 3 (`aw,g9,laptop`) that window returns to its G9 slot, and `desk:aw/browser#2` claims normally.
  - Repeated full-desk runs with one AW browser closed never move a G9 window to the AW: the empty AW slot stays empty, or takes an unreserved window.
  - Desk → laptop-only → desk returns every desk slot, including the desk's laptop stack.
  - A desk with two layouts on one display, both naming `browser`, gets distinct keys and keeps each window in its slot.
  - Prune drops dead ids, app-changed ids, and spec-mismatched entries. It keeps minimized and floating windows.
  - Record overwrites only filled keys and stamps them with a fresh `Seq`.

### Task 3: persistence and wiring (effects, commands)

- `Interfaces:`
  - `effects.Paths.WindowSlots string`, defaulting to `filepath.Join(cache, "yabai", "window-slots.json")`.
  - `func ReadSlots(path string) (engine.SlotMemory, error)`: a missing file returns empty with no error. A corrupt file is logged and returns empty, so a bad file never blocks layout.
  - `func LockSlots(ctx context.Context, path string) (unlock func(), err error)`: creates the parent directory, then takes a blocking flock on `window-slots.lock`, bounded by ctx.
  - `func WriteSlots(path string, memory engine.SlotMemory) error`: temp file + rename; creates the parent directory.
  - `Apply` takes `LockSlots` before its post-spawn snapshot. It sets `World.Slots = PruneSlots(profile, ReadSlots(...), snapshot.Windows)`, so entries for spec drift and reused ids are gone before planning. Then it runs the plan and, still holding the lock:
    1. re-queries windows,
    2. writes `PruneSlots(RecordSlots(World.Slots, filled, ...), fresh)`.

    The pruned memory is the base for recording, so an entry skipped in planning is never written back.
  - `laptop` holds the lock the same way around `RunConverge`: it prunes against the first snapshot and records `final.Filled` on that base.
  - README gets one line. Memory lives in the cache file and survives tess and daemon restarts. When an app restarts, its windows get new ids, so pruning drops that app's entries and the next layout learns them again. Deleting the file resets everything.
- Tests:
  - Read/write round trip.
  - A missing file and a corrupt file both return empty.
  - A title-specific window drifts to another spec and fills that spec's slot, then drifts back. After each run the file holds exactly one key for it.
  - A second `LockSlots` waits until the first unlocks, and returns ctx's error on timeout. Locking under a parent directory that does not exist yet succeeds.
  - A fake-driver `Apply` blocks inside planning while it holds the lock. A concurrent `laptop` does not query the world until `Apply` returns. The test also runs with the roles swapped.
  - A fake-driver `Apply` persists slots. A second `Apply` with reshuffled query order places the same windows.
  - A fake-driver `Laptop` run starts from persisted slots, reshuffles query order during convergence, places pinned windows by memory, and persists the result without changing `desk:` keys.
  - After an app restart (same app, new window ids), a fake-driver `Apply` drops the old entries, lays the new windows out as today, and records their keys.

## Tasks

- [ ] Task 1: `SlotKey`, `SlotMemory`, `SlotKeys`, `Reserve`, `ClaimSlot` with tests
- [ ] Task 2: `DeskPlan` and `stepPinned` return filled slots; `PruneSlots`, `RecordSlots`; topology and mode-switch tests
- [ ] Task 3: `Paths.WindowSlots`, `ReadSlots`, `LockSlots`, `WriteSlots`, `Apply`/`laptop` wiring, README line, command tests
