package main

import (
	"encoding/json"
	"errors"
	"fmt"
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
			filtered = append(filtered, window)
			if args[0] == "--window" {
				break
			}
		}
	}
	return json.Marshal(filtered)
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
