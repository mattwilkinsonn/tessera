//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"time"
)

// decodeObject decodes a State object without discarding its raw member values.
func decodeObject(value jsontext.Value) (map[string]jsontext.Value, error) {
	if !value.IsValid() {
		return nil, errors.New("invalid JSON object")
	}
	var object map[string]jsontext.Value
	if err := json.Unmarshal(value, &object); err != nil {
		return nil, fmt.Errorf("decoding JSON object: %w", err)
	}
	if object == nil {
		return nil, errors.New("JSON value is not an object")
	}
	return object, nil
}

func monitorWorkspaces(state jsontext.Value, monitorIndex int) (map[string]jsontext.Value, ringValue, error) {
	root, err := decodeObject(state)
	if err != nil {
		return nil, ringValue{}, fmt.Errorf("decoding State: %w", err)
	}
	var monitors ringValue
	if err := json.Unmarshal(root["monitors"], &monitors); err != nil {
		return nil, ringValue{}, fmt.Errorf("decoding monitor ring: %w", err)
	}
	if monitorIndex < 0 || monitorIndex >= len(monitors.Elements) {
		return nil, ringValue{}, fmt.Errorf("monitor index %d is out of range", monitorIndex)
	}
	monitor, err := decodeObject(monitors.Elements[monitorIndex])
	if err != nil {
		return nil, ringValue{}, fmt.Errorf("decoding monitor %d: %w", monitorIndex, err)
	}
	var workspaces ringValue
	if err := json.Unmarshal(monitor["workspaces"], &workspaces); err != nil {
		return nil, ringValue{}, fmt.Errorf("decoding monitor %d workspace ring: %w", monitorIndex, err)
	}
	return monitor, workspaces, nil
}

func workspaceIsEmpty(value jsontext.Value) bool {
	workspace, err := decodeObject(value)
	if err != nil {
		return false
	}
	var containers, floating ringValue
	if err := json.Unmarshal(workspace["containers"], &containers); err != nil {
		return false
	}
	if err := json.Unmarshal(workspace["floating_windows"], &floating); err != nil {
		return false
	}
	hasMonocle := workspace["monocle_container"].IsValid() && !bytes.Equal(workspace["monocle_container"], []byte("null"))
	hasMaximized := workspace["maximized_window"].IsValid() && !bytes.Equal(workspace["maximized_window"], []byte("null"))
	return len(containers.Elements) == 0 && len(floating.Elements) == 0 && !hasMonocle && !hasMaximized
}

func workspaceName(value jsontext.Value) string {
	workspace, err := decodeObject(value)
	if err != nil {
		return ""
	}
	return rawString(workspace["name"])
}

func rawString(value jsontext.Value) string {
	var result string
	if err := json.Unmarshal(value, &result); err != nil {
		return ""
	}
	return result
}

func workspaceLayout(value jsontext.Value) jsontext.Value {
	workspace, err := decodeObject(value)
	if err != nil {
		return nil
	}
	return workspace["layout"]
}

func workspaceBehaviour(value jsontext.Value) string {
	workspace, err := decodeObject(value)
	if err != nil {
		return ""
	}
	return rawString(workspace["window_container_behaviour"])
}

func findWorkspaceIndex(workspaces ringValue, name string) int {
	for index, workspace := range workspaces.Elements {
		if workspaceName(workspace) == name {
			return index
		}
	}
	return -1
}
func hasWorkspace(state jsontext.Value, name string) bool {
	root, err := decodeObject(state)
	if err != nil {
		return false
	}
	var monitors ringValue
	if err := json.Unmarshal(root["monitors"], &monitors); err != nil {
		return false
	}
	for monitorIndex := range monitors.Elements {
		_, workspaces, err := monitorWorkspaces(state, monitorIndex)
		if err == nil && findWorkspaceIndex(workspaces, name) >= 0 {
			return true
		}
	}
	return false
}

func reorderAndRemoveSpikeWorkspace(state jsontext.Value, spaces spikeSpaces, firstSlot, secondSlot, dropSlot int) (jsontext.Value, error) {
	root, monitor, monitors, monitorIndex, workspaces, err := spikeWorkspaceTree(state, spaces)
	if err != nil {
		return nil, err
	}
	if firstSlot < 0 || firstSlot >= len(spaces.spaces) || secondSlot < 0 || secondSlot >= len(spaces.spaces) || dropSlot < 0 || dropSlot >= len(spaces.spaces) {
		return nil, errors.New("spike workspace slot is out of range")
	}
	first, second, drop := spaces.spaces[firstSlot].index, spaces.spaces[secondSlot].index, spaces.spaces[dropSlot].index
	if first < 0 || second < 0 || drop < 0 || first >= len(workspaces.Elements) || second >= len(workspaces.Elements) || drop >= len(workspaces.Elements) || first == second || first == drop || second == drop {
		return nil, errors.New("spike workspace index is no longer present")
	}
	if !isSpikeWorkspace(spaces, workspaceName(workspaces.Elements[first])) || !isSpikeWorkspace(spaces, workspaceName(workspaces.Elements[second])) || !isSpikeWorkspace(spaces, workspaceName(workspaces.Elements[drop])) {
		return nil, errors.New("ApplyState edit would include a non-spike workspace")
	}
	firstValue, secondValue := workspaces.Elements[first], workspaces.Elements[second]
	updated := make([]jsontext.Value, 0, len(workspaces.Elements)-1)
	for index, value := range workspaces.Elements {
		switch index {
		case first:
			updated = append(updated, secondValue)
		case second:
			updated = append(updated, firstValue)
		case drop:
		default:
			updated = append(updated, value)
		}
	}
	workspaces.Focused = min(workspaces.Focused, len(updated)-1)
	workspaces.Elements = updated
	return encodeSpikeWorkspaceTree(root, monitor, monitors, monitorIndex, workspaces)
}

func spikeWorkspaceTree(state jsontext.Value, spaces spikeSpaces) (map[string]jsontext.Value, map[string]jsontext.Value, ringValue, int, ringValue, error) {
	root, err := decodeObject(state)
	if err != nil {
		return nil, nil, ringValue{}, 0, ringValue{}, fmt.Errorf("decoding ApplyState input: %w", err)
	}
	var monitors ringValue
	if err := json.Unmarshal(root["monitors"], &monitors); err != nil {
		return nil, nil, ringValue{}, 0, ringValue{}, fmt.Errorf("decoding monitor ring: %w", err)
	}
	monitorIndex := findMonitorByIdentity(monitors, spaces)
	if monitorIndex < 0 {
		return nil, nil, ringValue{}, 0, ringValue{}, errors.New("spike monitor is no longer present")
	}
	monitor, err := decodeObject(monitors.Elements[monitorIndex])
	if err != nil {
		return nil, nil, ringValue{}, 0, ringValue{}, fmt.Errorf("decoding spike monitor: %w", err)
	}
	var workspaces ringValue
	if err := json.Unmarshal(monitor["workspaces"], &workspaces); err != nil {
		return nil, nil, ringValue{}, 0, ringValue{}, fmt.Errorf("decoding workspace ring: %w", err)
	}
	return root, monitor, monitors, monitorIndex, workspaces, nil
}

func encodeSpikeWorkspaceTree(root, monitor map[string]jsontext.Value, monitors ringValue, monitorIndex int, workspaces ringValue) (jsontext.Value, error) {
	var err error
	monitor["workspaces"], err = json.Marshal(workspaces)
	if err != nil {
		return nil, fmt.Errorf("encoding reordered workspace ring: %w", err)
	}
	monitors.Elements[monitorIndex], err = json.Marshal(monitor)
	if err != nil {
		return nil, fmt.Errorf("encoding spike monitor: %w", err)
	}
	root["monitors"], err = json.Marshal(monitors)
	if err != nil {
		return nil, fmt.Errorf("encoding monitor ring: %w", err)
	}
	result, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("encoding ApplyState tree: %w", err)
	}
	return result, nil
}

func restoreWorkspaceValues(state jsontext.Value, spaces spikeSpaces) (jsontext.Value, error) {
	root, err := decodeObject(state)
	if err != nil {
		return nil, fmt.Errorf("decoding current State: %w", err)
	}
	var monitors ringValue
	if err := json.Unmarshal(root["monitors"], &monitors); err != nil {
		return nil, fmt.Errorf("decoding monitor ring: %w", err)
	}
	monitorIndex := findMonitorByIdentity(monitors, spaces)
	if monitorIndex < 0 {
		return nil, errors.New("original spike monitor is not available; refusing to apply a stale full State")
	}
	monitor, err := decodeObject(monitors.Elements[monitorIndex])
	if err != nil {
		return nil, fmt.Errorf("decoding original monitor: %w", err)
	}
	var workspaces ringValue
	if err := json.Unmarshal(monitor["workspaces"], &workspaces); err != nil {
		return nil, fmt.Errorf("decoding current workspace ring: %w", err)
	}
	filtered := make([]jsontext.Value, 0, len(workspaces.Elements))
	for _, workspace := range workspaces.Elements {
		if !isSpikeWorkspace(spaces, workspaceName(workspace)) {
			filtered = append(filtered, workspace)
		}
	}
	base := spaces.originalCount
	if base > len(filtered) {
		return nil, errors.New("workspace ring shrank before spike reset; refusing to overwrite user workspaces")
	}
	restored := make([]jsontext.Value, 0, len(filtered)+len(spaces.spaces))
	restored = append(restored, filtered[:base]...)
	for _, space := range spaces.spaces {
		if len(space.base) == 0 {
			return nil, fmt.Errorf("missing baseline for spike workspace %q", space.name)
		}
		restored = append(restored, space.base)
	}
	restored = append(restored, filtered[base:]...)
	workspaces.Elements = restored
	workspaces.Focused = spaces.originalFocus
	if workspaces.Focused < 0 || workspaces.Focused >= base {
		return nil, errors.New("saved workspace focus is outside the original workspaces")
	}
	monitor["workspaces"], err = json.Marshal(workspaces)
	if err != nil {
		return nil, fmt.Errorf("encoding restored workspace ring: %w", err)
	}
	monitors.Elements[monitorIndex], err = json.Marshal(monitor)
	if err != nil {
		return nil, fmt.Errorf("encoding restored monitor: %w", err)
	}
	root["monitors"], err = json.Marshal(monitors)
	if err != nil {
		return nil, fmt.Errorf("encoding restored monitor ring: %w", err)
	}
	result, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("encoding restored State: %w", err)
	}
	return result, nil
}

func sameMonitor(spaces spikeSpaces, value jsontext.Value) bool {
	monitor, err := decodeObject(value)
	if err != nil {
		return false
	}
	return bytes.Equal(spaces.monitorID, monitor["id"]) && bytes.Equal(spaces.monitorDevice, monitor["device_id"])
}

func findMonitorByIdentity(monitors ringValue, spaces spikeSpaces) int {
	for index, monitor := range monitors.Elements {
		if sameMonitor(spaces, monitor) {
			return index
		}
	}
	return -1
}

func isSpikeWorkspace(spaces spikeSpaces, name string) bool {
	for _, space := range spaces.spaces {
		if space.name == name {
			return true
		}
	}
	return false
}

func monitorWorkspacesFor(state jsontext.Value, spaces spikeSpaces) (int, ringValue, error) {
	root, err := decodeObject(state)
	if err != nil {
		return 0, ringValue{}, fmt.Errorf("decoding State: %w", err)
	}
	var monitors ringValue
	if err := json.Unmarshal(root["monitors"], &monitors); err != nil {
		return 0, ringValue{}, fmt.Errorf("decoding monitor ring: %w", err)
	}
	monitorIndex := findMonitorByIdentity(monitors, spaces)
	if monitorIndex < 0 {
		return 0, ringValue{}, errors.New("original spike monitor is not available")
	}
	_, workspaces, err := monitorWorkspaces(state, monitorIndex)
	if err != nil {
		return 0, ringValue{}, err
	}
	return monitorIndex, workspaces, nil
}
func stateOutsideSpikeWorkspaces(state jsontext.Value, spaces spikeSpaces) (jsontext.Value, error) {
	root, err := decodeObject(state)
	if err != nil {
		return nil, fmt.Errorf("decoding State for spike workspace comparison: %w", err)
	}
	var monitors ringValue
	if err := json.Unmarshal(root["monitors"], &monitors); err != nil {
		return nil, fmt.Errorf("decoding monitor ring for spike workspace comparison: %w", err)
	}
	for monitorIndex, monitorValue := range monitors.Elements {
		monitor, err := decodeObject(monitorValue)
		if err != nil {
			return nil, fmt.Errorf("decoding monitor %d for spike workspace comparison: %w", monitorIndex, err)
		}
		var workspaces ringValue
		if err := json.Unmarshal(monitor["workspaces"], &workspaces); err != nil {
			return nil, fmt.Errorf("decoding monitor %d workspaces for spike workspace comparison: %w", monitorIndex, err)
		}
		if sameMonitor(spaces, monitorValue) && (workspaces.Focused < 0 || workspaces.Focused >= len(workspaces.Elements)) {
			return nil, errors.New("spike monitor focus is out of range during State comparison")
		}
		filtered := make([]jsontext.Value, 0, len(workspaces.Elements))
		focused := -1
		for index, workspace := range workspaces.Elements {
			if sameMonitor(spaces, monitorValue) && isSpikeWorkspace(spaces, workspaceName(workspace)) {
				continue
			}
			if index == workspaces.Focused {
				focused = len(filtered)
			}
			filtered = append(filtered, workspace)
		}
		workspaces.Elements = filtered
		workspaces.Focused = focused
		monitor["workspaces"], err = json.Marshal(workspaces)
		if err != nil {
			return nil, fmt.Errorf("encoding monitor %d workspaces for spike workspace comparison: %w", monitorIndex, err)
		}
		monitors.Elements[monitorIndex], err = json.Marshal(monitor)
		if err != nil {
			return nil, fmt.Errorf("encoding monitor %d for spike workspace comparison: %w", monitorIndex, err)
		}
	}
	root["monitors"], err = json.Marshal(monitors)
	if err != nil {
		return nil, fmt.Errorf("encoding monitor ring for spike workspace comparison: %w", err)
	}
	result, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("encoding State for spike workspace comparison: %w", err)
	}
	return result, nil
}

func sameMonitorTopology(left, right jsontext.Value, spaces spikeSpaces) (bool, error) {
	leftRoot, err := decodeObject(left)
	if err != nil {
		return false, fmt.Errorf("decoding snapshot State: %w", err)
	}
	rightRoot, err := decodeObject(right)
	if err != nil {
		return false, fmt.Errorf("decoding fresh State: %w", err)
	}
	var leftMonitors, rightMonitors ringValue
	if err := json.Unmarshal(leftRoot["monitors"], &leftMonitors); err != nil {
		return false, fmt.Errorf("decoding snapshot monitor ring: %w", err)
	}
	if err := json.Unmarshal(rightRoot["monitors"], &rightMonitors); err != nil {
		return false, fmt.Errorf("decoding fresh monitor ring: %w", err)
	}
	if len(leftMonitors.Elements) != len(rightMonitors.Elements) {
		return false, nil
	}
	leftIndex := findMonitorByIdentity(leftMonitors, spaces)
	rightIndex := findMonitorByIdentity(rightMonitors, spaces)
	return leftIndex >= 0 && rightIndex >= 0 && leftIndex == rightIndex, nil
}

func prepareStaleWorkspaceApply(snapshot, fresh jsontext.Value, spaces spikeSpaces, slot int) (jsontext.Value, bool, error) {
	sameTopology, err := sameMonitorTopology(snapshot, fresh, spaces)
	if err != nil {
		return nil, false, err
	}
	if !sameTopology {
		return nil, false, nil
	}
	snapshotOutside, err := stateOutsideSpikeWorkspaces(snapshot, spaces)
	if err != nil {
		return nil, false, err
	}
	freshOutside, err := stateOutsideSpikeWorkspaces(fresh, spaces)
	if err != nil {
		return nil, false, err
	}
	if !bytes.Equal(snapshotOutside, freshOutside) {
		return nil, false, nil
	}
	if slot < 0 || slot >= len(spaces.spaces) {
		return nil, false, errors.New("spike workspace slot is out of range")
	}

	snapshotRoot, err := decodeObject(snapshot)
	if err != nil {
		return nil, false, fmt.Errorf("decoding stale State: %w", err)
	}
	var snapshotMonitors ringValue
	if err := json.Unmarshal(snapshotRoot["monitors"], &snapshotMonitors); err != nil {
		return nil, false, fmt.Errorf("decoding stale monitor ring: %w", err)
	}
	snapshotMonitorIndex := findMonitorByIdentity(snapshotMonitors, spaces)
	if snapshotMonitorIndex < 0 {
		return nil, false, errors.New("spike monitor is absent from stale State")
	}
	snapshotMonitor, err := decodeObject(snapshotMonitors.Elements[snapshotMonitorIndex])
	if err != nil {
		return nil, false, fmt.Errorf("decoding stale spike monitor: %w", err)
	}
	var snapshotWorkspaces ringValue
	if err := json.Unmarshal(snapshotMonitor["workspaces"], &snapshotWorkspaces); err != nil {
		return nil, false, fmt.Errorf("decoding stale spike workspaces: %w", err)
	}
	workspaceName := spaces.spaces[slot].name
	snapshotWorkspaceIndex := findWorkspaceIndex(snapshotWorkspaces, workspaceName)
	if snapshotWorkspaceIndex < 0 {
		return nil, false, fmt.Errorf("spike workspace %q is absent from stale State", workspaceName)
	}

	freshRoot, err := decodeObject(fresh)
	if err != nil {
		return nil, false, fmt.Errorf("decoding fresh State: %w", err)
	}
	var freshMonitors ringValue
	if err := json.Unmarshal(freshRoot["monitors"], &freshMonitors); err != nil {
		return nil, false, fmt.Errorf("decoding fresh monitor ring: %w", err)
	}
	freshMonitorIndex := findMonitorByIdentity(freshMonitors, spaces)
	if freshMonitorIndex < 0 {
		return nil, false, errors.New("spike monitor is absent from fresh State")
	}
	freshMonitor, err := decodeObject(freshMonitors.Elements[freshMonitorIndex])
	if err != nil {
		return nil, false, fmt.Errorf("decoding fresh spike monitor: %w", err)
	}
	var freshWorkspaces ringValue
	if err := json.Unmarshal(freshMonitor["workspaces"], &freshWorkspaces); err != nil {
		return nil, false, fmt.Errorf("decoding fresh spike workspaces: %w", err)
	}
	freshWorkspaceIndex := findWorkspaceIndex(freshWorkspaces, workspaceName)
	if freshWorkspaceIndex < 0 {
		return nil, false, fmt.Errorf("spike workspace %q is absent from fresh State", workspaceName)
	}
	freshWorkspaces.Elements[freshWorkspaceIndex] = snapshotWorkspaces.Elements[snapshotWorkspaceIndex]
	freshMonitor["workspaces"], err = json.Marshal(freshWorkspaces)
	if err != nil {
		return nil, false, fmt.Errorf("encoding fresh spike workspaces: %w", err)
	}
	freshMonitors.Elements[freshMonitorIndex], err = json.Marshal(freshMonitor)
	if err != nil {
		return nil, false, fmt.Errorf("encoding fresh spike monitor: %w", err)
	}
	freshRoot["monitors"], err = json.Marshal(freshMonitors)
	if err != nil {
		return nil, false, fmt.Errorf("encoding fresh monitor ring: %w", err)
	}
	result, err := json.Marshal(freshRoot)
	if err != nil {
		return nil, false, fmt.Errorf("encoding safe ApplyState payload: %w", err)
	}
	return result, true, nil
}

func workspaceWindowIDs(state jsontext.Value, name string) []string {
	root, err := decodeObject(state)
	if err != nil {
		return nil
	}
	var monitors ringValue
	if err := json.Unmarshal(root["monitors"], &monitors); err != nil {
		return nil
	}
	identifiers := make([]string, 0)
	for monitorIndex := range monitors.Elements {
		_, workspaces, err := monitorWorkspaces(state, monitorIndex)
		if err != nil {
			continue
		}
		index := findWorkspaceIndex(workspaces, name)
		if index >= 0 {
			identifiers = append(identifiers, workspaceWindowIDsFromValue(workspaces.Elements[index])...)
		}
	}
	return identifiers
}

func workspaceWindowIDsFromValue(value jsontext.Value) []string {
	workspace, err := decodeObject(value)
	if err != nil {
		return nil
	}
	identifiers := make([]string, 0)
	var containers ringValue
	if err := json.Unmarshal(workspace["containers"], &containers); err == nil {
		for _, containerValue := range containers.Elements {
			container, err := decodeObject(containerValue)
			if err != nil {
				continue
			}
			var windows ringValue
			if err := json.Unmarshal(container["windows"], &windows); err != nil {
				continue
			}
			for _, window := range windows.Elements {
				identifiers = appendWindowID(identifiers, window)
			}
		}
	}
	var floating ringValue
	if err := json.Unmarshal(workspace["floating_windows"], &floating); err == nil {
		for _, window := range floating.Elements {
			identifiers = appendWindowID(identifiers, window)
		}
	}
	return identifiers
}

func appendWindowID(identifiers []string, value jsontext.Value) []string {
	window, err := decodeObject(value)
	if err != nil || len(window["hwnd"]) == 0 {
		return identifiers
	}
	return append(identifiers, string(window["hwnd"]))
}

func workspaceHasWindowID(state jsontext.Value, workspace, identifier string) bool {
	return slices.Contains(workspaceWindowIDs(state, workspace), identifier)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func workspaceWindowRectangles(value jsontext.Value) map[string]map[string]jsontext.Value {
	rectangles := make(map[string]map[string]jsontext.Value)
	var visit func(jsontext.Value)
	visit = func(value jsontext.Value) {
		var object map[string]jsontext.Value
		if err := json.Unmarshal(value, &object); err == nil && object != nil {
			if len(object["hwnd"]) > 0 && object["rect"].IsValid() {
				var rectangle map[string]jsontext.Value
				if err := json.Unmarshal(object["rect"], &rectangle); err == nil && rectangle != nil {
					rectangles[string(object["hwnd"])] = rectangle
				}
			}
			for _, child := range object {
				visit(child)
			}
			return
		}
		var elements []jsontext.Value
		if err := json.Unmarshal(value, &elements); err == nil {
			for _, element := range elements {
				visit(element)
			}
		}
	}
	visit(value)
	return rectangles
}

func equalWindowRectangles(left, right map[string]map[string]jsontext.Value) bool {
	if len(left) != len(right) {
		return false
	}
	for hwnd, leftRect := range left {
		rightRect, ok := right[hwnd]
		if !ok || len(leftRect) != len(rightRect) {
			return false
		}
		for key, leftValue := range leftRect {
			if !bytes.Equal(leftValue, rightRect[key]) {
				return false
			}
		}
	}
	return true
}

func (r *runner) waitForNewWindow(ctx context.Context, before jsontext.Value, workspace string) (string, error) {
	beforeIDs := make(map[string]struct{})
	for _, identifier := range workspaceWindowIDs(before, workspace) {
		beforeIDs[identifier] = struct{}{}
	}
	deadline := time.Now().Add(checkTimeout)
	for time.Now().Before(deadline) {
		state, err := r.state(ctx)
		if err != nil {
			return "", err
		}
		for _, identifier := range workspaceWindowIDs(state, workspace) {
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
		if len(workspaceWindowIDs(state, workspace)) >= count {
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

func workspaceContainerCounts(value jsontext.Value) (int, int) {
	workspace, err := decodeObject(value)
	if err != nil {
		return 0, 0
	}
	var containers ringValue
	if err := json.Unmarshal(workspace["containers"], &containers); err != nil {
		return 0, 0
	}
	windows := 0
	for _, containerValue := range containers.Elements {
		container, err := decodeObject(containerValue)
		if err != nil {
			continue
		}
		var ring ringValue
		if err := json.Unmarshal(container["windows"], &ring); err == nil {
			windows += len(ring.Elements)
		}
	}
	return len(containers.Elements), windows
}

func (r *runner) waitForNoWindows(ctx context.Context, workspace string) error {
	deadline := time.Now().Add(checkTimeout)
	for time.Now().Before(deadline) {
		state, err := r.state(ctx)
		if err != nil {
			return err
		}
		if len(workspaceWindowIDs(state, workspace)) == 0 {
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

func (r *runner) waitForStableWindowRectangles(ctx context.Context, spaces spikeSpaces, slot int, started time.Time, timeout time.Duration) (time.Duration, bool, error) {
	if slot < 0 || slot >= len(spaces.spaces) {
		return 0, false, errors.New("spike workspace slot is out of range")
	}
	deadline := started.Add(timeout)
	var previous map[string]map[string]jsontext.Value
	stableSamples := 0
	for {
		state, err := r.state(ctx)
		if err != nil {
			return 0, false, err
		}
		_, workspaces, err := monitorWorkspacesFor(state, spaces)
		if err != nil {
			return 0, false, err
		}
		index := findWorkspaceIndex(workspaces, spaces.spaces[slot].name)
		if index < 0 {
			return 0, false, fmt.Errorf("spike workspace %q is not present", spaces.spaces[slot].name)
		}
		rectangles := workspaceWindowRectangles(workspaces.Elements[index])
		if len(rectangles) > 0 && equalWindowRectangles(previous, rectangles) {
			stableSamples++
		} else {
			stableSamples = 1
		}
		if len(rectangles) > 0 && stableSamples >= 2 {
			return time.Since(started), true, nil
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

func monitorCount(state jsontext.Value) int {
	root, err := decodeObject(state)
	if err != nil {
		return 0
	}
	var monitors ringValue
	if err := json.Unmarshal(root["monitors"], &monitors); err != nil {
		return 0
	}
	return len(monitors.Elements)
}

func (r *runner) waitForFocusedWorkspaceSince(ctx context.Context, spaces spikeSpaces, slot int, started time.Time, timeout time.Duration) (time.Duration, bool, error) {
	deadline := started.Add(timeout)
	for time.Now().Before(deadline) {
		state, err := r.state(ctx)
		if err != nil {
			return 0, false, err
		}
		_, workspaces, err := monitorWorkspacesFor(state, spaces)
		if err == nil && slot >= 0 && slot < len(spaces.spaces) && workspaces.Focused >= 0 && workspaces.Focused < len(workspaces.Elements) && workspaceName(workspaces.Elements[workspaces.Focused]) == spaces.spaces[slot].name {
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
