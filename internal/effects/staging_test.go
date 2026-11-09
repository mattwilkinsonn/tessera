package effects

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCorruptStagingLedgerIsPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "staged-windows.json")
	if err := os.WriteFile(path, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	ledger, err := ReadStagedWindows(path)
	if err != nil || len(ledger) != 0 {
		t.Fatalf("ledger = %v, error = %v", ledger, err)
	}
	data, err := os.ReadFile(path + ".corrupt")
	if err != nil || string(data) != "broken" {
		t.Fatalf("preserved = %q, error = %v", data, err)
	}
	if strings.Count(output.String(), "level=ERROR") != 1 {
		t.Fatalf("log = %s", output.String())
	}
}
