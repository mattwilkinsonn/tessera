//go:build unix

package daemon

import (
	"strings"
	"testing"
	"testing/synctest"

	"gotest.tools/v3/assert"

	"github.com/mattwilkinsonn/tessera/internal/wm"
)

func TestRegisterSignals(t *testing.T) {
	t.Run("no action holds a tess path", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			src := &signalRecorder{}
			assert.NilError(t, RegisterSignals(t.Context(), src, "/tmp/tessera-daemon.sock"))
			assert.DeepEqual(t, src.signals, []recordedSignal{
				{wm.EventDisplayAdded, "tessera-display_added", "echo display_added | /usr/bin/nc -U -w 1 '/tmp/tessera-daemon.sock'"},
				{wm.EventDisplayRemoved, "tessera-display_removed", "echo display_removed | /usr/bin/nc -U -w 1 '/tmp/tessera-daemon.sock'"},
				{wm.EventDisplayMoved, "tessera-display_moved", "echo display_moved | /usr/bin/nc -U -w 1 '/tmp/tessera-daemon.sock'"},
				{wm.EventApplicationLaunched, "tessera-application_launched", "echo application_launched | /usr/bin/nc -U -w 1 '/tmp/tessera-daemon.sock'"},
				{wm.EventApplicationTerminated, "tessera-application_terminated", "echo application_terminated | /usr/bin/nc -U -w 1 '/tmp/tessera-daemon.sock'"},
				{wm.EventWindowCreated, "tessera-window_created", `id="$YABAI_WINDOW_ID"; printf 'window_created %s\n' "$id" | /usr/bin/nc -U -w 1 '/tmp/tessera-daemon.sock'`},
				{wm.EventWindowDestroyed, "tessera-window_destroyed", "echo window_destroyed | /usr/bin/nc -U -w 1 '/tmp/tessera-daemon.sock'"},
				{wm.EventSpaceChanged, "tessera-space_changed", "/opt/homebrew/bin/sketchybar --trigger yabai_spaces_changed"},
				{wm.EventDisplayChanged, "tessera-display_changed", "/opt/homebrew/bin/sketchybar --trigger yabai_spaces_changed"},
			})
			for _, signal := range src.signals {
				assert.Assert(t, !strings.Contains(signal.Action, "tess "), "action %q runs tess", signal.Action)
				assert.Assert(t, signal.Event != wm.EventDockDidRestart)
			}
		})
	})

	t.Run("a relative socket path is written as absolute", func(t *testing.T) {
		src := &signalRecorder{}
		assert.NilError(t, RegisterSignals(t.Context(), src, "daemon.sock"))
		assert.Assert(t, strings.HasSuffix(src.signals[0].Action, "/daemon.sock'"))
		assert.Assert(t, strings.Contains(src.signals[0].Action, "-w 1 '/"))
	})
}
