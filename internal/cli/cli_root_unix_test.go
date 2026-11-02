//go:build unix

package cli

// cliTempDir is /tmp so socket fixture paths stay under macOS's path limit.
func cliTempDir() string {
	return "/tmp"
}
