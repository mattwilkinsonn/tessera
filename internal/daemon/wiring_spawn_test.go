package daemon

import (
	"bufio"
	"context"
	"net"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
)

func TestWakeRetriesHeldSpawns(t *testing.T) {
	profile := testProfile(t)
	profile.Windows["arc"] = config.WindowSpec{App: "Zen", AppRE: regexp.MustCompile("Zen"), Spawn: []string{"open", "Zen"}}
	driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: &wm.Frame{W: 5120, H: 2160}}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}}, SpawnNoWindow: true})
	driver.Uptime = 30*time.Second - 500*time.Millisecond
	env := daemonEnv(t)
	env.Nudge = func(context.Context, string, ...string) error { return nil }
	sock := filepath.Join(shortDir(t), "retry.sock")
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	wake := WakeRoutine(profile, driver, env, sock)
	start := time.Now()
	for range 3 {
		if err := wake(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Fatalf("held wakes blocked for %s", elapsed)
	}
	if len(driver.SpawnCalls) != 0 {
		t.Fatal("spawned before yabai was stable")
	}
	unix := listener.(*net.UnixListener)
	if err := unix.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	if err != nil || line != "wake\n" {
		t.Fatalf("retry = %q, %v", line, err)
	}
	if err := unix.SetDeadline(time.Now().Add(700 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	conn, err = listener.Accept()
	if err == nil {
		conn.Close()
		t.Fatal("repeated held wakes scheduled multiple retries")
	}
	if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatal(err)
	}
}

func TestServeSpawnRetryLifetime(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		name := "retry survives bounded wake"
		if cancelEarly {
			name = "shutdown cancels retry"
		}
		t.Run(name, func(t *testing.T) {
			profile := testProfile(t)
			profile.Windows["arc"] = config.WindowSpec{AppRE: regexp.MustCompile("Zen"), Spawn: []string{"open", "Zen"}}
			driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: &wm.Frame{W: 5120, H: 2160}}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}}})
			driver.Uptime = 29 * time.Second
			env := daemonEnv(t)
			env.Nudge = func(context.Context, string, ...string) error { return nil }
			sock := filepath.Join(shortDir(t), "serve.sock")
			listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sock)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			finished := make(chan error, 4)
			var calls atomic.Int32
			routine := WakeRoutine(profile, driver, env, sock)
			wake := func(ctx context.Context) error { calls.Add(1); err := routine(ctx); finished <- err; return err }
			done := make(chan error, 1)
			go func() { done <- Serve(ctx, listener, NewHub(idle, idle), Wakes{Run: wake}) }()
			awaitSpawnWake(t, finished, "held wake blocked")
			if cancelEarly {
				cancel()
				awaitSpawnShutdown(t, done)
				assertNoSpawnRetry(t, finished, &calls)
				return
			}
			awaitSpawnWake(t, finished, "deferred wake was lost after bounded context ended")
			cancel()
			awaitSpawnShutdown(t, done)
			if calls.Load() != 2 {
				t.Fatalf("wakes = %d, want two", calls.Load())
			}
		})
	}
}

func awaitSpawnWake(t *testing.T, finished <-chan error, message string) {
	t.Helper()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal(message)
	}
}

func awaitSpawnShutdown(t *testing.T, done <-chan error) {
	t.Helper()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func assertNoSpawnRetry(t *testing.T, finished <-chan error, calls *atomic.Int32) {
	t.Helper()
	select {
	case <-finished:
		t.Fatal("retry ran after shutdown")
	case <-time.After(1200 * time.Millisecond):
	}
	if calls.Load() != 1 {
		t.Fatalf("wakes = %d", calls.Load())
	}
}

type retryLifetimeContext struct {
	context.Context
	stopped chan struct{}
	active  atomic.Int32
}

func (c *retryLifetimeContext) Value(any) any { return nil }

func (c *retryLifetimeContext) AfterFunc(func()) func() bool {
	c.active.Add(1)
	var once sync.Once
	return func() bool {
		result := false
		once.Do(func() { c.active.Add(-1); c.stopped <- struct{}{}; result = true })
		return result
	}
}

func TestCompletedSpawnRetriesCleanLifetime(t *testing.T) {
	profile := testProfile(t)
	profile.Windows["arc"] = config.WindowSpec{AppRE: regexp.MustCompile("Zen"), Spawn: []string{"open", "Zen"}}
	driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: &wm.Frame{W: 5120, H: 2160}}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}}})
	driver.Uptime = 30*time.Second - 10*time.Millisecond
	env := daemonEnv(t)
	env.Nudge = func(context.Context, string, ...string) error { return nil }
	sock := filepath.Join(shortDir(t), "cleanup.sock")
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	lifetime := &retryLifetimeContext{Context: t.Context(), stopped: make(chan struct{}, 3)}
	ctx := context.WithValue(t.Context(), wakeLifetimeKey{}, context.Context(lifetime))
	wake := WakeRoutine(profile, driver, env, sock)
	for range 3 {
		if err := wake(ctx); err != nil {
			t.Fatal(err)
		}
		acceptCleanupWake(t, listener)
		select {
		case <-lifetime.stopped:
		case <-time.After(5 * time.Second):
			t.Fatal("completed retry retained lifetime registration")
		}
		if lifetime.active.Load() != 0 {
			t.Fatal("retry registration still active")
		}
	}
}

func acceptCleanupWake(t *testing.T, listener net.Listener) {
	t.Helper()
	if err := listener.(*net.UnixListener).SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || line != "wake\n" {
		t.Fatalf("retry = %q, %v", line, err)
	}
}
