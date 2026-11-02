//go:build unix

package effects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// SuppressSignals stamps the signal guard with the current epoch second.
// It writes only a regular, single-link file the caller owns, since /tmp is shared; it never blocks on a FIFO.
func SuppressSignals(path string) (err error) {
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("opening signal guard: %w", err)
	}
	defer func() {
		if closeErr := syscall.Close(fd); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing signal guard: %w", closeErr))
		}
	}()
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return fmt.Errorf("stat signal guard: %w", err)
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG || st.Nlink != 1 || int64(st.Uid) != int64(os.Getuid()) {
		return fmt.Errorf("signal guard %s is not a regular single-link file owned by this user", path)
	}
	if err := syscall.SetNonblock(fd, false); err != nil {
		return fmt.Errorf("clearing nonblock on signal guard: %w", err)
	}
	if err := syscall.Ftruncate(fd, 0); err != nil {
		return fmt.Errorf("truncating signal guard: %w", err)
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10) + "\n"
	n, err := syscall.Write(fd, []byte(stamp))
	if err != nil {
		return fmt.Errorf("writing signal guard: %w", err)
	}
	if n != len(stamp) {
		return fmt.Errorf("writing signal guard: short write of %d/%d bytes", n, len(stamp))
	}
	return nil
}

func signalGuardPath() string {
	return filepath.Join("/tmp", "yabai-apply.guard")
}
