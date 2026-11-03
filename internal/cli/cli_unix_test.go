//go:build unix

package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/effects"
	"gotest.tools/v3/assert"
)

func TestSIGTERMDuringStubBlockedApplyExits130AndLeavesLockFree(t *testing.T) {
	root := cliTestRoot(t, "tess-term-")
	defer func() { assert.NilError(t, os.RemoveAll(root)) }()
	binary := filepath.Join(root, "tess")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../../cmd/tess")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building tess: %v\n%s", err, output)
	}
	started := filepath.Join(root, "started")
	stub := filepath.Join(root, "blocked-yabai")
	stubCode := []byte("#!/bin/sh\n: > \"$TESS_BLOCK_STARTED\"\nexec /usr/bin/yes\n")
	if err := os.WriteFile(stub, stubCode, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, "tessera-apply.lock")
	command := exec.CommandContext(t.Context(), binary, "apply")
	command.Env = append(os.Environ(), "HOME="+root, "TMPDIR="+root, "XDG_CACHE_HOME="+root, "XDG_CONFIG_HOME="+root, "TESSERA_PROFILE=", "TESS_YABAI="+stub, "TESS_BLOCK_STARTED="+started)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	startedTicker := time.NewTicker(10 * time.Millisecond)
	defer startedTicker.Stop()
	startedTimeout := time.NewTimer(15 * time.Second)
	defer startedTimeout.Stop()
	for {
		if _, err := os.Stat(started); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			if killErr := command.Process.Kill(); killErr != nil {
				t.Errorf("killing tess after start marker error: %v", killErr)
			}
			if _, waitErr := command.Process.Wait(); waitErr != nil {
				t.Errorf("waiting for tess after start marker error: %v", waitErr)
			}
			t.Fatal(err)
		}
		select {
		case <-startedTicker.C:
		case <-startedTimeout.C:
			if killErr := command.Process.Kill(); killErr != nil {
				t.Errorf("killing tess after start timeout: %v", killErr)
			}
			if _, waitErr := command.Process.Wait(); waitErr != nil {
				t.Errorf("waiting for tess after start timeout: %v", waitErr)
			}
			t.Fatal("stub invocation was not observed")
		}
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		if killErr := command.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			t.Errorf("killing tess after SIGTERM failure: %v", killErr)
		}
		if waitErr := command.Wait(); waitErr != nil {
			if _, ok := errors.AsType[*exec.ExitError](waitErr); !ok {
				t.Errorf("waiting for tess after SIGTERM failure: %v", waitErr)
			}
		}
		t.Fatal(err)
	}
	waitErr := command.Wait()
	exitErr, ok := errors.AsType[*exec.ExitError](waitErr)
	if !ok || exitErr.ExitCode() != 130 {
		t.Fatalf("tess exit = %v; want 130", waitErr)
	}
	lock, acquired, err := effects.AcquireLock(lockPath)
	if err != nil || !acquired {
		t.Fatalf("apply lock after SIGTERM = %t, %v", acquired, err)
	}
	assert.NilError(t, lock.Unlock())
}

func TestSecondSIGTERMDuringShutdownKillsByDefault(t *testing.T) {
	root := cliTestRoot(t, "tess-term2-")
	defer func() { assert.NilError(t, os.RemoveAll(root)) }()
	binary := filepath.Join(root, "tess")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../../cmd/tess")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building tess: %v\n%s", err, output)
	}
	orphanPID := filepath.Join(root, "orphan.pid")
	stub := filepath.Join(root, "lingering-yabai")
	stubCode := []byte("#!/bin/sh\n/bin/sleep 600 &\necho $! > \"$TESS_ORPHAN_PID\"\nexec /bin/sleep 600\n")
	if err := os.WriteFile(stub, stubCode, 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), binary, "apply")
	command.Env = append(os.Environ(), "HOME="+root, "TMPDIR="+root, "XDG_CACHE_HOME="+root, "XDG_CONFIG_HOME="+root, "TESSERA_PROFILE=", "TESS_YABAI="+stub, "TESS_ORPHAN_PID="+orphanPID)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var pid []byte
	for len(pid) == 0 {
		<-ticker.C
		data, err := os.ReadFile(orphanPID)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		pid = data
	}
	orphan, err := strconv.Atoi(strings.TrimSpace(string(pid)))
	assert.NilError(t, err)
	defer func() {
		if err := syscall.Kill(orphan, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Error(err)
		}
	}()
	var waitErr error
	for done := false; !done; {
		select {
		case waitErr = <-waited:
			done = true
		case <-ticker.C:
			if err := command.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Fatal(err)
			}
		}
	}
	exitErr, ok := errors.AsType[*exec.ExitError](waitErr)
	assert.Assert(t, ok, "tess exit = %v", waitErr)
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	assert.Assert(t, ok && status.Signaled() && status.Signal() == syscall.SIGTERM, "tess exit = %v; want killed by SIGTERM", waitErr)
}
