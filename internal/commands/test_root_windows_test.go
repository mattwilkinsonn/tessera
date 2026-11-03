//go:build windows

package commands

import "os"

// commandsTempDir uses the per-user temp dir; Windows has no /tmp.
func commandsTempDir() string {
	return os.TempDir()
}
