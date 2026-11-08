package commands

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
)

func TestApplySpawnLedger(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		profile := testProfile(t)
		profile.Windows["arc"] = config.WindowSpec{App: "Zen", AppRE: regexp.MustCompile("Zen"), Spawn: []string{"open", "Zen"}}
		driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}}, SpawnNoWindow: true})
		env := commandEnv(t)
		if err := Apply(t.Context(), profile, driver, env); err != nil {
			t.Fatal(err)
		}
		first := len(driver.SpawnCalls)
		if first == 0 {
			t.Fatal("first apply did not launch")
		}
		if err := Apply(t.Context(), profile, driver, env); err != nil {
			t.Fatal(err)
		}
		if len(driver.SpawnCalls) != first {
			t.Fatalf("launcher called again: first %d, total %d", first, len(driver.SpawnCalls))
		}
	})
}

func TestApplySpawnUptime(t *testing.T) {
	for _, test := range []struct {
		name   string
		uptime time.Duration
		spawn  bool
	}{{name: "starting", uptime: 5 * time.Second}, {name: "stable", uptime: 60 * time.Second, spawn: true}} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				profile := testProfile(t)
				profile.Windows["arc"] = config.WindowSpec{App: "Zen", AppRE: regexp.MustCompile("Zen"), Spawn: []string{"open", "Zen"}}
				driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}}, SpawnNoWindow: true})
				driver.Uptime = test.uptime
				env := commandEnv(t)
				var stderr bytes.Buffer
				env.Stderr = &stderr
				if err := Apply(t.Context(), profile, driver, env); err != nil {
					t.Fatal(err)
				}
				if got := len(driver.SpawnCalls) > 0; got != test.spawn {
					t.Fatalf("spawned = %v, want %v", got, test.spawn)
				}
				if !test.spawn && !strings.Contains(stderr.String(), "tess: holding spawns:") {
					t.Fatalf("missing hold message: %s", stderr.String())
				}
			})
		})
	}
}
