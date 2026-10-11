package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/commands"
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

const (
	ncPath          = "/usr/bin/nc"
	signalPrefix    = "tessera-"
	barEvent        = "yabai_spaces_changed"
	displaySettled  = "display_relatch"
	bootoutDeadline = 5 * time.Second
)

// RegisterSignals labels every placement signal with a socket notifier and the sketchybar pair with its trigger.
func RegisterSignals(ctx context.Context, src wm.EventSource, sock string) error {
	abs, err := filepath.Abs(sock)
	if err != nil {
		return fmt.Errorf("resolving daemon socket: %w", err)
	}
	for _, ev := range append(append([]wm.Event{}, DisplayEvents...), FlexEvents...) {
		action := "echo " + string(ev) + " | " + ncPath + " -U -w 1 " + shellQuote(abs)
		if err := src.AddSignal(ctx, ev, signalPrefix+string(ev), action); err != nil {
			return fmt.Errorf("registering %s: %w", ev, err)
		}
	}
	for _, ev := range []wm.Event{wm.EventSpaceChanged, wm.EventDisplayChanged} {
		action := effects.SketchybarPath + " --trigger " + barEvent
		if err := src.AddSignal(ctx, ev, signalPrefix+string(ev), action); err != nil {
			return fmt.Errorf("registering %s: %w", ev, err)
		}
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// DisplayWork runs the display cascade; a contended laptop path restamps.
func DisplayWork(profile *config.Profile, driver wm.Driver, env commands.Env) Work {
	return func(ctx context.Context) (Step, error) {
		result, err := commands.RunDisplayCascade(ctx, profile, driver, env)
		if result == commands.CascadeContended {
			return Restamp, err
		}
		return Settled, err
	}
}

// FlexWork converges the laptop grid when laptop-only; a held guard or contended converge restamps (H2).
func FlexWork(profile *config.Profile, driver wm.Driver, env commands.Env) Work {
	return func(ctx context.Context) (Step, error) {
		displays, err := driver.QueryDisplays(ctx)
		if err != nil {
			return Settled, err
		}
		if len(displays) != 1 {
			return Settled, nil
		}
		guard := env.Paths.SignalGuard
		if guard == "" {
			defaults, err := effects.DefaultPaths()
			if err != nil {
				return Settled, fmt.Errorf("resolving default paths: %w", err)
			}
			guard = defaults.SignalGuard
		}
		suppressed, err := effects.SignalsSuppressed(guard, time.Now())
		if err != nil {
			return Settled, err
		}
		if suppressed {
			return Restamp, nil
		}
		result, err := commands.RunFlexConverge(ctx, profile, driver, env)
		if result == commands.LaptopContended {
			return Restamp, err
		}
		return Settled, err
	}
}

// WakeRoutine registers the signals, then runs the startup reclaim cascade.
func WakeRoutine(profile *config.Profile, driver wm.Driver, env commands.Env, sock string) func(context.Context) error {
	return func(ctx context.Context) error {
		if src, ok := driver.(wm.EventSource); ok {
			if err := RegisterSignals(ctx, src, sock); err != nil {
				return err
			}
		}
		_, err := commands.RunDisplayCascade(ctx, profile, driver, env)
		return err
	}
}

// Options configures Run.
type Options struct {
	Socket    string
	Bootstrap bool
	Launchctl Launchctl
	UID       int
	Services  []Service
	Display   Work
	Flex      Work
	Wake      func(context.Context) error
	Nudge     effects.Nudger
}

// Run holds the socket lock for the daemon's life, bootstraps services, and serves until ctx ends.
// A clean shutdown boots out only the services this run bootstrapped, logs a bootout failure, and exits 0.
func Run(ctx context.Context, opts Options) (err error) {
	ln, err := listen(ctx, opts.Socket)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, ln.Close()) }()
	var owned Owned
	if opts.Bootstrap {
		var bootErr error
		owned, bootErr = Bootstrap(ctx, opts.Launchctl, opts.UID, opts.Services)
		if bootErr != nil {
			slog.ErrorContext(ctx, "bootstrapping services", "error", bootErr)
		}
	}
	var hubOpts []HubOption
	if opts.Nudge != nil {
		nudge := opts.Nudge
		hubOpts = append(hubOpts, OnDisplaySettled(func(ctx context.Context) {
			if err := nudge(ctx, displaySettled); err != nil {
				slog.WarnContext(ctx, "nudging sketchybar", "error", err)
			}
		}))
	}
	if err := Serve(ctx, socketOnly{ln}, NewHub(opts.Display, opts.Flex, hubOpts...), opts.Wake); err != nil {
		return err
	}
	if len(owned) == 0 {
		return nil
	}
	bootoutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bootoutDeadline)
	defer cancel()
	if err := Bootout(bootoutCtx, opts.Launchctl, opts.UID, owned); err != nil {
		slog.ErrorContext(ctx, "booting out services", "error", err)
	}
	return nil
}
