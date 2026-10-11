package daemon

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"

	"gotest.tools/v3/assert"
)

// shortDir returns a temp dir under /tmp, which keeps socket paths under the macOS limit.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tessd")
	assert.NilError(t, err)
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}

func staleSocket(t *testing.T, path string) {
	t.Helper()
	ln, err := net.Listen("unix", path)
	assert.NilError(t, err)
	unix, ok := ln.(*net.UnixListener)
	assert.Assert(t, ok)
	unix.SetUnlinkOnClose(false)
	assert.NilError(t, ln.Close())
	_, err = os.Stat(path)
	assert.NilError(t, err)
}

func TestListen(t *testing.T) {
	t.Run("two concurrent starters on a stale socket file: one listener, one ErrRunning", func(t *testing.T) {
		path := filepath.Join(shortDir(t), "d.sock")
		staleSocket(t, path)
		start := make(chan struct{})
		var wg sync.WaitGroup
		lns := make([]net.Listener, 2)
		errs := make([]error, 2)
		for i := range 2 {
			wg.Go(func() {
				<-start
				lns[i], errs[i] = Listen(t.Context(), path)
			})
		}
		close(start)
		wg.Wait()
		winner := slices.IndexFunc(errs, func(err error) bool { return err == nil })
		assert.Assert(t, winner >= 0, "errors %v", errs)
		loser := 1 - winner
		assert.Assert(t, errors.Is(errs[loser], ErrRunning), "loser error %v", errs[loser])
		assert.Assert(t, lns[loser] == nil)

		conn, err := net.Dial("unix", path)
		assert.NilError(t, err)
		assert.NilError(t, conn.Close())
		assert.NilError(t, lns[winner].Close())
		_, err = os.Stat(path)
		assert.Assert(t, errors.Is(err, os.ErrNotExist), "socket left after Close: %v", err)

		again, err := Listen(t.Context(), path)
		assert.NilError(t, err)
		assert.NilError(t, again.Close())
	})

	t.Run("Serve ends on cancel and releases the socket and the lock", func(t *testing.T) {
		path := filepath.Join(shortDir(t), "d.sock")
		ln, err := Listen(t.Context(), path)
		assert.NilError(t, err)
		ctx, cancel := context.WithCancel(t.Context())
		woke := make(chan struct{}, 2)
		done := startServe(ctx, ln, NewHub(idle, idle), func(context.Context) error {
			woke <- struct{}{}
			return nil
		})
		<-woke
		conn, err := net.Dial("unix", path)
		assert.NilError(t, err)
		_, err = io.WriteString(conn, "wake\n")
		assert.NilError(t, err)
		_, err = io.ReadAll(conn)
		assert.NilError(t, err)
		assert.NilError(t, conn.Close())
		<-woke
		cancel()
		assert.NilError(t, (<-done).err)
		_, err = os.Stat(path)
		assert.Assert(t, errors.Is(err, os.ErrNotExist))
		next, err := Listen(t.Context(), path)
		assert.NilError(t, err)
		assert.NilError(t, next.Close())
	})

	t.Run("a kill -9'd holder's lock frees, and the next Listen binds", func(t *testing.T) {
		path := filepath.Join(shortDir(t), "d.sock")
		cmd := exec.Command(os.Args[0], "-test.run=^TestListenHelperProcess$")
		cmd.Env = append(os.Environ(), "TESSERA_LISTEN_HELPER=1", "TESSERA_LISTEN_PATH="+path)
		_, err := cmd.StdinPipe()
		assert.NilError(t, err)
		stdout, err := cmd.StdoutPipe()
		assert.NilError(t, err)
		assert.NilError(t, cmd.Start())
		ready, err := bufio.NewReader(stdout).ReadString('\n')
		if err != nil || strings.TrimSpace(ready) != "listening" {
			killErr := cmd.Process.Kill()
			t.Fatalf("helper readiness = %q, %v (kill %v, wait %v)", ready, err, killErr, cmd.Wait())
		}
		_, err = Listen(t.Context(), path)
		assert.Assert(t, errors.Is(err, ErrRunning), "Listen() while held = %v", err)

		assert.NilError(t, cmd.Process.Signal(syscall.SIGKILL))
		assert.Assert(t, cmd.Wait() != nil)
		_, err = os.Stat(path)
		assert.NilError(t, err, "a SIGKILLed holder leaves its socket file")

		ln, err := Listen(t.Context(), path)
		assert.NilError(t, err)
		conn, err := net.Dial("unix", path)
		assert.NilError(t, err)
		assert.NilError(t, conn.Close())
		assert.NilError(t, ln.Close())
	})
}

func TestListenHelperProcess(t *testing.T) {
	if os.Getenv("TESSERA_LISTEN_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	ln, err := Listen(t.Context(), os.Getenv("TESSERA_LISTEN_PATH"))
	assert.NilError(t, err)
	_, err = io.WriteString(os.Stdout, "listening\n")
	assert.NilError(t, err)
	_, err = os.Stdin.Read(make([]byte, 1))
	assert.NilError(t, err)
	assert.NilError(t, ln.Close())
}
