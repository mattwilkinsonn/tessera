//go:build windows

package effects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// SuppressSignals stamps the signal guard with the current epoch second.
// Windows has no FIFOs, so the regular-file check protects the target before overwriting it.
func SuppressSignals(path string) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o600) //nolint:gosec // The guard path comes from DefaultPaths, not user input.
	if err != nil {
		return fmt.Errorf("opening signal guard: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing signal guard: %w", closeErr))
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat signal guard: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("signal guard %s is not a regular file", path)
	}
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("truncating signal guard: %w", err)
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10) + "\n"
	n, err := file.WriteString(stamp)
	if err != nil {
		return fmt.Errorf("writing signal guard: %w", err)
	}
	if n != len(stamp) {
		return fmt.Errorf("writing signal guard: short write of %d/%d bytes", n, len(stamp))
	}
	return nil
}

func signalGuardPath(tmpDir string) string {
	return filepath.Join(tmpDir, "tessera-apply.guard")
}
