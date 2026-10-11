package commands

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

// CascadeResult reports the layout selected for a display change.
type CascadeResult string

const (
	CascadeApplied   CascadeResult = "applied"
	CascadeRan       CascadeResult = "ran"
	CascadeSkipped   CascadeResult = "skipped"
	CascadeContended CascadeResult = "contended"
)

// Rules replaces Tessera-managed arrival rules and applies them to open windows.
func Rules(ctx context.Context, profile *config.Profile, driver wm.Driver) error {
	ops, ok := driver.(wm.RuleOps)
	if !ok {
		return nil
	}
	existing, err := ops.List(ctx)
	if err != nil {
		return err
	}
	for _, rule := range existing {
		if strings.HasPrefix(rule.Label, "auto:") {
			if err := ops.Remove(ctx, rule.Label); err != nil {
				return err
			}
		}
	}
	displays, err := driver.QueryDisplays(ctx)
	if err != nil {
		return err
	}
	g9, hasG9 := engineResolveDisplay(profile, config.DisplayG9, displays)
	aw, hasAW := engineResolveDisplay(profile, config.DisplayAW, displays)
	laptop, hasLaptop := engineResolveDisplay(profile, config.DisplayLaptop, displays)
	var laptopSpaceIndex *int
	if hasLaptop {
		for _, display := range displays {
			if display.Idx != laptop || len(display.SpaceIDs) == 0 {
				continue
			}
			index, found := spaceGlobalIndex(display.SpaceIDs[0], displays)
			if found {
				laptopSpaceIndex = &index
			}
			break
		}
	}
	if laptopSpaceIndex != nil {
		if err := ops.Add(ctx, wm.Rule{Label: "auto:default-laptop-stack", App: "^.*$", Space: laptopSpaceIndex}); err != nil {
			return err
		}
	}
	if hasG9 {
		if err := ops.Add(ctx, wm.Rule{Label: "auto:code", App: "^Code$", Display: &g9}); err != nil {
			return err
		}
	}
	if hasAW {
		if err := ops.Add(ctx, wm.Rule{Label: "auto:linear", App: "^Linear$", Display: &aw}); err != nil {
			return err
		}
	}
	if laptopSpaceIndex != nil {
		if err := ops.Add(ctx, wm.Rule{Label: "auto:akiflow", App: "^Akiflow$", Space: laptopSpaceIndex}); err != nil {
			return err
		}
	}
	manage := false
	if err := ops.Add(ctx, wm.Rule{Label: "auto:finder", App: "^Finder$", Manage: &manage}); err != nil {
		return err
	}
	if err := ops.Add(ctx, wm.Rule{Label: "auto:akiflow-dialog", App: "^Akiflow$", Subrole: "AXDialog", Manage: &manage}); err != nil {
		return err
	}
	return ops.Apply(ctx)
}

// DisplaySetup sets the laptop home space to stack layout when available.
func DisplaySetup(ctx context.Context, profile *config.Profile, driver wm.Driver) error {
	displays, err := driver.QueryDisplays(ctx)
	if err != nil {
		return err
	}
	index, ok := engineResolveDisplay(profile, config.DisplayLaptop, displays)
	if !ok {
		return nil
	}
	for _, display := range displays {
		if display.Idx == index && len(display.SpaceIDs) > 0 {
			return driver.SetSpaceLayout(ctx, display.SpaceIDs[0], wm.LayoutStack)
		}
	}
	return nil
}

// RunDisplayCascade applies display defaults, rules, and the current layout reclaim.
func RunDisplayCascade(ctx context.Context, profile *config.Profile, driver wm.Driver, env Env) (CascadeResult, error) {
	if err := DisplaySetup(ctx, profile, driver); err != nil {
		return "", err
	}
	if err := Rules(ctx, profile, driver); err != nil {
		return "", err
	}
	displays, err := driver.QueryDisplays(ctx)
	if err != nil {
		return "", err
	}
	if len(displays) <= 1 {
		result, err := laptop(ctx, profile, driver, env, true)
		switch result {
		case LaptopContended:
			return CascadeContended, err
		case LaptopSkipped:
			return CascadeSkipped, err
		default:
			return CascadeRan, err
		}
	}
	if err := Apply(ctx, profile, driver, env); err != nil {
		return "", err
	}
	return CascadeApplied, nil
}

// RunFlexConverge converges a laptop event without spawning windows or reaping blank spaces.
func RunFlexConverge(ctx context.Context, profile *config.Profile, driver wm.Driver, env Env) (LaptopResult, error) {
	return laptop(ctx, profile, driver, env, false)
}

// Wake asks a running daemon to run its wake routine.
func Wake(ctx context.Context, _ *config.Profile, _ wm.Driver, env Env, sock string) error {
	paths, _, stderr, err := normalizedEnv(env)
	if err != nil {
		return err
	}
	if sock == "" {
		sock = paths.DaemonSocket
	}
	dialer := net.Dialer{Timeout: time.Second}
	conn, err := dialer.DialContext(ctx, "unix", sock)
	if err != nil {
		if _, writeErr := fmt.Fprintln(stderr, "tess: daemon not running"); writeErr != nil {
			return fmt.Errorf("writing daemon status: %w", writeErr)
		}
		return fmt.Errorf("%w: %w", ErrDaemonNotRunning, err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			slog.WarnContext(ctx, "failed to close wake socket", "error", err)
		}
	}()
	if _, err := conn.Write([]byte("wake\n")); err != nil {
		return fmt.Errorf("sending wake request: %w", err)
	}
	return nil
}

func spaceGlobalIndex(id wm.SpaceID, displays []wm.Display) (int, bool) {
	index := 0
	for _, display := range displays {
		for _, spaceID := range display.SpaceIDs {
			index++
			if spaceID == id {
				return index, true
			}
		}
	}
	return 0, false
}

func engineResolveDisplay(profile *config.Profile, name config.DisplayName, displays []wm.Display) (int, bool) {
	return engine.ResolveDisplay(profile, name, displays)
}
