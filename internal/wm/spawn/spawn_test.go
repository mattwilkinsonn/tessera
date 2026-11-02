package spawn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

func TestRun(t *testing.T) {
	t.Run("runs argv directly and reports non-zero exit", func(t *testing.T) {
		assert.ErrorContains(t, Run(t.Context(), spawnHelperCommand(t, "stderr"), time.Second), "boom")
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
}

func spawnHelperCommand(t *testing.T, mode string) []string {
	t.Helper()
	t.Setenv("TESS_SPAWN_HELPER_MODE", mode)
	return []string{os.Args[0], "-test.run=^TestSpawnHelperProcess$"}
}

func TestSpawnHelperProcess(t *testing.T) {
	switch os.Getenv("TESS_SPAWN_HELPER_MODE") {
	case "stderr":
		if _, err := fmt.Fprintln(os.Stderr, "boom"); err != nil {
			t.Fatal(err)
		}
		os.Exit(1)
	case "wait":
		<-time.NewTimer(time.Hour).C
	}
}
