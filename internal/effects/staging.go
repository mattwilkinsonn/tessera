package effects

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mattwilkinsonn/tessera/internal/engine"
)

// ReadStagedWindows reads IDs retained across interrupted layout plans.
func ReadStagedWindows(path string) (map[int]engine.StagedWindow, error) {
	staged := make(map[int]engine.StagedWindow)
	//nolint:gosec // The cache path is selected by the caller.
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return staged, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading staged windows: %w", err)
	}
	if err := json.Unmarshal(data, &staged); err != nil {
		slog.Warn("ignoring corrupt staged windows", "path", path, "error", err)
		return make(map[int]engine.StagedWindow), nil
	}
	return staged, nil
}

// WriteStagedWindows atomically replaces the staging ledger.
func WriteStagedWindows(path string, staged map[int]engine.StagedWindow) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating staged windows directory: %w", err)
	}
	data, err := json.Marshal(staged)
	if err != nil {
		return fmt.Errorf("encoding staged windows: %w", err)
	}
	file, err := os.CreateTemp(dir, ".staged-windows-*.tmp")
	if err != nil {
		return fmt.Errorf("creating staged windows temporary file: %w", err)
	}
	defer func() {
		if err := os.Remove(file.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("removing staged windows temporary file", "path", file.Name(), "error", err)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return errors.Join(fmt.Errorf("writing staged windows: %w", err), file.Close())
	}
	if err := file.Sync(); err != nil {
		return errors.Join(fmt.Errorf("syncing staged windows: %w", err), file.Close())
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing staged windows: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replacing staged windows: %w", err)
	}
	return nil
}
