//go:build windows

package daemon

import "os"

// daemonTempDir uses the per-user temp dir; Windows has no /tmp.
func daemonTempDir() string {
	return os.TempDir()
}
