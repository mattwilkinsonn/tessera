package commands

import (
	"fmt"
	"strconv"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

// FromCLI converts a validated Cobra kind and its positional argument to a command.
func FromCLI(name string, args []string) (Command, error) {
	value := ""
	if len(args) > 0 {
		value = args[0]
	}
	switch name {
	case "apply":
		return ApplyCommand{}, nil
	case "laptop":
		return LaptopCommand{}, nil
	case "rules":
		return RulesCommand{}, nil
	case "display-setup":
		return DisplaySetupCommand{}, nil
	case "focus-slot":
		number, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("parsing focus slot: %w", err)
		}
		return FocusSlotCommand{Number: number}, nil
	case "snap":
		return SnapCommand{Mode: engine.SnapMode(value)}, nil
	case "stack-cycle":
		return StackCycleCommand{Direction: CycleDir(value)}, nil
	case "resize":
		return ResizeCommand{Direction: ResizeDir(value)}, nil
	case "move-display":
		return MoveDisplayCommand{Display: config.DisplayName(value)}, nil
	case "cycle-display":
		return CycleDisplayCommand{Direction: CycleDir(value)}, nil
	case "reset-splits":
		return ResetSplitsCommand{}, nil
	case "columns":
		return ColumnsCommand{}, nil
	case "focus":
		return FocusCommand{Direction: wm.Direction(value)}, nil
	case "swap":
		return SwapCommand{Direction: wm.Direction(value)}, nil
	case "warp":
		return WarpCommand{Direction: wm.Direction(value)}, nil
	case "insert":
		return InsertCommand{Direction: InsertDir(value)}, nil
	case "toggle-float":
		return ToggleFloatCommand{}, nil
	case "balance":
		return BalanceCommand{}, nil
	case "space":
		return SpaceCommand{Layout: SpaceLayoutMode(value)}, nil
	case "daemon":
		return DaemonCommand{}, nil
	case "wake":
		return WakeCommand{}, nil
	default:
		return nil, fmt.Errorf("unknown CLI kind %q", name)
	}
}
