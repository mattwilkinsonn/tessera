//go:build windows

package effects

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSuppressSignalsRejectsDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guard")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SuppressSignals(path); err == nil {
		t.Fatal("SuppressSignals() on a directory = nil; want an error")
	}
}

func TestSignalsSuppressedReadsFreshStamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guard")
	if err := SuppressSignals(path); err != nil {
		t.Fatal(err)
	}
	got, err := SignalsSuppressed(path, time.Now())
	if err != nil || !got {
		t.Fatalf("SignalsSuppressed() = %t, %v; want true, nil", got, err)
	}
}
