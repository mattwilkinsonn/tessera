package daemon

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/samber/oops"
)

const (
	readTimeout = time.Second
	wakeMessage = "wake"
)

// Serve runs the wake routine at startup and dispatches one line per connection until ctx ends.
// It closes ln and waits for every goroutine it started. A wedged work call or accept failure ends it with that error.
func Serve(ctx context.Context, ln net.Listener, h *Hub, wake func(context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wakes := make(chan struct{}, 1)
	wakes <- struct{}{}
	acceptErr := make(chan error, 1)

	wg.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-wakes:
				if err := h.bounded(ctx, wake); err != nil && ctx.Err() == nil {
					slog.ErrorContext(ctx, "wake routine failed", "error", err)
				}
			}
		}
	})
	wg.Go(func() {
		<-ctx.Done()
		if err := ln.Close(); err != nil {
			slog.ErrorContext(ctx, "closing daemon listener", "error", err)
		}
	})
	wg.Go(func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				if ctx.Err() == nil {
					acceptErr <- oops.Wrapf(err, "accepting daemon connection")
				}
				return
			}
			wg.Go(func() { handle(ctx, conn, h, wakes) })
		}
	})

	var err error
	select {
	case <-ctx.Done():
	case err = <-h.fatal:
	case err = <-acceptErr:
	}
	cancel()
	wg.Wait()
	h.waiters.Wait()
	return err
}

// handle reads one line under a deadline, closes the connection at once, then dispatches it.
func handle(ctx context.Context, conn net.Conn, h *Hub, wakes chan<- struct{}) {
	line, err := readLine(conn)
	if closeErr := conn.Close(); closeErr != nil {
		slog.DebugContext(ctx, "closing daemon connection", "error", closeErr)
	}
	if err != nil {
		slog.DebugContext(ctx, "dropping daemon connection", "error", err)
		return
	}
	if line == wakeMessage {
		select {
		case wakes <- struct{}{}:
		default:
		}
		return
	}
	h.Notify(ctx, line)
}

func readLine(conn net.Conn) (string, error) {
	if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(line, "\n"), nil
}

// bounded runs fn under CascadeTimeout and reports a deadline as fatal to Serve.
func (h *Hub) bounded(ctx context.Context, fn func(context.Context) error) error {
	boundedCtx, cancel := context.WithTimeout(ctx, CascadeTimeout)
	defer cancel()
	err := fn(boundedCtx)
	if ctx.Err() == nil && errors.Is(boundedCtx.Err(), context.DeadlineExceeded) {
		h.reportFatal(oops.With("timeout", CascadeTimeout).Wrapf(context.DeadlineExceeded, "daemon work wedged"))
	}
	return err
}
