package spikestate

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"testing"

	"gotest.tools/v3/assert"
)

func TestStatesSafeToApply(t *testing.T) {
	spaces := syntheticSpikeSpaces()
	snapshot := syntheticState(t, "home", [3]string{"old-0", "old-1", "old-2"}, false)
	tests := []struct {
		name     string
		snapshot jsontext.Value
		fresh    jsontext.Value
	}{
		{
			name:     "outside workspace change",
			snapshot: snapshot,
			fresh:    syntheticState(t, "changed-home", [3]string{"old-0", "old-1", "old-2"}, false),
		},
		{
			name:     "monitor topology change",
			snapshot: snapshot,
			fresh:    syntheticState(t, "home", [3]string{"old-0", "old-1", "old-2"}, true),
		},
		{
			name:     "monitor reorder",
			snapshot: syntheticState(t, "home", [3]string{"old-0", "old-1", "old-2"}, true),
			fresh:    syntheticStateWithTopology(t, "home", [3]string{"old-0", "old-1", "old-2"}, true, true),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			safe, err := StatesSafeToApply(test.snapshot, test.fresh, spaces)
			assert.NilError(t, err)
			assert.Assert(t, !safe)
		})
	}
	removed := removeSyntheticWorkspace(t, snapshot, "spike-2")
	payload, safe, err := PrepareFreshWorkspaceApply(snapshot, removed, spaces, 0, 1, 2)
	assert.Assert(t, err != nil)
	assert.Assert(t, !safe)
	assert.Assert(t, !payload.IsValid())
}

func TestPrepareSelectedWorkspaceApply(t *testing.T) {
	spaces := syntheticSpikeSpaces()
	snapshot := syntheticState(t, "home", [3]string{"snapshot-0", "snapshot-1", "snapshot-2"}, false)
	fresh := syntheticState(t, "home", [3]string{"fresh-0", "fresh-1", "fresh-2"}, false)

	payload, safe, err := PrepareSelectedWorkspaceApply(snapshot, fresh, spaces, 1)
	assert.NilError(t, err)
	assert.Assert(t, safe)

	_, workspaces, err := MonitorWorkspacesFor(payload, spaces)
	assert.NilError(t, err)
	assert.Equal(t, workspaceVersion(t, workspaces, "home"), "home")
	assert.Equal(t, workspaceVersion(t, workspaces, "spike-0"), "fresh-0")
	assert.Equal(t, workspaceVersion(t, workspaces, "spike-1"), "snapshot-1")
	assert.Equal(t, workspaceVersion(t, workspaces, "spike-2"), "fresh-2")

	changed := syntheticState(t, "changed-home", [3]string{"fresh-0", "fresh-1", "fresh-2"}, false)
	aborted, safe, err := PrepareSelectedWorkspaceApply(snapshot, changed, spaces, 1)
	assert.NilError(t, err)
	assert.Assert(t, !safe)
	assert.Assert(t, !aborted.IsValid())
}

func TestPrepareFreshWorkspaceApply(t *testing.T) {
	spaces := syntheticSpikeSpaces()
	snapshot := syntheticState(t, "home", [3]string{"old-0", "old-1", "old-2"}, false)
	fresh := syntheticState(t, "home", [3]string{"fresh-0", "fresh-1", "fresh-2"}, false)

	payload, safe, err := PrepareFreshWorkspaceApply(snapshot, fresh, spaces, 0, 1, 2)
	assert.NilError(t, err)
	assert.Assert(t, safe)

	_, workspaces, err := MonitorWorkspacesFor(payload, spaces)
	assert.NilError(t, err)
	assert.Equal(t, WorkspaceName(workspaces.Elements[0]), "home")
	assert.Equal(t, WorkspaceName(workspaces.Elements[1]), "spike-1")
	assert.Equal(t, WorkspaceName(workspaces.Elements[2]), "spike-0")
	assert.Equal(t, len(workspaces.Elements), 3)
	assert.Equal(t, workspaceVersion(t, workspaces, "home"), "home")
	assert.Equal(t, workspaceVersion(t, workspaces, "spike-0"), "fresh-0")
	assert.Equal(t, workspaceVersion(t, workspaces, "spike-1"), "fresh-1")
	assert.Equal(t, FindWorkspaceIndex(workspaces, "spike-2"), -1)
}

func TestFocusedSpikeWorkspace(t *testing.T) {
	spaces := syntheticSpikeSpaces()
	state := syntheticState(t, "home", [3]string{"v0", "v1", "v2"}, false)
	focused, err := FocusedSpikeWorkspace(state, spaces, 0)
	assert.NilError(t, err)
	assert.Assert(t, focused)

	root, err := DecodeObject(state)
	assert.NilError(t, err)
	var monitors RingValue
	assert.NilError(t, json.Unmarshal(root["monitors"], &monitors))
	monitor, err := DecodeObject(monitors.Elements[0])
	assert.NilError(t, err)
	var workspaces RingValue
	assert.NilError(t, json.Unmarshal(monitor["workspaces"], &workspaces))
	workspaces.Focused = 0
	monitor["workspaces"] = marshalStateValue(t, workspaces)
	monitors.Elements[0] = marshalStateValue(t, monitor)
	root["monitors"] = marshalStateValue(t, monitors)
	focused, err = FocusedSpikeWorkspace(marshalStateValue(t, root), spaces, 0)
	assert.NilError(t, err)
	assert.Assert(t, !focused)
}

func TestFocusedSpikeWorkspaceRequiresActiveMonitor(t *testing.T) {
	spaces := syntheticSpikeSpaces()
	state := syntheticState(t, "home", [3]string{"v0", "v1", "v2"}, true)
	focused, err := FocusedSpikeWorkspace(state, spaces, 0)
	assert.NilError(t, err)
	assert.Assert(t, focused)

	root, err := DecodeObject(state)
	assert.NilError(t, err)
	var monitors RingValue
	assert.NilError(t, json.Unmarshal(root["monitors"], &monitors))
	monitors.Focused = 1
	root["monitors"] = marshalStateValue(t, monitors)
	focused, err = FocusedSpikeWorkspace(marshalStateValue(t, root), spaces, 0)
	assert.NilError(t, err)
	assert.Assert(t, !focused)
}

func TestRectangleChangesNeedTwoStableSamples(t *testing.T) {
	baseline := map[string]map[string]jsontext.Value{"1": {"x": jsontext.Value("0")}}
	changed := map[string]map[string]jsontext.Value{"1": {"x": jsontext.Value("10")}}
	assert.Assert(t, !EqualWindowRectangles(baseline, changed))
	assert.Assert(t, EqualWindowRectangles(changed, changed))
	assert.Assert(t, EqualWindowRectangles(nil, nil))
}

func syntheticSpikeSpaces() SpikeSpaces {
	return SpikeSpaces{
		MonitorID:     jsontext.Value(`"monitor-1"`),
		MonitorDevice: jsontext.Value(`"device-1"`),
		Spaces: []SpikeSpace{
			{Index: 1, Name: "spike-0"},
			{Index: 2, Name: "spike-1"},
			{Index: 3, Name: "spike-2"},
		},
	}
}

func syntheticState(t *testing.T, homeVersion string, spikeVersions [3]string, secondMonitor bool) jsontext.Value {
	t.Helper()
	return syntheticStateWithTopology(t, homeVersion, spikeVersions, secondMonitor, false)
}

func syntheticStateWithTopology(t *testing.T, homeVersion string, spikeVersions [3]string, secondMonitor, reverse bool) jsontext.Value {
	t.Helper()
	workspaces := []jsontext.Value{
		syntheticWorkspace(t, "home", homeVersion),
		syntheticWorkspace(t, "spike-0", spikeVersions[0]),
		syntheticWorkspace(t, "spike-1", spikeVersions[1]),
		syntheticWorkspace(t, "spike-2", spikeVersions[2]),
	}
	primary := syntheticMonitor(t, "monitor-1", "device-1", workspaces)
	monitors := []jsontext.Value{primary}
	if secondMonitor {
		other := syntheticMonitor(t, "monitor-2", "device-2", []jsontext.Value{syntheticWorkspace(t, "other", "other")})
		monitors = append(monitors, other)
		if reverse {
			monitors[0], monitors[1] = monitors[1], monitors[0]
		}
	}
	focused := 0
	if reverse {
		focused = 1
	}
	return marshalStateValue(t, map[string]any{"monitors": RingValue{Elements: monitors, Focused: focused}})
}

func syntheticMonitor(t *testing.T, id, device string, workspaces []jsontext.Value) jsontext.Value {
	t.Helper()
	focused := 0
	if len(workspaces) > 1 {
		focused = 1
	}
	return marshalStateValue(t, map[string]any{
		"id":         id,
		"device_id":  device,
		"workspaces": RingValue{Elements: workspaces, Focused: focused},
	})
}

func syntheticWorkspace(t *testing.T, name, version string) jsontext.Value {
	t.Helper()
	return marshalStateValue(t, map[string]any{
		"name":             name,
		"version":          version,
		"containers":       RingValue{Elements: []jsontext.Value{}, Focused: 0},
		"floating_windows": RingValue{Elements: []jsontext.Value{}, Focused: 0},
	})
}

func marshalStateValue(t *testing.T, value any) jsontext.Value {
	t.Helper()
	encoded, err := json.Marshal(value)
	assert.NilError(t, err)
	return jsontext.Value(encoded)
}

func workspaceVersion(t *testing.T, workspaces RingValue, name string) string {
	t.Helper()
	index := FindWorkspaceIndex(workspaces, name)
	assert.Assert(t, index >= 0)
	workspace, err := DecodeObject(workspaces.Elements[index])
	assert.NilError(t, err)
	return RawString(workspace["version"])
}

func removeSyntheticWorkspace(t *testing.T, state jsontext.Value, name string) jsontext.Value {
	t.Helper()
	root, err := DecodeObject(state)
	assert.NilError(t, err)
	var monitors RingValue
	assert.NilError(t, json.Unmarshal(root["monitors"], &monitors))
	monitorIndex, workspaces, err := MonitorWorkspacesFor(state, syntheticSpikeSpaces())
	assert.NilError(t, err)
	assert.Assert(t, monitorIndex >= 0)
	monitor, err := DecodeObject(monitors.Elements[monitorIndex])
	assert.NilError(t, err)
	index := FindWorkspaceIndex(workspaces, name)
	assert.Assert(t, index >= 0)
	workspaces.Elements = append(workspaces.Elements[:index], workspaces.Elements[index+1:]...)
	workspaces.Focused = min(workspaces.Focused, len(workspaces.Elements)-1)
	monitor["workspaces"] = marshalStateValue(t, workspaces)
	monitors.Elements[monitorIndex] = marshalStateValue(t, monitor)
	root["monitors"] = marshalStateValue(t, monitors)
	return marshalStateValue(t, root)
}
