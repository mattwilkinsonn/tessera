//go:build windows

package cli

import "os"

// cliTempDir is the per-user temp dir; Windows has no /tmp.
func cliTempDir() string {
	return os.TempDir()
}
