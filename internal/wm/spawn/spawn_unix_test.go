//go:build unix

package spawn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestRunLeavesLauncherAliveAfterWait(t *testing.T) {
	dir := t.TempDir()
	gate, started, out := filepath.Join(dir, "gate"), filepath.Join(dir, "started"), filepath.Join(dir, "out")
	for _, path := range []string{gate, started, out} {
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	startedRead := make(chan error, 1)
	go func() {
		f, err := os.Open(started)
		if err != nil {
			startedRead <- err
			return
		}
		defer f.Close()
		var marker [1]byte
		_, err = f.Read(marker[:])
		startedRead <- err
	}()
	ctx, cancel := context.WithCancel(t.Context())
	script := `printf x > "$3"; read x < "$1"; echo alive > "$2"`
	if err := Run(ctx, []string{"/bin/sh", "-c", script, "sh", gate, out, started}, 0); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-startedRead:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("launcher did not reach the event gate")
	}
	gateOpened := make(chan error, 1)
	go func() {
		f, err := os.OpenFile(gate, os.O_WRONLY, 0)
		if err == nil {
			_, err = f.WriteString("go\n")
			err = errors.Join(err, f.Close())
		}
		gateOpened <- err
	}()
	select {
	case err := <-gateOpened:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("launcher did not survive caller cancellation")
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "alive\n" {
		t.Fatalf("launcher wrote %q; want alive", got)
	}
}
