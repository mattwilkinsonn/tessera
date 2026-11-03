package spikestate

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
)

// RingValue is a komorebi ring with its ordered values and focused index.
type RingValue struct {
	Elements []jsontext.Value `json:"elements"`
	Focused  int              `json:"focused"`
}

// SpikeSpace records one workspace created for the spike.
type SpikeSpace struct {
	Index int
	Name  string
	Base  jsontext.Value
}

// SpikeSpaces identifies the monitor and workspaces owned by the spike.
type SpikeSpaces struct {
	MonitorIndex  int
	MonitorID     jsontext.Value
	MonitorDevice jsontext.Value
	OriginalFocus int
	OriginalCount int
	Spaces        []SpikeSpace
}

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

// DecodeObject decodes an object without discarding its raw member values.
func DecodeObject(value jsontext.Value) (map[string]jsontext.Value, error) {
	return decodeObject(value)
}
func monitorWorkspaces(state jsontext.Value, monitorIndex int) (map[string]jsontext.Value, RingValue, error) {
	root, err := decodeObject(state)
	if err != nil {
		return nil, RingValue{}, fmt.Errorf("decoding State: %w", err)
	}
	var monitors RingValue
	if err := json.Unmarshal(root["monitors"], &monitors); err != nil {
		return nil, RingValue{}, fmt.Errorf("decoding monitor ring: %w", err)
	}
	if monitorIndex < 0 || monitorIndex >= len(monitors.Elements) {
		return nil, RingValue{}, fmt.Errorf("monitor index %d is out of range", monitorIndex)
	}
	monitor, err := decodeObject(monitors.Elements[monitorIndex])
	if err != nil {
		return nil, RingValue{}, fmt.Errorf("decoding monitor %d: %w", monitorIndex, err)
	}
	var workspaces RingValue
	if err := json.Unmarshal(monitor["workspaces"], &workspaces); err != nil {
		return nil, RingValue{}, fmt.Errorf("decoding monitor %d workspace ring: %w", monitorIndex, err)
	}
	return monitor, workspaces, nil
}

// MonitorWorkspaces decodes a monitor and its workspace ring by index.
func MonitorWorkspaces(state jsontext.Value, monitorIndex int) (map[string]jsontext.Value, RingValue, error) {
	return monitorWorkspaces(state, monitorIndex)
}

func workspaceIsEmpty(value jsontext.Value) bool {
	workspace, err := decodeObject(value)
	if err != nil {
		return false
	}
	var containers, floating RingValue
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

// WorkspaceIsEmpty reports whether a workspace has no managed windows or overlays.
func WorkspaceIsEmpty(value jsontext.Value) bool {
	return workspaceIsEmpty(value)
}

func workspaceName(value jsontext.Value) string {
	workspace, err := decodeObject(value)
	if err != nil {
		return ""
	}
	return rawString(workspace["name"])
}

// WorkspaceName returns the decoded name, or an empty string for invalid input.
func WorkspaceName(value jsontext.Value) string {
	return workspaceName(value)
}

func rawString(value jsontext.Value) string {
	var result string
	if err := json.Unmarshal(value, &result); err != nil {
		return ""
	}
	return result
}

// RawString decodes a JSON string, or returns an empty string on invalid input.
func RawString(value jsontext.Value) string {
	return rawString(value)
}

func workspaceLayout(value jsontext.Value) jsontext.Value {
	workspace, err := decodeObject(value)
	if err != nil {
		return nil
	}
	return workspace["layout"]
}

// WorkspaceLayout returns the raw layout member of a workspace.
func WorkspaceLayout(value jsontext.Value) jsontext.Value {
	return workspaceLayout(value)
}

func workspaceBehaviour(value jsontext.Value) string {
	workspace, err := decodeObject(value)
	if err != nil {
		return ""
	}
	return rawString(workspace["window_container_behaviour"])
}

// WorkspaceBehaviour returns the workspace's window container behaviour.
func WorkspaceBehaviour(value jsontext.Value) string {
	return workspaceBehaviour(value)
}

func findWorkspaceIndex(workspaces RingValue, name string) int {
	for index, workspace := range workspaces.Elements {
		if workspaceName(workspace) == name {
			return index
		}
	}
	return -1
}

// FindWorkspaceIndex finds a workspace by name or returns -1.
func FindWorkspaceIndex(workspaces RingValue, name string) int {
	return findWorkspaceIndex(workspaces, name)
}

func hasWorkspace(state jsontext.Value, name string) bool {
	root, err := decodeObject(state)
	if err != nil {
		return false
	}
	var monitors RingValue
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

// HasWorkspace reports whether any monitor contains a workspace with this name.
func HasWorkspace(state jsontext.Value, name string) bool {
	return hasWorkspace(state, name)
}

func reorderAndRemoveSpikeWorkspace(state jsontext.Value, spaces SpikeSpaces, firstSlot, secondSlot, dropSlot int) (jsontext.Value, error) {
	root, monitor, monitors, monitorIndex, workspaces, err := spikeWorkspaceTree(state, spaces)
	if err != nil {
		return nil, err
	}
	if firstSlot < 0 || firstSlot >= len(spaces.Spaces) || secondSlot < 0 || secondSlot >= len(spaces.Spaces) || dropSlot < 0 || dropSlot >= len(spaces.Spaces) {
		return nil, errors.New("spike workspace slot is out of range")
	}
	first := findWorkspaceIndex(workspaces, spaces.Spaces[firstSlot].Name)
	second := findWorkspaceIndex(workspaces, spaces.Spaces[secondSlot].Name)
	drop := findWorkspaceIndex(workspaces, spaces.Spaces[dropSlot].Name)
	if first < 0 || second < 0 || drop < 0 || first == second || first == drop || second == drop {
		return nil, errors.New("spike workspace is no longer present")
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

// ReorderAndRemoveSpikeWorkspace swaps two spike workspaces and removes another.
func ReorderAndRemoveSpikeWorkspace(state jsontext.Value, spaces SpikeSpaces, firstSlot, secondSlot, dropSlot int) (jsontext.Value, error) {
	return reorderAndRemoveSpikeWorkspace(state, spaces, firstSlot, secondSlot, dropSlot)
}

func spikeWorkspaceTree(state jsontext.Value, spaces SpikeSpaces) (map[string]jsontext.Value, map[string]jsontext.Value, RingValue, int, RingValue, error) {
	root, err := decodeObject(state)
	if err != nil {
		return nil, nil, RingValue{}, 0, RingValue{}, fmt.Errorf("decoding ApplyState input: %w", err)
	}
	var monitors RingValue
	if err := json.Unmarshal(root["monitors"], &monitors); err != nil {
		return nil, nil, RingValue{}, 0, RingValue{}, fmt.Errorf("decoding monitor ring: %w", err)
	}
	monitorIndex := findMonitorByIdentity(monitors, spaces)
	if monitorIndex < 0 {
		return nil, nil, RingValue{}, 0, RingValue{}, errors.New("spike monitor is no longer present")
	}
	monitor, err := decodeObject(monitors.Elements[monitorIndex])
	if err != nil {
		return nil, nil, RingValue{}, 0, RingValue{}, fmt.Errorf("decoding spike monitor: %w", err)
	}
	var workspaces RingValue
	if err := json.Unmarshal(monitor["workspaces"], &workspaces); err != nil {
		return nil, nil, RingValue{}, 0, RingValue{}, fmt.Errorf("decoding workspace ring: %w", err)
	}
	return root, monitor, monitors, monitorIndex, workspaces, nil
}

func encodeSpikeWorkspaceTree(root, monitor map[string]jsontext.Value, monitors RingValue, monitorIndex int, workspaces RingValue) (jsontext.Value, error) {
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

func restoreWorkspaceValues(state jsontext.Value, spaces SpikeSpaces) (jsontext.Value, error) {
	root, err := decodeObject(state)
	if err != nil {
		return nil, fmt.Errorf("decoding current State: %w", err)
	}
	var monitors RingValue
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
	var workspaces RingValue
	if err := json.Unmarshal(monitor["workspaces"], &workspaces); err != nil {
		return nil, fmt.Errorf("decoding current workspace ring: %w", err)
	}
	filtered := make([]jsontext.Value, 0, len(workspaces.Elements))
	for _, workspace := range workspaces.Elements {
		if !isSpikeWorkspace(spaces, workspaceName(workspace)) {
			filtered = append(filtered, workspace)
		}
	}
	base := spaces.OriginalCount
	if base > len(filtered) {
		return nil, errors.New("workspace ring shrank before spike reset; refusing to overwrite user workspaces")
	}
	restored := make([]jsontext.Value, 0, len(filtered)+len(spaces.Spaces))
	restored = append(restored, filtered[:base]...)
	for _, space := range spaces.Spaces {
		if len(space.Base) == 0 {
			return nil, fmt.Errorf("missing baseline for spike workspace %q", space.Name)
		}
		restored = append(restored, space.Base)
	}
	restored = append(restored, filtered[base:]...)
	workspaces.Elements = restored
	workspaces.Focused = spaces.OriginalFocus
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

// RestoreWorkspaceValues restores the original spike workspaces in fresh State.
func RestoreWorkspaceValues(state jsontext.Value, spaces SpikeSpaces) (jsontext.Value, error) {
	return restoreWorkspaceValues(state, spaces)
}

func sameMonitor(spaces SpikeSpaces, value jsontext.Value) bool {
	monitor, err := decodeObject(value)
	if err != nil {
		return false
	}
	return bytes.Equal(spaces.MonitorID, monitor["id"]) && bytes.Equal(spaces.MonitorDevice, monitor["device_id"])
}

func findMonitorByIdentity(monitors RingValue, spaces SpikeSpaces) int {
	for index, monitor := range monitors.Elements {
		if sameMonitor(spaces, monitor) {
			return index
		}
	}
	return -1
}

func isSpikeWorkspace(spaces SpikeSpaces, name string) bool {
	for _, space := range spaces.Spaces {
		if space.Name == name {
			return true
		}
	}
	return false
}

func monitorWorkspacesFor(state jsontext.Value, spaces SpikeSpaces) (int, RingValue, error) {
	root, err := decodeObject(state)
	if err != nil {
		return 0, RingValue{}, fmt.Errorf("decoding State: %w", err)
	}
	var monitors RingValue
	if err := json.Unmarshal(root["monitors"], &monitors); err != nil {
		return 0, RingValue{}, fmt.Errorf("decoding monitor ring: %w", err)
	}
	monitorIndex := findMonitorByIdentity(monitors, spaces)
	if monitorIndex < 0 {
		return 0, RingValue{}, errors.New("original spike monitor is not available")
	}
	_, workspaces, err := monitorWorkspaces(state, monitorIndex)
	if err != nil {
		return 0, RingValue{}, err
	}
	return monitorIndex, workspaces, nil
}

// MonitorWorkspacesFor locates the original spike monitor by identity.
func MonitorWorkspacesFor(state jsontext.Value, spaces SpikeSpaces) (int, RingValue, error) {
	return monitorWorkspacesFor(state, spaces)
}

func focusedSpikeWorkspace(state jsontext.Value, spaces SpikeSpaces, slot int) (bool, error) {
	if slot < 0 || slot >= len(spaces.Spaces) {
		return false, errors.New("spike workspace slot is out of range")
	}
	root, err := decodeObject(state)
	if err != nil {
		return false, fmt.Errorf("decoding focused workspace State: %w", err)
	}
	var monitors RingValue
	if err := json.Unmarshal(root["monitors"], &monitors); err != nil {
		return false, fmt.Errorf("decoding focused workspace monitors: %w", err)
	}
	monitorIndex := findMonitorByIdentity(monitors, spaces)
	if monitorIndex < 0 || monitors.Focused != monitorIndex {
		return false, nil
	}
	monitor, err := decodeObject(monitors.Elements[monitorIndex])
	if err != nil {
		return false, fmt.Errorf("decoding focused spike monitor: %w", err)
	}
	var workspaces RingValue
	if err := json.Unmarshal(monitor["workspaces"], &workspaces); err != nil {
		return false, fmt.Errorf("decoding focused workspace ring: %w", err)
	}
	if workspaces.Focused < 0 || workspaces.Focused >= len(workspaces.Elements) {
		return false, nil
	}
	return workspaceName(workspaces.Elements[workspaces.Focused]) == spaces.Spaces[slot].Name, nil
}

// FocusedSpikeWorkspace checks both active monitor and workspace identity.
func FocusedSpikeWorkspace(state jsontext.Value, spaces SpikeSpaces, slot int) (bool, error) {
	return focusedSpikeWorkspace(state, spaces, slot)
}

func stateOutsideSpikeWorkspaces(state jsontext.Value, spaces SpikeSpaces) (jsontext.Value, error) {
	root, err := decodeObject(state)
	if err != nil {
		return nil, fmt.Errorf("decoding State for spike workspace comparison: %w", err)
	}
	var monitors RingValue
	if err := json.Unmarshal(root["monitors"], &monitors); err != nil {
		return nil, fmt.Errorf("decoding monitor ring for spike workspace comparison: %w", err)
	}
	for monitorIndex, monitorValue := range monitors.Elements {
		monitor, err := decodeObject(monitorValue)
		if err != nil {
			return nil, fmt.Errorf("decoding monitor %d for spike workspace comparison: %w", monitorIndex, err)
		}
		var workspaces RingValue
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
	normalized := jsontext.Value(result)
	if err := normalized.Format(jsontext.ReorderRawObjects(true)); err != nil {
		return nil, fmt.Errorf("normalizing State for spike workspace comparison: %w", err)
	}
	return normalized, nil
}

func sameMonitorTopology(left, right jsontext.Value, spaces SpikeSpaces) (bool, error) {
	leftRoot, err := decodeObject(left)
	if err != nil {
		return false, fmt.Errorf("decoding snapshot State: %w", err)
	}
	rightRoot, err := decodeObject(right)
	if err != nil {
		return false, fmt.Errorf("decoding fresh State: %w", err)
	}
	var leftMonitors, rightMonitors RingValue
	if err := json.Unmarshal(leftRoot["monitors"], &leftMonitors); err != nil {
		return false, fmt.Errorf("decoding snapshot monitor ring: %w", err)
	}
	if err := json.Unmarshal(rightRoot["monitors"], &rightMonitors); err != nil {
		return false, fmt.Errorf("decoding fresh monitor ring: %w", err)
	}
	if len(leftMonitors.Elements) != len(rightMonitors.Elements) {
		return false, nil
	}
	leftSpikeIndex := findMonitorByIdentity(leftMonitors, spaces)
	rightSpikeIndex := findMonitorByIdentity(rightMonitors, spaces)
	if leftSpikeIndex < 0 || leftSpikeIndex != rightSpikeIndex {
		return false, nil
	}
	for index := range leftMonitors.Elements {
		leftMonitor, err := decodeObject(leftMonitors.Elements[index])
		if err != nil {
			return false, fmt.Errorf("decoding snapshot monitor %d: %w", index, err)
		}
		rightMonitor, err := decodeObject(rightMonitors.Elements[index])
		if err != nil {
			return false, fmt.Errorf("decoding fresh monitor %d: %w", index, err)
		}
		if !bytes.Equal(leftMonitor["id"], rightMonitor["id"]) || !bytes.Equal(leftMonitor["device_id"], rightMonitor["device_id"]) {
			return false, nil
		}
	}
	return true, nil
}

func statesSafeToApply(snapshot, fresh jsontext.Value, spaces SpikeSpaces) (bool, error) {
	sameTopology, err := sameMonitorTopology(snapshot, fresh, spaces)
	if err != nil {
		return false, err
	}
	if !sameTopology {
		return false, nil
	}
	snapshotOutside, err := stateOutsideSpikeWorkspaces(snapshot, spaces)
	if err != nil {
		return false, err
	}
	freshOutside, err := stateOutsideSpikeWorkspaces(fresh, spaces)
	if err != nil {
		return false, err
	}
	return bytes.Equal(snapshotOutside, freshOutside), nil
}

// StatesSafeToApply checks that outside-spike State and monitor topology match.
func StatesSafeToApply(snapshot, fresh jsontext.Value, spaces SpikeSpaces) (bool, error) {
	return statesSafeToApply(snapshot, fresh, spaces)
}

func prepareFreshWorkspaceApply(snapshot, fresh jsontext.Value, spaces SpikeSpaces, firstSlot, secondSlot, dropSlot int) (jsontext.Value, bool, error) {
	safe, err := statesSafeToApply(snapshot, fresh, spaces)
	if err != nil || !safe {
		return nil, safe, err
	}
	modified, err := reorderAndRemoveSpikeWorkspace(fresh, spaces, firstSlot, secondSlot, dropSlot)
	if err != nil {
		return nil, false, err
	}
	return modified, true, nil
}

// PrepareFreshWorkspaceApply transforms fresh State after the safety check.
func PrepareFreshWorkspaceApply(snapshot, fresh jsontext.Value, spaces SpikeSpaces, firstSlot, secondSlot, dropSlot int) (jsontext.Value, bool, error) {
	return prepareFreshWorkspaceApply(snapshot, fresh, spaces, firstSlot, secondSlot, dropSlot)
}

func prepareSelectedWorkspaceApply(snapshot, fresh jsontext.Value, spaces SpikeSpaces, slot int) (jsontext.Value, bool, error) {
	safe, err := statesSafeToApply(snapshot, fresh, spaces)
	if err != nil || !safe {
		return nil, safe, err
	}
	if slot < 0 || slot >= len(spaces.Spaces) {
		return nil, false, errors.New("spike workspace slot is out of range")
	}
	name := spaces.Spaces[slot].Name
	_, snapshotWorkspaces, err := monitorWorkspacesFor(snapshot, spaces)
	if err != nil {
		return nil, false, err
	}
	snapshotIndex := findWorkspaceIndex(snapshotWorkspaces, name)
	if snapshotIndex < 0 {
		return nil, false, fmt.Errorf("spike workspace %q is absent from snapshot State", name)
	}
	monitorIndex, freshWorkspaces, err := monitorWorkspacesFor(fresh, spaces)
	if err != nil {
		return nil, false, err
	}
	freshIndex := findWorkspaceIndex(freshWorkspaces, name)
	if freshIndex < 0 {
		return nil, false, fmt.Errorf("spike workspace %q is absent from fresh State", name)
	}
	freshWorkspaces.Elements[freshIndex] = snapshotWorkspaces.Elements[snapshotIndex]
	root, err := decodeObject(fresh)
	if err != nil {
		return nil, false, fmt.Errorf("decoding fresh State: %w", err)
	}
	var monitors RingValue
	if err := json.Unmarshal(root["monitors"], &monitors); err != nil {
		return nil, false, fmt.Errorf("decoding fresh monitor ring: %w", err)
	}
	monitor, err := decodeObject(monitors.Elements[monitorIndex])
	if err != nil {
		return nil, false, fmt.Errorf("decoding fresh spike monitor: %w", err)
	}
	monitor["workspaces"], err = json.Marshal(freshWorkspaces)
	if err != nil {
		return nil, false, fmt.Errorf("encoding fresh spike workspaces: %w", err)
	}
	monitors.Elements[monitorIndex], err = json.Marshal(monitor)
	if err != nil {
		return nil, false, fmt.Errorf("encoding fresh spike monitor: %w", err)
	}
	root["monitors"], err = json.Marshal(monitors)
	if err != nil {
		return nil, false, fmt.Errorf("encoding fresh monitor ring: %w", err)
	}
	result, err := json.Marshal(root)
	if err != nil {
		return nil, false, fmt.Errorf("encoding guarded ApplyState payload: %w", err)
	}
	return result, true, nil
}

// PrepareSelectedWorkspaceApply replaces one spike workspace in fresh State.
func PrepareSelectedWorkspaceApply(snapshot, fresh jsontext.Value, spaces SpikeSpaces, slot int) (jsontext.Value, bool, error) {
	return prepareSelectedWorkspaceApply(snapshot, fresh, spaces, slot)
}

func workspaceWindowIDs(state jsontext.Value, name string) []string {
	root, err := decodeObject(state)
	if err != nil {
		return nil
	}
	var monitors RingValue
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
	var containers RingValue
	if err := json.Unmarshal(workspace["containers"], &containers); err == nil {
		for _, containerValue := range containers.Elements {
			container, err := decodeObject(containerValue)
			if err != nil {
				continue
			}
			var windows RingValue
			if err := json.Unmarshal(container["windows"], &windows); err != nil {
				continue
			}
			for _, window := range windows.Elements {
				identifiers = appendWindowID(identifiers, window)
			}
		}
	}
	var floating RingValue
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

// WorkspaceWindowIDs returns handles in the named workspace.
func WorkspaceWindowIDs(state jsontext.Value, name string) []string {
	return workspaceWindowIDs(state, name)
}

func workspaceHasWindowID(state jsontext.Value, workspace, identifier string) bool {
	return slices.Contains(workspaceWindowIDs(state, workspace), identifier)
}

// WorkspaceHasWindowID reports whether a workspace contains the given handle.
func WorkspaceHasWindowID(state jsontext.Value, workspace, identifier string) bool {
	return workspaceHasWindowID(state, workspace, identifier)
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

// EqualStrings compares two ordered string slices.
func EqualStrings(left, right []string) bool {
	return equalStrings(left, right)
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
					rectangles[rawString(object["hwnd"])] = rectangle
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

// WorkspaceWindowRectangles indexes window rectangles by handle.
func WorkspaceWindowRectangles(value jsontext.Value) map[string]map[string]jsontext.Value {
	return workspaceWindowRectangles(value)
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

// EqualWindowRectangles compares handle-indexed rectangle values.
func EqualWindowRectangles(left, right map[string]map[string]jsontext.Value) bool {
	return equalWindowRectangles(left, right)
}

func workspaceContainerCounts(value jsontext.Value) (int, int) {
	workspace, err := decodeObject(value)
	if err != nil {
		return 0, 0
	}
	var containers RingValue
	if err := json.Unmarshal(workspace["containers"], &containers); err != nil {
		return 0, 0
	}
	windows := 0
	for _, containerValue := range containers.Elements {
		container, err := decodeObject(containerValue)
		if err != nil {
			continue
		}
		var ring RingValue
		if err := json.Unmarshal(container["windows"], &ring); err == nil {
			windows += len(ring.Elements)
		}
	}
	return len(containers.Elements), windows
}

// WorkspaceContainerCounts returns the container and managed-window counts.
func WorkspaceContainerCounts(value jsontext.Value) (int, int) {
	return workspaceContainerCounts(value)
}

func workspaceRectanglesForState(state jsontext.Value, spaces SpikeSpaces, slot int) (map[string]map[string]jsontext.Value, error) {
	if slot < 0 || slot >= len(spaces.Spaces) {
		return nil, errors.New("spike workspace slot is out of range")
	}
	_, workspaces, err := monitorWorkspacesFor(state, spaces)
	if err != nil {
		return nil, err
	}
	index := findWorkspaceIndex(workspaces, spaces.Spaces[slot].Name)
	if index < 0 {
		return nil, fmt.Errorf("spike workspace %q is not present", spaces.Spaces[slot].Name)
	}
	return workspaceWindowRectangles(workspaces.Elements[index]), nil
}

// WorkspaceRectanglesForState reads a spike workspace's window rectangles.
func WorkspaceRectanglesForState(state jsontext.Value, spaces SpikeSpaces, slot int) (map[string]map[string]jsontext.Value, error) {
	return workspaceRectanglesForState(state, spaces, slot)
}

func monitorCount(state jsontext.Value) int {
	root, err := decodeObject(state)
	if err != nil {
		return 0
	}
	var monitors RingValue
	if err := json.Unmarshal(root["monitors"], &monitors); err != nil {
		return 0
	}
	return len(monitors.Elements)
}

// MonitorCount returns the number of monitors in State.
func MonitorCount(state jsontext.Value) int {
	return monitorCount(state)
}
