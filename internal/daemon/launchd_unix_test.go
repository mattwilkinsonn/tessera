//go:build unix

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func TestDefaultServices(t *testing.T) {
	dir := filepath.Join("/home/u", "Library", "LaunchAgents")
	yabaiLegacy := Service{Label: "com.koekeishiya.yabai", Plist: filepath.Join(dir, "com.koekeishiya.yabai.plist")}
	skhdLegacy := Service{Label: "com.koekeishiya.skhd", Plist: filepath.Join(dir, "com.koekeishiya.skhd.plist")}
	assert.DeepEqual(t, DefaultServices("/home/u"), []Candidates{
		{yabai, yabaiLegacy},
		{skhd, skhdLegacy},
	})
}

func TestExecLaunchctlMarksNotLoaded(t *testing.T) {
	stub := filepath.Join(t.TempDir(), "launchctl")
	assert.NilError(t, os.WriteFile(stub, []byte("#!/bin/sh\necho 'Could not find service'\nexit 113\n"), 0o700))
	err := ExecLaunchctl{Path: stub}.Run(t.Context(), "bootout", "gui/501/com.example")
	assert.Assert(t, errors.Is(err, ErrNotLoaded), "Run() = %v", err)
}
