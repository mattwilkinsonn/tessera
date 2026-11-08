package engine

import (
	"regexp"
	"testing"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

func TestSpawnsNeededPreservesTitles(t *testing.T) {
	profile := &config.Profile{Windows: map[string]config.WindowSpec{}}
	for _, name := range []string{"devbox", "mbp"} {
		profile.Windows[name] = config.WindowSpec{App: "Ghostty", AppRE: regexp.MustCompile("Ghostty"), Title: name, TitleRE: regexp.MustCompile(name), Spawn: []string{"open", "Ghostty"}}
	}
	profile.Windows["generic"] = config.WindowSpec{App: "Ghostty", AppRE: regexp.MustCompile("Ghostty"), Spawn: []string{"open", "Ghostty"}}
	for _, test := range []struct {
		name  string
		names []string
		want  []string
	}{
		{name: "titled floating", names: []string{"devbox", "mbp"}, want: []string{"devbox"}},
		{name: "generic cannot steal", names: []string{"generic", "mbp"}, want: []string{"generic"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := SpawnsNeeded(profile, test.names, []wm.Window{{ID: 1, App: "Ghostty", Title: "mbp", Floating: true}})
			if len(got) != 1 || got[0] != test.want[0] {
				t.Fatalf("spawns = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSpawnsNeededAugmentingPaths(t *testing.T) {
	profile := &config.Profile{Windows: map[string]config.WindowSpec{}}
	for name, title := range map[string]string{"broad": "work|scratch", "narrow": "^work$"} {
		profile.Windows[name] = config.WindowSpec{AppRE: regexp.MustCompile("Ghostty"), TitleRE: regexp.MustCompile(title), Spawn: []string{"open", "Ghostty"}}
	}
	for _, order := range []struct {
		name   string
		titles []string
	}{
		{name: "work first", titles: []string{"work", "scratch"}},
		{name: "scratch first", titles: []string{"scratch", "work"}},
	} {
		t.Run(order.name, func(t *testing.T) {
			windows := []wm.Window{{ID: 1, App: "Ghostty", Title: order.titles[0], Floating: true}, {ID: 2, App: "Ghostty", Title: order.titles[1], Floating: true}}
			if got := SpawnsNeeded(profile, []string{"broad", "narrow"}, windows); len(got) != 0 {
				t.Fatalf("spawns = %v, want none", got)
			}
		})
	}
}
