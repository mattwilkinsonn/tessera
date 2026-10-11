package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/mattwilkinsonn/tessera/internal/commands"
	"github.com/mattwilkinsonn/tessera/internal/daemon"
)

// DaemonOptions builds daemon settings after the command has parsed.
type DaemonOptions func(context.Context, commands.DaemonCommand) (daemon.Options, error)

// Runner executes one parsed command.
type Runner func(context.Context, commands.Command) (int, error)

// Deps contains the runtime hooks used by Main.
type Deps struct {
	Version       string
	Run           Runner
	DaemonOptions DaemonOptions
}

// Main parses argv, executes its command, and returns the process exit code.
func Main(ctx context.Context, argv []string, stdout, stderr io.Writer, deps Deps) int {
	if stderr == nil {
		stderr = io.Discard
	}
	command, err := Parse(ctx, argv, deps.Version, stdout, stderr)
	if err != nil {
		if usage, ok := errors.AsType[*UsageError](err); ok {
			if _, writeErr := fmt.Fprintf(stderr, "tess: %s\nusage: %s\n", usage.Msg, usage.UseLine); writeErr != nil {
				slog.ErrorContext(ctx, "writing CLI usage error", "error", writeErr)
			}
		} else {
			slog.ErrorContext(ctx, "parsing CLI arguments", "error", err)
		}
		return 1
	}
	if command == nil {
		return 0
	}
	if deps.Run == nil {
		slog.ErrorContext(ctx, "command runner is unavailable")
		return 1
	}
	code, err := deps.Run(ctx, command)
	if errors.Is(err, commands.ErrDaemonCommand) {
		daemonCommand, ok := command.(commands.DaemonCommand)
		if !ok {
			slog.ErrorContext(ctx, "daemon routing requested for a non-daemon command")
			return 1
		}
		if deps.DaemonOptions == nil {
			slog.ErrorContext(ctx, "daemon options are unavailable")
			return 1
		}
		options, optionsErr := deps.DaemonOptions(ctx, daemonCommand)
		if optionsErr != nil {
			slog.ErrorContext(ctx, "configuring daemon", "error", optionsErr)
			return 1
		}
		err = daemon.Run(ctx, options)
		if errors.Is(err, daemon.ErrRunning) {
			return 0
		}
		if err != nil {
			slog.ErrorContext(ctx, "running daemon", "error", err)
			return 1
		}
		return 0
	}
	if ctx.Err() != nil {
		return 130
	}
	if err != nil {
		slog.ErrorContext(ctx, "running command", "error", err)
		return 1
	}
	return code
}
