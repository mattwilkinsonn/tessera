// Package effects contains filesystem and process effects shared by commands and the daemon.
package effects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec" //nolint:depguard // Sketchybar and Darwin temp-dir discovery are intentional subprocess seams.
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/mattwilkinsonn/tessera/internal/engine"
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
	WindowSlots  string
	SpawnLedger  string
	DaemonSocket string
}

func darwinUserTempDir(ctx context.Context) (string, error) {
	output, err := exec.CommandContext(ctx, "/usr/bin/getconf", "DARWIN_USER_TEMP_DIR").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// resolveTempDir prefers the per-user Darwin temp dir: launchd agents and shells
// can carry different $TMPDIR values, and the socket must be the same for both.
func resolveTempDir(goos, fallback string, lookupDarwin func() (string, error)) string {
	if goos != "darwin" {
		return fallback
	}
	dir, err := lookupDarwin()
	if err != nil || dir == "" {
		return fallback
	}
	return filepath.Clean(dir)
}

// DefaultPaths returns the paths shared by commands and the daemon.
func DefaultPaths(ctx context.Context) (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		cache = filepath.Join(home, ".cache")
	}
	tmpDir := resolveTempDir(runtime.GOOS, os.TempDir(), func() (string, error) { return darwinUserTempDir(ctx) })
	return Paths{
		ApplyLock:    filepath.Join(tmpDir, "tessera-apply.lock"),
		LaptopLock:   filepath.Join(tmpDir, "tessera-laptop.lock"),
		SignalGuard:  signalGuardPath(),
		FlexOrder:    filepath.Join(cache, "yabai", "laptop-flex-order"),
		WindowSlots:  filepath.Join(cache, "yabai", "window-slots.json"),
		SpawnLedger:  filepath.Join(cache, "yabai", "spawn-ledger.json"),
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
	//nolint:gosec // The cache path is chosen by the caller.
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
	//nolint:gosec // Flex-order state is stored at the caller-selected config path.
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

// ReadSlots reads cached slot memory, treating absent or corrupt state as empty.
func ReadSlots(path string) (engine.SlotMemory, error) {
	empty := engine.SlotMemory{Slots: make(map[engine.SlotKey]engine.WindowRef)}
	//nolint:gosec // Slot memory uses the caller-selected cache path.
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return empty, nil
		}
		return engine.SlotMemory{}, fmt.Errorf("reading slot memory: %w", err)
	}
	var memory engine.SlotMemory
	if err := json.Unmarshal(data, &memory); err != nil {
		slog.Warn("ignoring corrupt window slot memory", "path", path, "error", err)
		return empty, nil
	}
	if memory.Slots == nil {
		memory.Slots = make(map[engine.SlotKey]engine.WindowRef)
	}
	return memory, nil
}

// LockSlots blocks until it holds the slot-memory flock or ctx ends.
// The caller holds it from reading memory through writing it, so two plans never interleave.
func LockSlots(ctx context.Context, path string) (func(), error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("creating slot memory directory: %w", err)
	}
	lock := flock.New(filepath.Join(dir, "window-slots.lock"))
	if _, err := lock.TryLockContext(ctx, 25*time.Millisecond); err != nil {
		return nil, fmt.Errorf("locking window slot memory: %w", err)
	}
	return func() {
		if err := lock.Unlock(); err != nil {
			slog.Error("unlocking window slot memory", "path", path, "error", err)
		}
	}, nil
}

// WriteSlots atomically replaces cached slot memory after creating its parent.
func WriteSlots(path string, memory engine.SlotMemory) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating slot memory directory: %w", err)
	}
	data, err := json.Marshal(memory)
	if err != nil {
		return fmt.Errorf("encoding slot memory: %w", err)
	}
	file, err := os.CreateTemp(dir, ".window-slots-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary slot memory: %w", err)
	}
	defer func() {
		if err := os.Remove(file.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("removing temporary slot memory", "path", file.Name(), "error", err)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return errors.Join(fmt.Errorf("setting slot memory permissions: %w", err), fmt.Errorf("closing slot memory temporary file: %w", closeErr))
		}
		return fmt.Errorf("setting slot memory permissions: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return errors.Join(fmt.Errorf("writing slot memory: %w", err), fmt.Errorf("closing slot memory temporary file: %w", closeErr))
		}
		return fmt.Errorf("writing slot memory: %w", err)
	}
	if err := file.Sync(); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return errors.Join(fmt.Errorf("syncing slot memory: %w", err), fmt.Errorf("closing slot memory temporary file: %w", closeErr))
		}
		return fmt.Errorf("syncing slot memory: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing slot memory temporary file: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replacing slot memory: %w", err)
	}
	return nil
}

// Nudger triggers a status-bar event. It is deliberately outside wm.Driver.
type Nudger func(ctx context.Context, event string) error

// NudgeSketchybar triggers an event when sketchybar is available. Its failure is best-effort.
func NudgeSketchybar(ctx context.Context, event string) error {
	//nolint:gosec // This executes the fixed sketchybar binary with one event argument.
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
	//nolint:gosec // Signal stamps use the caller-selected state path.
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
