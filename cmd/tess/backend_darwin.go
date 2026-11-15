//go:build darwin

package main

import (
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/wm/yabai"
)

// newBackend composes yabai, sketchybar and the launchd agents.
func newBackend() backend {
	return backend{
		Driver:    yabai.New(yabaiPath()),
		Nudge:     effects.NudgeSketchybar,
		Bootstrap: true,
	}
}
