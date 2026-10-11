package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"time"
)

type measurement struct {
	binary  string
	command string
	p50     time.Duration
	p90     time.Duration
	min     time.Duration
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func run(argv []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("benchstartup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	count := flags.Int("n", 40, "measurements per command")
	if err := flags.Parse(argv); err != nil {
		return err
	}
	args := flags.Args()
	if len(args) != 2 || *count < 1 {
		return errors.New("usage: benchstartup [-n count] <base-binary> <new-binary>")
	}
	base, err := resolveBinary(args[0])
	if err != nil {
		return fmt.Errorf("resolving base binary: %w", err)
	}
	current, err := resolveBinary(args[1])
	if err != nil {
		return fmt.Errorf("resolving new binary: %w", err)
	}
	temp, err := os.MkdirTemp("/tmp", "tess-startup-")
	if err != nil {
		return fmt.Errorf("creating benchmark directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(temp); err != nil {
			_, _ = fmt.Fprintf(stderr, "removing benchmark directory: %v\n", err)
		}
	}()
	home := filepath.Join(temp, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		return fmt.Errorf("creating benchmark home: %w", err)
	}
	shim := filepath.Join(temp, "yabai")
	marker := filepath.Join(temp, "shim-ran")
	shimBody := []byte("#!/bin/sh\nprintf x > \"$TESS_BENCH_MARKER\"\nexit 1\n")
	if err := os.WriteFile(shim, shimBody, 0o755); err != nil {
		return fmt.Errorf("writing yabai stub: %w", err)
	}
	binaries := []struct {
		name string
		path string
	}{{name: "base", path: base}, {name: "new", path: current}}
	for _, binary := range binaries {
		if err := os.WriteFile(marker, nil, 0o600); err != nil {
			return fmt.Errorf("resetting benchmark marker: %w", err)
		}
		if err := probe(binary.path, []string{"snap", "3col"}, temp, home, shim, marker); err != nil {
			return fmt.Errorf("probing %s binary: %w", binary.name, err)
		}
		info, err := os.Stat(marker)
		if err != nil || info.Size() == 0 {
			return fmt.Errorf("%s binary did not invoke TESS_YABAI; refusing an invalid comparison", binary.name)
		}
	}
	rows := make([]measurement, 0, 5)
	for _, binary := range binaries {
		commands := [][]string{{"snap", "3col"}, {"focus", "east"}}
		if binary.name == "new" {
			commands = append(commands, []string{"snap", "--help"})
		}
		for _, argv := range commands {
			times := make([]time.Duration, 0, *count)
			for range *count {
				if err := os.WriteFile(marker, nil, 0o600); err != nil {
					return fmt.Errorf("resetting benchmark marker: %w", err)
				}
				start := time.Now()
				command := commandFor(binary.path, argv, temp, home, shim, marker)
				command.Stdout = io.Discard
				command.Stderr = io.Discard
				if err := command.Run(); err != nil {
					return fmt.Errorf("%s %s failed: %w", binary.name, joinArgs(argv), err)
				}
				times = append(times, time.Since(start))
				if argv[0] == "snap" && len(argv) == 2 && argv[1] == "3col" {
					info, err := os.Stat(marker)
					if err != nil || info.Size() == 0 {
						return fmt.Errorf("%s binary stopped invoking TESS_YABAI; refusing an invalid measurement", binary.name)
					}
				}
			}
			slices.Sort(times)
			rows = append(rows, measurement{binary: binary.name, command: joinArgs(argv), p50: percentile(times, 0.50), p90: percentile(times, 0.90), min: times[0]})
		}
	}
	baseSize, err := fileSize(base)
	if err != nil {
		return fmt.Errorf("stat base binary: %w", err)
	}
	newSize, err := fileSize(current)
	if err != nil {
		return fmt.Errorf("stat new binary: %w", err)
	}
	snapBase := findMeasurement(rows, "base", "snap 3col")
	focusBase := findMeasurement(rows, "base", "focus east")
	snapNew := findMeasurement(rows, "new", "snap 3col")
	focusNew := findMeasurement(rows, "new", "focus east")
	helpNew := findMeasurement(rows, "new", "snap --help")
	hotRatio := max(float64(snapNew.p50)/float64(snapBase.p50), float64(focusNew.p50)/float64(focusBase.p50))
	bars := []struct {
		name string
		pass bool
	}{
		{name: "hot-path p50 ratio <= 2.0x", pass: hotRatio <= 2},
		{name: "hot-path p90 <= 45 ms", pass: snapNew.p90 <= 45*time.Millisecond && focusNew.p90 <= 45*time.Millisecond},
		{name: "help p50 <= 60 ms", pass: helpNew.p50 <= 60*time.Millisecond},
		{name: "binary size <= 70 MB", pass: newSize <= 70_000_000},
	}
	if _, err := fmt.Fprintln(stdout, "| binary | command | p50 (ms) | p90 (ms) | min (ms) | size (MB) |\n| --- | --- | ---: | ---: | ---: | ---: |"); err != nil {
		return fmt.Errorf("writing benchmark output: %w", err)
	}
	for _, row := range rows {
		size := baseSize
		if row.binary == "new" {
			size = newSize
		}
		if _, err := fmt.Fprintf(stdout, "| %s | `%s` | %.1f | %.1f | %.1f | %.2f |\n", row.binary, row.command, millis(row.p50), millis(row.p90), millis(row.min), float64(size)/1_000_000); err != nil {
			return fmt.Errorf("writing benchmark row: %w", err)
		}
	}
	if _, err := fmt.Fprintf(stdout, "\nHot-path p50 ratio (worst row): %.2fx\n\nBinary sizes: base %d bytes; new %d bytes.\n", hotRatio, baseSize, newSize); err != nil {
		return fmt.Errorf("writing benchmark summary: %w", err)
	}
	passed := true
	for _, bar := range bars {
		if _, err := fmt.Fprintf(stdout, "%s %s\n", map[bool]string{true: "PASS", false: "FAIL"}[bar.pass], bar.name); err != nil {
			return fmt.Errorf("writing benchmark bar: %w", err)
		}
		passed = passed && bar.pass
	}
	if !passed {
		return errors.New("startup acceptance bar failed")
	}
	return nil
}

func resolveBinary(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		absolute = filepath.Join(absolute, "bin", "tess")
		if _, err := os.Stat(absolute); err != nil {
			return "", err
		}
	}
	return absolute, nil
}

func commandFor(binary string, args []string, dir, home, shim, marker string) *exec.Cmd {
	command := exec.Command(binary, args...)
	command.Dir = dir
	// An empty TESSERA_PROFILE keeps a host profile out of the measurement.
	command.Env = append(os.Environ(), "HOME="+home, "TESSERA_PROFILE=", "TESS_YABAI="+shim, "TESS_BENCH_MARKER="+marker)
	return command
}

func probe(binary string, args []string, dir, home, shim, marker string) error {
	command := commandFor(binary, args, dir, home, shim, marker)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
}

func findMeasurement(rows []measurement, binary, command string) measurement {
	for _, row := range rows {
		if row.binary == binary && row.command == command {
			return row
		}
	}
	return measurement{}
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func joinArgs(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	return args[0] + " " + args[1]
}

func percentile(values []time.Duration, fraction float64) time.Duration {
	return values[int(float64(len(values)-1)*fraction)]
}

func millis(value time.Duration) float64 {
	return float64(value) / float64(time.Millisecond)
}
