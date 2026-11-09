package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var versionLine = regexp.MustCompile(`(?m)^  version "([^"]+)"$`)

func compareVersion(a, b string) (int, error) {
	parse := func(s string) ([]int, string, error) {
		s = strings.TrimPrefix(s, "v")
		s = strings.SplitN(s, "+", 2)[0]
		parts := strings.SplitN(s, "-", 2)
		nums := strings.Split(parts[0], ".")
		if len(nums) != 3 {
			return nil, "", fmt.Errorf("invalid version %q", s)
		}
		v := make([]int, 3)
		for i, n := range nums {
			x, err := strconv.Atoi(n)
			if err != nil || x < 0 {
				return nil, "", fmt.Errorf("invalid version %q", s)
			}
			v[i] = x
		}
		pre := ""
		if len(parts) == 2 {
			pre = parts[1]
		}
		return v, pre, nil
	}
	av, ap, err := parse(a)
	if err != nil {
		return 0, err
	}
	bv, bp, err := parse(b)
	if err != nil {
		return 0, err
	}
	for i := range av {
		if av[i] < bv[i] {
			return -1, nil
		}
		if av[i] > bv[i] {
			return 1, nil
		}
	}
	if ap == bp {
		return 0, nil
	}
	if ap == "" {
		return 1, nil
	}
	if bp == "" {
		return -1, nil
	}
	aa, bb := strings.Split(ap, "."), strings.Split(bp, ".")
	for i := 0; i < len(aa) && i < len(bb); i++ {
		if aa[i] == bb[i] {
			continue
		}
		an, ae := strconv.Atoi(aa[i])
		bn, be := strconv.Atoi(bb[i])
		if ae == nil && be == nil {
			if an < bn {
				return -1, nil
			}
			return 1, nil
		}
		if ae == nil {
			return -1, nil
		}
		if be == nil {
			return 1, nil
		}
		if aa[i] < bb[i] {
			return -1, nil
		}
		return 1, nil
	}
	if len(aa) < len(bb) {
		return -1, nil
	}
	return 1, nil
}
func cask(tar, version, repo, dest string) error {
	old, err := os.ReadFile(dest) //nolint:gosec // The output path is supplied by the CLI caller.
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := compareVersion(version, "0.0.0"); err != nil {
		return err
	}
	if len(old) > 0 {
		m := versionLine.FindSubmatch(old)
		if len(m) != 2 {
			return errors.New("existing cask has no version")
		}
		cmp, err := compareVersion(string(m[1]), version)
		if err != nil {
			return err
		}
		if cmp >= 0 {
			return errNoChange
		}
	}
	b, err := os.ReadFile(tar) //nolint:gosec // The tarball path is supplied by the CLI caller.
	if err != nil {
		return err
	}
	content := fmt.Sprintf(`cask "tessera" do
  version "%s"
  sha256 "%s"

  url "https://github.com/%s/releases/download/v#{version}/tess-darwin-arm64.tar.gz"
  name "Tessera"
  desc "Portable window manager configuration"
  homepage "https://github.com/%s"

  depends_on arch: :arm64
  binary "bin/tess"

  postflight do
    path = "#{staged_path}/bin/tess"
    # A Result object is always truthy, so the guard must test success?.
    system_command! "/usr/bin/xattr", args: ["-d", "com.apple.quarantine", path] if
      system_command("/usr/bin/xattr", args: ["-p", "com.apple.quarantine", path], print_stderr: false).success?
  end

  caveats "This binary is ad-hoc signed, not notarized. Removing quarantine bypasses macOS security protections."
end
`, version, digest(b), repo, repo)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil { //nolint:gosec // The cask directory is committed and must be traversable.
		return err
	}
	return os.WriteFile(dest, []byte(content), 0o644) //nolint:gosec // The cask is a committed file.
}
