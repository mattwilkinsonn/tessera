package effects

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsQuiet(t *testing.T) {
	t.Run("age exactly equal to the window counts as quiet", func(t *testing.T) {
		if !IsQuiet(time.Unix(103, 0), time.Unix(100, 0), 3*time.Second) {
			t.Fatal("age equal to window was not quiet")
		}
	})
	t.Run("age one second under the window is not quiet", func(t *testing.T) {
		if IsQuiet(time.Unix(102, 0), time.Unix(100, 0), 3*time.Second) {
			t.Fatal("age below window was quiet")
		}
	})
}

func TestIsFlexQuiet(t *testing.T) {
	t.Run("own-quiet AND display-quiet → quiet", func(t *testing.T) {
		if !IsFlexQuiet(time.Unix(200, 0), time.Unix(198, 0), time.Unix(197, 0)) {
			t.Fatal("both quiet windows satisfied, got false")
		}
	})
	t.Run("own-quiet but display NOT quiet → keep waiting (H7)", func(t *testing.T) {
		if IsFlexQuiet(time.Unix(200, 0), time.Unix(198, 0), time.Unix(198, 0)) {
			t.Fatal("display window not quiet, got true")
		}
	})
	t.Run("own NOT quiet (even with display quiet) → keep waiting", func(t *testing.T) {
		if IsFlexQuiet(time.Unix(200, 0), time.Unix(199, 0), time.Unix(190, 0)) {
			t.Fatal("flex window not quiet, got true")
		}
	})
}

func TestStampFile(t *testing.T) {
	t.Run("writeStamp then readStamp round-trips a recent epoch second", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "s")
		before := time.Now().Unix()
		if err := WriteStamp(path); err != nil {
			t.Fatal(err)
		}
		got, ok, err := ReadStamp(path)
		if err != nil || !ok || got < before || got > before+2 {
			t.Fatalf("ReadStamp() = %d, %t, %v; want a recent epoch second", got, ok, err)
		}
	})
	t.Run("a missing file reads as null (the bash `cat || echo 0` absence)", func(t *testing.T) {
		_, ok, err := ReadStamp(filepath.Join(t.TempDir(), "nope"))
		if err != nil || ok {
			t.Fatalf("ReadStamp() = present %t, %v; want absent, nil", ok, err)
		}
	})
	t.Run("a non-numeric file reads as null, not NaN", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "junk")
		if err := os.WriteFile(path, []byte("not-a-number\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, ok, err := ReadStamp(path)
		if err != nil || ok {
			t.Fatalf("ReadStamp() = present %t, %v; want absent, nil", ok, err)
		}
	})
}

func TestSignalsSuppressed(t *testing.T) {
	t.Run("no guard file → not suppressed", func(t *testing.T) {
		got, err := SignalsSuppressed(filepath.Join(t.TempDir(), "g"), time.Unix(100, 0))
		if err != nil || got {
			t.Fatalf("SignalsSuppressed() = %t, %v; want false, nil", got, err)
		}
	})
	t.Run("freshly stamped guard suppresses signals", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "g")
		if err := SuppressSignals(path); err != nil {
			t.Fatal(err)
		}
		got, err := SignalsSuppressed(path, time.Now())
		if err != nil || !got {
			t.Fatalf("SignalsSuppressed() = %t, %v; want true, nil", got, err)
		}
	})
	t.Run("a guard just under the TTL still suppresses", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "g")
		if err := os.WriteFile(path, []byte("100\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := SignalsSuppressed(path, time.Unix(100+int64(guardTTL/time.Second)-1, 0))
		if err != nil || !got {
			t.Fatalf("SignalsSuppressed() = %t, %v; want true, nil", got, err)
		}
	})
	t.Run("a guard at/over the TTL expires, reads false, and is removed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "g")
		if err := os.WriteFile(path, []byte("100\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := SignalsSuppressed(path, time.Unix(100+int64(guardTTL/time.Second), 0))
		if err != nil || got {
			t.Fatalf("SignalsSuppressed() = %t, %v; want false, nil", got, err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expired guard stat error = %v; want not-exist", err)
		}
	})
	t.Run("release clears a held guard", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "g")
		if err := SuppressSignals(path); err != nil {
			t.Fatal(err)
		}
		if err := ReleaseSignals(path); err != nil {
			t.Fatal(err)
		}
		got, err := SignalsSuppressed(path, time.Now())
		if err != nil || got {
			t.Fatalf("SignalsSuppressed() = %t, %v; want false, nil", got, err)
		}
	})
	t.Run("a symlink at the guard path is refused and its target untouched", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		if err := os.WriteFile(target, []byte("keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "g")
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if err := SuppressSignals(path); err == nil {
			t.Fatal("SuppressSignals() through a symlink = nil; want an error")
		}
		got, err := os.ReadFile(target)
		if err != nil || string(got) != "keep\n" {
			t.Fatalf("symlink target = %q, %v; want %q untouched", got, err, "keep\n")
		}
	})
}

func TestFlexOrder(t *testing.T) {
	t.Run("write then read round-trips the order", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "laptop-flex-order")
		want := []string{"arc", "obsidian", "lap-arc-5"}
		if err := WriteFlexOrder(want, path); err != nil {
			t.Fatal(err)
		}
		got, err := ReadFlexOrder(path)
		if err != nil || len(got) != len(want) {
			t.Fatalf("ReadFlexOrder() = %v, %v; want %v", got, err, want)
		}
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("ReadFlexOrder() = %v, want %v", got, want)
			}
		}
	})
	t.Run("a missing file reads as empty (first run, before any touch)", func(t *testing.T) {
		got, err := ReadFlexOrder(filepath.Join(t.TempDir(), "nope"))
		if err != nil || len(got) != 0 {
			t.Fatalf("ReadFlexOrder() = %v, %v; want empty, nil", got, err)
		}
	})
	t.Run("blank lines are dropped on read", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(path, []byte("arc\n\nobsidian\n\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := ReadFlexOrder(path)
		if err != nil || len(got) != 2 || got[0] != "arc" || got[1] != "obsidian" {
			t.Fatalf("ReadFlexOrder() = %v, %v; want [arc obsidian]", got, err)
		}
	})
	t.Run("write is one slug per line with a trailing newline", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "f")
		if err := WriteFlexOrder([]string{"a", "b"}, path); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != "a\nb\n" {
			t.Fatalf("file = %q, %v; want %q", got, err, "a\nb\n")
		}
	})
	t.Run("writing an empty order truncates to an empty file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "f")
		if err := WriteFlexOrder([]string{"x"}, path); err != nil {
			t.Fatal(err)
		}
		if err := WriteFlexOrder([]string{}, path); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || len(got) != 0 {
			t.Fatalf("file = %q, %v; want empty", got, err)
		}
		order, err := ReadFlexOrder(path)
		if err != nil || len(order) != 0 {
			t.Fatalf("ReadFlexOrder() = %v, %v; want empty, nil", order, err)
		}
	})
	t.Run("write creates the parent cache dir if absent", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nested", "deeper", "laptop-flex-order")
		if err := WriteFlexOrder([]string{"arc"}, path); err != nil {
			t.Fatal(err)
		}
		got, err := ReadFlexOrder(path)
		if err != nil || len(got) != 1 || got[0] != "arc" {
			t.Fatalf("ReadFlexOrder() = %v, %v; want [arc]", got, err)
		}
	})
}
