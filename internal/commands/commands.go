package commands

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

var (
	ErrDaemonCommand    = errors.New("daemon command must be handled by the CLI")
	ErrDaemonNotRunning = errors.New("daemon not running")
)

// Run executes a command and returns its process exit code.
func Run(ctx context.Context, profile *config.Profile, command Command, driver wm.Driver, env Env) (int, error) {
	switch command := command.(type) {
	case ApplyCommand:
		return 0, Apply(ctx, profile, driver, env)
	case LaptopCommand:
		result, err := Laptop(ctx, profile, driver, env)
		if err != nil {
			return 0, err
		}
		if result == LaptopContended {
			return 1, nil
		}
		return 0, nil
	case RulesCommand:
		return 0, Rules(ctx, profile, driver)
	case DisplaySetupCommand:
		return 0, DisplaySetup(ctx, profile, driver)
	case FocusSlotCommand:
		return 0, FocusSlot(ctx, profile, driver, command.Number)
	case SnapCommand:
		return 0, Snap(ctx, profile, driver, command.Mode)
	case StackCycleCommand:
		return 0, StackCycle(ctx, driver, command.Direction)
	case ResizeCommand:
		return 0, Resize(ctx, driver, command.Direction)
	case MoveDisplayCommand:
		return 0, MoveDisplay(ctx, profile, driver, command.Display)
	case CycleDisplayCommand:
		return 0, CycleDisplay(ctx, driver, command.Direction)
	case ResetSplitsCommand:
		return 0, ResetSplits(ctx, driver)
	case ColumnsCommand:
		return 0, Columns(ctx, driver)
	case FocusCommand:
		_, err := driver.FocusWindowDir(ctx, wm.FocusSelector(command.Direction))
		return 0, err
	case SwapCommand:
		_, err := driver.SwapWindows(ctx, command.Direction)
		return 0, err
	case WarpCommand:
		_, err := driver.WarpWindow(ctx, command.Direction)
		return 0, err
	case InsertCommand:
		return 0, Insert(ctx, driver, command.Direction)
	case ToggleFloatCommand:
		return 0, ToggleFloat(ctx, driver)
	case BalanceCommand:
		return 0, driver.BalanceSpace(ctx, nil)
	case SpaceCommand:
		return 0, SpaceLayout(ctx, driver, command.Layout)
	case WakeCommand:
		return 0, Wake(ctx, profile, driver, env, "")
	case DaemonCommand:
		return 0, fmt.Errorf("routing daemon command: %w", ErrDaemonCommand)
	default:
		return 0, fmt.Errorf("unknown command %T", command)
	}
}

func normalizedEnv(env Env) (effects.Paths, effects.Nudger, io.Writer, error) {
	defaults, err := effects.DefaultPaths()
	if err != nil {
		return effects.Paths{}, nil, nil, fmt.Errorf("resolving default paths: %w", err)
	}
	paths := env.Paths
	if paths.ApplyLock == "" {
		paths.ApplyLock = defaults.ApplyLock
	}
	if paths.LaptopLock == "" {
		paths.LaptopLock = defaults.LaptopLock
	}
	if paths.SignalGuard == "" {
		paths.SignalGuard = defaults.SignalGuard
	}
	if paths.FlexOrder == "" {
		paths.FlexOrder = defaults.FlexOrder
	}
	if paths.DaemonSocket == "" {
		paths.DaemonSocket = defaults.DaemonSocket
	}
	nudge := env.Nudge
	if nudge == nil {
		nudge = effects.NudgeSketchybar
	}
	stderr := env.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	return paths, nudge, stderr, nil
}
