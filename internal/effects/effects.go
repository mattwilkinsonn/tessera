// Package effects contains filesystem and process effects shared by commands and the daemon.
package effects

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gofrs/flock"
)

// The guard TTL, the H7 quiet windows, and the sketchybar binary.
const (
	guardTTL       = 8 * time.Second
	DisplayQuiet   = 3 * time.Second
	FlexQuiet      = 2 * time.Second
	SketchybarPath = "/opt/homebrew/bin/sketchybar"
)

// Paths contains the on-disk locations used by Tessera.
type Paths struct {
	ApplyLock    string
	LaptopLock   string
	SignalGuard  string
	FlexOrder    string
	DaemonSocket string
}

// DefaultPaths returns the paths shared by commands and the daemon.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		cache = filepath.Join(home, ".cache")
	}
	tmpDir := os.TempDir()
	return Paths{
		ApplyLock:    filepath.Join(tmpDir, "tessera-apply.lock"),
		LaptopLock:   filepath.Join(tmpDir, "tessera-laptop.lock"),
		SignalGuard:  filepath.Join("/tmp", "yabai-apply.guard"),
		FlexOrder:    filepath.Join(cache, "yabai", "laptop-flex-order"),
		DaemonSocket: filepath.Join(tmpDir, "tessera-daemon.sock"),
	}, nil
}

// AcquireLock takes a non-blocking exclusive flock. A false result means another holder owns it.
func AcquireLock(path string) (*flock.Flock, bool, error) {
	lock := flock.New(path)
	acquired, err := lock.TryLock()
	if err != nil {
		return nil, false, err
	}
	if !acquired {
		return nil, false, nil
	}
	return lock, true, nil
}

// SuppressSignals stamps the signal guard with the current epoch second.
// It writes only a regular, single-link file the caller owns, since /tmp is shared; it never blocks on a FIFO.
func SuppressSignals(path string) (err error) {
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("opening signal guard: %w", err)
	}
	defer func() {
		if closeErr := syscall.Close(fd); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing signal guard: %w", closeErr))
		}
	}()
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return fmt.Errorf("stat signal guard: %w", err)
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG || st.Nlink != 1 || int64(st.Uid) != int64(os.Getuid()) {
		return fmt.Errorf("signal guard %s is not a regular single-link file owned by this user", path)
	}
	if err := syscall.SetNonblock(fd, false); err != nil {
		return fmt.Errorf("clearing nonblock on signal guard: %w", err)
	}
	if err := syscall.Ftruncate(fd, 0); err != nil {
		return fmt.Errorf("truncating signal guard: %w", err)
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10) + "\n"
	n, err := syscall.Write(fd, []byte(stamp))
	if err != nil {
		return fmt.Errorf("writing signal guard: %w", err)
	}
	if n != len(stamp) {
		return fmt.Errorf("writing signal guard: short write of %d/%d bytes", n, len(stamp))
	}
	return nil
}

// ReleaseSignals removes the signal guard. An absent guard is already released.
func ReleaseSignals(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// SignalsSuppressed reports whether the guard is present and younger than its TTL.
// An expired guard is removed as a side effect.
func SignalsSuppressed(path string, now time.Time) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	stamp, ok := parseStamp(data)
	if !ok {
		return false, nil
	}
	if now.Sub(time.Unix(stamp, 0)) >= guardTTL {
		if err := ReleaseSignals(path); err != nil {
			return false, err
		}
		return false, nil
	}
	return true, nil
}

// ReadFlexOrder reads the persisted order, dropping empty lines.
func ReadFlexOrder(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	order := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			order = append(order, line)
		}
	}
	return order, nil
}

// WriteFlexOrder persists one slug per line and creates its parent directory.
func WriteFlexOrder(order []string, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	var content strings.Builder
	for _, slug := range order {
		content.WriteString(slug)
		content.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(content.String()), 0o600); err != nil {
		return err
	}
	return nil
}

// Nudger triggers a status-bar event. It is deliberately outside wm.Driver.
type Nudger func(ctx context.Context, event string) error

// NudgeSketchybar triggers an event when sketchybar is available. Its failure is best-effort.
func NudgeSketchybar(ctx context.Context, event string) error {
	if err := exec.CommandContext(ctx, SketchybarPath, "--trigger", event).Run(); err != nil {
		slog.DebugContext(ctx, "sketchybar nudge failed", "event", event, "error", err)
	}
	return nil
}

// IsQuiet reports whether the stamp has been quiet for at least d.
func IsQuiet(now, stamp time.Time, d time.Duration) bool {
	return now.Sub(stamp) >= d
}

// IsFlexQuiet requires both the flex and display event windows to be quiet.
func IsFlexQuiet(now, own, display time.Time) bool {
	return IsQuiet(now, own, FlexQuiet) && IsQuiet(now, display, DisplayQuiet)
}

func parseStamp(data []byte) (int64, bool) {
	stamp, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, false
	}
	return stamp, true
}

// WriteStamp records the current epoch second at path.
func WriteStamp(path string) error {
	stamp := strconv.FormatInt(time.Now().Unix(), 10) + "\n"
	if err := os.WriteFile(path, []byte(stamp), 0o600); err != nil {
		return err
	}
	return nil
}

// ReadStamp reads an epoch-second stamp. Missing or malformed content is absent.
func ReadStamp(path string) (int64, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	stamp, ok := parseStamp(data)
	return stamp, ok, nil
}
