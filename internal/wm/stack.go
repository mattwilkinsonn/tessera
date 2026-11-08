package wm

import (
	"context"
	"fmt"
	"log/slog"
)

const StackWindowCap = 24

func CanStack(ctx context.Context, driver QueryOps, anchorID, windowID int) (bool, error) {
	windows, err := driver.QueryWindows(ctx)
	if err != nil {
		return false, fmt.Errorf("checking stack capacity: %w", err)
	}
	for _, window := range windows {
		if window.ID == anchorID {
			return spaceHasCapacity(windows, window.SpaceID, windowID), nil
		}
	}
	return false, nil
}

func CanAddToSpace(ctx context.Context, driver QueryOps, space SpaceID, windowID int) (bool, error) {
	windows, err := driver.QueryWindows(ctx)
	if err != nil {
		return false, fmt.Errorf("checking space capacity: %w", err)
	}
	return spaceHasCapacity(windows, space, windowID), nil
}

// Count the whole space because yabai stack members can report different frames.
func spaceHasCapacity(windows []Window, space SpaceID, windowID int) bool {
	size := 0
	for _, window := range windows {
		if window.ID != windowID && !window.Floating && !window.Minimized && window.SpaceID == space {
			size++
		}
	}
	if size >= StackWindowCap {
		slog.Warn("holding window outside full stack", "window", windowID, "stack_size", size)
		return false
	}
	return true
}
