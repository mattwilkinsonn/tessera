package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/samber/oops"
)

const launchctlPath = "/bin/launchctl"

// Launchctl runs one launchctl invocation.
type Launchctl interface {
	Run(ctx context.Context, args ...string) error
}

// ExecLaunchctl runs /bin/launchctl.
type ExecLaunchctl struct{}

// Run executes launchctl with args and returns its exit error.
func (ExecLaunchctl) Run(ctx context.Context, args ...string) error {
	out, err := exec.CommandContext(ctx, launchctlPath, args...).CombinedOutput()
	if err != nil {
		return oops.With("args", args, "output", string(out)).Wrap(err)
	}
	return nil
}

// Service is one launchd agent the daemon brings up.
type Service struct {
	Label string
	Plist string
}

// Owned lists the services this daemon bootstrapped, in bootstrap order.
type Owned []Service

// DefaultServices returns yabai and skhd under their upstream labels.
func DefaultServices(home string) []Service {
	labels := []string{"com.asmvik.yabai", "com.asmvik.skhd"}
	services := make([]Service, 0, len(labels))
	for _, label := range labels {
		services = append(services, Service{Label: label, Plist: filepath.Join(home, "Library", "LaunchAgents", label+".plist")})
	}
	return services
}

// Bootstrap follows upstream service_start for each service and returns the ones it loaded.
// A failed bootstrap leaves the service not owned; the joined failures are returned beside Owned.
func Bootstrap(ctx context.Context, lc Launchctl, uid int, svcs []Service) (Owned, error) {
	domain := "gui/" + strconv.Itoa(uid)
	var owned Owned
	var errs []error
	for _, svc := range svcs {
		target := domain + "/" + svc.Label
		if err := lc.Run(ctx, "print", target); err == nil {
			if err := lc.Run(ctx, "kickstart", target); err != nil {
				errs = append(errs, oops.With("label", svc.Label).Wrapf(err, "kickstarting service"))
			}
			continue
		}
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

// Bootout unloads only the owned services, last bootstrapped first.
func Bootout(ctx context.Context, lc Launchctl, uid int, owned Owned) error {
	var errs []error
	for _, svc := range slices.Backward(owned) {
		target := fmt.Sprintf("gui/%d/%s", uid, svc.Label)
		if err := lc.Run(ctx, "bootout", target); err != nil {
			errs = append(errs, oops.With("label", svc.Label).Wrapf(err, "booting out service"))
		}
	}
	return errors.Join(errs...)
}
