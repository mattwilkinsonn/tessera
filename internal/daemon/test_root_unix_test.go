//go:build unix

package daemon

// daemonTempDir keeps socket fixtures under the Unix socket path limit.
func daemonTempDir() string {
	return "/tmp"
}
