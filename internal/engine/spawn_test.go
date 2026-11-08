package engine

import (
	"regexp"
	"testing"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

func TestSpawnsNeededCountsAllStates(t *testing.T) {
	profile := &config.Profile{Windows: map[string]config.WindowSpec{"zen": {App: "^Zen$", AppRE: regexp.MustCompile("^Zen$"), Spawn: []string{"open", "Zen"}}}}
	for _, test := range []struct {
		name    string
		windows []wm.Window
		want    int
	}{
		{name: "three floating", windows: []wm.Window{{ID: 1, App: "Zen", Floating: true}, {ID: 2, App: "Zen", Floating: true}, {ID: 3, App: "Zen", Floating: true}}, want: 0},
		{name: "one floating", windows: []wm.Window{{ID: 1, App: "Zen", Floating: true}}, want: 2},
		{name: "minimized other space", windows: []wm.Window{{ID: 1, App: "Zen", Minimized: true, SpaceID: "other"}}, want: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := SpawnsNeeded(profile, []string{"zen", "zen", "zen"}, test.windows); len(got) != test.want {
				t.Fatalf("spawns = %v, want %d", got, test.want)
			}
		})
	}
}
