//go:build unix

package effects

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mattwilkinsonn/tessera/internal/engine"
	"gotest.tools/v3/assert"
)

// A read-only directory makes the temp-file write fail; Windows ignores directory mode bits.
func TestWriteSlotsFailureLeavesOriginalReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "window-slots.json")
	original := engine.SlotMemory{Seq: 7, Slots: map[engine.SlotKey]engine.WindowRef{
		"desk:g9/arc#1": {ID: 3, App: "Arc", Displays: "g9", Seq: 7},
	}}
	replacement := engine.SlotMemory{Seq: 8, Slots: map[engine.SlotKey]engine.WindowRef{
		"desk:aw/browser#1": {ID: 9, App: "Zen", Displays: "aw,g9", Seq: 8},
	}}
	assert.NilError(t, WriteSlots(path, original))
	assert.NilError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Errorf("restoring directory permissions: %v", err)
		}
	})
	assert.Assert(t, WriteSlots(path, replacement) != nil)
	got, err := ReadSlots(path)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, original)
}
