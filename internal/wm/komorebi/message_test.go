package komorebi

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"
)

func TestSocketMessageGoldens(t *testing.T) {
	state := jsontext.Value(`{"monitors":[],"unknown":{"keep":true}}`)
	tests := []struct {
		name    string
		message SocketMessage
		golden  string
	}{
		{name: "query", message: Query(QueryFocusedMonitorIndex), golden: "messages/query.json"},
		{name: "state", message: State(), golden: "messages/state.json"},
		{name: "ensure workspaces", message: EnsureWorkspaces(1, 4), golden: "messages/ensure-workspaces.json"},
		{name: "workspace name", message: WorkspaceName(1, 2, "spike-room"), golden: "messages/workspace-name.json"},
		{name: "workspace layout", message: WorkspaceLayout(1, 2, LayoutBSP), golden: "messages/workspace-layout.json"},
		{name: "workspace tiling", message: WorkspaceTiling(1, 2, true), golden: "messages/workspace-tiling.json"},
		{name: "layout ratios", message: LayoutRatios([]float32{}, nil), golden: "messages/layout-ratios.json"},
		{name: "stack all", message: StackAll(), golden: "messages/stack-all.json"},
		{name: "unstack all", message: UnstackAll(), golden: "messages/unstack-all.json"},
		{name: "promote", message: Promote(), golden: "messages/promote.json"},
		{name: "stack window", message: StackWindow(DirectionLeft), golden: "messages/stack-window.json"},
		{name: "apply state", message: ApplyState(state), golden: "messages/apply-state.json"},
		{name: "focus monitor workspace number", message: FocusMonitorWorkspaceNumber(1, 2), golden: "messages/focus-monitor-workspace-number.json"},
		{name: "named workspace rule", message: NamedWorkspaceRule(IdentifierExe, "App.exe", "spike-room"), golden: "messages/named-workspace-rule.json"},
		{name: "initial named workspace rule", message: InitialNamedWorkspaceRule(IdentifierExe, "App.exe", "spike-room"), golden: "messages/initial-named-workspace-rule.json"},
		{name: "clear named workspace rules", message: ClearNamedWorkspaceRules("spike-room"), golden: "messages/clear-named-workspace-rules.json"},
		{name: "add subscriber with options", message: AddSubscriberSocketWithOptions("spike.sock", SubscribeOptions{FilterStateChanges: true}), golden: "messages/add-subscriber-socket-with-options.json"},
		{name: "toggle workspace window container behaviour", message: ToggleWorkspaceWindowContainerBehaviour(), golden: "messages/toggle-workspace-window-container-behaviour.json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.message)
			assert.NilError(t, err)
			golden.Assert(t, string(encoded), test.golden)
		})
	}
}

func TestClientSendStateRoundTrip(t *testing.T) {
	//nolint:usetesting // t.TempDir paths exceed the macOS socket path limit.
	dir, err := os.MkdirTemp("", "kmb")
	assert.NilError(t, err)
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})

	listener, err := new(net.ListenConfig).Listen(t.Context(), "unix", filepath.Join(dir, socketName))
	assert.NilError(t, err)
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
	})

	type serverResult struct {
		request []byte
		err     error
	}
	result := make(chan serverResult, 1)
	go func() {
		result <- serveStateRequest(listener)
	}()

	reply, err := NewClient(dir).Send(t.Context(), State())
	assert.NilError(t, err)
	assert.Equal(t, string(reply), `{"monitors":[],"unknown":{"keep":true}}`)

	server := <-result
	assert.NilError(t, server.err)
	assert.Equal(t, string(server.request), `{"type":"State"}`)
}

// serveStateRequest reads one request and writes the test State reply.
func serveStateRequest(listener net.Listener) (result struct {
	request []byte
	err     error
}) {
	conn, err := listener.Accept()
	if err != nil {
		result.err = fmt.Errorf("accepting test request: %w", err)
		return result
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			result.err = errors.Join(result.err, fmt.Errorf("closing test connection: %w", closeErr))
		}
	}()

	result.request, result.err = io.ReadAll(conn)
	if result.err != nil {
		result.err = fmt.Errorf("reading test request: %w", result.err)
		return result
	}
	if err := writeAll(conn, []byte(`{"monitors":[],"unknown":{"keep":true}}`)); err != nil {
		result.err = fmt.Errorf("writing test reply: %w", err)
	}
	return result
}
