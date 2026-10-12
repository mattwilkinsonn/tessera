package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/gofrs/flock"
)

// ErrRunning reports that another daemon holds the socket lock.
var ErrRunning = errors.New("daemon already running")

// Listen takes the <path>.lock flock, removes any leftover socket file, and binds path.
// The lock is held until the returned listener is closed.
func Listen(ctx context.Context, path string) (net.Listener, error) {
	ln, err := listen(ctx, path)
	if err != nil {
		return nil, err
	}
	return ln, nil
}

func listen(ctx context.Context, path string) (*lockedListener, error) {
	lock := flock.New(path + ".lock")
	locked, err := lock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("locking daemon socket: %w", err)
	}
	if !locked {
		return nil, ErrRunning
	}
	//nolint:gosec // The socket path is caller-selected and unlinked only under its lock.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, errors.Join(fmt.Errorf("removing stale daemon socket: %w", err), lock.Unlock())
	}
	var config net.ListenConfig
	ln, err := config.Listen(ctx, "unix", path)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("binding daemon socket: %w", err), lock.Unlock())
	}
	unix, ok := ln.(*net.UnixListener)
	if !ok {
		return nil, errors.Join(fmt.Errorf("unexpected listener %T", ln), ln.Close(), lock.Unlock())
	}
	unix.SetUnlinkOnClose(false)
	return &lockedListener{UnixListener: unix, path: path, lock: lock}, nil
}

// lockedListener unlinks the socket first and releases the lock second on Close.
type lockedListener struct {
	*net.UnixListener

	path     string
	lock     *flock.Flock
	sockOnce sync.Once
	sockErr  error
	lockOnce sync.Once
	lockErr  error
}

func (l *lockedListener) Close() error {
	sockErr := l.closeSocket()
	l.lockOnce.Do(func() {
		if err := l.lock.Unlock(); err != nil {
			l.lockErr = fmt.Errorf("releasing daemon lock: %w", err)
		}
	})
	return errors.Join(sockErr, l.lockErr)
}

// closeSocket unlinks and closes the socket but keeps the lock.
func (l *lockedListener) closeSocket() error {
	l.sockOnce.Do(func() {
		var errs []error
		if err := os.Remove(l.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("unlinking daemon socket: %w", err))
		}
		if err := l.UnixListener.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing daemon socket: %w", err))
		}
		l.sockErr = errors.Join(errs...)
	})
	return l.sockErr
}

// socketOnly lets Serve close the socket while Run keeps the lock through bootout.
type socketOnly struct{ *lockedListener }

func (s socketOnly) Close() error { return s.closeSocket() }
