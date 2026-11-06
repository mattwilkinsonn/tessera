// Package daemon is the resident event subscriber: socket, debounce hub, signal wiring and launchd bootstrap.
package daemon

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/commands"
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

// CascadeTimeout bounds each work call; a deadline turns a wedge into a crash launchd can see.
const CascadeTimeout = 60 * time.Second

// Step tells the waiter what to do after its work ran.
type Step int

const (
	// Settled ends the waiter unless an event landed during the work.
	Settled Step = iota
	// Restamp writes "now" into the channel stamp and waits again (H2).
	Restamp
)

// Work is one debounced unit of work for a channel.
type Work func(ctx context.Context) (Step, error)

// DisplayEvents feed the display channel.
var DisplayEvents = []wm.Event{wm.EventDisplayAdded, wm.EventDisplayRemoved, wm.EventDisplayMoved}

// FlexEvents feed the flex channel.
var FlexEvents = []wm.Event{
	wm.EventApplicationLaunched,
	wm.EventApplicationTerminated,
	wm.EventWindowCreated,
	wm.EventWindowDestroyed,
}

type channelID int

const (
	displayChannel channelID = iota
	flexChannel
)

type channel struct {
	stamp   time.Time
	running bool
	done    chan struct{}
	work    Work
	settled func(ctx context.Context)
}

// Hub holds one stamp and one running flag per channel and runs one waiter per channel.
type Hub struct {
	mu              sync.Mutex
	channels        [2]channel
	waiters         sync.WaitGroup
	fatal           chan error
	topology        *commands.TopologyState
	windowCreated   func(context.Context, int, func() bool) error
	pendingArrivals map[int]struct{}
	arrivalRunning  bool
}

// HubOption configures a Hub.
type HubOption func(*Hub)

// WithTopology shares the cascade lock with arrivals.
func WithTopology(topology *commands.TopologyState) HubOption {
	return func(h *Hub) { h.topology = topology }
}

// OnDisplaySettled runs fn once each time the display channel settles.
func OnDisplaySettled(fn func(ctx context.Context)) HubOption {
	return func(h *Hub) { h.channels[displayChannel].settled = fn }
}

// WithWindowCreated enables immediate targeted arrival handling.
func WithWindowCreated(topology *commands.TopologyState, handler func(context.Context, int, func() bool) error) HubOption {
	return func(h *Hub) {
		h.topology = topology
		h.windowCreated = handler
	}
}

// NewHub builds a hub that runs display and flex work after their quiet windows.
func NewHub(display, flex Work, opts ...HubOption) *Hub {
	h := &Hub{fatal: make(chan error, 1), topology: &commands.TopologyState{}}
	h.channels[displayChannel].work = display
	h.channels[flexChannel].work = flex
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Notify stamps the event's channel with now and starts its waiter if none runs.
func (h *Hub) Notify(ctx context.Context, ev string) {
	id, ok := channelFor(wm.Event(ev))
	if !ok {
		slog.WarnContext(ctx, "dropping unknown event", "event", ev)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := &h.channels[id]
	ch.stamp = time.Now()
	if ch.running {
		return
	}
	ch.running = true
	ch.done = make(chan struct{})
	h.waiters.Go(func() { h.wait(ctx, id) })
}

// NotifyWindowCreated queues the ID separately from debounced flex work.
func (h *Hub) NotifyWindowCreated(ctx context.Context, id int) {
	if id <= 0 {
		return
	}
	h.Notify(ctx, string(wm.EventWindowCreated))
	if h.windowCreated == nil {
		return
	}
	h.mu.Lock()
	if h.pendingArrivals == nil {
		h.pendingArrivals = make(map[int]struct{})
	}
	h.pendingArrivals[id] = struct{}{}
	if h.arrivalRunning {
		h.mu.Unlock()
		return
	}
	h.arrivalRunning = true
	h.waiters.Go(func() { h.runArrivals(ctx) })
	h.mu.Unlock()
}

func (h *Hub) runArrivals(ctx context.Context) {
	defer func() {
		h.mu.Lock()
		h.arrivalRunning = false
		if ctx.Err() == nil && len(h.pendingArrivals) > 0 {
			h.arrivalRunning = true
			h.waiters.Go(func() { h.runArrivals(ctx) })
		}
		h.mu.Unlock()
	}()
	for {
		h.mu.Lock()
		var id int
		for pending := range h.pendingArrivals {
			id = pending
			break
		}
		h.mu.Unlock()
		if id == 0 {
			return
		}
		if !h.waitForDisplay(ctx) {
			return
		}
		ready, err := bounded(ctx, h, func(ctx context.Context) (bool, error) {
			return h.topology.SerializeArrival(ctx, h.displayReady, func(ctx context.Context) error {
				return h.windowCreated(ctx, id, h.displayReady)
			})
		})
		if errors.Is(err, commands.ErrDisplayPending) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.ErrorContext(ctx, "window arrival failed", "window_id", id, "error", err)
		}
		if !ready && err == nil {
			continue
		}
		h.mu.Lock()
		delete(h.pendingArrivals, id)
		h.mu.Unlock()
	}
}

func (h *Hub) displayReady() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.channels[displayChannel].running && effects.IsQuiet(time.Now(), h.channels[displayChannel].stamp, effects.DisplayQuiet)
}

func (h *Hub) waitForDisplay(ctx context.Context) bool {
	for {
		if _, ok := h.untilQuiet(ctx, displayChannel); !ok {
			return false
		}
		h.mu.Lock()
		running := h.channels[displayChannel].running
		done := h.channels[displayChannel].done
		h.mu.Unlock()
		if !running {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-done:
		}
	}
}

func channelFor(ev wm.Event) (channelID, bool) {
	switch {
	case slices.Contains(DisplayEvents, ev):
		return displayChannel, true
	case slices.Contains(FlexEvents, ev):
		return flexChannel, true
	}
	return 0, false
}

// wait is the waiter loop. It exits only when, under mu, the stamp still equals actedOn.
func (h *Hub) wait(ctx context.Context, id channelID) {
	ch := &h.channels[id]
	for {
		actedOn, ok := h.untilQuiet(ctx, id)
		if !ok {
			h.stop(id)
			return
		}
		var step Step
		var err error
		if id == displayChannel {
			step, err = h.runCascade(ctx, ch.work)
		} else {
			step, err = h.run(ctx, ch.work)
		}
		if err != nil {
			slog.ErrorContext(ctx, "daemon work failed", "error", err)
		}
		h.mu.Lock()
		if step == Restamp {
			ch.stamp = time.Now()
			h.mu.Unlock()
			continue
		}
		if ch.stamp.Equal(actedOn) {
			ch.running = false
			close(ch.done)
			h.mu.Unlock()
			if ch.settled != nil {
				ch.settled(ctx)
			}
			return
		}
		h.mu.Unlock()
	}
}

// untilQuiet sleeps to the quiet deadline and captures actedOn inside the passing check.
func (h *Hub) untilQuiet(ctx context.Context, id channelID) (time.Time, bool) {
	for {
		h.mu.Lock()
		now := time.Now()
		own := h.channels[id].stamp
		display := h.channels[displayChannel].stamp
		var quiet bool
		var deadline time.Time
		switch id {
		case displayChannel:
			quiet = effects.IsQuiet(now, own, effects.DisplayQuiet)
			deadline = own.Add(effects.DisplayQuiet)
		case flexChannel:
			quiet = effects.IsFlexQuiet(now, own, display)
			deadline = later(own.Add(effects.FlexQuiet), display.Add(effects.DisplayQuiet))
		}
		h.mu.Unlock()
		if quiet {
			return own, true
		}
		timer := time.NewTimer(deadline.Sub(now))
		select {
		case <-ctx.Done():
			timer.Stop()
			return time.Time{}, false
		case <-timer.C:
		}
	}
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (h *Hub) runCascade(ctx context.Context, work Work) (Step, error) {
	return bounded(ctx, h, func(ctx context.Context) (Step, error) {
		var step Step
		err := h.topology.SerializeCascade(ctx, func(ctx context.Context) error {
			var err error
			step, err = work(ctx)
			return err
		})
		return step, err
	})
}

// run calls work under CascadeTimeout; a deadline is fatal to Serve.
func (h *Hub) run(ctx context.Context, work Work) (Step, error) {
	return bounded(ctx, h, work)
}

func (h *Hub) reportFatal(err error) {
	select {
	case h.fatal <- err:
	default:
	}
}

func (h *Hub) stop(id channelID) {
	h.mu.Lock()
	h.channels[id].running = false
	close(h.channels[id].done)
	h.mu.Unlock()
}
