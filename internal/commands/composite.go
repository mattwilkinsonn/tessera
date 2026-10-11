package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/executor"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

const (
	spawnPolls          = 50
	spawnPollInterval   = 100 * time.Millisecond
	columnsIterationCap = 200
)

// Apply lays out each present display and nudges sketchybar once.
func Apply(ctx context.Context, profile *config.Profile, driver wm.Driver, env Env) error {
	paths, nudge, stderr, err := normalizedEnv(env)
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
	if err := spawnMissingWindows(ctx, profile, driver, names, world.Windows, stderr); err != nil {
		return err
	}
	world, err = worldSnapshot(ctx, driver)
	if err != nil {
		return err
	}
	if err := executor.RunPlan(ctx, driver, engine.DeskPlan(profile, world), func(engine.Op) error {
		return effects.SuppressSignals(paths.SignalGuard)
	}); err != nil {
		return err
	}
	return nudge(ctx, "yabai_spaces_changed")
}

// Laptop converges the laptop-only layout and returns the lock outcome.
func Laptop(ctx context.Context, profile *config.Profile, driver wm.Driver, env Env) (LaptopResult, error) {
	return laptop(ctx, profile, driver, env, true)
}

func laptop(ctx context.Context, profile *config.Profile, driver wm.Driver, env Env, explicit bool) (LaptopResult, error) {
	paths, nudge, stderr, err := normalizedEnv(env)
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
	persisted, err := effects.ReadFlexOrder(paths.FlexOrder)
	if err != nil {
		return "", err
	}
	initial := engine.InitialConvergeState(home, persisted, explicit)
	var guardErr error
	final, err := executor.RunConverge(ctx, driver, func(world engine.World, state engine.ConvergeState) (engine.ConvergeAction, engine.ConvergeState, bool) {
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
	if err := nudge(ctx, "yabai_spaces_changed"); err != nil {
		return "", err
	}
	return LaptopRan, nil
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
	for attempt := range spawnPolls {
		current, err := driver.QueryWindows(ctx)
		if err != nil {
			return err
		}
		missing = missingSpawnTargets(current, targets)
		if len(missing) == 0 {
			return nil
		}
		if attempt+1 < spawnPolls {
			if err := waitForSpawnPoll(ctx); err != nil {
				return err
			}
		}
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
