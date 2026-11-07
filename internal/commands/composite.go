package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/executor"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/samber/mo"
)

const (
	spawnPolls          = 50
	spawnPollInterval   = 100 * time.Millisecond
	columnsIterationCap = 200
)

// Apply lays out each present display and nudges sketchybar once.
func Apply(ctx context.Context, profile *config.Profile, driver wm.Driver, env Env) error {
	paths, nudge, stderr, err := normalizedEnv(ctx, env)
	if err != nil {
		return err
	}
	lock, acquired, err := effects.AcquireLock(paths.ApplyLock)
	if err != nil || !acquired {
		return err
	}
	defer unlock(lock)
	if err := effects.SuppressSignals(paths.SignalGuard); err != nil {
		return fmt.Errorf("suppressing signals: %w", err)
	}
	defer releaseGuard(paths.SignalGuard)

	slotsUnlock, err := effects.LockSlots(ctx, paths.WindowSlots)
	if err != nil {
		return err
	}
	defer slotsUnlock()
	world, err := worldSnapshot(ctx, driver)
	if err != nil {
		return err
	}
	names := make([]string, 0)
	for _, layout := range engine.ResolveDesk(profile, world.Displays) {
		if _, present := engine.ResolveDisplay(profile, layout.Display, world.Displays); !present {
			continue
		}
		for _, track := range layout.Tracks {
			names = append(names, track...)
		}
	}
	spawnedNames := engine.SpawnsNeeded(profile, names, world.Windows)
	if err := spawnMissingWindows(ctx, profile, driver, names, world.Windows, stderr); err != nil {
		return err
	}
	beforeSpawn := world.Windows
	deskTopology := len(world.Displays) > 1 || externalDeskOnly(profile, world.Displays)
	world, err = worldSnapshot(ctx, driver)
	if err != nil {
		return err
	}
	memory, err := effects.ReadSlots(paths.WindowSlots)
	if err != nil {
		return err
	}
	world.Slots = engine.PruneSlots(profile, memory, world.Windows)
	ops, filled := engine.DeskPlan(profile, world)
	if deskTopology && len(spawnedNames) > 0 {
		world, filled, err = reconcileSpawnedWindows(ctx, profile, driver, spawnedNames, beforeSpawn, world, filled)
	} else {
		err = executor.RunPlan(ctx, driver, ops, func(engine.Op) error {
			return effects.SuppressSignals(paths.SignalGuard)
		})
	}
	if err != nil {
		return err
	}
	fresh, err := driver.QueryWindows(ctx)
	if err != nil {
		return fmt.Errorf("querying windows after desk plan: %w", err)
	}
	displays := engine.DisplaySet(profile, world.Displays)
	if _, hasLaptop := engine.ResolveDisplay(profile, config.DisplayLaptop, world.Displays); !hasLaptop {
		target, hasTarget := arrivalTargetFor(profile, world.Displays)
		if hasTarget && target.display != config.DisplayLaptop {
			claimed := make(map[int]struct{}, len(filled))
			for _, id := range filled {
				claimed[id] = struct{}{}
			}
			if err := stackDisplacedDeskWindows(ctx, driver, target, fresh, claimed); err != nil {
				return fmt.Errorf("stacking displaced desk windows: %w", err)
			}
			fresh, err = driver.QueryWindows(ctx)
			if err != nil {
				return fmt.Errorf("querying windows after displaced-window stacking: %w", err)
			}
		}
	}
	world.Windows = fresh
	filled = placedDeskSlots(profile, world, filled)
	updated := engine.PruneSlots(profile, engine.RecordSlots(world.Slots, filled, displays, fresh), fresh)
	if err := effects.WriteSlots(paths.WindowSlots, updated); err != nil {
		return fmt.Errorf("persisting window slot memory: %w", err)
	}
	for _, id := range filled {
		if env.ReclaimedDeskWindows != nil {
			env.ReclaimedDeskWindows[id] = struct{}{}
		}
	}
	return nudge(ctx, "yabai_spaces_changed")
}

// Laptop converges the laptop-only layout and returns the lock outcome.
func Laptop(ctx context.Context, profile *config.Profile, driver wm.Driver, env Env) (LaptopResult, error) {
	return laptop(ctx, profile, driver, env, true)
}

func handoffDeskWindows(ctx context.Context, profile *config.Profile, driver wm.Driver, env Env) error {
	paths, _, _, err := normalizedEnv(ctx, env)
	if err != nil {
		return err
	}
	release, err := lockPlacement(ctx, paths)
	if err != nil {
		return err
	}
	defer release()
	return HandoffDeskWindows(ctx, profile, driver)
}

func laptop(ctx context.Context, profile *config.Profile, driver wm.Driver, env Env, explicit bool) (LaptopResult, error) {
	paths, nudge, stderr, err := normalizedEnv(ctx, env)
	if err != nil {
		return "", err
	}
	lock, acquired, err := effects.AcquireLock(paths.LaptopLock)
	if err != nil {
		return "", err
	}
	if !acquired {
		return LaptopContended, nil
	}
	defer unlock(lock)
	if err := effects.SuppressSignals(paths.SignalGuard); err != nil {
		return "", fmt.Errorf("suppressing signals: %w", err)
	}
	defer releaseGuard(paths.SignalGuard)

	slotsUnlock, err := effects.LockSlots(ctx, paths.WindowSlots)
	if err != nil {
		return "", err
	}
	defer slotsUnlock()
	displays, err := driver.QueryDisplays(ctx)
	if err != nil {
		return "", err
	}
	if len(displays) != 1 {
		return LaptopSkipped, nil
	}
	laptopIdx, found := engine.ResolveDisplay(profile, config.DisplayLaptop, displays)
	if !found {
		return LaptopSkipped, nil
	}
	var home wm.SpaceID
	for _, display := range displays {
		if display.Idx == laptopIdx && len(display.SpaceIDs) > 0 {
			home = display.SpaceIDs[0]
			break
		}
	}
	if home == "" {
		return LaptopSkipped, nil
	}
	if explicit {
		windows, err := driver.QueryWindows(ctx)
		if err != nil {
			return "", err
		}
		if err := spawnMissingWindows(ctx, profile, driver, profile.LaptopPinned, windows, stderr); err != nil {
			return "", err
		}
	}
	persistedSlots, err := effects.ReadSlots(paths.WindowSlots)
	if err != nil {
		return "", err
	}
	initialWorld, err := worldSnapshot(ctx, driver)
	if err != nil {
		return "", err
	}
	memory := engine.PruneSlots(profile, persistedSlots, initialWorld.Windows)
	persisted, err := effects.ReadFlexOrder(paths.FlexOrder)
	if err != nil {
		return "", err
	}
	initial := engine.InitialConvergeState(home, persisted, explicit)
	initial.Filled = make(map[engine.SlotKey]int)
	var guardErr error
	final, err := executor.RunConverge(ctx, driver, func(world engine.World, state engine.ConvergeState) (engine.ConvergeAction, engine.ConvergeState, bool) {
		world.Slots = memory
		if guardErr = effects.SuppressSignals(paths.SignalGuard); guardErr != nil {
			return nil, state, true
		}
		for {
			action, next, emitted := engine.LaptopConvergeStep(profile, world, state)
			if emitted {
				return action, next, false
			}
			if next.Phase == state.Phase {
				return nil, next, true
			}
			state = next
		}
	}, initial)
	if err != nil {
		return "", err
	}
	if guardErr != nil {
		return "", fmt.Errorf("restamping signal guard: %w", guardErr)
	}
	if err := effects.WriteFlexOrder(final.ToPersist, paths.FlexOrder); err != nil {
		return "", fmt.Errorf("persisting flex order: %w", err)
	}
	if err := driver.SetSpaceLayout(ctx, home, wm.LayoutStack); err != nil {
		return "", err
	}
	fresh, err := driver.QueryWindows(ctx)
	if err != nil {
		return "", fmt.Errorf("querying windows after laptop converge: %w", err)
	}
	updated := engine.PruneSlots(profile, engine.RecordSlots(memory, final.Filled, engine.DisplaySet(profile, displays), fresh), fresh)
	if err := effects.WriteSlots(paths.WindowSlots, updated); err != nil {
		return "", fmt.Errorf("persisting window slot memory: %w", err)
	}
	if err := nudge(ctx, "yabai_spaces_changed"); err != nil {
		return "", err
	}
	return LaptopRan, nil
}

// adoptSpawnedWindows admits only new matching tile candidates before desk claims.
// The caller holds the apply and slot locks and the signal guard throughout spawning.
func adoptSpawnedWindows(ctx context.Context, profile *config.Profile, driver wm.Driver, names []string, before []wm.Window) error {
	baseline := make(map[int]struct{}, len(before))
	for _, window := range before {
		baseline[window.ID] = struct{}{}
	}
	windows, err := driver.QueryWindows(ctx)
	if err != nil {
		return err
	}
	// Project only eligible new floats as available to reuse the planner's exact
	// match, spawn fallback, classification, and distinct-ID selection policy.
	candidates := make([]wm.Window, 0, len(windows))
	for _, window := range windows {
		if _, old := baseline[window.ID]; old || !window.Floating || window.Minimized || !IsTileCandidate(window) {
			continue
		}
		window.Floating = false
		candidates = append(candidates, window)
	}
	claims := engine.NewClaimSet(profile, engine.SlotMemory{}, nil)
	for _, id := range claims.ClaimMany(candidates, names, mo.None[int]()) {
		for _, window := range windows {
			if window.ID != id {
				continue
			}
			if err := placeWindow(ctx, driver, window, window.SpaceID); err != nil {
				return fmt.Errorf("adopting spawned window %d: %w", window.ID, err)
			}
			break
		}
	}
	return nil
}

func projectSpawnedFloats(profile *config.Profile, names []string, windows []wm.Window, baseline, protected map[int]struct{}) map[int]wm.Window {
	candidates := make(map[int]wm.Window)
	for i, window := range windows {
		_, old := baseline[window.ID]
		_, owner := protected[window.ID]
		if old && !owner || !window.Floating || window.Minimized || !IsTileCandidate(window) {
			continue
		}
		if !owner && !matchesSpawnedApp(profile, names, window) {
			continue
		}
		candidates[window.ID] = window
		windows[i].Floating = false
	}
	return candidates
}

// reconcileSpawnedWindows reserves owners and gives late floats first refusal on
// vacant slots. Overflow uses the final reconciled snapshot, never a newer view.
func reconcileSpawnedWindows(
	ctx context.Context,
	profile *config.Profile,
	driver wm.Driver,
	names []string,
	before []wm.Window,
	world engine.World,
	filled map[engine.SlotKey]int,
) (engine.World, map[engine.SlotKey]int, error) {
	if len(names) == 0 {
		return world, filled, nil
	}
	protected := make(map[int]struct{}, len(filled))
	baseline := make(map[int]struct{}, len(before))
	for _, window := range before {
		baseline[window.ID] = struct{}{}
	}
	for _, id := range filled {
		protected[id] = struct{}{}
	}
	// Phase A spends only its polling budget. No layout mutation occurs here.
	for poll := 0; ; poll++ {
		fresh, err := worldSnapshot(ctx, driver)
		if err != nil {
			return world, filled, err
		}
		fresh.Slots = world.Slots
		projected := fresh
		projected.Windows = slices.Clone(fresh.Windows)
		projectSpawnedFloats(profile, names, projected.Windows, baseline, protected)
		_, available := engine.DeskPlan(profile, projected)
		world = fresh
		if spawnedDemandFilled(profile, names, fresh.Displays, available) || poll >= spawnPolls {
			break
		}
		if err := waitForSpawnPoll(ctx); err != nil {
			return world, filled, err
		}
	}
	// Phases B and C: one unconditional rebuild, then at most one repair for
	// candidates or owners that changed while that rebuild was running.
	for rebuild := range 2 {
		projected := world
		projected.Windows = slices.Clone(world.Windows)
		candidates := projectSpawnedFloats(profile, names, projected.Windows, baseline, protected)
		_, selected := engine.DeskPlan(profile, projected)
		if rebuild > 0 && maps.Equal(placedDeskSlots(profile, world, selected), selected) {
			break
		}
		for _, id := range selected {
			protected[id] = struct{}{}
			if window, ok := candidates[id]; ok {
				if err := placeWindow(ctx, driver, window, window.SpaceID); err != nil {
					return world, filled, err
				}
			}
		}
		var err error
		world.Windows, err = driver.QueryWindows(ctx)
		if err != nil {
			return world, filled, err
		}
		ops, planned := engine.DeskPlan(profile, world)
		for _, id := range planned {
			protected[id] = struct{}{}
		}
		if err := executor.RunPlan(ctx, driver, ops, nil); err != nil {
			return world, filled, err
		}
		world, err = worldSnapshot(ctx, driver)
		if err != nil {
			return world, filled, err
		}
		filled = placedDeskSlots(profile, world, planned)
		world.Slots = engine.RecordSlots(projected.Slots, planned, engine.DisplaySet(profile, world.Displays), world.Windows)
	}
	return world, filled, placeSpawnedArrivals(ctx, profile, driver, names, baseline, world, protected)
}

func matchesSpawnedApp(profile *config.Profile, names []string, window wm.Window) bool {
	return slices.ContainsFunc(names, func(name string) bool {
		spec := profile.Windows[name]
		return engine.MatchesSpec(config.WindowSpec{App: spec.App, AppRE: spec.AppRE}, window.App, window.Title)
	})
}

func spawnedDemandFilled(profile *config.Profile, names []string, displays []wm.Display, filled map[engine.SlotKey]int) bool {
	namesByDisplay := make(map[config.DisplayName][]string)
	for _, layout := range engine.ResolveDesk(profile, displays) {
		if _, present := engine.ResolveDisplay(profile, layout.Display, displays); !present {
			continue
		}
		for _, track := range layout.Tracks {
			namesByDisplay[layout.Display] = append(namesByDisplay[layout.Display], track...)
		}
	}
	for display, configured := range namesByDisplay {
		keys := engine.SlotKeys("desk", display, configured)
		for i, name := range configured {
			if slices.Contains(names, name) {
				if _, ok := filled[keys[i]]; !ok {
					return false
				}
			}
		}
	}
	return true
}

// A claim is successful only while its owner is tiled on the intended display.
func placedDeskSlots(profile *config.Profile, world engine.World, planned map[engine.SlotKey]int) map[engine.SlotKey]int {
	placed := make(map[engine.SlotKey]int, len(planned))
	for key, id := range planned {
		for _, layout := range engine.ResolveDesk(profile, world.Displays) {
			if !strings.HasPrefix(string(key), "desk:"+string(layout.Display)+"/") {
				continue
			}
			index, present := engine.ResolveDisplay(profile, layout.Display, world.Displays)
			home := wm.SpaceID("")
			for _, display := range world.Displays {
				if display.Idx == index && len(display.SpaceIDs) > 0 {
					home = display.SpaceIDs[0]
				}
			}
			for _, window := range world.Windows {
				if present && window.ID == id && !window.Floating && !window.Minimized && window.DisplayIdx == index && window.SpaceID == home {
					placed[key] = id
				}
			}
		}
	}
	return placed
}

// placeSpawnedArrivals handles restore overflow after configured slots are filled.
// The baseline excludes floats that existed before this apply's launches.
func placeSpawnedArrivals(ctx context.Context, profile *config.Profile, driver wm.Driver, names []string, baseline map[int]struct{}, world engine.World, protected map[int]struct{}) error {
	target, ok := arrivalTargetFor(profile, world.Displays)
	if !ok || len(names) == 0 {
		return nil
	}
	anchor := 0
	if target.display != config.DisplayLaptop {
		anchor = leftDeskSlotWindow(target, world.Windows, 0)
	}
	// Project this exact overflow snapshot to protect any still-vacant slot candidate.
	projected := world
	projected.Windows = slices.Clone(world.Windows)
	for i, window := range projected.Windows {
		if _, old := baseline[window.ID]; !old && IsTileCandidate(window) && matchesSpawnedApp(profile, names, window) {
			projected.Windows[i].Floating = false
		}
	}
	_, selected := engine.DeskPlan(profile, projected)
	for _, id := range selected {
		protected[id] = struct{}{}
	}
	for _, window := range world.Windows {
		if _, old := baseline[window.ID]; old || !window.Floating || window.Minimized || !IsTileCandidate(window) {
			continue
		}
		if _, owner := protected[window.ID]; owner {
			continue
		}
		if !matchesSpawnedApp(profile, names, window) {
			continue
		}
		if err := placeArrivalWindow(ctx, driver, window, target, anchor); err != nil {
			return fmt.Errorf("placing restored window %d: %w", window.ID, err)
		}
		if target.display != config.DisplayLaptop && anchor == 0 {
			var err error
			anchor, err = placedArrivalAnchor(ctx, driver, target, window.ID)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func spawnMissingWindows(ctx context.Context, profile *config.Profile, driver wm.Driver, names []string, windows []wm.Window, stderr io.Writer) error {
	if profile == nil || driver == nil || stderr == nil {
		return errors.New("spawning missing windows: profile, driver, and stderr are required")
	}
	targets, err := startMissingWindows(ctx, profile, driver, names, windows, stderr)
	if err != nil || len(targets) == 0 {
		return err
	}
	return waitForSpawnedWindows(ctx, driver, targets, stderr)
}

type spawnTarget struct {
	app      *regexp.Regexp
	baseline map[int]struct{}
	count    int
}

func startMissingWindows(ctx context.Context, profile *config.Profile, driver wm.Driver, names []string, windows []wm.Window, stderr io.Writer) (map[string]*spawnTarget, error) {
	targets := make(map[string]*spawnTarget)
	for _, name := range engine.SpawnsNeeded(profile, names, windows) {
		spec, ok := profile.Windows[name]
		if !ok {
			continue
		}
		target := targets[spec.App]
		if target == nil {
			target = &spawnTarget{app: spec.AppRE, baseline: make(map[int]struct{})}
			for _, window := range windows {
				if engine.MatchesSpec(config.WindowSpec{AppRE: spec.AppRE}, window.App, window.Title) {
					target.baseline[window.ID] = struct{}{}
				}
			}
			targets[spec.App] = target
		}
		if err := driver.SpawnWindow(ctx, spec.Spawn); err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "tess: spawn failed for %s: %v\n", name, err); writeErr != nil {
				return nil, fmt.Errorf("writing spawn failure: %w", writeErr)
			}
			continue
		}
		target.count++
	}
	return targets, nil
}

func waitForSpawnedWindows(ctx context.Context, driver wm.Driver, targets map[string]*spawnTarget, stderr io.Writer) error {
	missing := make([]string, 0, len(targets))
	previous := make(map[int]struct{})
	quietPolls := 0
	for attempt := range spawnPolls {
		current, err := driver.QueryWindows(ctx)
		if err != nil {
			return err
		}
		missing = missingSpawnTargets(current, targets)
		// Session restoration can satisfy the requested count before the launcher
		// finishes creating windows. Keep the existing bounded wait, but require
		// a quiet second of matching IDs before taking the adoption snapshot.
		currentIDs := make(map[int]struct{})
		for _, window := range current {
			for _, target := range targets {
				if _, old := target.baseline[window.ID]; !old && target.app.MatchString(window.App) {
					currentIDs[window.ID] = struct{}{}
					break
				}
			}
		}
		if maps.Equal(previous, currentIDs) {
			quietPolls++
		} else {
			quietPolls = 0
		}
		previous = currentIDs
		if len(missing) == 0 && quietPolls >= 10 {
			return nil
		}
		if attempt+1 < spawnPolls {
			if err := waitForSpawnPoll(ctx); err != nil {
				return err
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	slices.Sort(missing)
	if _, err := fmt.Fprintf(stderr, "tess: spawned windows not found for %s\n", strings.Join(missing, ", ")); err != nil {
		return fmt.Errorf("writing spawn timeout: %w", err)
	}
	return nil
}

func missingSpawnTargets(current []wm.Window, targets map[string]*spawnTarget) []string {
	missing := make([]string, 0, len(targets))
	for appName, target := range targets {
		found := 0
		for _, window := range current {
			if _, old := target.baseline[window.ID]; old {
				continue
			}
			if target.app.MatchString(window.App) {
				found++
			}
		}
		if found < target.count {
			missing = append(missing, appName)
		}
	}
	return missing
}

func waitForSpawnPoll(ctx context.Context) error {
	timer := time.NewTimer(spawnPollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func unlock(lock interface{ Unlock() error }) {
	if err := lock.Unlock(); err != nil {
		slog.Warn("failed to unlock command lock", "error", err)
	}
}

func releaseGuard(path string) {
	if err := effects.ReleaseSignals(path); err != nil {
		slog.Warn("failed to release signal guard", "path", path, "error", err)
	}
}
