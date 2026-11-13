//go:build darwin

package main

import (
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/wm/yabai"
)

// newBackend composes yabai, sketchybar and the launchd agents.
//
//nolint:unparam // The caller handles errors from platform backend setup.
func newBackend() (backend, error) {
	return backend{
		Driver:    yabai.New(yabaiPath()),
		Nudge:     effects.NudgeSketchybar,
		Bootstrap: true,
	}, nil
}
