// Package daemon is the resident event subscriber: socket, debounce hub, signal wiring and launchd bootstrap.
package daemon

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

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
	work    Work
	settled func(ctx context.Context)
}

// Hub holds one stamp and one running flag per channel and runs one waiter per channel.
type Hub struct {
	mu       sync.Mutex
	channels [2]channel
	waiters  sync.WaitGroup
	fatal    chan error
}

// HubOption configures a Hub.
type HubOption func(*Hub)

// OnDisplaySettled runs fn once each time the display channel settles.
func OnDisplaySettled(fn func(ctx context.Context)) HubOption {
	return func(h *Hub) { h.channels[displayChannel].settled = fn }
}

// NewHub builds a hub that runs display and flex work after their quiet windows.
func NewHub(display, flex Work, opts ...HubOption) *Hub {
	h := &Hub{fatal: make(chan error, 1)}
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
	h.waiters.Go(func() { h.wait(ctx, id) })
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
		step, err := h.run(ctx, ch.work)
		if ctx.Err() != nil {
			h.stop(id)
			return
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
	h.mu.Unlock()
}
