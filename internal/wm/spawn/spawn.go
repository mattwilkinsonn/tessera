// Package spawn runs window launchers without tying their lifetime to the window manager.
package spawn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec" //nolint:depguard // Running configured window launchers is the package's purpose.
	"strings"
	"time"
)

// Run starts argv and waits up to wait before leaving a still-running launcher detached.
func Run(ctx context.Context, argv []string, wait time.Duration) error {
	if len(argv) == 0 {
		return errors.New("spawning window: empty argv")
	}
	// A file, not a pipe: a lingering launcher must not block or SIGPIPE on stderr.
	stderr, err := os.CreateTemp("", "tess-spawn-*.err")
	if err != nil {
		return fmt.Errorf("creating spawn stderr file: %w", err)
	}
	//nolint:noctx,gosec // Explicit launcher argv may outlive the caller's wait.
	command := exec.Command(argv[0], argv[1:]...)
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return errors.Join(fmt.Errorf("spawning window %s: %w", argv[0], err), discardSpawnStderr(stderr))
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case waitErr := <-done:
		return spawnResult(argv, stderr, waitErr)
	case <-ctx.Done():
		killErr := command.Process.Kill()
		if errors.Is(killErr, os.ErrProcessDone) {
			killErr = nil
		}
		waitErr := <-done
		return errors.Join(fmt.Errorf("spawning window: %w", ctx.Err()), killErr, waitErr, discardSpawnStderr(stderr))
	case <-timer.C:
		go func() {
			if waitErr := <-done; waitErr != nil {
				slog.Debug("detached launcher exited", "error", waitErr)
			}
			if err := discardSpawnStderr(stderr); err != nil {
				slog.Warn("cleaning up after a detached launcher", "error", err)
			}
		}()
		return nil
	}
}

// spawnResult turns a finished launcher's exit into an error carrying its stderr.
func spawnResult(argv []string, stderr *os.File, waitErr error) error {
	content, readErr := os.ReadFile(stderr.Name())
	if cleanupErr := discardSpawnStderr(stderr); cleanupErr != nil {
		return errors.Join(waitErr, cleanupErr)
	}
	if waitErr == nil {
		return nil
	}
	exitError, ok := errors.AsType[*exec.ExitError](waitErr)
	if !ok {
		return fmt.Errorf("spawning window %s: %w", argv[0], waitErr)
	}
	if readErr != nil {
		return fmt.Errorf("reading spawn stderr: %w", readErr)
	}
	if message := strings.TrimSpace(string(content)); message != "" {
		return fmt.Errorf("spawning window failed (exit %d): %s: %s", exitError.ExitCode(), strings.Join(argv, " "), message)
	}
	return fmt.Errorf("spawning window failed (exit %d): %s", exitError.ExitCode(), strings.Join(argv, " "))
}

func discardSpawnStderr(stderr *os.File) error {
	closeErr := stderr.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("closing spawn stderr file: %w", closeErr)
	}
	removeErr := os.Remove(stderr.Name())
	if removeErr != nil {
		removeErr = fmt.Errorf("removing spawn stderr file: %w", removeErr)
	}
	return errors.Join(closeErr, removeErr)
}
