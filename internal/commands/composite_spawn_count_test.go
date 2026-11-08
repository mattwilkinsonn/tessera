package commands

import (
	"context"
	"regexp"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
)

type mismatchedLaunchDriver struct {
	*fake.Driver
	closed map[int]bool
}

func (d *mismatchedLaunchDriver) QueryWindows(ctx context.Context) ([]wm.Window, error) {
	windows, err := d.Driver.QueryWindows(ctx)
	if err != nil {
		return nil, err
	}
	current := make([]wm.Window, 0, len(windows))
	for _, window := range windows {
		if !d.closed[window.ID] {
			window.Title = "~"
			current = append(current, window)
		}
	}
	return current, nil
}

func TestApplyLaunchCountPreventsTitleStorm(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		profile := testProfile(t)
		profile.Windows["ghostty-wave"] = config.WindowSpec{App: "Ghostty", AppRE: regexp.MustCompile("Ghostty"), Title: "devbox", TitleRE: regexp.MustCompile("devbox"), Spawn: []string{"open", "-na", "Ghostty"}}
		for i := range profile.Desk {
			profile.Desk[i].Tracks = nil
		}
		profile.Desk[0].Tracks = [][]string{{"ghostty-wave"}}
		driver := &mismatchedLaunchDriver{Driver: fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}}, SpawnApp: "Ghostty"}), closed: make(map[int]bool)}
		now := time.Now()
		env := commandEnv(t)
		env.Now = func() time.Time { return now }
		for run := range 3 {
			if err := Apply(t.Context(), profile, driver, env); err != nil {
				t.Fatal(err)
			}
			if len(driver.SpawnCalls) != 1 {
				t.Fatalf("run %d launched %d times, want one total", run, len(driver.SpawnCalls))
			}
			now = now.Add(6 * time.Minute)
		}
		windows, err := driver.QueryWindows(t.Context())
		if err != nil || len(windows) != 1 {
			t.Fatalf("windows = %v, %v", windows, err)
		}
		driver.closed[windows[0].ID] = true
		if err := Apply(t.Context(), profile, driver, env); err != nil {
			t.Fatal(err)
		}
		if len(driver.SpawnCalls) != 2 {
			t.Fatalf("after close launches = %d, want two total", len(driver.SpawnCalls))
		}
	})
}

func TestApplyUnobservedLaunchRetries(t *testing.T) {
	for _, test := range []struct {
		name     string
		fails    int
		noWindow bool
	}{{name: "failed", fails: 1}, {name: "windowless", noWindow: true}} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				profile := testProfile(t)
				profile.Windows["arc"] = config.WindowSpec{App: "Zen", AppRE: regexp.MustCompile("Zen"), Spawn: []string{"open", "Zen"}}
				driver := fake.New(fake.Seed{Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(5120, 2160)}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}}, SpawnApp: "Zen", SpawnFails: test.fails, SpawnNoWindow: test.noWindow})
				now := time.Now()
				env := commandEnv(t)
				env.Now = func() time.Time { return now }
				if err := Apply(t.Context(), profile, driver, env); err != nil {
					t.Fatal(err)
				}
				first := len(driver.SpawnCalls)
				now = now.Add(6 * time.Minute)
				if err := Apply(t.Context(), profile, driver, env); err != nil {
					t.Fatal(err)
				}
				if len(driver.SpawnCalls) != first+1 {
					t.Fatalf("attempts = %d, want %d", len(driver.SpawnCalls), first+1)
				}
			})
		})
	}
}
