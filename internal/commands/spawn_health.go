package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/wm"
)

func spawnHold(ctx context.Context, driver wm.Driver) (string, time.Duration) {
	if wm.QueryFailed(ctx) {
		return "yabai query failed", wm.SpawnReadyUptime
	}
	health, ok := driver.(wm.SpawnHealth)
	if !ok {
		return "", 0
	}
	uptime, err := health.ServiceUptime(ctx)
	if err != nil {
		return err.Error(), wm.SpawnReadyUptime
	}
	if uptime < wm.SpawnReadyUptime {
		return fmt.Sprintf("yabai uptime %s is below %s", uptime, wm.SpawnReadyUptime), wm.SpawnReadyUptime - uptime
	}
	return "", 0
}
