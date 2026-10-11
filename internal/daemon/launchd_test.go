package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"gotest.tools/v3/assert"
)

// fakeLaunchctl records argv, fails the commands whose joined argv is listed, and reports gone ones as not loaded.
type fakeLaunchctl struct {
	mu    sync.Mutex
	calls []string
	fail  map[string]bool
	gone  map[string]bool
}

func (f *fakeLaunchctl) Run(_ context.Context, args ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	line := strings.Join(args, " ")
	f.calls = append(f.calls, line)
	if f.gone[line] {
		return fmt.Errorf("launchctl exited 113: %w", ErrNotLoaded)
	}
	if f.fail[line] {
		return errors.New("launchctl failed")
	}
	return nil
}

func (f *fakeLaunchctl) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeLaunchctl) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

var (
	yabai = Service{Label: "com.asmvik.yabai", Plist: "/home/u/Library/LaunchAgents/com.asmvik.yabai.plist"}
	skhd  = Service{Label: "com.asmvik.skhd", Plist: "/home/u/Library/LaunchAgents/com.asmvik.skhd.plist"}
)

func TestBootstrap(t *testing.T) {
	t.Run("default services use the upstream labels and plists", func(t *testing.T) {
		assert.DeepEqual(t, DefaultServices("/home/u"), []Service{yabai, skhd})
	})

	t.Run("enable runs before bootstrap and a bootstrapped service is owned", func(t *testing.T) {
		lc := &fakeLaunchctl{fail: map[string]bool{"print gui/501/com.asmvik.yabai": true}}
		owned, err := Bootstrap(t.Context(), lc, 501, []Service{yabai})
		assert.NilError(t, err)
		assert.DeepEqual(t, owned, Owned{yabai})
		assert.DeepEqual(t, lc.recorded(), []string{
			"print gui/501/com.asmvik.yabai",
			"enable gui/501/com.asmvik.yabai",
			"bootstrap gui/501 " + yabai.Plist,
		})
	})

	t.Run("a failed enable is logged and bootstrap still runs", func(t *testing.T) {
		lc := &fakeLaunchctl{fail: map[string]bool{
			"print gui/501/com.asmvik.skhd":  true,
			"enable gui/501/com.asmvik.skhd": true,
		}}
		owned, err := Bootstrap(t.Context(), lc, 501, []Service{skhd})
		assert.NilError(t, err)
		assert.DeepEqual(t, owned, Owned{skhd})
	})

	t.Run("an already-loaded service is kickstarted, not owned, and never booted out", func(t *testing.T) {
		lc := &fakeLaunchctl{}
		owned, err := Bootstrap(t.Context(), lc, 501, []Service{yabai})
		assert.NilError(t, err)
		assert.Equal(t, len(owned), 0)
		assert.DeepEqual(t, lc.recorded(), []string{
			"print gui/501/com.asmvik.yabai",
			"kickstart gui/501/com.asmvik.yabai",
		})
		lc.reset()
		assert.NilError(t, Bootout(t.Context(), lc, 501, owned))
		assert.Equal(t, len(lc.recorded()), 0)
	})

	t.Run("a failed bootstrap leaves it not owned", func(t *testing.T) {
		lc := &fakeLaunchctl{fail: map[string]bool{
			"print gui/501/com.asmvik.yabai":   true,
			"bootstrap gui/501 " + yabai.Plist: true,
			"print gui/501/com.asmvik.skhd":    true,
		}}
		owned, err := Bootstrap(t.Context(), lc, 501, []Service{yabai, skhd})
		assert.ErrorContains(t, err, "bootstrapping service")
		assert.DeepEqual(t, owned, Owned{skhd})
	})

	t.Run("bootout of a service that is no longer loaded succeeds", func(t *testing.T) {
		lc := &fakeLaunchctl{gone: map[string]bool{"bootout gui/501/com.asmvik.yabai": true}}
		assert.NilError(t, Bootout(t.Context(), lc, 501, Owned{yabai}))
	})

	t.Run("any other bootout failure is returned", func(t *testing.T) {
		lc := &fakeLaunchctl{fail: map[string]bool{"bootout gui/501/com.asmvik.yabai": true}}
		assert.ErrorContains(t, Bootout(t.Context(), lc, 501, Owned{yabai}), "booting out service")
	})
}

func TestRun(t *testing.T) {
	t.Run("with one service loaded and the other bootstrapped, clean shutdown boots out exactly the bootstrapped one", func(t *testing.T) {
		path := filepath.Join(shortDir(t), "d.sock")
		lc := &fakeLaunchctl{fail: map[string]bool{"print gui/501/com.asmvik.skhd": true}}
		ctx, cancel := context.WithCancel(t.Context())
		woke := make(chan struct{}, 1)
		done := make(chan error, 1)
		go func() {
			done <- Run(ctx, Options{
				Socket:    path,
				Bootstrap: true,
				Launchctl: lc,
				UID:       501,
				Services:  []Service{yabai, skhd},
				Display:   idle,
				Flex:      idle,
				Wake: func(context.Context) error {
					woke <- struct{}{}
					return nil
				},
			})
		}()
		<-woke
		_, err := Listen(t.Context(), path)
		assert.Assert(t, errors.Is(err, ErrRunning), "second start = %v", err)
		cancel()
		assert.NilError(t, <-done)
		assert.DeepEqual(t, lc.recorded(), []string{
			"print gui/501/com.asmvik.yabai",
			"kickstart gui/501/com.asmvik.yabai",
			"print gui/501/com.asmvik.skhd",
			"enable gui/501/com.asmvik.skhd",
			"bootstrap gui/501 " + skhd.Plist,
			"bootout gui/501/com.asmvik.skhd",
		})
		ln, err := Listen(t.Context(), path)
		assert.NilError(t, err, "lock and socket released after shutdown")
		assert.NilError(t, ln.Close())
	})

	t.Run("a failed bootout after a clean shutdown is logged and Run returns nil", func(t *testing.T) {
		path := filepath.Join(shortDir(t), "d.sock")
		lc := &fakeLaunchctl{fail: map[string]bool{
			"print gui/501/com.asmvik.skhd":   true,
			"bootout gui/501/com.asmvik.skhd": true,
		}}
		ctx, cancel := context.WithCancel(t.Context())
		woke := make(chan struct{}, 1)
		done := make(chan error, 1)
		go func() {
			done <- Run(ctx, Options{
				Socket:    path,
				Bootstrap: true,
				Launchctl: lc,
				UID:       501,
				Services:  []Service{skhd},
				Display:   idle,
				Flex:      idle,
				Wake: func(context.Context) error {
					woke <- struct{}{}
					return nil
				},
			})
		}()
		<-woke
		cancel()
		assert.NilError(t, <-done)
		assert.Equal(t, lc.recorded()[len(lc.recorded())-1], "bootout gui/501/com.asmvik.skhd")
	})

	t.Run("a second start loses the lock and runs no launchctl", func(t *testing.T) {
		path := filepath.Join(shortDir(t), "d.sock")
		held, err := Listen(t.Context(), path)
		assert.NilError(t, err)
		lc := &fakeLaunchctl{}
		err = Run(t.Context(), Options{Socket: path, Bootstrap: true, Launchctl: lc, UID: 501, Services: []Service{yabai}})
		assert.Assert(t, errors.Is(err, ErrRunning), "Run() = %v", err)
		assert.Equal(t, len(lc.recorded()), 0)
		dialer := net.Dialer{}
		conn, dialErr := dialer.DialContext(t.Context(), "unix", path)
		assert.NilError(t, dialErr)
		assert.NilError(t, conn.Close())
		assert.NilError(t, held.Close())
	})

	t.Run("--no-bootstrap runs no launchctl and nudges once after the display channel settles", func(t *testing.T) {
		path := filepath.Join(shortDir(t), "d.sock")
		lc := &fakeLaunchctl{}
		ctx, cancel := context.WithCancel(t.Context())
		woke := make(chan struct{}, 1)
		nudged := make(chan string, 1)
		done := make(chan error, 1)
		go func() {
			done <- Run(ctx, Options{
				Socket:    path,
				Launchctl: lc,
				Display:   idle,
				Flex:      idle,
				Wake: func(context.Context) error {
					woke <- struct{}{}
					return nil
				},
				Nudge: func(_ context.Context, event string) error {
					nudged <- event
					return nil
				},
			})
		}()
		<-woke
		dialer := net.Dialer{}
		conn, err := dialer.DialContext(t.Context(), "unix", path)
		assert.NilError(t, err)
		_, err = io.WriteString(conn, "display_added\n")
		assert.NilError(t, err)
		assert.NilError(t, conn.Close())
		assert.Equal(t, <-nudged, "display_relatch")
		cancel()
		assert.NilError(t, <-done)
		assert.Equal(t, len(lc.recorded()), 0)
	})
}

func TestNotLoadedExit(t *testing.T) {
	for _, test := range []struct {
		name string
		code int
		want bool
	}{
		{name: "bootout of a missing service", code: 3, want: true},
		{name: "print of a missing service", code: 113, want: true},
		{name: "other failure", code: 1, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, notLoadedExit(test.code), test.want)
		})
	}
}

func TestExecLaunchctlMarksNotLoaded(t *testing.T) {
	stub := filepath.Join(t.TempDir(), "launchctl")
	assert.NilError(t, os.WriteFile(stub, []byte("#!/bin/sh\necho 'Could not find service'\nexit 113\n"), 0o700))
	err := ExecLaunchctl{Path: stub}.Run(t.Context(), "bootout", "gui/501/com.example")
	assert.Assert(t, errors.Is(err, ErrNotLoaded), "Run() = %v", err)
}
