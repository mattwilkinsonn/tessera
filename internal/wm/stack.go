package wm

import (
	"context"
	"fmt"
	"log/slog"
)

const StackWindowCap = 24

// CanStack checks live geometry because all members of a yabai stack share a frame.
func CanStack(ctx context.Context, driver QueryOps, anchorID, windowID int) (bool, error) {
	windows, err := driver.QueryWindows(ctx)
	if err != nil {
		return false, fmt.Errorf("checking stack capacity: %w", err)
	}
	var anchor Window
	found := false
	for _, window := range windows {
		if window.ID == anchorID {
			anchor, found = window, true
			break
		}
	}
	if !found {
		return false, nil
	}
	size := 0
	for _, window := range windows {
		if window.ID != windowID && !window.Floating && !window.Minimized && window.SpaceID == anchor.SpaceID && window.Frame == anchor.Frame {
			size++
		}
	}
	if size >= StackWindowCap {
		slog.Warn("holding window outside full stack", "window", windowID, "stack_size", size)
		return false, nil
	}
	return true, nil
}
