package komorebi

import "encoding/json/jsontext"

// SocketMessage has komorebi's externally tagged wire format.
type SocketMessage struct {
	Type    string `json:"type"`
	Content any    `json:"content,omitempty"`
}

func (message SocketMessage) expectsReply() bool {
	return message.Type == "State" || message.Type == "Query"
}

// StateQuery selects a value returned by komorebi's Query command.
type StateQuery string

const (
	QueryFocusedMonitorIndex    StateQuery = "FocusedMonitorIndex"
	QueryFocusedWorkspaceIndex  StateQuery = "FocusedWorkspaceIndex"
	QueryFocusedContainerIndex  StateQuery = "FocusedContainerIndex"
	QueryFocusedWindowIndex     StateQuery = "FocusedWindowIndex"
	QueryFocusedWorkspaceName   StateQuery = "FocusedWorkspaceName"
	QueryFocusedWorkspaceLayout StateQuery = "FocusedWorkspaceLayout"
	QueryFocusedContainerKind   StateQuery = "FocusedContainerKind"
	QueryVersion                StateQuery = "Version"
)

// SubscribeOptions controls the notifications sent to a subscriber socket.
type SubscribeOptions struct {
	FilterStateChanges bool `json:"filter_state_changes"`
}

// DefaultLayout names a predefined komorebi layout.
type DefaultLayout string

const (
	LayoutBSP           DefaultLayout = "BSP"
	LayoutColumns       DefaultLayout = "Columns"
	LayoutRows          DefaultLayout = "Rows"
	LayoutVerticalStack DefaultLayout = "VerticalStack"
)

// OperationDirection names a direction understood by komorebi operations.
type OperationDirection string

const (
	DirectionLeft  OperationDirection = "Left"
	DirectionRight OperationDirection = "Right"
	DirectionUp    OperationDirection = "Up"
	DirectionDown  OperationDirection = "Down"
)

// ApplicationIdentifier selects a window property for workspace rules.
type ApplicationIdentifier string

const (
	IdentifierExe   ApplicationIdentifier = "Exe"
	IdentifierClass ApplicationIdentifier = "Class"
	IdentifierTitle ApplicationIdentifier = "Title"
	IdentifierPath  ApplicationIdentifier = "Path"
)

func State() SocketMessage {
	return SocketMessage{Type: "State"}
}

func Query(query StateQuery) SocketMessage {
	return SocketMessage{Type: "Query", Content: query}
}

func EnsureWorkspaces(monitorIndex, count int) SocketMessage {
	return SocketMessage{Type: "EnsureWorkspaces", Content: []int{monitorIndex, count}}
}

func WorkspaceName(monitorIndex, workspaceIndex int, name string) SocketMessage {
	return SocketMessage{Type: "WorkspaceName", Content: []any{monitorIndex, workspaceIndex, name}}
}

func WorkspaceLayout(monitorIndex, workspaceIndex int, layout DefaultLayout) SocketMessage {
	return SocketMessage{Type: "WorkspaceLayout", Content: []any{monitorIndex, workspaceIndex, layout}}
}

func WorkspaceTiling(monitorIndex, workspaceIndex int, tiled bool) SocketMessage {
	return SocketMessage{Type: "WorkspaceTiling", Content: []any{monitorIndex, workspaceIndex, tiled}}
}

func LayoutRatios(columnRatios, rowRatios []float32) SocketMessage {
	return SocketMessage{
		Type:    "LayoutRatios",
		Content: []any{ratiosValue(columnRatios), ratiosValue(rowRatios)},
	}
}

func ratiosValue(ratios []float32) any {
	if ratios == nil {
		return nil
	}
	return ratios
}

func StackAll() SocketMessage {
	return SocketMessage{Type: "StackAll"}
}

func UnstackAll() SocketMessage {
	return SocketMessage{Type: "UnstackAll"}
}

func Promote() SocketMessage {
	return SocketMessage{Type: "Promote"}
}

func StackWindow(direction OperationDirection) SocketMessage {
	return SocketMessage{Type: "StackWindow", Content: direction}
}

func ApplyState(state jsontext.Value) SocketMessage {
	return SocketMessage{Type: "ApplyState", Content: state}
}

func FocusMonitorWorkspaceNumber(monitorIndex, workspaceIndex int) SocketMessage {
	return SocketMessage{Type: "FocusMonitorWorkspaceNumber", Content: []int{monitorIndex, workspaceIndex}}
}

func NamedWorkspaceRule(kind ApplicationIdentifier, identifier, workspaceName string) SocketMessage {
	return SocketMessage{
		Type:    "NamedWorkspaceRule",
		Content: []any{kind, identifier, workspaceName},
	}
}

func InitialNamedWorkspaceRule(kind ApplicationIdentifier, identifier, workspaceName string) SocketMessage {
	return SocketMessage{
		Type:    "InitialNamedWorkspaceRule",
		Content: []any{kind, identifier, workspaceName},
	}
}

func ClearNamedWorkspaceRules(workspaceName string) SocketMessage {
	return SocketMessage{Type: "ClearNamedWorkspaceRules", Content: workspaceName}
}

func AddSubscriberSocketWithOptions(socketName string, options SubscribeOptions) SocketMessage {
	return SocketMessage{
		Type:    "AddSubscriberSocketWithOptions",
		Content: []any{socketName, options},
	}
}

func ToggleWorkspaceWindowContainerBehaviour() SocketMessage {
	return SocketMessage{Type: "ToggleWorkspaceWindowContainerBehaviour"}
}
