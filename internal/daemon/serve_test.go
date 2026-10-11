package daemon

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"gotest.tools/v3/assert"

	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
)

// pipeListener hands Serve the server ends of net.Pipe connections, so it works inside a bubble.
type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), closed: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return &net.UnixAddr{Name: "pipe", Net: "unix"} }

// dial opens a connection to Serve and returns the client end.
func (l *pipeListener) dial() net.Conn {
	client, server := net.Pipe()
	l.conns <- server
	return client
}

// send writes one line and waits for Serve to close the connection.
func (l *pipeListener) send(t *testing.T, line string) {
	t.Helper()
	client := l.dial()
	_, err := io.WriteString(client, line+"\n")
	assert.NilError(t, err)
	_, err = io.ReadAll(client)
	assert.NilError(t, err)
	assert.NilError(t, client.Close())
}

type serveResult struct{ err error }

func startServe(ctx context.Context, ln net.Listener, h *Hub, wake func(context.Context) error) <-chan serveResult {
	done := make(chan serveResult, 1)
	go func() { done <- serveResult{Serve(ctx, ln, h, wake)} }()
	return done
}

func noWake(context.Context) error { return nil }

func TestServe(t *testing.T) {
	t.Run("a burst of N events gives one cascade", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			ln := newPipeListener()
			var display, flex recorder
			done := startServe(ctx, ln, NewHub(display.work, flex.work), noWake)
			start := time.Now()
			events := []wm.Event{wm.EventDisplayAdded, wm.EventDisplayRemoved, wm.EventDisplayMoved}
			for i := range 30 {
				ln.send(t, string(events[i%len(events)]))
				<-time.After(100 * time.Millisecond)
			}
			<-time.After(time.Minute)
			synctest.Wait()
			assert.DeepEqual(t, display.elapsed(start), []time.Duration{2900*time.Millisecond + 3*time.Second})
			assert.Equal(t, len(flex.elapsed(start)), 0)
			cancel()
			assert.NilError(t, (<-done).err)
		})
	})

	t.Run("runs the wake routine at startup and on each wake line", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			ln := newPipeListener()
			wakes := make(chan struct{}, 4)
			wake := func(context.Context) error {
				wakes <- struct{}{}
				return errors.New("yabai not up yet")
			}
			done := startServe(ctx, ln, NewHub(idle, idle), wake)
			<-wakes
			ln.send(t, "wake")
			<-wakes
			synctest.Wait()
			assert.Equal(t, len(wakes), 0)
			cancel()
			// A failed wake routine is logged, not fatal.
			assert.NilError(t, (<-done).err)
		})
	})

	t.Run("drops an unknown line and closes a silent connection after one second", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			ln := newPipeListener()
			var display, flex recorder
			done := startServe(ctx, ln, NewHub(display.work, flex.work), noWake)
			ln.send(t, "bogus")
			silent := ln.dial()
			start := time.Now()
			_, err := io.ReadAll(silent)
			assert.NilError(t, err)
			assert.Equal(t, time.Since(start), time.Second)
			assert.NilError(t, silent.Close())
			<-time.After(time.Minute)
			synctest.Wait()
			assert.Equal(t, len(display.elapsed(start))+len(flex.elapsed(start)), 0)
			cancel()
			assert.NilError(t, (<-done).err)
		})
	})

	t.Run("a timeout ends Serve", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ln := newPipeListener()
			wedged := func(ctx context.Context) (Step, error) {
				<-ctx.Done()
				return Settled, ctx.Err()
			}
			done := startServe(t.Context(), ln, NewHub(wedged, idle), noWake)
			start := time.Now()
			ln.send(t, string(wm.EventDisplayAdded))
			err := (<-done).err
			assert.Assert(t, errors.Is(err, context.DeadlineExceeded), "Serve() = %v", err)
			assert.Equal(t, time.Since(start), 3*time.Second+CascadeTimeout)
			_, acceptErr := ln.Accept()
			assert.Assert(t, errors.Is(acceptErr, net.ErrClosed))
		})
	})

	t.Run("a wedged wake routine ends Serve", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			wedged := func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			}
			err := Serve(t.Context(), newPipeListener(), NewHub(idle, idle), wedged)
			assert.Assert(t, errors.Is(err, context.DeadlineExceeded), "Serve() = %v", err)
		})
	})

	t.Run("cancel ends Serve cleanly while work runs", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			ln := newPipeListener()
			entered := make(chan struct{})
			blocked := func(ctx context.Context) (Step, error) {
				close(entered)
				<-ctx.Done()
				return Settled, ctx.Err()
			}
			done := startServe(ctx, ln, NewHub(idle, blocked), noWake)
			ln.send(t, string(wm.EventWindowCreated))
			<-entered
			cancel()
			assert.NilError(t, (<-done).err)
		})
	})
}

// signalRecorder is a fake driver that also records signal registrations.
type signalRecorder struct {
	*fake.Driver

	signals []recordedSignal
}

type recordedSignal struct {
	Event  wm.Event
	Label  string
	Action string
}

func (s *signalRecorder) AddSignal(_ context.Context, ev wm.Event, label, action string) error {
	s.signals = append(s.signals, recordedSignal{Event: ev, Label: label, Action: action})
	return nil
}

func TestRegisterSignals(t *testing.T) {
	t.Run("no action holds a tess path", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			src := &signalRecorder{}
			assert.NilError(t, RegisterSignals(t.Context(), src, "/tmp/tessera-daemon.sock"))
			assert.DeepEqual(t, src.signals, []recordedSignal{
				{wm.EventDisplayAdded, "tessera-display_added", "echo display_added | /usr/bin/nc -U -w 1 '/tmp/tessera-daemon.sock'"},
				{wm.EventDisplayRemoved, "tessera-display_removed", "echo display_removed | /usr/bin/nc -U -w 1 '/tmp/tessera-daemon.sock'"},
				{wm.EventDisplayMoved, "tessera-display_moved", "echo display_moved | /usr/bin/nc -U -w 1 '/tmp/tessera-daemon.sock'"},
				{wm.EventApplicationLaunched, "tessera-application_launched", "echo application_launched | /usr/bin/nc -U -w 1 '/tmp/tessera-daemon.sock'"},
				{wm.EventApplicationTerminated, "tessera-application_terminated", "echo application_terminated | /usr/bin/nc -U -w 1 '/tmp/tessera-daemon.sock'"},
				{wm.EventWindowCreated, "tessera-window_created", "echo window_created | /usr/bin/nc -U -w 1 '/tmp/tessera-daemon.sock'"},
				{wm.EventWindowDestroyed, "tessera-window_destroyed", "echo window_destroyed | /usr/bin/nc -U -w 1 '/tmp/tessera-daemon.sock'"},
				{wm.EventSpaceChanged, "tessera-space_changed", "/opt/homebrew/bin/sketchybar --trigger yabai_spaces_changed"},
				{wm.EventDisplayChanged, "tessera-display_changed", "/opt/homebrew/bin/sketchybar --trigger yabai_spaces_changed"},
			})
			for _, signal := range src.signals {
				assert.Assert(t, !strings.Contains(signal.Action, "tess "), "action %q runs tess", signal.Action)
				assert.Assert(t, signal.Event != wm.EventDockDidRestart)
			}
		})
	})

	t.Run("a relative socket path is written as absolute", func(t *testing.T) {
		src := &signalRecorder{}
		assert.NilError(t, RegisterSignals(t.Context(), src, "daemon.sock"))
		assert.Assert(t, strings.HasSuffix(src.signals[0].Action, "/daemon.sock'"))
		assert.Assert(t, strings.Contains(src.signals[0].Action, "-w 1 '/"))
	})
}

func TestWakeRoutine(t *testing.T) {
	t.Run("registers the nine signal wirings and runs the startup cascade", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			driver := &signalRecorder{Driver: fake.New(fake.Seed{
				Displays: []fake.SeedDisplay{
					{Idx: 1, Frame: &wm.Frame{W: 5120, H: 2160}},
					{Idx: 2, Frame: &wm.Frame{W: 3440, H: 1440}},
					{Idx: 3, Frame: &wm.Frame{W: 1728, H: 1117}},
				},
				Spaces:  []fake.SeedSpace{{DisplayIdx: 1}, {DisplayIdx: 2}, {DisplayIdx: 3}},
				Windows: []fake.SeedWindow{{ID: 1, App: "Arc", Title: "one", SpaceIndex: 1}, {ID: 5, App: "Code", SpaceIndex: 1}},
			})}
			assert.NilError(t, WakeRoutine(testProfile(t), driver, daemonEnv(t), "/tmp/t.sock")(t.Context()))
			assert.Equal(t, len(driver.signals), 9)
			spaces, err := driver.QuerySpaces(t.Context())
			assert.NilError(t, err)
			labels := make([]string, 0, len(spaces))
			for _, space := range spaces {
				labels = append(labels, space.Label)
			}
			assert.Assert(t, strings.Contains(strings.Join(labels, ","), "main"), "labels %v", labels)
		})
	})

	t.Run("converges the laptop grid at startup on a laptop-only world", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			driver := laptopWorld()
			assert.NilError(t, WakeRoutine(testProfile(t), driver, daemonEnv(t), "/tmp/t.sock")(t.Context()))
			assert.Assert(t, hasLapSpace(t, driver))
			spaces, err := driver.QuerySpaces(t.Context())
			assert.NilError(t, err)
			for _, space := range spaces {
				assert.Assert(t, space.Label != "main")
				if space.Label == "laptop" {
					assert.Equal(t, space.Layout, wm.LayoutStack)
				}
			}
		})
	})
}
