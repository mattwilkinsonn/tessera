// Package cli parses Tessera command-line input.
package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/mattwilkinsonn/tessera/internal/commands"
	"github.com/spf13/cobra"
)

type kind struct {
	name        string
	description string
	choices     []string
	integer     bool
}

var kinds = [...]kind{
	{name: "apply", description: "Converge the active space onto its configured layout."},
	{name: "laptop", description: "Converge the laptop-only fallback layout."},
	{name: "rules", description: "Apply window rules from the profile."},
	{name: "display-setup", description: "Set up displays for the current profile."},
	{name: "focus-slot", description: "Focus a named slot.", integer: true},
	{name: "snap", description: "Snap the focused window to a layout mode.", choices: []string{"3col", "50-50", "columns"}},
	{name: "stack-cycle", description: "Cycle the focused stack.", choices: []string{"next", "prev"}},
	{name: "resize", description: "Resize the active column.", choices: []string{"grow", "shrink"}},
	{name: "move-display", description: "Move the focused window to a display.", choices: []string{"g9", "aw", "laptop"}},
	{name: "cycle-display", description: "Cycle the focused display.", choices: []string{"next", "prev"}},
	{name: "reset-splits", description: "Reset splits in the active space."},
	{name: "columns", description: "Re-flow the active space into its columns."},
	{name: "focus", description: "Focus the neighboring window.", choices: []string{"west", "south", "north", "east"}},
	{name: "swap", description: "Swap the focused window with its neighbor.", choices: []string{"west", "south", "north", "east"}},
	{name: "warp", description: "Warp the focused window to its neighbor.", choices: []string{"west", "south", "north", "east"}},
	{name: "insert", description: "Set the insertion direction for the focused window.", choices: []string{"east", "west", "north", "south", "stack"}},
	{name: "toggle-float", description: "Toggle floating for the focused window."},
	{name: "balance", description: "Balance the active space."},
	{name: "space", description: "Apply a named space layout.", choices: []string{"bsp", "stack"}},
	{name: "daemon", description: "Run the resident event daemon."},
	{name: "wake", description: "Ask the resident daemon to run its wake routine."},
}

// UsageError reports a Cobra parse or validation failure.
type UsageError struct {
	Msg     string
	UseLine string
}

func (e *UsageError) Error() string { return e.Msg }

// Parse executes the Cobra tree and returns its parsed command, if any.
func Parse(ctx context.Context, argv []string, version string, stdout, stderr io.Writer) (commands.Command, error) {
	root := &cobra.Command{
		Use:           "tess",
		Short:         "Tessera window-manager driver",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetVersionTemplate("tess v{{.Version}}\n")

	var parsed commands.Command
	for _, entry := range kinds {
		var noBootstrap bool
		command := &cobra.Command{
			Use:       commandUse(entry),
			Short:     entry.description,
			Args:      commandArgs(entry),
			ValidArgs: entry.choices,
			RunE: func(_ *cobra.Command, args []string) error {
				if entry.name == "daemon" {
					parsed = commands.DaemonCommand{NoBootstrap: noBootstrap}
					return nil
				}
				var err error
				parsed, err = commands.FromCLI(entry.name, args)
				return err
			},
		}
		if entry.name == "daemon" {
			command.Flags().BoolVar(&noBootstrap, "no-bootstrap", false, "Skip launchd service bootstrap")
		}
		root.AddCommand(command)
	}

	root.SetArgs(nonNilArgs(argv))
	_, err := root.ExecuteContextC(ctx)
	if err != nil {
		useLine := root.UseLine()
		if command, _, findErr := root.Find(nonNilArgs(argv)); findErr == nil {
			useLine = command.UseLine()
		}
		return nil, &UsageError{Msg: err.Error(), UseLine: useLine}
	}
	return parsed, nil
}

func nonNilArgs(argv []string) []string {
	if argv == nil {
		return []string{}
	}
	return argv
}

func commandUse(entry kind) string {
	if entry.integer {
		return entry.name + " N"
	}
	if len(entry.choices) == 0 {
		return entry.name
	}
	return entry.name + " {" + strings.Join(entry.choices, "|") + "}"
}

func commandArgs(entry kind) cobra.PositionalArgs {
	switch {
	case len(entry.choices) > 0:
		return cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs)
	case entry.integer:
		return cobra.MatchAll(cobra.ExactArgs(1), func(command *cobra.Command, args []string) error {
			if _, err := strconv.Atoi(args[0]); err != nil {
				return fmt.Errorf("invalid integer %q", args[0])
			}
			return nil
		})
	default:
		return cobra.NoArgs
	}
}
