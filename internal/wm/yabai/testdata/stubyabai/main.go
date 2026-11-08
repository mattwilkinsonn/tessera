package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
)

func main() {
	if err := record(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if code := os.Getenv("TESS_YABAI_EXIT_CODE"); code != "" {
		status, err := strconv.Atoi(code)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(status)
	}
	if err := serve(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serve(args []string) error {
	if len(args) == 3 && args[0] == "-m" && args[1] == "rule" && args[2] == "--list" {
		_, err := os.Stdout.WriteString("[]")
		return err
	}
	if os.Getenv("TESS_YABAI_ACTIVE_UNFLOAT") == "1" && len(args) == 4 && args[0] == "-m" && args[1] == "window" && args[2] == "--focus" {
		return focusWindow(args[3])
	}
	if os.Getenv("TESS_YABAI_MUTATE") == "1" && len(args) == 5 && args[0] == "-m" && args[1] == "window" {
		return mutateWindow(args)
	}
	if len(args) < 3 || args[0] != "-m" || args[1] != "query" {
		return nil
	}
	var name string
	switch args[2] {
	case "--windows":
		name = "TESS_YABAI_WINDOWS"
	case "--spaces":
		name = "TESS_YABAI_SPACES"
	case "--displays":
		name = "TESS_YABAI_DISPLAYS"
	default:
		return fmt.Errorf("unsupported query: %s", strings.Join(args, " "))
	}
	if args[2] == "--windows" && len(args) == 5 && args[3] == "--window" && os.Getenv("TESS_YABAI_MISSING_WINDOW") == args[4] {
		return errors.New("could not locate window")
	}
	if exitCode := os.Getenv("TESS_YABAI_WINDOWS_EXIT_CODE"); args[2] == "--windows" && exitCode != "" && exitCode != "0" {
		return errors.New("windows query failed")
	}
	path := os.Getenv(name)
	if path == "" {
		return fmt.Errorf("%s is not set", name)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if args[2] == "--windows" {
		data, err = filterWindows(data, args[3:])
		if err != nil {
			return err
		}
	}
	if args[2] == "--spaces" {
		data, err = filterSpaces(data, args[3:])
		if err != nil {
			return err
		}
	}
	_, err = os.Stdout.Write(data)
	return err
}

func filterWindows(data []byte, args []string) ([]byte, error) {
	var windows []json.RawMessage
	if err := json.Unmarshal(data, &windows); err != nil {
		return nil, fmt.Errorf("decoding windows: %w", err)
	}
	if len(args) == 1 && args[0] == "--window" {
		return focusedWindow(windows)
	}
	if len(args) < 2 {
		return data, nil
	}
	value, err := strconv.Atoi(args[1])
	if err != nil {
		return nil, fmt.Errorf("parsing query index: %w", err)
	}
	key := ""
	switch args[0] {
	case "--window":
		key = "id"
	case "--space":
		key = "space"
	default:
		return nil, fmt.Errorf("unsupported window query: %s", strings.Join(args, " "))
	}
	filtered := make([]json.RawMessage, 0, len(windows))
	for _, window := range windows {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(window, &fields); err != nil {
			return nil, fmt.Errorf("decoding window: %w", err)
		}
		var actual int
		if err := json.Unmarshal(fields[key], &actual); err != nil {
			return nil, fmt.Errorf("decoding window %s: %w", key, err)
		}
		if actual == value {
			// yabai returns one object for --window, and an array for --space.
			if args[0] == "--window" {
				return window, nil
			}
			filtered = append(filtered, window)
		}
	}
	if args[0] == "--window" {
		return nil, fmt.Errorf("could not locate window %d", value)
	}
	return json.Marshal(filtered)
}

// focusedWindow mirrors bare `--window`: the focused window as one object.
func focusedWindow(windows []json.RawMessage) ([]byte, error) {
	for _, window := range windows {
		var fields struct {
			HasFocus bool `json:"has-focus"`
		}
		if err := json.Unmarshal(window, &fields); err != nil {
			return nil, fmt.Errorf("decoding window: %w", err)
		}
		if fields.HasFocus {
			return window, nil
		}
	}
	return nil, errors.New("could not retrieve window details")
}

func filterSpaces(data []byte, args []string) ([]byte, error) {
	var spaces []json.RawMessage
	if err := json.Unmarshal(data, &spaces); err != nil {
		return nil, fmt.Errorf("decoding spaces: %w", err)
	}
	if len(args) == 0 {
		return data, nil
	}
	if args[0] != "--space" {
		return nil, fmt.Errorf("unsupported space query: %s", strings.Join(args, " "))
	}
	for _, space := range spaces {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(space, &fields); err != nil {
			return nil, fmt.Errorf("decoding space: %w", err)
		}
		var focused bool
		if err := json.Unmarshal(fields["has-focus"], &focused); err != nil {
			return nil, fmt.Errorf("decoding space focus: %w", err)
		}
		if focused {
			return space, nil
		}
	}
	return nil, nil
}

func record(args []string) error {
	path := os.Getenv("TESS_YABAI_LOG")
	if path == "" {
		return fmt.Errorf("TESS_YABAI_LOG is not set")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening argv log: %w", err)
	}
	if _, err := file.WriteString(strings.Join(args, "\n") + "\n\n"); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return errors.Join(fmt.Errorf("recording argv: %w", err), fmt.Errorf("closing argv log: %w", closeErr))
		}
		return fmt.Errorf("recording argv: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing argv log: %w", err)
	}
	return nil
}

func mutateWindow(args []string) error {
	path := os.Getenv("TESS_YABAI_WINDOWS")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var windows []map[string]any
	if err := json.Unmarshal(data, &windows); err != nil {
		return err
	}
	id, err := strconv.Atoi(args[2])
	if err != nil {
		return err
	}
	for _, window := range windows {
		if window["id"] != float64(id) {
			continue
		}
		switch args[3] {
		case "--space":
			space, err := strconv.Atoi(args[4])
			if err != nil {
				return err
			}
			window["space"] = space
			if os.Getenv("TESS_YABAI_ACTIVE_UNFLOAT") == "1" {
				spaces, err := readSpaces()
				if err != nil {
					return err
				}
				for _, destination := range spaces {
					if destination["index"] == float64(space) {
						window["display"] = destination["display"]
						break
					}
				}
			}
		case "--toggle":
			if args[4] == "float" {
				floating, _ := window["is-floating"].(bool)
				window["is-floating"] = !floating
				if floating && os.Getenv("TESS_YABAI_ACTIVE_UNFLOAT") == "1" {
					spaces, err := readSpaces()
					if err != nil {
						return err
					}
					for _, space := range spaces {
						if space["has-focus"] == true {
							window["space"] = space["index"]
							window["display"] = space["display"]
							break
						}
					}
				}
			}
		}
	}
	data, err = json.Marshal(windows)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func focusWindow(value string) error {
	if address := os.Getenv("TESS_YABAI_FOCUS_NOTIFY"); address != "" {
		conn, err := net.Dial("tcp", address)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(io.Discard, conn)
		return errors.Join(copyErr, conn.Close())
	}
	if os.Getenv("TESS_YABAI_FOCUS_FAIL") == "1" {
		return errors.New("focus rejected")
	}
	id, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	path := os.Getenv("TESS_YABAI_WINDOWS")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var windows []map[string]any
	if err := json.Unmarshal(data, &windows); err != nil {
		return err
	}
	var focusedSpace any
	found := false
	for _, window := range windows {
		window["has-focus"] = window["id"] == float64(id)
		if window["has-focus"] == true {
			found = true
			focusedSpace = window["space"]
		}
	}
	if !found {
		return errors.New("could not locate window")
	}
	if os.Getenv("TESS_YABAI_FOCUS_STALE") != "1" {
		spaces, err := readSpaces()
		if err != nil {
			return err
		}
		for _, space := range spaces {
			space["has-focus"] = space["index"] == focusedSpace
		}
		data, err := json.Marshal(spaces)
		if err != nil {
			return err
		}
		if err := os.WriteFile(os.Getenv("TESS_YABAI_SPACES"), data, 0o600); err != nil {
			return err
		}
	}
	data, err = json.Marshal(windows)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func readSpaces() ([]map[string]any, error) {
	data, err := os.ReadFile(os.Getenv("TESS_YABAI_SPACES"))
	if err != nil {
		return nil, err
	}
	var spaces []map[string]any
	if err := json.Unmarshal(data, &spaces); err != nil {
		return nil, err
	}
	return spaces, nil
}
