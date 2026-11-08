package effects

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWriteSpawnsPrunesLedger(t *testing.T) {
	now := time.Now()
	latest := now.Add(-time.Minute)
	ledger := SpawnLedger{"expired": {now.Add(-10 * time.Minute)}, "empty": {}, "active": {now.Add(-2 * time.Minute), latest, now.Add(-3 * time.Minute)}}
	path := filepath.Join(t.TempDir(), "spawn-ledger.json")
	if err := WriteSpawns(path, ledger); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSpawns(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got["active"]) != 1 || !got["active"][0].Equal(latest) {
		t.Fatalf("ledger = %v", got)
	}
}
