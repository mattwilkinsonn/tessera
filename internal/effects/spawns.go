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
type SpawnLedger map[string]SpawnRecord

type SpawnRecord struct {
	At       time.Time `json:"at"`
	AppCount int       `json:"appCount"`
	Observed bool      `json:"observed"`
}

func (record *SpawnRecord) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '[' {
		*record = SpawnRecord{}
		return nil
	}
	type entry SpawnRecord
	return json.Unmarshal(data, (*entry)(record))
}

const SpawnCooldown = 5 * time.Minute

const SpawnRecordTTL = 24 * time.Hour

func PruneSpawns(ledger SpawnLedger, now time.Time) {
	for name, record := range ledger {
		if record.At.IsZero() || now.Sub(record.At) >= SpawnRecordTTL {
			delete(ledger, name)
		}
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
	return WriteSpawnsAt(path, ledger, time.Now())
}

func WriteSpawnsAt(path string, ledger SpawnLedger, now time.Time) error {
	PruneSpawns(ledger, now)
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
