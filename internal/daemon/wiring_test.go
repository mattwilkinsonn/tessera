package daemon

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"gotest.tools/v3/assert"

	"github.com/mattwilkinsonn/tessera/internal/wm"
)

type eventStreamFake struct {
	events <-chan wm.Event
	err    error
	calls  int
	ctx    context.Context
}

func (f *eventStreamFake) Events(ctx context.Context) (<-chan wm.Event, error) {
	f.calls++
	f.ctx = ctx
	return f.events, f.err
}

type processStart struct {
	started bool
	exited  <-chan error
	err     error
}

type processSupervisorFake struct {
	mu         sync.Mutex
	starts     []processStart
	startCalls chan struct{}
	startCount int
	stopCount  int
}

func (f *processSupervisorFake) Start(context.Context) (bool, <-chan error, error) {
	f.mu.Lock()
	f.startCount++
	start := f.starts[0]
	f.starts = f.starts[1:]
	f.mu.Unlock()
	select {
	case f.startCalls <- struct{}{}:
	default:
	}
	return start.started, start.exited, start.err
}

func (f *processSupervisorFake) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCount++
	return nil
}

func (f *processSupervisorFake) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.startCount, f.stopCount
}

func TestRunForwardsEvents(t *testing.T) {
	events := append(append([]wm.Event{}, DisplayEvents...), FlexEvents...)
	for _, event := range events {
		t.Run(string(event), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				pushedEvents := make(chan wm.Event, 1)
				pushedEvents <- event
				close(pushedEvents)
				stream := &eventStreamFake{events: pushedEvents}
				var pushedDisplay, pushedFlex recorder
				pushedListener := newPipeListener()
				pushedCtx, cancelPushed := context.WithCancel(t.Context())
				defer cancelPushed()
				pushedDone := make(chan error, 1)
				go func() {
					pushedDone <- run(pushedCtx, Options{
						Events:  stream,
						Display: pushedDisplay.work,
						Flex:    pushedFlex.work,
						Wake:    noWake,
					}, pushedListener, pushedListener.Close)
				}()
				synctest.Wait()
				pushedStart := time.Now()
				<-time.After(3 * time.Second)
				synctest.Wait()

				isDisplay := slices.Contains(DisplayEvents, event)
				wantDisplay, wantFlex := 0, 0
				if isDisplay {
					wantDisplay = 1
				} else {
					wantFlex = 1
				}
				assert.Equal(t, len(pushedDisplay.elapsed(pushedStart)), wantDisplay)
				assert.Equal(t, len(pushedFlex.elapsed(pushedStart)), wantFlex)
				select {
				case err := <-pushedDone:
					t.Fatalf("Run() ended after event channel closed: %v", err)
				default:
				}
				cancelPushed()
				assert.NilError(t, <-pushedDone)
				assert.Equal(t, stream.calls, 1)

				var socketDisplay, socketFlex recorder
				socketListener := newPipeListener()
				socketCtx, cancelSocket := context.WithCancel(t.Context())
				defer cancelSocket()
				socketDone := make(chan error, 1)
				go func() {
					socketDone <- run(socketCtx, Options{
						Display: socketDisplay.work,
						Flex:    socketFlex.work,
						Wake:    noWake,
					}, socketListener, socketListener.Close)
				}()
				socketStart := time.Now()
				socketListener.send(t, string(event))
				<-time.After(3 * time.Second)
				synctest.Wait()
				assert.DeepEqual(t, pushedDisplay.elapsed(pushedStart), socketDisplay.elapsed(socketStart))
				assert.DeepEqual(t, pushedFlex.elapsed(pushedStart), socketFlex.elapsed(socketStart))
				cancelSocket()
				assert.NilError(t, <-socketDone)
			})
		})
	}
}

func TestRunEventsError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sentinel := errors.New("subscribe failed")
		stream := &eventStreamFake{err: sentinel}
		err := run(t.Context(), Options{Events: stream}, newPipeListener(), func() error { return nil })
		assert.Assert(t, errors.Is(err, sentinel), "Run() error = %v", err)
		assert.Equal(t, stream.calls, 1)
	})
}

func TestRunProcessSupervision(t *testing.T) {
	t.Run("restarts an owned process after an error exit and stops its replacement", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			firstExit := make(chan error, 1)
			supervisor := &processSupervisorFake{
				starts: []processStart{
					{started: true, exited: firstExit},
					{started: true, exited: make(chan error)},
				},
				startCalls: make(chan struct{}, 2),
			}
			done := make(chan error, 1)
			ln := newPipeListener()
			go func() {
				done <- run(ctx, Options{Bootstrap: true, Supervisor: supervisor, Wake: noWake}, ln, ln.Close)
			}()
			<-supervisor.startCalls
			firstExit <- errors.New("process crashed")
			<-supervisor.startCalls
			synctest.Wait()
			cancel()
			assert.NilError(t, <-done)
			starts, stops := supervisor.counts()
			assert.Equal(t, starts, 2)
			assert.Equal(t, stops, 1)
		})
	})

	t.Run("does not restart or stop after a clean exit", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			exited := make(chan error, 1)
			supervisor := &processSupervisorFake{
				starts:     []processStart{{started: true, exited: exited}},
				startCalls: make(chan struct{}, 1),
			}
			done := make(chan error, 1)
			ln := newPipeListener()
			go func() {
				done <- run(ctx, Options{Bootstrap: true, Supervisor: supervisor, Wake: noWake}, ln, ln.Close)
			}()
			<-supervisor.startCalls
			exited <- nil
			synctest.Wait()
			starts, stops := supervisor.counts()
			assert.Equal(t, starts, 1)
			assert.Equal(t, stops, 0)
			cancel()
			assert.NilError(t, <-done)
		})
	})

	t.Run("does not own or stop an existing process", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			exited := make(chan error, 1)
			supervisor := &processSupervisorFake{
				starts:     []processStart{{started: false, exited: exited}},
				startCalls: make(chan struct{}, 1),
			}
			done := make(chan error, 1)
			ln := newPipeListener()
			go func() {
				done <- run(ctx, Options{Bootstrap: true, Supervisor: supervisor, Wake: noWake}, ln, ln.Close)
			}()
			<-supervisor.startCalls
			exited <- errors.New("existing process ended")
			synctest.Wait()
			starts, stops := supervisor.counts()
			assert.Equal(t, starts, 1)
			assert.Equal(t, stops, 0)
			cancel()
			assert.NilError(t, <-done)
		})
	})
}

func TestRunProcessSupervisionOwnership(t *testing.T) {
	t.Run("does not stop after a restart that found an existing process", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			firstExit := make(chan error, 1)
			supervisor := &processSupervisorFake{
				starts: []processStart{
					{started: true, exited: firstExit},
					{started: false, exited: make(chan error)},
				},
				startCalls: make(chan struct{}, 2),
			}
			done := make(chan error, 1)
			ln := newPipeListener()
			go func() {
				done <- run(ctx, Options{Bootstrap: true, Supervisor: supervisor, Wake: noWake}, ln, ln.Close)
			}()
			<-supervisor.startCalls
			firstExit <- errors.New("process crashed")
			<-supervisor.startCalls
			synctest.Wait()
			cancel()
			assert.NilError(t, <-done)
			starts, stops := supervisor.counts()
			assert.Equal(t, starts, 2)
			assert.Equal(t, stops, 0)
		})
	})

	t.Run("never starts or stops without bootstrap", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			supervisor := &processSupervisorFake{startCalls: make(chan struct{}, 1)}
			done := make(chan error, 1)
			ln := newPipeListener()
			go func() {
				done <- run(ctx, Options{Supervisor: supervisor, Wake: noWake}, ln, ln.Close)
			}()
			synctest.Wait()
			cancel()
			assert.NilError(t, <-done)
			starts, stops := supervisor.counts()
			assert.Equal(t, starts, 0)
			assert.Equal(t, stops, 0)
		})
	})
}

func TestRunEndsSubscriptionWhenServeReturnsEarly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stream := &eventStreamFake{events: make(chan wm.Event)}
		ln := newPipeListener()
		done := make(chan error, 1)
		go func() { done <- run(t.Context(), Options{Events: stream, Wake: noWake}, ln, ln.Close) }()
		synctest.Wait()
		assert.NilError(t, ln.Close())
		<-done
		assert.Assert(t, stream.ctx != nil)
		assert.Assert(t, stream.ctx.Err() != nil, "Events context still live after Run returned")
	})
}

func TestRunCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		ln := newPipeListener()
		done := make(chan error, 1)
		go func() { done <- run(ctx, Options{Wake: noWake}, ln, ln.Close) }()
		synctest.Wait()
		cancel()
		assert.NilError(t, <-done)
	})
}
