//go:build windows

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec" //nolint:depguard // check 8 launches a GUI application to observe console behavior.
	"path/filepath"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/wm/komorebi"
	"github.com/mattwilkinsonn/tessera/internal/wm/komorebi/spike/spikestate"
)

func (r *runner) checkFocus(ctx context.Context, spaces spikeSpaces) error {
	if err := r.focusSpace(ctx, spaces, 1); err != nil {
		return err
	}
	if err := r.prompt("Put two disposable windows in the focused spike workspace. Press Enter when both are managed."); err != nil {
		return err
	}
	if err := r.waitForWindowCount(ctx, spaces.Spaces[1].Name, 2); err != nil {
		return err
	}
	if err := r.prompt("Connect or disconnect an external monitor without further input. Do not disconnect the monitor holding the spike workspaces. Press Enter after the display settles."); err != nil {
		return err
	}
	started, err := r.sendIndexedWorkspace(ctx, spaces, 1, komorebi.FocusMonitorWorkspaceNumber)
	if err != nil {
		return err
	}
	latency, focused, err := r.waitForFocusedWorkspaceSince(ctx, spaces, 1, started, time.Second)
	if err != nil {
		return err
	}
	promoted, err := r.sendFocusedWorkspaceCommand(ctx, spaces, 1, "5", komorebi.Promote())
	if err != nil {
		return err
	}
	if !promoted {
		return nil
	}
	stacked, err := r.sendFocusedWorkspaceCommand(ctx, spaces, 1, "5", komorebi.StackWindow(komorebi.DirectionLeft))
	if err != nil {
		return err
	}
	if !stacked {
		return nil
	}
	if err := r.prompt("Type a test key. Do not enter a title or window name."); err != nil {
		return err
	}
	landed, err := r.readChoice("Where did the key land? Enter f for the intended spike window, s for another spike window, o for outside the spike workspaces, or n for nowhere.", "f", "s", "o", "n")
	if err != nil {
		return err
	}
	foreground, err := r.readAnswerPrompt("Did the foreground move to the intended window within one second? Record y or n.")
	if err != nil {
		return err
	}
	r.record("5 foreground", "foreground_within_1s="+foreground, foreground == "y", "foreground observation is separate from the State focus timer")
	r.record("5", fmt.Sprintf("State_focus_latency=%s focused=%t key_landed=%s", latency.Round(time.Millisecond), focused, landed), focused && latency <= time.Second, "State polling starts just before FocusMonitorWorkspaceNumber and ends when State shows the target")
	return nil
}

func (r *runner) checkSubscriber(ctx context.Context, spaces spikeSpaces) error {
	const socketBase = "tessera-w4.sock"
	socketPath := filepath.Join(r.dataDir, socketBase)
	if _, err := os.Lstat(socketPath); err == nil {
		return fmt.Errorf("subscriber socket path %q already exists", socketBase)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking subscriber socket path: %w", err)
	}
	listener, err := new(net.ListenConfig).Listen(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("listening for komorebi notifications: %w", err)
	}
	unixListener, ok := listener.(*net.UnixListener)
	if !ok {
		return errors.New("notification listener is not a Unix listener")
	}
	defer func() {
		if err := listener.Close(); err != nil {
			slog.Error("closing notification listener", "err", err)
		}
		if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Error("removing notification socket", "err", err)
		}
	}()
	if err := r.send(ctx, komorebi.AddSubscriberSocketWithOptions(socketBase, komorebi.SubscribeOptions{FilterStateChanges: true})); err != nil {
		return err
	}
	if err := r.focusSpace(ctx, spaces, 2); err != nil {
		return err
	}
	if _, err := r.sendIndexedWorkspace(ctx, spaces, 2, func(monitorIndex, workspaceIndex int) komorebi.SocketMessage {
		return komorebi.WorkspaceLayout(monitorIndex, workspaceIndex, komorebi.LayoutColumns)
	}); err != nil {
		return err
	}
	if err := unixListener.SetDeadline(time.Now().Add(checkTimeout)); err != nil {
		return fmt.Errorf("setting notification accept deadline: %w", err)
	}
	payload, err := acceptNotification(unixListener)
	if err != nil {
		return err
	}
	if len(payload) == 0 {
		return errors.New("komorebi sent an empty notification before shutdown")
	}
	if err := r.saveNotification("notification-1.json", payload); err != nil {
		return err
	}
	r.record("6 event", fmt.Sprintf("bytes=%d trailing_newline=%t", len(payload), bytes.HasSuffix(payload, []byte("\n"))), !bytes.HasSuffix(payload, []byte("\n")), "one connection, no newline, scrubbed fixture saved")
	if err := r.prompt("Stop komorebi cleanly now. Press Enter after it stops."); err != nil {
		return err
	}
	if err := unixListener.SetDeadline(time.Now().Add(checkTimeout)); err != nil {
		return fmt.Errorf("setting clean-stop accept deadline: %w", err)
	}
	cleanStop, err := acceptNotification(unixListener)
	if err != nil {
		return err
	}
	r.record("6 clean stop", fmt.Sprintf("connection=true bytes=%d", len(cleanStop)), len(cleanStop) == 0, "clean stop sends one empty connection")
	if err := r.prompt("Restart komorebi and press Enter."); err != nil {
		return err
	}
	if err := r.send(ctx, komorebi.AddSubscriberSocketWithOptions(socketBase, komorebi.SubscribeOptions{FilterStateChanges: true})); err != nil {
		return err
	}
	if err := r.prompt("Run taskkill /F against komorebi, then press Enter."); err != nil {
		return err
	}
	if err := unixListener.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return fmt.Errorf("setting forced-stop accept deadline: %w", err)
	}
	forcedStop, err := acceptNotification(unixListener)
	if err != nil {
		if networkError, ok := errors.AsType[net.Error](err); ok && networkError.Timeout() {
			r.record("6 forced stop", "no connection", true, "taskkill /F sends no empty connection")
			return nil
		}
		return err
	}
	r.record("6 forced stop", fmt.Sprintf("connection=true bytes=%d", len(forcedStop)), len(forcedStop) != 0, "taskkill /F sends no empty connection")
	return nil
}

func (r *runner) checkSettle(ctx context.Context, spaces spikeSpaces) error {
	if err := r.focusSpace(ctx, spaces, 2); err != nil {
		return err
	}
	if err := r.prompt("Open two disposable app windows on this spike workspace."); err != nil {
		return err
	}
	if err := r.waitForWindowCount(ctx, spaces.Spaces[2].Name, 2); err != nil {
		return err
	}
	before, err := r.state(ctx)
	if err != nil {
		return err
	}
	layoutBaseline, err := spikestate.WorkspaceRectanglesForState(before, spaces, 2)
	if err != nil {
		return err
	}
	started := time.Now()
	if _, err := r.sendIndexedWorkspace(ctx, spaces, 2, func(monitorIndex, workspaceIndex int) komorebi.SocketMessage {
		return komorebi.WorkspaceLayout(monitorIndex, workspaceIndex, komorebi.LayoutColumns)
	}); err != nil {
		return err
	}
	layoutDuration, layoutSettled, err := r.waitForStableWindowRectangles(ctx, spaces, 2, layoutBaseline, started, time.Second)
	if err != nil {
		return err
	}
	if !layoutSettled {
		r.record("7", fmt.Sprintf("WorkspaceLayout did not settle within one second (%s)", layoutDuration.Round(time.Millisecond)), false, "window rectangles did not stabilize across consecutive State samples")
		return nil
	}
	before, err = r.state(ctx)
	if err != nil {
		return err
	}
	focusBaseline, err := spikestate.WorkspaceRectanglesForState(before, spaces, 2)
	if err != nil {
		return err
	}
	focusStarted := time.Now()
	promoted, err := r.sendFocusedWorkspaceCommand(ctx, spaces, 2, "7", komorebi.Promote())
	if err != nil {
		return err
	}
	if !promoted {
		return nil
	}
	stacked, err := r.sendFocusedWorkspaceCommand(ctx, spaces, 2, "7", komorebi.StackWindow(komorebi.DirectionLeft))
	if err != nil {
		return err
	}
	if !stacked {
		return nil
	}
	focusDuration, focusSettled, err := r.waitForStableWindowRectangles(ctx, spaces, 2, focusBaseline, focusStarted, time.Second)
	if err != nil {
		return err
	}
	passed := focusSettled
	if !focusSettled {
		r.record("7", fmt.Sprintf("focus sequence did not settle within one second (%s)", focusDuration.Round(time.Millisecond)), false, "window rectangles did not stabilize across consecutive State samples")
		return nil
	}
	r.record("7", fmt.Sprintf("WorkspaceLayout=%s focus_sequence=%s", layoutDuration.Round(time.Millisecond), focusDuration.Round(time.Millisecond)), passed, "window rectangles settled across consecutive State samples")
	return nil
}

func (r *runner) checkLauncher(ctx context.Context) error {
	command := exec.CommandContext(context.WithoutCancel(ctx), "notepad.exe")
	if err := command.Start(); err != nil {
		return fmt.Errorf("starting notepad.exe: %w", err)
	}
	if err := command.Process.Release(); err != nil {
		return fmt.Errorf("releasing notepad.exe process handle: %w", err)
	}
	return r.observe("8", "GUI launcher console behavior", "Record y only if launching notepad.exe without a window flag caused no console flash.", "no console flash")
}

func (r *runner) checkNamedRules(ctx context.Context, spaces spikeSpaces) error {
	if err := r.focusSpace(ctx, spaces, 0); err != nil {
		return err
	}
	executable, err := r.readExecutableName("Enter the executable basename to test, for example Code.exe. Do not enter a path.")
	if err != nil {
		return err
	}
	if err := r.send(ctx, komorebi.NamedWorkspaceRule(komorebi.IdentifierExe, executable, spaces.Spaces[0].Name)); err != nil {
		return err
	}
	if err := r.prompt("Launch one matching executable and one title-only decoy. Compare only their workspace placement."); err != nil {
		return err
	}
	if err := r.observe("9", "Executable workspace rule", "Record y only if the executable match landed on the spike workspace and the title-only decoy did not.", "matching executable routes to its workspace; title-only decoy does not"); err != nil {
		return err
	}
	if err := r.send(ctx, komorebi.ClearNamedWorkspaceRules(spaces.Spaces[0].Name)); err != nil {
		return err
	}
	firstExe, err := r.readExecutableName("Check 10: enter an executable basename for the first spike workspace.")
	if err != nil {
		return err
	}
	secondExe, err := r.readExecutableName("Enter a different executable basename for the other spike workspace.")
	if err != nil {
		return err
	}
	if firstExe == secondExe {
		return errors.New("check 10 needs two different executable basenames")
	}
	if err := r.send(ctx, komorebi.NamedWorkspaceRule(komorebi.IdentifierExe, firstExe, spaces.Spaces[0].Name)); err != nil {
		return err
	}
	if err := r.send(ctx, komorebi.NamedWorkspaceRule(komorebi.IdentifierExe, secondExe, spaces.Spaces[1].Name)); err != nil {
		return err
	}
	if err := r.send(ctx, komorebi.ClearNamedWorkspaceRules(spaces.Spaces[0].Name)); err != nil {
		return err
	}
	if err := r.prompt("Launch one window from each executable. Check whether the second rule still routes its window to its spike workspace."); err != nil {
		return err
	}
	if err := r.observe("10", "Clear one named workspace's rules", "Record y only if the rule on the other spike workspace still routes its window.", "rules on other workspaces stay"); err != nil {
		return err
	}
	for _, space := range spaces.Spaces {
		if err := r.send(ctx, komorebi.ClearNamedWorkspaceRules(space.Name)); err != nil {
			return err
		}
	}
	return nil
}

func acceptNotification(listener *net.UnixListener) ([]byte, error) {
	connection, err := listener.AcceptUnix()
	if err != nil {
		return nil, fmt.Errorf("accepting komorebi notification: %w", err)
	}
	defer func() {
		if err := connection.Close(); err != nil {
			slog.Error("closing notification connection", "err", err)
		}
	}()
	payload, err := io.ReadAll(io.LimitReader(connection, maxFixtureBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading komorebi notification: %w", err)
	}
	if len(payload) > maxFixtureBytes {
		return nil, fmt.Errorf("notification exceeds the %d-byte fixture limit", maxFixtureBytes)
	}
	return payload, nil
}
