package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRequiresTwoBinariesAndPositiveSampleCount(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "missing binary", args: []string{}},
		{name: "one binary", args: []string{"base"}},
		{name: "zero count", args: []string{"-n", "0", "base", "new"}},
		{name: "non-integer count", args: []string{"-n", "many", "base", "new"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if err := run(test.args, &bytes.Buffer{}, &stderr); err == nil {
				t.Fatal("run() succeeded; want argument error")
			}
		})
	}
}

func TestRunComparesBothBinariesAndChecksStartupBars(t *testing.T) {
	root := t.TempDir()
	base := writeBinary(t, root, "base")
	current := writeBinary(t, root, "current")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-n", "2", base, current}, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v; stderr=%q", err, stderr.String())
	}
	for _, expected := range []string{
		"| base | `snap 3col` |",
		"| base | `focus east` |",
		"| new | `snap 3col` |",
		"| new | `focus east` |",
		"| new | `snap --help` |",
		"Hot-path p50 ratio",
		"PASS hot-path p50 ratio <= 2.0x",
		"PASS hot-path p90 <= 45 ms",
		"PASS help p50 <= 60 ms",
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
	err := run([]string{"-n", "1", base, current}, &bytes.Buffer{}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "did not invoke TESS_YABAI") {
		t.Fatalf("run() error = %v; want invalid comparison error", err)
	}
}

func writeBinary(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	body := "#!/bin/sh\nif [ \"$1\" = snap ] && [ \"$2\" = 3col ]; then\n  \"$TESS_YABAI\" -m query --windows\n  exit 0\nfi\nif [ \"$1\" = --help ] || [ \"$1\" = snap ]; then\n  echo help\n  exit 0\nfi\nexit 0\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("chmod", "755", path).CombinedOutput(); err != nil {
		t.Fatalf("chmod: %v: %s", err, output)
	}
	return path
}
