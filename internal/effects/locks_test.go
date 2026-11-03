package effects

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireLockLifecycle(t *testing.T) {
	t.Run("first caller acquires and release leaves the lock file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "l.lock")
		lock, acquired, err := AcquireLock(path)
		if err != nil || !acquired {
			t.Fatalf("AcquireLock() = %t, %v; want true, nil", acquired, err)
		}
		if err := lock.Unlock(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("flock file was removed on unlock: %v", err)
		}
		reacquired, ok, err := AcquireLock(path)
		if err != nil || !ok {
			t.Fatalf("AcquireLock() after unlock = %t, %v; want true, nil", ok, err)
		}
		if err := reacquired.Unlock(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("release is idempotent", func(t *testing.T) {
		lock, acquired, err := AcquireLock(filepath.Join(t.TempDir(), "l.lock"))
		if err != nil || !acquired {
			t.Fatalf("AcquireLock() = %t, %v; want true, nil", acquired, err)
		}
		if err := lock.Unlock(); err != nil {
			t.Fatal(err)
		}
		if err := lock.Unlock(); err != nil {
			t.Fatalf("second Unlock() = %v; want nil", err)
		}
	})
}

func TestAcquireLockContention(t *testing.T) {
	t.Run("a live holder is genuine contention", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "l.lock")
		lock, acquired, err := AcquireLock(path)
		if err != nil || !acquired {
			t.Fatalf("first AcquireLock() = %t, %v; want true, nil", acquired, err)
		}
		defer func() {
			if err := lock.Unlock(); err != nil {
				t.Error(err)
			}
		}()
		second, got, err := AcquireLock(path)
		if err != nil || got || second != nil {
			t.Fatalf("second AcquireLock() = %v, %t, %v; want nil, false, nil", second, got, err)
		}
	})

	t.Run("acquires when free", func(t *testing.T) {
		lock, acquired, err := AcquireLock(filepath.Join(t.TempDir(), "a.lock"))
		if err != nil || !acquired {
			t.Fatalf("AcquireLock() = %t, %v; want true, nil", acquired, err)
		}
		if err := lock.Unlock(); err != nil {
			t.Fatal(err)
		}
	})
}
