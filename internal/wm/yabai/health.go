package yabai

import (
	"context"
	"fmt"
	"os/exec" //nolint:depguard // Process metadata establishes yabai readiness.
	"strconv"
	"strings"
	"time"
)

func (d *Driver) ServiceUptime(ctx context.Context) (time.Duration, error) {
	output, err := exec.CommandContext(ctx, "/usr/bin/pgrep", "-x", "yabai").Output()
	if err != nil {
		return 0, fmt.Errorf("finding yabai process: %w", err)
	}
	pids := strings.Fields(string(output))
	if len(pids) == 0 {
		return 0, fmt.Errorf("yabai process is absent")
	}
	uptime := time.Duration(1<<63 - 1)
	for _, pid := range pids {
		output, err := exec.CommandContext(ctx, "/bin/ps", "-o", "etime=", "-p", pid).Output()
		if err != nil {
			return 0, fmt.Errorf("reading yabai uptime: %w", err)
		}
		elapsed, err := parseElapsed(strings.TrimSpace(string(output)))
		if err != nil {
			return 0, err
		}
		uptime = min(uptime, elapsed)
	}
	return uptime, nil
}

func parseElapsed(value string) (time.Duration, error) {
	var days int
	if day, rest, ok := strings.Cut(value, "-"); ok {
		n, err := strconv.Atoi(day)
		if err != nil {
			return 0, fmt.Errorf("parsing yabai uptime %q: %w", value, err)
		}
		days, value = n, rest
	}
	seconds := days * 86400
	parts := strings.Split(value, ":")
	total := 0
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return 0, fmt.Errorf("parsing yabai uptime %q: %w", value, err)
		}
		total = total*60 + n
	}
	return time.Duration(seconds+total) * time.Second, nil
}
