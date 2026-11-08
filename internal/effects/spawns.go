package effects

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SpawnLedger is accessed only while holding the window-slots lock.
type SpawnLedger map[string][]time.Time

const SpawnCooldown = 5 * time.Minute

func PruneSpawns(ledger SpawnLedger, now time.Time) {
	for name, times := range ledger {
		var latest time.Time
		for _, at := range times {
			if at.After(latest) {
				latest = at
			}
		}
		if latest.IsZero() || now.Sub(latest) >= SpawnCooldown {
			delete(ledger, name)
			continue
		}
		ledger[name] = []time.Time{latest}
	}
}

func ReadSpawns(path string) (SpawnLedger, error) {
	ledger := make(SpawnLedger)
	data, err := os.ReadFile(path) //nolint:gosec // Caller-selected state path.
	if errors.Is(err, os.ErrNotExist) {
		return ledger, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading spawn ledger: %w", err)
	}
	if err := json.Unmarshal(data, &ledger); err != nil {
		return nil, fmt.Errorf("decoding spawn ledger: %w", err)
	}
	if ledger == nil {
		ledger = make(SpawnLedger)
	}
	return ledger, nil
}

func WriteSpawns(path string, ledger SpawnLedger) error {
	PruneSpawns(ledger, time.Now())
	data, err := json.Marshal(ledger)
	if err != nil {
		return fmt.Errorf("encoding spawn ledger: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".spawn-ledger-*.tmp")
	if err != nil {
		return fmt.Errorf("creating spawn ledger: %w", err)
	}
	defer os.Remove(file.Name()) //nolint:errcheck // Rename normally removes the temporary name.
	if _, err := file.Write(data); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing spawn ledger: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replacing spawn ledger: %w", err)
	}
	return nil
}
