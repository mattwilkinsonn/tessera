package wm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

const StackWindowCap = 24

var ErrSpaceFull = errors.New("space is full")

func CanStack(ctx context.Context, driver QueryOps, anchorID, windowID int) (bool, error) {
	windows, err := driver.QueryWindows(ctx)
	if err != nil {
		return false, fmt.Errorf("checking stack capacity: %w", err)
	}
	for _, window := range windows {
		if window.ID == anchorID {
			return spaceHasCapacity(windows, window.SpaceID, windowID, StackWindowCap-1)
		}
	}
	return false, nil
}

func CanAddToSpace(ctx context.Context, driver QueryOps, space SpaceID, windowID int) (bool, error) {
	windows, err := driver.QueryWindows(ctx)
	if err != nil {
		return false, fmt.Errorf("checking space capacity: %w", err)
	}
	return spaceHasCapacity(windows, space, windowID, StackWindowCap-1)
}

// Count the whole space because yabai stack members can report different frames.
func spaceHasCapacity(windows []Window, space SpaceID, windowID, maximum int) (bool, error) {
	size := 0
	for _, window := range windows {
		if window.ID != windowID && !window.Floating && !window.Minimized && window.SpaceID == space {
			size++
		}
	}
	if size > maximum {
		slog.Warn("holding window outside full stack", "window", windowID, "stack_size", size)
		return false, fmt.Errorf("window %d, space %s, managed count %d: %w", windowID, space, size, ErrSpaceFull)
	}
	return true, nil
}

func CheckStackConversion(ctx context.Context, driver QueryOps, space SpaceID) error {
	windows, err := driver.QueryWindows(ctx)
	if err != nil {
		return fmt.Errorf("checking stack conversion: %w", err)
	}
	_, err = spaceHasCapacity(windows, space, 0, StackWindowCap)
	return err
}
