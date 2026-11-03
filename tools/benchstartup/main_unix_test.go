//go:build unix

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Wall-clock bars depend on host load, so this checks the table and treats a bar miss as data.
func TestRunComparesBothBinariesAndReportsStartupBars(t *testing.T) {
	root := t.TempDir()
	base := writeBinary(t, root, "base")
	current := writeBinary(t, root, "current")
	var stdout, stderr bytes.Buffer
	if err := run(t.Context(), []string{"-n", "2", base, current}, &stdout, &stderr); err != nil && !errors.Is(err, errStartupBar) {
		t.Fatalf("run() error = %v; stderr=%q", err, stderr.String())
	}
	for _, expected := range []string{
		"| base | `snap 3col` |",
		"| base | `focus east` |",
		"| new | `snap 3col` |",
		"| new | `focus east` |",
		"| new | `snap --help` |",
		"Hot-path p50 ratio",
		"hot-path p50 ratio <= 2.0x",
		"hot-path p90 <= 45 ms",
		"help p50 <= 60 ms",
		"PASS binary size <= 70 MB",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("output %q lacks %q", stdout.String(), expected)
		}
	}
}

func TestRunRejectsBinaryThatDoesNotInvokeTESSYABAI(t *testing.T) {
	root := t.TempDir()
	base := writeBinary(t, root, "base")
	current := writeBinary(t, root, "no-yabai")
	if err := os.WriteFile(current, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	err := run(t.Context(), []string{"-n", "1", base, current}, &bytes.Buffer{}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "did not invoke TESS_YABAI") {
		t.Fatalf("run() error = %v; want invalid comparison error", err)
	}
}

func TestProbeRunsFromMarkerDirectory(t *testing.T) {
	dir := t.TempDir()
	wantDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "marker")
	path := filepath.Join(dir, "probe")
	body := "#!/bin/sh\nprintf '%s' \"$PWD\" > \"$TESS_BENCH_MARKER\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := probe(t.Context(), path, nil, dir, "/bin/false", marker); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != wantDir {
		t.Fatalf("probe working directory = %q; want %q", got, wantDir)
	}
}

func writeBinary(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	body := "#!/bin/sh\nif [ \"$1\" = snap ] && [ \"$2\" = 3col ]; then\n  \"$TESS_YABAI\" -m query --windows\n  exit 0\nfi\nif [ \"$1\" = --help ] || [ \"$1\" = snap ]; then\n  echo help\n  exit 0\nfi\nexit 0\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return path
}
