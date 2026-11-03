//go:build windows

package main

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/wm/komorebi"
	"github.com/mattwilkinsonn/tessera/internal/wm/komorebi/spike/spikestate"
)

func (r *runner) sendFocusedWorkspaceCommand(ctx context.Context, spaces spikeSpaces, slot int, check string, message komorebi.SocketMessage) (bool, error) {
	state, err := r.state(ctx)
	if err != nil {
		return false, err
	}
	focused, err := spikestate.FocusedSpikeWorkspace(state, spaces, slot)
	if err != nil {
		return false, err
	}
	if !focused {
		r.record(check, fmt.Sprintf("focused_spike_workspace=%t; %s=skipped", focused, message.Type), false, "focused-window command skipped because the active monitor and workspace are not the intended spike workspace")
		return false, nil
	}
	if err := r.send(ctx, message); err != nil {
		return false, err
	}
	return true, nil
}

func (r *runner) waitForNewWindow(ctx context.Context, before jsontext.Value, workspace string) (string, error) {
	beforeIDs := make(map[string]struct{})
	for _, identifier := range spikestate.WorkspaceWindowIDs(before, workspace) {
		beforeIDs[identifier] = struct{}{}
	}
	deadline := time.Now().Add(checkTimeout)
	for time.Now().Before(deadline) {
		state, err := r.state(ctx)
		if err != nil {
			return "", err
		}
		for _, identifier := range spikestate.WorkspaceWindowIDs(state, workspace) {
			if _, exists := beforeIDs[identifier]; !exists {
				return identifier, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("waiting for a newly managed spike window: %w", ctx.Err())
		case <-time.After(pollInterval):
		}
	}
	return "", errors.New("timed out waiting for a newly managed window in the spike workspace")
}

func (r *runner) waitForWindowCount(ctx context.Context, workspace string, count int) error {
	deadline := time.Now().Add(checkTimeout)
	for time.Now().Before(deadline) {
		state, err := r.state(ctx)
		if err != nil {
			return err
		}
		if len(spikestate.WorkspaceWindowIDs(state, workspace)) >= count {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for spike workspace windows: %w", ctx.Err())
		case <-time.After(pollInterval):
		}
	}
	return fmt.Errorf("spike workspace did not reach %d managed windows", count)
}

func (r *runner) waitForNoWindows(ctx context.Context, workspace string) error {
	deadline := time.Now().Add(checkTimeout)
	for time.Now().Before(deadline) {
		state, err := r.state(ctx)
		if err != nil {
			return err
		}
		if len(spikestate.WorkspaceWindowIDs(state, workspace)) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for disposable spike windows to close: %w", ctx.Err())
		case <-time.After(pollInterval):
		}
	}
	return errors.New("timed out waiting for disposable spike windows to close")
}

func (r *runner) waitForStableWindowRectangles(ctx context.Context, spaces spikeSpaces, slot int, baseline map[string]map[string]jsontext.Value, started time.Time, timeout time.Duration) (time.Duration, bool, error) {
	if slot < 0 || slot >= len(spaces.Spaces) {
		return 0, false, errors.New("spike workspace slot is out of range")
	}
	deadline := started.Add(timeout)
	var previous map[string]map[string]jsontext.Value
	changed, stableSamples := false, 0
	for {
		state, err := r.state(ctx)
		if err != nil {
			return 0, false, err
		}
		sampledAt := time.Now()
		if !sampledAt.Before(deadline) {
			return sampledAt.Sub(started), false, nil
		}
		rectangles, err := spikestate.WorkspaceRectanglesForState(state, spaces, slot)
		if err != nil {
			return 0, false, err
		}
		switch {
		case len(rectangles) > 0 && !changed && !spikestate.EqualWindowRectangles(baseline, rectangles):
			changed = true
			stableSamples = 0
		case changed && spikestate.EqualWindowRectangles(previous, rectangles):
			stableSamples++
		case changed:
			stableSamples = 0
		}
		if changed && stableSamples >= 2 {
			return sampledAt.Sub(started), true, nil
		}
		if !time.Now().Before(deadline) {
			return time.Since(started), false, nil
		}
		select {
		case <-ctx.Done():
			return 0, false, fmt.Errorf("waiting for spike window rectangles to settle: %w", ctx.Err())
		case <-time.After(min(pollInterval, time.Until(deadline))):
		}
		previous = rectangles
	}
}

func (r *runner) waitForFocusedWorkspaceSince(ctx context.Context, spaces spikeSpaces, slot int, started time.Time, timeout time.Duration) (time.Duration, bool, error) {
	deadline := started.Add(timeout)
	for time.Now().Before(deadline) {
		state, err := r.state(ctx)
		if err != nil {
			return 0, false, err
		}
		focused, err := spikestate.FocusedSpikeWorkspace(state, spaces, slot)
		if err != nil {
			return 0, false, err
		}
		if focused {
			return time.Since(started), true, nil
		}
		select {
		case <-ctx.Done():
			return 0, false, fmt.Errorf("waiting for spike workspace focus: %w", ctx.Err())
		case <-time.After(pollInterval):
		}
	}
	return time.Since(started), false, nil
}
