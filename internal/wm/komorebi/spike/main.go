//go:build windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/wm/komorebi"
	"github.com/mattwilkinsonn/tessera/internal/wm/komorebi/spike/spikestate"
)

const (
	pollInterval    = 25 * time.Millisecond
	checkTimeout    = 30 * time.Second
	maxFixtureBytes = 16 << 20
)

type resultRow struct {
	check    string
	observed string
	pass     string
	notes    string
}

type runner struct {
	client  *komorebi.Client
	dataDir string
	outDir  string
	reader  *bufio.Reader
	rows    []resultRow
}

type spikeSpaces = spikestate.SpikeSpaces

func main() {
	if err := run(context.Background()); err != nil {
		slog.Error("komorebi W4 spike failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) (runErr error) {
	outDir := flag.String("out", ".\\spike-out", "output directory for scrubbed fixtures and results")
	topology := flag.String("topology", "laptop", "fixture name: laptop or desk")
	flag.Parse()
	if *topology != "laptop" && *topology != "desk" {
		return errors.New("topology must be laptop or desk")
	}
	dataDir, err := komorebi.DataDir()
	if err != nil {
		return fmt.Errorf("locating komorebi data directory: %w", err)
	}
	if err := os.MkdirAll(*outDir, 0o700); err != nil {
		return fmt.Errorf("creating spike output directory: %w", err)
	}
	r := &runner{
		client:  komorebi.NewClient(dataDir),
		dataDir: dataDir,
		outDir:  *outDir,
		reader:  bufio.NewReader(os.Stdin),
	}
	defer func() {
		if err := r.writeResults(); err != nil {
			runErr = errors.Join(runErr, err)
		}
	}()
	return r.runChecks(ctx, *topology)
}

func (r *runner) runChecks(ctx context.Context, topology string) (runErr error) {
	state, err := r.state(ctx)
	if err != nil {
		return err
	}
	if err := r.saveJSON("state-"+topology+".json", state); err != nil {
		return err
	}
	if err := r.observe("1", "State shape and physical pixel units", "Inspect Ring elements/focused and Rect fields in the fixture. Record whether dimensions are physical pixels.", "State fixture recorded"); err != nil {
		return err
	}
	spaces, err := r.createSpikeSpaces(ctx, 3)
	if len(spaces.Spaces) > 0 {
		defer func() {
			if cleanupErr := r.restoreSpikeSpaces(context.WithoutCancel(ctx), spaces); cleanupErr != nil {
				runErr = errors.Join(runErr, fmt.Errorf("restoring spike workspaces: %w", cleanupErr))
			}
		}()
	}
	if err != nil {
		return err
	}
	defer func() {
		if err := r.restoreSpikeSpaces(context.WithoutCancel(ctx), spaces); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("restoring spike workspaces: %w", err))
		}
	}()
	checks := []struct {
		name string
		run  func(context.Context, spikeSpaces) error
	}{
		{name: "2", run: r.checkApplyState},
		{name: "3", run: r.checkRatios},
		{name: "4", run: r.checkAppendBehavior},
		{name: "5", run: r.checkFocus},
		{name: "6", run: r.checkSubscriber},
		{name: "7", run: r.checkSettle},
	}
	for _, check := range checks {
		if err := check.run(ctx, spaces); err != nil {
			return fmt.Errorf("running check %s: %w", check.name, err)
		}
		if err := r.closeSpikeWindows(ctx, spaces); err != nil {
			return fmt.Errorf("cleaning windows after check %s: %w", check.name, err)
		}
		if err := r.resetSpikeSpaces(ctx, spaces); err != nil {
			return fmt.Errorf("resetting workspaces after check %s: %w", check.name, err)
		}
	}
	if err := r.checkLauncher(ctx); err != nil {
		return err
	}
	if err := r.checkNamedRules(ctx, spaces); err != nil {
		return err
	}
	return r.closeSpikeWindows(ctx, spaces)
}

func (r *runner) state(ctx context.Context) (jsontext.Value, error) {
	state, err := r.client.Send(ctx, komorebi.State())
	if err != nil {
		return nil, fmt.Errorf("querying komorebi State: %w", err)
	}
	return state, nil
}

func (r *runner) send(ctx context.Context, message komorebi.SocketMessage) error {
	if _, err := r.client.Send(ctx, message); err != nil {
		return fmt.Errorf("sending komorebi %s: %w", message.Type, err)
	}
	return nil
}

func (r *runner) createSpikeSpaces(ctx context.Context, count int) (spikeSpaces, error) {
	state, err := r.state(ctx)
	if err != nil {
		return spikeSpaces{}, err
	}
	root, err := spikestate.DecodeObject(state)
	if err != nil {
		return spikeSpaces{}, fmt.Errorf("decoding komorebi State: %w", err)
	}
	var monitors spikestate.RingValue
	if err := json.Unmarshal(root["monitors"], &monitors); err != nil {
		return spikeSpaces{}, fmt.Errorf("decoding monitor ring: %w", err)
	}
	if len(monitors.Elements) == 0 || monitors.Focused < 0 || monitors.Focused >= len(monitors.Elements) {
		return spikeSpaces{}, errors.New("komorebi State has no valid focused monitor")
	}
	monitorIndex := monitors.Focused
	monitor, err := spikestate.DecodeObject(monitors.Elements[monitorIndex])
	if err != nil {
		return spikeSpaces{}, fmt.Errorf("decoding focused monitor: %w", err)
	}
	var workspaces spikestate.RingValue
	if err := json.Unmarshal(monitor["workspaces"], &workspaces); err != nil {
		return spikeSpaces{}, fmt.Errorf("decoding focused monitor workspaces: %w", err)
	}
	base := len(workspaces.Elements)
	created := spikeSpaces{
		MonitorIndex:  monitorIndex,
		MonitorID:     bytes.Clone(monitor["id"]),
		MonitorDevice: bytes.Clone(monitor["device_id"]),
		OriginalFocus: workspaces.Focused,
		OriginalCount: base,
		Spaces:        make([]spikestate.SpikeSpace, 0, count),
	}
	if err := r.send(ctx, komorebi.EnsureWorkspaces(monitorIndex, base+count)); err != nil {
		return spikeSpaces{}, err
	}
	state, err = r.state(ctx)
	if err != nil {
		return spikeSpaces{}, err
	}
	_, workspaces, err = spikestate.MonitorWorkspaces(state, monitorIndex)
	if err != nil {
		return spikeSpaces{}, err
	}
	if len(workspaces.Elements) < base+count {
		return spikeSpaces{}, fmt.Errorf("komorebi created %d workspaces; expected at least %d", len(workspaces.Elements)-base, count)
	}
	for offset := range count {
		index := base + offset
		if !spikestate.WorkspaceIsEmpty(workspaces.Elements[index]) {
			return spikeSpaces{}, fmt.Errorf("new workspace at index %d is not empty; refusing to use it", index)
		}
		name := fmt.Sprintf("spike-%d-%d", time.Now().UnixNano(), offset)
		if err := r.send(ctx, komorebi.WorkspaceName(monitorIndex, index, name)); err != nil {
			return spikeSpaces{}, err
		}
		created.Spaces = append(created.Spaces, spikestate.SpikeSpace{Index: index, Name: name})
	}
	state, err = r.state(ctx)
	if err != nil {
		return spikeSpaces{}, err
	}
	monitor, workspaces, err = spikestate.MonitorWorkspaces(state, monitorIndex)
	if err != nil {
		return spikeSpaces{}, err
	}
	if !bytes.Equal(created.MonitorID, monitor["id"]) || !bytes.Equal(created.MonitorDevice, monitor["device_id"]) {
		return spikeSpaces{}, errors.New("focused monitor changed while creating spike workspaces")
	}
	for i := range created.Spaces {
		space := &created.Spaces[i]
		if space.Index >= len(workspaces.Elements) || spikestate.WorkspaceName(workspaces.Elements[space.Index]) != space.Name {
			return spikeSpaces{}, fmt.Errorf("komorebi did not name spike workspace %q", space.Name)
		}
		space.Base = bytes.Clone(workspaces.Elements[space.Index])
	}
	return created, nil
}

func (r *runner) focusSpace(ctx context.Context, spaces spikeSpaces, slot int) error {
	_, err := r.sendIndexedWorkspace(ctx, spaces, slot, komorebi.FocusMonitorWorkspaceNumber)
	return err
}

func (r *runner) sendIndexedWorkspace(ctx context.Context, spaces spikeSpaces, slot int, build func(int, int) komorebi.SocketMessage) (time.Time, error) {
	if slot < 0 || slot >= len(spaces.Spaces) {
		return time.Time{}, fmt.Errorf("spike workspace slot %d is out of range", slot)
	}
	state, err := r.state(ctx)
	if err != nil {
		return time.Time{}, err
	}
	monitorIndex, workspaces, err := spikestate.MonitorWorkspacesFor(state, spaces)
	if err != nil {
		return time.Time{}, err
	}
	workspace := spaces.Spaces[slot]
	workspaceIndex := spikestate.FindWorkspaceIndex(workspaces, workspace.Name)
	if workspaceIndex < 0 || spikestate.WorkspaceName(workspaces.Elements[workspaceIndex]) != workspace.Name {
		return time.Time{}, fmt.Errorf("spike workspace %q is not present on its original monitor", workspace.Name)
	}
	started := time.Now()
	if err := r.send(ctx, build(monitorIndex, workspaceIndex)); err != nil {
		return time.Time{}, err
	}
	return started, nil
}

func (r *runner) readObservationText(prompt string) (string, error) {
	if _, err := fmt.Fprintln(os.Stdout, prompt); err != nil {
		return "", fmt.Errorf("writing spike observation prompt: %w", err)
	}
	line, err := r.reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading spike observation: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func (r *runner) resetSpikeSpaces(ctx context.Context, spaces spikeSpaces) error {
	state, err := r.state(ctx)
	if err != nil {
		return err
	}
	restored, err := spikestate.RestoreWorkspaceValues(state, spaces)
	if err != nil {
		return fmt.Errorf("preparing spike workspace reset: %w", err)
	}
	return r.send(ctx, komorebi.ApplyState(restored))
}

func (r *runner) restoreSpikeSpaces(ctx context.Context, spaces spikeSpaces) error {
	if err := r.resetSpikeSpaces(ctx, spaces); err != nil {
		return fmt.Errorf("restoring spike workspaces: %w", err)
	}
	return nil
}

func (r *runner) closeSpikeWindows(ctx context.Context, spaces spikeSpaces) error {
	state, err := r.state(ctx)
	if err != nil {
		return err
	}
	for _, space := range spaces.Spaces {
		if len(spikestate.WorkspaceWindowIDs(state, space.Name)) == 0 {
			continue
		}
		if err := r.prompt(fmt.Sprintf("Close every disposable window in spike workspace %q. Do not close other windows. Press Enter when it is empty.", space.Name)); err != nil {
			return err
		}
		if err := r.waitForNoWindows(ctx, space.Name); err != nil {
			return err
		}
		state, err = r.state(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) observe(number, title, prompt, expected string) error {
	if _, err := fmt.Fprintf(os.Stdout, "Check %s: %s\n%s\n", number, title, prompt); err != nil {
		return fmt.Errorf("writing spike observation prompt: %w", err)
	}
	answer, err := r.readAnswer()
	if err != nil {
		return err
	}
	pass := "no"
	if answer == "y" {
		pass = "yes"
	}
	r.rows = append(r.rows, resultRow{check: number, observed: answer, pass: pass, notes: expected})
	return nil
}

func (r *runner) record(number, observed string, passed bool, notes string) {
	pass := "no"
	if passed {
		pass = "yes"
	}
	r.rows = append(r.rows, resultRow{check: number, observed: observed, pass: pass, notes: notes})
}

func (r *runner) prompt(message string) error {
	if _, err := fmt.Fprintln(os.Stdout, message); err != nil {
		return fmt.Errorf("writing spike prompt: %w", err)
	}
	if _, err := r.reader.ReadString('\n'); err != nil {
		return fmt.Errorf("reading spike response: %w", err)
	}
	return nil
}

func (r *runner) readAnswer() (string, error) {
	if _, err := fmt.Fprintln(os.Stdout, "Record y or n only. Do not enter titles, paths, names, or serials:"); err != nil {
		return "", fmt.Errorf("writing spike answer prompt: %w", err)
	}
	line, err := r.reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading spike result: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer != "y" && answer != "n" {
		return "", errors.New("answer must be y or n")
	}
	return answer, nil
}

func (r *runner) readAnswerPrompt(prompt string) (string, error) {
	if _, err := fmt.Fprintln(os.Stdout, prompt); err != nil {
		return "", fmt.Errorf("writing spike answer prompt: %w", err)
	}
	return r.readAnswer()
}

func (r *runner) readChoice(prompt string, choices ...string) (string, error) {
	if _, err := fmt.Fprintln(os.Stdout, prompt); err != nil {
		return "", fmt.Errorf("writing spike choice prompt: %w", err)
	}
	line, err := r.reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading spike choice: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if !slices.Contains(choices, answer) {
		return "", errors.New("answer is not one of the listed choices")
	}
	return answer, nil
}

func (r *runner) readExecutableName(prompt string) (string, error) {
	if _, err := fmt.Fprintln(os.Stdout, prompt); err != nil {
		return "", fmt.Errorf("writing executable prompt: %w", err)
	}
	line, err := r.reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading executable name: %w", err)
	}
	name := strings.TrimSpace(line)
	if name == "" || strings.ContainsAny(name, `/\\:`) || filepath.Base(name) != name || !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return "", errors.New("executable must be a basename ending in .exe")
	}
	return name, nil
}

func (r *runner) saveJSON(name string, value jsontext.Value) error {
	scrubbed, err := scrubJSON(value)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.outDir, name), scrubbed, 0o600); err != nil {
		return fmt.Errorf("writing scrubbed fixture %q: %w", name, err)
	}
	return nil
}

func (r *runner) saveNotification(name string, payload []byte) error {
	if len(payload) > maxFixtureBytes {
		return fmt.Errorf("notification exceeds the %d-byte fixture limit", maxFixtureBytes)
	}
	return r.saveJSON(name, jsontext.Value(payload))
}

func (r *runner) writeResults() error {
	var output strings.Builder
	output.WriteString("# komorebi W4 spike results\n\n| Check | Observed answer | Pass | Notes |\n| --- | --- | --- | --- |\n")
	for _, row := range r.rows {
		fmt.Fprintf(&output, "| %s | %s | %s | %s |\n", row.check, escapeTable(row.observed), row.pass, escapeTable(row.notes))
	}
	if err := os.WriteFile(filepath.Join(r.outDir, "spike-results.md"), []byte(output.String()), 0o600); err != nil {
		return fmt.Errorf("writing spike results: %w", err)
	}
	return nil
}

func escapeTable(value string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ", "\r", " ").Replace(value)
}
