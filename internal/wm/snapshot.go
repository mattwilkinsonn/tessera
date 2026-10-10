package wm

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
)

// Snapshot captures one point-in-time view of the window world.
func Snapshot(ctx context.Context, driver Driver) (World, error) {
	var world World
	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error {
		windows, err := driver.QueryWindows(ctx)
		if err != nil {
			return fmt.Errorf("query windows: %w", err)
		}
		world.Windows = windows
		return nil
	})
	group.Go(func() error {
		spaces, err := driver.QuerySpaces(ctx)
		if err != nil {
			return fmt.Errorf("query spaces: %w", err)
		}
		world.Spaces = spaces
		return nil
	})
	group.Go(func() error {
		displays, err := driver.QueryDisplays(ctx)
		if err != nil {
			return fmt.Errorf("query displays: %w", err)
		}
		world.Displays = displays
		return nil
	})
	if err := group.Wait(); err != nil {
		return World{}, err
	}
	return world, nil
}
