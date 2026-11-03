//go:build unix

package commands

// commandsTempDir keeps socket fixtures under the Unix socket path limit.
func commandsTempDir() string {
	return "/tmp"
}
