//go:build unix

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

func TestAcquireLockAfterProcessDeath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "child.lock")
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestLockHelperProcess$")
	cmd.Env = append(os.Environ(), "TESSERA_LOCK_HELPER=1", "TESSERA_LOCK_PATH="+path)
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
