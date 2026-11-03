package spawn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

func TestRun(t *testing.T) {
	t.Run("reports stderr from an early launcher exit", func(t *testing.T) {
		assert.ErrorContains(t, Run(t.Context(), spawnHelperCommand(t, "stderr"), time.Second), "boom")
	})
	t.Run("reports the exit code and argv when stderr is empty", func(t *testing.T) {
		argv := spawnHelperCommand(t, "exit")
		err := Run(t.Context(), argv, time.Second)
		assert.ErrorContains(t, err, "spawning window failed (exit 1): "+strings.Join(argv, " "))
	})
	t.Run("returns after caller cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err := Run(ctx, spawnHelperCommand(t, "wait"), time.Second)
		assert.Assert(t, errors.Is(err, context.Canceled))
	})
	t.Run("returns after the caller deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
		defer cancel()
		err := Run(ctx, spawnHelperCommand(t, "wait"), time.Second)
		assert.Assert(t, errors.Is(err, context.DeadlineExceeded))
	})
	t.Run("rejects an empty argv", func(t *testing.T) {
		assert.ErrorContains(t, Run(t.Context(), nil, time.Second), "empty argv")
	})
	t.Run("a launcher outliving the wait survives caller cancellation", func(t *testing.T) {
		dir := t.TempDir()
		gate, out := filepath.Join(dir, "gate"), filepath.Join(dir, "out")
		t.Setenv("TESS_SPAWN_GATE", gate)
		t.Setenv("TESS_SPAWN_OUT", out)
		ctx, cancel := context.WithCancel(t.Context())
		assert.NilError(t, Run(ctx, spawnHelperCommand(t, "linger"), 0))
		cancel()
		assert.NilError(t, os.WriteFile(gate, nil, 0o600))
		got, err := waitForFile(out)
		assert.NilError(t, err, "launcher did not survive caller cancellation")
		assert.Equal(t, string(got), "alive\n")
	})
}

func spawnHelperCommand(t *testing.T, mode string) []string {
	t.Helper()
	t.Setenv("TESS_SPAWN_HELPER_MODE", mode)
	return []string{os.Args[0], "-test.run=^TestSpawnHelperProcess$"}
}

// waitForFile polls for a file the helper writes; the helper is another process, so no event exists.
func waitForFile(path string) ([]byte, error) {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		content, err := os.ReadFile(path)
		if err == nil {
			return content, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			return nil, fmt.Errorf("%s never appeared", path)
		}
	}
}

func TestSpawnHelperProcess(t *testing.T) {
	switch os.Getenv("TESS_SPAWN_HELPER_MODE") {
	case "stderr":
		if _, err := fmt.Fprintln(os.Stderr, "boom"); err != nil {
			t.Fatal(err)
		}
		os.Exit(1)
	case "exit":
		os.Exit(1)
	case "wait":
		<-time.NewTimer(time.Hour).C
	case "linger":
		if _, err := waitForFile(os.Getenv("TESS_SPAWN_GATE")); err != nil {
			os.Exit(2)
		}
		// Write then rename, so the parent never reads a partial file.
		out := os.Getenv("TESS_SPAWN_OUT")
		if err := os.WriteFile(out+".tmp", []byte("alive\n"), 0o600); err != nil {
			os.Exit(2)
		}
		if err := os.Rename(out+".tmp", out); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
}
