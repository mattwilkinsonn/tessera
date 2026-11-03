package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/commands"
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

const (
	ncPath                   = "/usr/bin/nc"
	signalPrefix             = "tessera-"
	barEvent                 = "yabai_spaces_changed"
	displaySettled           = "display_relatch"
	bootoutDeadline          = 5 * time.Second
	startupReadinessTimeout  = 60 * time.Second
	startupReadinessInterval = 250 * time.Millisecond
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
			defaults, err := effects.DefaultPaths(ctx)
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

// StartupReadiness waits for yabai to answer a display query before the daemon's
// first wake. At login launchd can start the daemon before yabai serves queries.
func StartupReadiness(driver wm.Driver) func(context.Context) error {
	return func(ctx context.Context) error {
		return waitForYabai(ctx, func(ctx context.Context) error {
			_, err := driver.QueryDisplays(ctx)
			return err
		}, startupReadinessTimeout)
	}
}

// waitForYabai polls query until it succeeds or timeout passes. The bound holds
// only if query honours ctx, as the yabai driver's exec.CommandContext does.
func waitForYabai(ctx context.Context, query func(context.Context) error, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(startupReadinessInterval)
	defer ticker.Stop()
	for {
		err := query(ctx)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("yabai not ready: %w", errors.Join(err, ctx.Err()))
		case <-ticker.C:
		}
	}
}

// Options configures Run.
type Options struct {
	Socket     string
	Bootstrap  bool
	Launchctl  Launchctl
	UID        int
	Services   []Candidates
	Display    Work
	Flex       Work
	Wake       func(context.Context) error
	WakeReady  func(context.Context) error
	Nudge      effects.Nudger
	Events     wm.EventStream
	Supervisor wm.ProcessSupervisor
}

// Run holds the socket lock for the daemon's life, bootstraps services, and serves until ctx ends.
// A clean shutdown boots out only the services this run bootstrapped, logs a bootout failure, and exits 0.
func Run(ctx context.Context, opts Options) error {
	ln, err := listen(ctx, opts.Socket)
	if err != nil {
		return err
	}
	return run(ctx, opts, ln, ln.closeSocket)
}

func run(ctx context.Context, opts Options, ln net.Listener, closeSocket func() error) (err error) {
	defer func() { err = errors.Join(err, ln.Close()) }()
	var owned Owned
	var processOwned bool
	var exited <-chan error
	supervised := opts.Bootstrap && opts.Supervisor != nil
	if supervised {
		defer func() {
			if !processOwned {
				return
			}
			stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bootoutDeadline)
			defer cancel()
			if stopErr := opts.Supervisor.Stop(stopCtx); stopErr != nil {
				err = errors.Join(err, fmt.Errorf("stopping window manager: %w", stopErr))
			}
		}()
		var startErr error
		processOwned, exited, startErr = opts.Supervisor.Start(ctx)
		if startErr != nil {
			return fmt.Errorf("starting window manager: %w", startErr)
		}
		if !processOwned {
			exited = nil
		}
	} else if opts.Bootstrap {
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
	h := NewHub(opts.Display, opts.Flex, hubOpts...)
	wakes := Wakes{Run: opts.Wake, Ready: opts.WakeReady}
	if opts.Events == nil && !supervised {
		err = Serve(ctx, socketOnly{Listener: ln, closeFn: closeSocket}, h, wakes)
	} else {
		processOwned, err = serveWithEvents(ctx, opts.Events, exited, opts.Supervisor, processOwned, ln, closeSocket, h, wakes)
	}
	if err != nil {
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

func serveWithEvents(
	ctx context.Context,
	eventStream wm.EventStream,
	exited <-chan error,
	supervisor wm.ProcessSupervisor,
	processOwned bool,
	ln net.Listener,
	closeSocket func() error,
	h *Hub,
	wakes Wakes,
) (bool, error) {
	var events <-chan wm.Event
	if eventStream != nil {
		var err error
		events, err = eventStream.Events(ctx)
		if err != nil {
			return processOwned, fmt.Errorf("subscribing to window manager events: %w", err)
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	forwardDone := make(chan struct{})
	if eventStream != nil {
		go func() {
			defer close(forwardDone)
			forwardEvents(runCtx, events, h)
		}()
	} else {
		close(forwardDone)
	}
	defer func() {
		cancel()
		<-forwardDone
	}()
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- Serve(runCtx, socketOnly{Listener: ln, closeFn: closeSocket}, h, wakes)
	}()
	ctxDone := runCtx.Done()
	for {
		select {
		case serveErr := <-serveDone:
			return processOwned, serveErr
		case <-ctxDone:
			return processOwned, <-serveDone
		case exitErr, ok := <-exited:
			exited = nil
			if !ok || exitErr == nil {
				processOwned = false
				continue
			}
			processOwned = false
			if runCtx.Err() != nil {
				continue
			}
			started, nextExit, startErr := supervisor.Start(runCtx)
			processOwned = started
			if startErr != nil {
				cancel()
				serveErr := <-serveDone
				return started, errors.Join(fmt.Errorf("restarting window manager: %w", startErr), serveErr)
			}
			if !started {
				nextExit = nil
			}
			exited = nextExit
		}
	}
}

func forwardEvents(ctx context.Context, events <-chan wm.Event, h *Hub) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			h.Notify(ctx, string(ev))
		}
	}
}
