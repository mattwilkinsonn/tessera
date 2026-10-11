package effects

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestAcquireLock(t *testing.T) {
	t.Run("first caller acquires; release frees the dir", func(t *testing.T) {
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
	t.Run("a LIVE holder (our own pid) is genuine contention → null", func(t *testing.T) {
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
	t.Run("a SIGKILLed child's lock is free at once, with no reclaim step", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "child.lock")
		cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelperProcess$")
		cmd.Env = append(os.Environ(), "TESSERA_LOCK_HELPER=1", "TESSERA_LOCK_PATH="+path)
		_, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		ready, err := bufio.NewReader(stdout).ReadString('\n')
		if err != nil || strings.TrimSpace(ready) != "locked" {
			if killErr := cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
				t.Errorf("killing child after readiness failure: %v", killErr)
			}
			if waitErr := cmd.Wait(); waitErr != nil {
				t.Errorf("waiting for child after readiness failure: %v", waitErr)
			}
			t.Fatalf("child readiness = %q, %v; want locked", ready, err)
		}
		if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Wait(); err == nil {
			t.Fatal("child exited without SIGKILL")
		}
		lock, acquired, err := AcquireLock(path)
		if err != nil || !acquired {
			t.Fatalf("AcquireLock() after SIGKILL = %t, %v; want true, nil", acquired, err)
		}
		if err := lock.Unlock(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestLockHelperProcess(t *testing.T) {
	if os.Getenv("TESSERA_LOCK_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	path := os.Getenv("TESSERA_LOCK_PATH")
	lock, acquired, err := AcquireLock(path)
	if err != nil || !acquired {
		t.Fatalf("AcquireLock() = %t, %v; want true, nil", acquired, err)
	}
	if _, err := fmt.Fprintln(os.Stdout, "locked"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stdin.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
}
