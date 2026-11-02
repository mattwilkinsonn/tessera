//go:build unix

package effects

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSuppressSignalsRejectsUnsafeFiles(t *testing.T) {
	t.Run("a symlink is refused without changing its target", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		if err := os.WriteFile(target, []byte("keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "g")
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if err := SuppressSignals(path); err == nil {
			t.Fatal("SuppressSignals() through a symlink = nil; want an error")
		}
		got, err := os.ReadFile(target)
		if err != nil || string(got) != "keep\n" {
			t.Fatalf("symlink target = %q, %v; want %q untouched", got, err, "keep\n")
		}
	})
	t.Run("a hard link is refused without changing its victim", func(t *testing.T) {
		dir := t.TempDir()
		victim := filepath.Join(dir, "victim")
		if err := os.WriteFile(victim, []byte("keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "g")
		if err := os.Link(victim, path); err != nil {
			t.Fatal(err)
		}
		if err := SuppressSignals(path); err == nil {
			t.Fatal("SuppressSignals() through a hard link = nil; want an error")
		}
		got, err := os.ReadFile(victim)
		if err != nil || string(got) != "keep\n" {
			t.Fatalf("hard-link victim = %q, %v; want %q untouched", got, err, "keep\n")
		}
	})
	t.Run("a FIFO is refused without blocking", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "g")
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- SuppressSignals(path) }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("SuppressSignals() on a FIFO = nil; want an error")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("SuppressSignals() on a FIFO with no reader blocked")
		}
	})
}
