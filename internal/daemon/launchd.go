package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec" //nolint:depguard // launchctl is the required subprocess interface.
	"path/filepath"
	"slices"
	"strconv"

	"github.com/samber/oops"
)

const launchctlPath = "/bin/launchctl"

// ErrNotLoaded marks a launchctl call that found no such service loaded.
var ErrNotLoaded = errors.New("service not loaded")

// Launchctl runs one launchctl invocation.
type Launchctl interface {
	Run(ctx context.Context, args ...string) error
}

// ExecLaunchctl runs launchctl at Path, or /bin/launchctl when Path is empty.
type ExecLaunchctl struct {
	Path string
}

// Run executes launchctl with args and returns its exit error, marked ErrNotLoaded when the service is absent.
func (l ExecLaunchctl) Run(ctx context.Context, args ...string) error {
	path := l.Path
	if path == "" {
		path = launchctlPath
	}
	//nolint:gosec // launchctl is fixed by default; tests inject a local stub executable.
	out, err := exec.CommandContext(ctx, path, args...).CombinedOutput()
	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && notLoadedExit(exitErr.ExitCode()) {
			err = errors.Join(ErrNotLoaded, err)
		}
		return oops.With("args", args, "output", string(out)).Wrap(err)
	}
	return nil
}

// notLoadedExit reports launchctl's "no such process" (3) and "could not find service" (113) exits.
func notLoadedExit(code int) bool {
	return code == 3 || code == 113
}

// Service is one launchd agent the daemon brings up.
type Service struct {
	Label string
	Plist string
}

// Owned lists the services this daemon bootstrapped, in bootstrap order.
type Owned []Service

// Candidates lists one tool's launchd labels, current upstream name first.
type Candidates []Service

// DefaultServices returns yabai and skhd, each under its current and pre-rename labels.
// Upstream renamed both to com.asmvik.*; releases before the rename still install com.koekeishiya.*.
func DefaultServices(home string) []Candidates {
	dir := filepath.Join(home, "Library", "LaunchAgents")
	tools := []string{"yabai", "skhd"}
	services := make([]Candidates, 0, len(tools))
	for _, name := range tools {
		var candidates Candidates
		for _, label := range []string{"com.asmvik." + name, "com.koekeishiya." + name} {
			candidates = append(candidates, Service{Label: label, Plist: filepath.Join(dir, label+".plist")})
		}
		services = append(services, candidates)
	}
	return services
}

// installed returns the first candidate whose plist exists, else the current upstream label.
func (c Candidates) installed() (Service, bool) {
	for _, svc := range c {
		if _, err := os.Stat(svc.Plist); err == nil {
			return svc, true
		}
	}
	if len(c) == 0 {
		return Service{}, false
	}
	return c[0], true
}

// Bootstrap follows upstream service_start for each tool and returns the services it loaded.
// A loaded candidate is kickstarted; otherwise the installed plist is bootstrapped.
// A failed bootstrap leaves the service not owned; the joined failures are returned beside Owned.
func Bootstrap(ctx context.Context, lc Launchctl, uid int, tools []Candidates) (Owned, error) {
	domain := "gui/" + strconv.Itoa(uid)
	var owned Owned
	var errs []error
	for _, candidates := range tools {
		loaded := false
		for _, svc := range candidates {
			target := domain + "/" + svc.Label
			if err := lc.Run(ctx, "print", target); err != nil {
				continue
			}
			loaded = true
			if err := lc.Run(ctx, "kickstart", target); err != nil {
				errs = append(errs, oops.With("label", svc.Label).Wrapf(err, "kickstarting service"))
			}
			break
		}
		if loaded {
			continue
		}
		svc, ok := candidates.installed()
		if !ok {
			continue
		}
		target := domain + "/" + svc.Label
		if err := lc.Run(ctx, "enable", target); err != nil {
			slog.WarnContext(ctx, "enabling service failed", "label", svc.Label, "error", err)
		}
		if err := lc.Run(ctx, "bootstrap", domain, svc.Plist); err != nil {
			errs = append(errs, oops.With("label", svc.Label, "plist", svc.Plist).Wrapf(err, "bootstrapping service"))
			continue
		}
		owned = append(owned, svc)
	}
	return owned, errors.Join(errs...)
}

// Bootout unloads only the owned services, last bootstrapped first. A service already gone counts as booted out.
func Bootout(ctx context.Context, lc Launchctl, uid int, owned Owned) error {
	var errs []error
	for _, svc := range slices.Backward(owned) {
		target := fmt.Sprintf("gui/%d/%s", uid, svc.Label)
		if err := lc.Run(ctx, "bootout", target); err != nil && !errors.Is(err, ErrNotLoaded) {
			errs = append(errs, oops.With("label", svc.Label).Wrapf(err, "booting out service"))
		}
	}
	return errors.Join(errs...)
}
