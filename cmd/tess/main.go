package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/mattwilkinsonn/tessera/internal/cli"
	"github.com/mattwilkinsonn/tessera/internal/commands"
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/daemon"
	"github.com/mattwilkinsonn/tessera/internal/effects"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// After the first signal cancels ctx, a second one gets the default behaviour.
	context.AfterFunc(ctx, stop)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	if code != 0 {
		os.Exit(code)
	}
}

func run(ctx context.Context, argv []string, stdout, stderr io.Writer) int {
	deps := cli.Deps{
		Version: version,
		Run: func(ctx context.Context, command commands.Command) (int, error) {
			// wake only dials the daemon socket, so it loads no profile.
			var profile *config.Profile
			if _, wake := command.(commands.WakeCommand); !wake {
				loaded, err := loadProfile(ctx)
				if err != nil {
					return 1, err
				}
				profile = loaded
			}
			// Unset paths resolve lazily, so commands that need none skip getconf.
			b, err := newBackend()
			if err != nil {
				return 1, err
			}
			return commands.Run(ctx, profile, command, b.Driver, commands.Env{Nudge: b.Nudge, Stderr: stderr})
		},
		DaemonOptions: func(ctx context.Context, command commands.DaemonCommand) (daemon.Options, error) {
			home, err := os.UserHomeDir()
			if err != nil {
				return daemon.Options{}, fmt.Errorf("resolving home directory: %w", err)
			}
			profile, err := config.Load(ctx, os.Getenv, home)
			if err != nil {
				return daemon.Options{}, err
			}
			paths, err := effects.DefaultPaths(ctx)
			if err != nil {
				return daemon.Options{}, err
			}
			b, err := newBackend()
			if err != nil {
				return daemon.Options{}, err
			}
			driver := b.Driver
			env := commands.Env{Paths: paths, Nudge: b.Nudge, Stderr: stderr}
			return daemon.Options{
				Socket:    paths.DaemonSocket,
				Bootstrap: b.Bootstrap && !command.NoBootstrap,
				Launchctl: daemon.ExecLaunchctl{},
				UID:       os.Getuid(),
				Services:  daemon.DefaultServices(home),
				Display:   daemon.DisplayWork(profile, driver, env),
				Flex:      daemon.FlexWork(profile, driver, env),
				WakeReady: daemon.StartupReadiness(driver),
				Wake:      daemon.WakeRoutine(profile, driver, env, paths.DaemonSocket),
				Nudge:     b.Nudge,
			}, nil
		},
	}
	return cli.Main(ctx, argv, stdout, stderr, deps)
}

func loadProfile(ctx context.Context) (*config.Profile, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory: %w", err)
	}
	return config.Load(ctx, os.Getenv, home)
}

func yabaiPath() string {
	if path := os.Getenv("TESS_YABAI"); path != "" {
		return path
	}
	return "/opt/homebrew/bin/yabai"
}
