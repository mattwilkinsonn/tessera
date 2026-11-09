package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

func TestSRI(t *testing.T) {
	if got := sriSHA256([]byte("abc")); got != "sha256-ungWv48Bz+pBQUDeXa4iI7ADYaOWF3qctBD/YfIAFa0=" {
		t.Fatal(got)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func gitTest(t *testing.T, args ...string) string {
	t.Helper()
	b, err := command(t.Context(), "git", args...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func testRepo(t *testing.T) (string, string) {
	t.Helper()
	t.Chdir(t.TempDir())
	gitTest(t, "init")
	gitTest(t, "config", "user.email", "test@example.com")
	gitTest(t, "config", "user.name", "Test")
	writeTestFile(t, "VERSION", "1.2.3\n")
	gitTest(t, "add", "VERSION")
	gitTest(t, "commit", "-m", "version")
	first := gitTest(t, "rev-parse", "HEAD")
	writeTestFile(t, "other", "child\n")
	gitTest(t, "add", "other")
	gitTest(t, "commit", "-m", "child")
	return first, gitTest(t, "rev-parse", "HEAD")
}

func TestPin(t *testing.T) {
	first, child := testRepo(t)
	writeTestFile(t, "asset", "abc")
	if err := pin(t.Context(), "asset", "build-first", first, "1.2.3", "owner/repo", "pin.json"); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"version\": \"1.2.3\",\n  \"rev\": \"" + first + "\",\n  \"url\": \"https://github.com/owner/repo/releases/download/build-first/tess-darwin-arm64.tar.gz\",\n  \"hash\": \"sha256-ungWv48Bz+pBQUDeXa4iI7ADYaOWF3qctBD/YfIAFa0=\"\n}\n"
	if got := string(readTestFile(t, "pin.json")); got != want {
		t.Fatalf("pin = %s, want %s", got, want)
	}
	t.Run("same revision", func(t *testing.T) {
		if err := pin(t.Context(), "asset", "build-first", first, "1.2.3", "owner/repo", "pin.json"); !errors.Is(err, errNoChange) {
			t.Fatalf("got %v, want no change", err)
		}
	})
	t.Run("newer revision", func(t *testing.T) {
		if err := pin(t.Context(), "asset", "build-child", child, "1.2.3", "owner/repo", "pin.json"); err != nil {
			t.Fatal(err)
		}
		var p Pin
		if err := json.Unmarshal(readTestFile(t, "pin.json"), &p); err != nil || p.Rev != child {
			t.Fatalf("pin = %+v, error = %v", p, err)
		}
	})
	t.Run("existing descendant", func(t *testing.T) {
		before := readTestFile(t, "pin.json")
		if err := pin(t.Context(), "asset", "build-first", first, "1.2.3", "owner/repo", "pin.json"); !errors.Is(err, errNoChange) {
			t.Fatalf("got %v, want no change", err)
		}
		if !bytes.Equal(before, readTestFile(t, "pin.json")) {
			t.Fatal("pin changed")
		}
	})
}

func TestOwes(t *testing.T) {
	first, child := testRepo(t)
	for _, tc := range []struct{ name, sha, want string }{
		{name: "version commit", sha: first, want: "stable=true\nversion=1.2.3\n"},
		{name: "child", sha: child, want: "stable=false\nversion=1.2.3\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := run(t.Context(), []string{"owes", "-sha", tc.sha}, &out, ghAPI{}); err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.want {
				t.Fatalf("got %q, want %q", out.String(), tc.want)
			}
		})
	}
}

func TestCask(t *testing.T) {
	dir := t.TempDir()
	tar := filepath.Join(dir, "asset")
	out := filepath.Join(dir, "cask.rb")
	writeTestFile(t, tar, "abc")
	if err := cask(tar, "1.2.3", "owner/repo", out); err != nil {
		t.Fatal(err)
	}
	got := readTestFile(t, out)
	guard := `system_command("/usr/bin/xattr", args: ["-p", "com.apple.quarantine", path], print_stderr: false).success?`
	if !bytes.Contains(got, []byte(guard)) {
		t.Fatal("cask quarantine guard must check success?")
	}
	golden := filepath.Join("testdata", "tessera.rb.golden")
	if *update {
		if err := os.WriteFile(golden, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if want := readTestFile(t, golden); !bytes.Equal(got, want) {
		t.Fatalf("cask differs from golden:\n%s", got)
	}
	for _, version := range []string{"1.2.3", "1.2.2"} {
		t.Run(version, func(t *testing.T) {
			if err := cask(tar, version, "owner/repo", out); !errors.Is(err, errNoChange) {
				t.Fatalf("got %v, want no change", err)
			}
			if !bytes.Equal(got, readTestFile(t, out)) {
				t.Fatal("cask changed")
			}
		})
	}
	t.Run("invalid version without existing cask", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "cask.rb")
		if err := cask(tar, "not-a-version", "owner/repo", out); err == nil {
			t.Fatal("expected invalid version error")
		}
	})
}

func TestProvenance(t *testing.T) {
	dir := t.TempDir()
	tar := filepath.Join(dir, "asset")
	out := filepath.Join(dir, "provenance.json")
	writeTestFile(t, tar, "abc")
	var stdout bytes.Buffer
	args := []string{"provenance", "-tarball", tar, "-commit", "commit", "-source-run", "12", "-source-artifact", "34", "-out", out}
	if err := run(t.Context(), args, &stdout, ghAPI{}); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"commit\": \"commit\",\n  \"sha256\": \"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\",\n  \"source_run\": 12,\n  \"source_artifact\": 34\n}\n"
	if got := string(readTestFile(t, out)); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
