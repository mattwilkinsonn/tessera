package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec" //nolint:depguard // This command intentionally runs the benchmark binaries.
	"path/filepath"
	"slices"
	"time"
)

const (
	newBinaryName = "new"
	snapCommand   = "snap"
	threeColumn   = "3col"
)

type measurement struct {
	binary  string
	command string
	p50     time.Duration
	p90     time.Duration
	min     time.Duration
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func run(ctx context.Context, argv []string, stdout, stderr io.Writer) error {
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
	return compareBinaries(ctx, base, current, *count, stdout, stderr)
}

func compareBinaries(ctx context.Context, base, current string, count int, stdout, stderr io.Writer) error {
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
	shim, marker := filepath.Join(temp, "yabai"), filepath.Join(temp, "shim-ran")
	shimBody := []byte("#!/bin/sh\nprintf x > \"$TESS_BENCH_MARKER\"\nexit 1\n")
	//nolint:gosec // The benchmark must create an executable yabai test shim.
	if err := os.WriteFile(shim, shimBody, 0o755); err != nil {
		return fmt.Errorf("writing yabai stub: %w", err)
	}
	binaries := []benchmarkBinary{{name: "base", path: base}, {name: newBinaryName, path: current}}
	if err := verifyBinaryShims(ctx, binaries, home, shim, marker); err != nil {
		return err
	}
	rows, err := measureCommands(ctx, binaries, count, temp, home, shim, marker)
	if err != nil {
		return err
	}
	baseSize, err := fileSize(base)
	if err != nil {
		return fmt.Errorf("stat base binary: %w", err)
	}
	newSize, err := fileSize(current)
	if err != nil {
		return fmt.Errorf("stat new binary: %w", err)
	}
	return reportMeasurements(stdout, rows, baseSize, newSize)
}

type benchmarkBinary struct {
	name string
	path string
}

func verifyBinaryShims(ctx context.Context, binaries []benchmarkBinary, home, shim, marker string) error {
	for _, binary := range binaries {
		if err := os.WriteFile(marker, nil, 0o600); err != nil {
			return fmt.Errorf("resetting benchmark marker: %w", err)
		}
		if err := probe(ctx, binary.path, []string{snapCommand, threeColumn}, home, shim, marker); err != nil {
			return fmt.Errorf("probing %s binary: %w", binary.name, err)
		}
		if err := verifyMarker(binary.name, marker, "did not invoke"); err != nil {
			return err
		}
	}
	return nil
}

func measureCommands(ctx context.Context, binaries []benchmarkBinary, count int, temp, home, shim, marker string) ([]measurement, error) {
	rows := make([]measurement, 0, 5)
	for _, binary := range binaries {
		commands := [][]string{{snapCommand, threeColumn}, {"focus", "east"}}
		if binary.name == newBinaryName {
			commands = append(commands, []string{snapCommand, "--help"})
		}
		for _, argv := range commands {
			times, err := timeCommand(ctx, binary, argv, count, temp, home, shim, marker)
			if err != nil {
				return nil, err
			}
			slices.Sort(times)
			rows = append(rows, measurement{binary: binary.name, command: joinArgs(argv), p50: percentile(times, 0.50), p90: percentile(times, 0.90), min: times[0]})
		}
	}
	return rows, nil
}

func timeCommand(ctx context.Context, binary benchmarkBinary, argv []string, count int, temp, home, shim, marker string) ([]time.Duration, error) {
	times := make([]time.Duration, 0, count)
	for range count {
		if err := os.WriteFile(marker, nil, 0o600); err != nil {
			return nil, fmt.Errorf("resetting benchmark marker: %w", err)
		}
		start := time.Now()
		command := commandFor(ctx, binary.path, argv, home, shim, marker)
		command.Dir = temp
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		if err := command.Run(); err != nil {
			return nil, fmt.Errorf("%s %s failed: %w", binary.name, joinArgs(argv), err)
		}
		times = append(times, time.Since(start))
		if len(argv) == 2 && argv[0] == snapCommand && argv[1] == threeColumn {
			if err := verifyMarker(binary.name, marker, "stopped invoking"); err != nil {
				return nil, err
			}
		}
	}
	return times, nil
}

func verifyMarker(binary, marker, action string) error {
	info, err := os.Stat(marker)
	if err != nil {
		return fmt.Errorf("checking %s benchmark marker: %w", binary, err)
	}
	if info == nil || info.Size() == 0 {
		return fmt.Errorf("%s binary %s TESS_YABAI; refusing an invalid comparison", binary, action)
	}
	return nil
}

func reportMeasurements(stdout io.Writer, rows []measurement, baseSize, newSize int64) error {
	hotRatio, bars := evaluateBars(rows, newSize)
	if _, err := fmt.Fprintln(stdout, "| binary | command | p50 (ms) | p90 (ms) | min (ms) | size (MB) |\n| --- | --- | ---: | ---: | ---: | ---: |"); err != nil {
		return fmt.Errorf("writing benchmark output: %w", err)
	}
	for _, row := range rows {
		size := baseSize
		if row.binary == newBinaryName {
			size = newSize
		}
		if _, err := fmt.Fprintf(stdout, "| %s | `%s` | %.1f | %.1f | %.1f | %.2f |\n", row.binary, row.command, millis(row.p50), millis(row.p90), millis(row.min), float64(size)/1_000_000); err != nil {
			return fmt.Errorf("writing benchmark row: %w", err)
		}
	}
	if _, err := fmt.Fprintf(stdout, "\nHot-path p50 ratio (worst row): %.2fx\n\nBinary sizes: base %d bytes; new %d bytes.\n", hotRatio, baseSize, newSize); err != nil {
		return fmt.Errorf("writing benchmark summary: %w", err)
	}
	return writeBars(stdout, bars)
}

func writeBars(stdout io.Writer, bars []bar) error {
	passed := true
	for _, bar := range bars {
		if _, err := fmt.Fprintf(stdout, "%s %s\n", map[bool]string{true: "PASS", false: "FAIL"}[bar.pass], bar.name); err != nil {
			return fmt.Errorf("writing benchmark bar: %w", err)
		}
		passed = passed && bar.pass
	}
	if !passed {
		return errStartupBar
	}
	return nil
}

var errStartupBar = errors.New("startup acceptance bar failed")

type bar struct {
	name string
	pass bool
}

// evaluateBars applies the record's startup bar to measured rows; it does no timing itself.
func evaluateBars(rows []measurement, newSize int64) (float64, []bar) {
	snapBase := findMeasurement(rows, "base", "snap 3col")
	focusBase := findMeasurement(rows, "base", "focus east")
	snapNew := findMeasurement(rows, "new", "snap 3col")
	focusNew := findMeasurement(rows, "new", "focus east")
	helpNew := findMeasurement(rows, "new", "snap --help")
	hotRatio := max(float64(snapNew.p50)/float64(snapBase.p50), float64(focusNew.p50)/float64(focusBase.p50))
	return hotRatio, []bar{
		{name: "hot-path p50 ratio <= 2.0x", pass: hotRatio <= 2},
		{name: "hot-path p90 <= 45 ms", pass: snapNew.p90 <= 45*time.Millisecond && focusNew.p90 <= 45*time.Millisecond},
		{name: "help p50 <= 60 ms", pass: helpNew.p50 <= 60*time.Millisecond},
		{name: "binary size <= 70 MB", pass: newSize <= 70_000_000},
	}
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

func commandFor(ctx context.Context, binary string, args []string, home, shim, marker string) *exec.Cmd {
	//nolint:gosec // Benchmarked binaries are explicit caller-supplied paths.
	command := exec.CommandContext(ctx, binary, args...)
	// An empty TESSERA_PROFILE keeps a host profile out of the measurement.
	command.Env = append(os.Environ(), "HOME="+home, "TESSERA_PROFILE=", "TESS_YABAI="+shim, "TESS_BENCH_MARKER="+marker)
	return command
}

func probe(ctx context.Context, binary string, args []string, home, shim, marker string) error {
	command := commandFor(ctx, binary, args, home, shim, marker)
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
