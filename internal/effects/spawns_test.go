package effects

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteSpawnsPrunesLedger(t *testing.T) {
	now := time.Now()
	latest := now.Add(-time.Minute)
	ledger := SpawnLedger{"expired": {At: now.Add(-25 * time.Hour)}, "empty": {}, "active": {At: latest, AppCount: 3, Observed: true}}
	path := filepath.Join(t.TempDir(), "spawn-ledger.json")
	if err := WriteSpawnsAt(path, ledger, now); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSpawns(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got["active"].At.Equal(latest) || got["active"].AppCount != 3 {
		t.Fatalf("ledger = %v", got)
	}
}

func TestReadSpawnsExpiresOldFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spawn-ledger.json")
	if err := os.WriteFile(path, []byte(`{"ghostty-wave":["2026-11-07T00:00:00Z"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	ledger, err := ReadSpawns(path)
	if err != nil {
		t.Fatal(err)
	}
	PruneSpawns(ledger, time.Now())
	if len(ledger) != 0 {
		t.Fatalf("old records survive: %v", ledger)
	}
}
