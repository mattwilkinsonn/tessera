package main

import (
	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

// backend is the per-OS composition; backend_other.go and backend_windows.go define newBackend.
type backend struct {
	Driver    wm.Driver
	Nudge     effects.Nudger
	Bootstrap bool // start the WM: launchd agents on macOS, komorebi on Windows
}
