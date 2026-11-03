//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/mattwilkinsonn/tessera/internal/wm/komorebi"
)

func (r *runner) checkApplyState(ctx context.Context, spaces spikeSpaces) error {
	if err := r.focusSpace(ctx, spaces, 0); err != nil {
		return err
	}
	state, err := r.state(ctx)
	if err != nil {
		return err
	}
	root, err := decodeObject(state)
	if err != nil {
		return fmt.Errorf("decoding guarded ApplyState input: %w", err)
	}
	unknownValue, err := json.Marshal(map[string]string{"preserve": spaces.spaces[0].name})
	if err != nil {
		return fmt.Errorf("encoding unknown ApplyState member: %w", err)
	}
	root["spike_unknown"] = jsontext.Value(unknownValue)
	state, err = json.Marshal(root)
	if err != nil {
		return fmt.Errorf("encoding guarded ApplyState input: %w", err)
	}
	modified, err := reorderAndRemoveSpikeWorkspace(state, spaces, 0, 1, 2)
	if err != nil {
		return err
	}
	transformed, err := decodeObject(modified)
	if err != nil {
		return fmt.Errorf("decoding guarded ApplyState output: %w", err)
	}
	unknownPreserved := bytes.Equal(transformed["spike_unknown"], unknownValue)
	if err := r.prompt("Check 2 uses only three empty workspaces created by this program. Do not put personal windows there."); err != nil {
		return err
	}
	if err := r.send(ctx, komorebi.ApplyState(modified)); err != nil {
		return err
	}
	after, err := r.state(ctx)
	if err != nil {
		return err
	}
	_, workspaces, err := monitorWorkspacesFor(after, spaces)
	if err != nil {
		return err
	}
	firstIndex := findWorkspaceIndex(workspaces, spaces.spaces[1].name)
	secondIndex := findWorkspaceIndex(workspaces, spaces.spaces[0].name)
	reordered := firstIndex >= 0 && secondIndex == firstIndex+1
	thirdExists := hasWorkspace(after, spaces.spaces[2].name)
	passed := reordered && !thirdExists
	r.record("2", fmt.Sprintf("reordered=%t removed=%t", reordered, !thirdExists), passed, "only the three newly created empty spike workspaces were edited")
	r.record("2 unknown", fmt.Sprintf("unknown_member_preserved=%t", unknownPreserved), unknownPreserved, "a unique unknown member present before transformation must survive in the ApplyState payload")
	if err := r.checkClosedWindow(ctx, spaces); err != nil {
		return err
	}
	if err := r.checkNewManagedWindow(ctx, spaces); err != nil {
		return err
	}
	if err := r.checkMonitorHotplug(ctx); err != nil {
		return err
	}
	focusDestination, err := r.readObservationText("Check 2d: record where focus landed after ApplyState. Use only the spike workspace number or outside.")
	if err != nil {
		return err
	}
	flicker, err := r.readObservationText("Check 2d: record visible flicker or focus changes during ApplyState. Do not record titles.")
	if err != nil {
		return err
	}
	r.record("2d", fmt.Sprintf("focus_destination=%s; flicker=%s", focusDestination, flicker), true, "separate manual observations; no titles recorded")
	return r.checkDroppedMessage(ctx, spaces)
}

func (r *runner) checkClosedWindow(ctx context.Context, spaces spikeSpaces) error {
	if err := r.focusSpace(ctx, spaces, 0); err != nil {
		return err
	}
	before, err := r.state(ctx)
	if err != nil {
		return err
	}
	windowIDs := workspaceWindowIDs(before, spaces.spaces[0].name)
	if len(windowIDs) == 0 {
		r.record("2a", "no test window in the spike workspace", false, "add a disposable test window only to the named spike workspace and rerun check 2a")
		return nil
	}
	if err := r.prompt("Close one disposable test window in the focused spike workspace, then press Enter."); err != nil {
		return err
	}
	closedState, err := r.state(ctx)
	if err != nil {
		return err
	}
	closedIDs := workspaceWindowIDs(closedState, spaces.spaces[0].name)
	if len(closedIDs) >= len(windowIDs) {
		r.record("2a", "test window remained managed after close", false, "close one test window before applying the stale State")
		return nil
	}
	fresh, err := r.state(ctx)
	if err != nil {
		return err
	}
	payload, safe, err := prepareStaleWorkspaceApply(before, fresh, spaces, 0)
	if err != nil {
		return err
	}
	if !safe {
		r.record("2a", "outside_spike_workspaces_changed=true; ApplyState=aborted", false, "do not apply a stale full State after unrelated changes")
		return nil
	}
	if err := r.send(ctx, komorebi.ApplyState(payload)); err != nil {
		return err
	}
	after, err := r.state(ctx)
	if err != nil {
		return err
	}
	unchanged := equalStrings(workspaceWindowIDs(after, spaces.spaces[0].name), closedIDs)
	r.record("2a", fmt.Sprintf("post_close_window_ids_unchanged=%t", unchanged), unchanged, "ApplyState must be ignored when a sent window has closed")
	return nil
}

func (r *runner) checkNewManagedWindow(ctx context.Context, spaces spikeSpaces) error {
	if err := r.focusSpace(ctx, spaces, 1); err != nil {
		return err
	}
	before, err := r.state(ctx)
	if err != nil {
		return err
	}
	if err := r.prompt("Open one disposable app window on the focused spike workspace. Wait until komorebi manages it, then press Enter."); err != nil {
		return err
	}
	newID, err := r.waitForNewWindow(ctx, before, spaces.spaces[1].name)
	if err != nil {
		return err
	}
	fresh, err := r.state(ctx)
	if err != nil {
		return err
	}
	payload, safe, err := prepareStaleWorkspaceApply(before, fresh, spaces, 1)
	if err != nil {
		return err
	}
	if !safe {
		r.record("2b", "outside_spike_workspaces_changed=true; ApplyState=aborted", false, "do not apply a stale full State after unrelated changes")
		return nil
	}
	if err := r.send(ctx, komorebi.ApplyState(payload)); err != nil {
		return err
	}
	after, err := r.state(ctx)
	if err != nil {
		return err
	}
	preserved := workspaceHasWindowID(after, spaces.spaces[1].name, newID)
	r.record("2b", fmt.Sprintf("new_window_still_managed=%t", preserved), preserved, "a newly managed window must remain after ApplyState")
	return nil
}

func (r *runner) checkMonitorHotplug(ctx context.Context) error {
	before, err := r.state(ctx)
	if err != nil {
		return err
	}
	beforeCount := monitorCount(before)
	if err := r.prompt("Connect or disconnect one monitor now, then press Enter. Keep test windows inside spike workspaces."); err != nil {
		return err
	}
	changed, err := r.state(ctx)
	if err != nil {
		return err
	}
	changedCount := monitorCount(changed)
	if changedCount == beforeCount {
		r.record("2c", fmt.Sprintf("monitor_count=%d (unchanged)", beforeCount), false, "the monitor count must differ for this check")
		return nil
	}
	if err := r.send(ctx, komorebi.ApplyState(before)); err != nil {
		return err
	}
	after, err := r.state(ctx)
	if err != nil {
		return err
	}
	preserved := monitorCount(after) == changedCount
	r.record("2c", fmt.Sprintf("before=%d hotplug=%d after_apply=%d", beforeCount, changedCount, monitorCount(after)), preserved, "ApplyState must be ignored when monitor counts differ")
	return nil
}

func (r *runner) checkDroppedMessage(ctx context.Context, spaces spikeSpaces) error {
	state, err := r.state(ctx)
	if err != nil {
		return err
	}
	_, workspaces, err := monitorWorkspacesFor(state, spaces)
	if err != nil {
		return err
	}
	index := findWorkspaceIndex(workspaces, spaces.spaces[0].name)
	if index < 0 {
		return errors.New("check 2e could not find its spike workspace")
	}
	beforeLayout := workspaceLayout(workspaces.Elements[index])
	layout := komorebi.LayoutColumns
	if string(beforeLayout) == `"Columns"` {
		layout = komorebi.LayoutRows
	}
	if err := r.prompt("Pause komorebi with its normal control. The program sends WorkspaceLayout only to its spike workspace."); err != nil {
		return err
	}
	if _, err := r.sendIndexedWorkspace(ctx, spaces, 0, func(monitorIndex, workspaceIndex int) komorebi.SocketMessage {
		return komorebi.WorkspaceLayout(monitorIndex, workspaceIndex, layout)
	}); err != nil {
		return err
	}
	after, err := r.state(ctx)
	if err != nil {
		return err
	}
	_, workspaces, err = monitorWorkspacesFor(after, spaces)
	if err != nil {
		return err
	}
	index = findWorkspaceIndex(workspaces, spaces.spaces[0].name)
	dropped := index >= 0 && bytes.Equal(workspaceLayout(workspaces.Elements[index]), beforeLayout)
	r.record("2e", fmt.Sprintf("layout_unchanged=%t", dropped), dropped, "a paused message is dropped and State confirms no change")
	return r.prompt("Resume komorebi with its normal control, then press Enter.")
}

func (r *runner) checkRatios(ctx context.Context, spaces spikeSpaces) error {
	if err := r.focusSpace(ctx, spaces, 0); err != nil {
		return err
	}
	if err := r.prompt("Put at least two disposable windows in the focused spike workspace."); err != nil {
		return err
	}
	if err := r.send(ctx, komorebi.LayoutRatios([]float32{0.7}, []float32{})); err != nil {
		return err
	}
	if err := r.observe("3b setup", "LayoutRatios([0.7], [])", "Observe and record the known non-equal primary width before testing null ratios.", "known non-equal ratio established"); err != nil {
		return err
	}
	if err := r.send(ctx, komorebi.LayoutRatios(nil, nil)); err != nil {
		return err
	}
	if err := r.observe("3b", "LayoutRatios(null, null)", "Observe whether null ratios keep the known non-equal widths. Record y or n.", "manual observation"); err != nil {
		return err
	}
	if err := r.send(ctx, komorebi.LayoutRatios([]float32{}, []float32{})); err != nil {
		return err
	}
	return r.observe("3a", "LayoutRatios([], [])", "Observe whether empty ratios give equal widths. Record y or n.", "manual observation")
}
func (r *runner) checkAppendBehavior(ctx context.Context, spaces spikeSpaces) error {
	if err := r.focusSpace(ctx, spaces, 0); err != nil {
		return err
	}
	state, err := r.state(ctx)
	if err != nil {
		return err
	}
	_, workspaces, err := monitorWorkspaces(state, spaces.monitorIndex)
	if err != nil {
		return err
	}
	index := findWorkspaceIndex(workspaces, spaces.spaces[0].name)
	if index < 0 {
		return errors.New("check 4 could not find its spike workspace")
	}
	current := workspaceBehaviour(workspaces.Elements[index])
	if current == "" {
		root, err := decodeObject(state)
		if err != nil {
			return fmt.Errorf("decoding global window behavior: %w", err)
		}
		current = rawString(root["new_window_behaviour"])
	}
	if current != "Append" {
		if err := r.send(ctx, komorebi.ToggleWorkspaceWindowContainerBehaviour()); err != nil {
			return err
		}
	}
	state, err = r.state(ctx)
	if err != nil {
		return err
	}
	_, workspaces, err = monitorWorkspaces(state, spaces.monitorIndex)
	if err != nil {
		return err
	}
	index = findWorkspaceIndex(workspaces, spaces.spaces[0].name)
	if index < 0 || workspaceBehaviour(workspaces.Elements[index]) != "Append" {
		r.record("4", "Append could not be selected on the spike workspace", false, "do not test window placement until Append is confirmed")
		return nil
	}
	if err := r.prompt("Open two disposable app windows on the focused spike workspace."); err != nil {
		return err
	}
	if err := r.waitForWindowCount(ctx, spaces.spaces[0].name, 2); err != nil {
		return err
	}
	state, err = r.state(ctx)
	if err != nil {
		return err
	}
	_, workspaces, err = monitorWorkspaces(state, spaces.monitorIndex)
	if err != nil {
		return err
	}
	index = findWorkspaceIndex(workspaces, spaces.spaces[0].name)
	if index < 0 {
		return errors.New("check 4 lost its spike workspace")
	}
	containers, windows := workspaceContainerCounts(workspaces.Elements[index])
	passed := containers == 1 && windows >= 2
	r.record("4", fmt.Sprintf("containers=%d windows=%d", containers, windows), passed, "new windows should append to their home stack")
	return nil
}
