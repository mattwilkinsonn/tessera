package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mattwilkinsonn/tessera/internal/effects"
	"github.com/mattwilkinsonn/tessera/internal/engine"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/mattwilkinsonn/tessera/internal/wm/fake"
)

type failingRealizeDriver struct {
	*fake.Driver
	failRollback bool
	fail         bool
}

func (d *failingRealizeDriver) RealizeSpaceLayout(ctx context.Context, space wm.SpaceID, target wm.SpaceLayoutTarget) error {
	if d.fail {
		return errors.New("unfloat failed")
	}
	return d.Driver.RealizeSpaceLayout(ctx, space, target)
}

func (d *failingRealizeDriver) ToggleFloat(ctx context.Context, id int) error {
	window, found, err := d.Driver.QueryWindow(ctx, id)
	if err == nil && found && window.Floating && d.failRollback {
		return errors.New("rollback failed")
	}
	return d.Driver.ToggleFloat(ctx, id)
}

func TestApplyRecoversStagedWindowsAfterUnfloatFailure(t *testing.T) {
	driver := &failingRealizeDriver{Driver: deskWorld(), fail: true, failRollback: true}
	env := commandEnv(t)
	profile := testProfile(t)
	baseline := deskWorld()
	if err := Apply(t.Context(), profile, baseline, commandEnv(t)); err != nil {
		t.Fatal(err)
	}
	if err := Apply(t.Context(), profile, driver, env); err == nil {
		t.Fatal("first apply succeeded; want unfloat failure")
	}
	windows, err := driver.QueryWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	staged := make([]int, 0)
	for _, window := range windows {
		if window.Floating {
			staged = append(staged, window.ID)
		}
	}
	if len(staged) == 0 {
		t.Fatal("first apply staged no windows")
	}
	driver.fail = false
	driver.failRollback = false
	if err := Apply(t.Context(), profile, driver, env); err != nil {
		t.Fatal(err)
	}
	for _, id := range staged {
		window, found, err := driver.QueryWindow(t.Context(), id)
		want, exists, queryErr := baseline.QueryWindow(t.Context(), id)
		if queryErr != nil || !exists || window.SpaceID != want.SpaceID || window.Frame != want.Frame {
			t.Errorf("window %d placement = %+v, want %+v, error = %v", id, window, want, queryErr)
		}
		if err != nil || !found || window.Floating {
			t.Errorf("staged window %d not tiled: %+v, found=%t, err=%v", id, window, found, err)
		}
	}
}

func TestApplyRollsBackStagedWindows(t *testing.T) {
	driver := &failingRealizeDriver{Driver: deskWorld(), fail: true}
	if err := Apply(t.Context(), testProfile(t), driver, commandEnv(t)); err == nil {
		t.Fatal("apply succeeded; want unfloat failure")
	}
	windows, err := driver.QueryWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, window := range windows {
		if window.Floating {
			t.Errorf("window %d remains floating after rollback", window.ID)
		}
	}
}

type observingFloatDriver struct {
	*fake.Driver
	path string
	seen bool
}

func (d *observingFloatDriver) ToggleFloat(ctx context.Context, id int) error {
	ledger, err := effects.ReadStagedWindows(d.path)
	if err != nil {
		return err
	}
	if _, exists := ledger[id]; !exists {
		return errors.New("window missing from ledger before float")
	}
	d.seen = true
	return errors.New("abort at float")
}

func TestStagingLedgerWrittenBeforeFloat(t *testing.T) {
	env := commandEnv(t)
	path := filepath.Join(filepath.Dir(env.Paths.WindowSlots), "staged-windows.json")
	driver := &observingFloatDriver{Driver: deskWorld(), path: path}
	if err := Apply(t.Context(), testProfile(t), driver, env); err == nil || !driver.seen {
		t.Fatalf("apply error = %v, float observed = %t", err, driver.seen)
	}
}

func TestStagingPrunesTiledAndMissingWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "staged.json")
	staged := map[int]engine.StagedWindow{1: {}, 2: {App: "Arc"}, 3: {}}
	if err := effects.WriteStagedWindows(path, staged); err != nil {
		t.Fatal(err)
	}
	plan := stagingPlan{path: path, staged: staged}
	if err := plan.prune([]wm.Window{{ID: 1}, {ID: 2, App: "Arc", Floating: true}}); err != nil {
		t.Fatal(err)
	}
	got, err := effects.ReadStagedWindows(path)
	if err != nil || len(got) != 1 {
		t.Fatalf("ledger = %v, error = %v", got, err)
	}
	if _, ok := got[2]; !ok {
		t.Fatalf("floating staged window lost: %v", got)
	}
}

func TestLaptopRecoversStagedWindow(t *testing.T) {
	env := commandEnv(t)
	path := filepath.Join(filepath.Dir(env.Paths.WindowSlots), "staged-windows.json")
	if err := effects.WriteStagedWindows(path, map[int]engine.StagedWindow{1: {App: "Arc"}}); err != nil {
		t.Fatal(err)
	}
	driver := fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(1728, 1117)}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 1, Label: "laptop"}},
		Windows:  []fake.SeedWindow{{ID: 1, App: "Arc", SpaceIndex: 1, Floating: true}, {ID: 2, App: "Arc", SpaceIndex: 1, Floating: true}},
	})
	if _, err := Laptop(t.Context(), testProfile(t), driver, env); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 2} {
		window, found, err := driver.QueryWindow(t.Context(), id)
		if err != nil || !found || window.Floating != (id == 2) {
			t.Errorf("window %d = %+v, found = %t, error = %v", id, window, found, err)
		}
	}
}

type selectiveFailureDriver struct{ *fake.Driver }

func (d *selectiveFailureDriver) ToggleFloat(ctx context.Context, id int) error {
	if id == 1 {
		return errors.New("cannot unfloat first window")
	}
	return d.Driver.ToggleFloat(ctx, id)
}

func TestLaptopRecoveryContinuesAfterFailure(t *testing.T) {
	driver := &selectiveFailureDriver{Driver: fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1}}, Spaces: []fake.SeedSpace{{DisplayIdx: 1}},
		Windows: []fake.SeedWindow{{ID: 1, App: "Arc", SpaceIndex: 1, Floating: true}, {ID: 2, App: "Arc", SpaceIndex: 1, Floating: true}},
	})}
	windows, err := driver.QueryWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	plan := stagingPlan{staged: map[int]engine.StagedWindow{1: {App: "Arc"}, 2: {App: "Arc"}}}
	if err := plan.recoverLaptop(t.Context(), driver, windows); err == nil {
		t.Fatal("missing recovery error")
	}
	window, _, err := driver.QueryWindow(t.Context(), 2)
	if err != nil || window.Floating {
		t.Fatalf("second window remains floating: %+v, %v", window, err)
	}
}

func TestDeskRecoversAfterCrash(t *testing.T) {
	env := commandEnv(t)
	driver := deskWorld()
	windows, err := driver.QueryWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ids := make(map[int]engine.StagedWindow)
	for _, window := range windows {
		ids[window.ID] = engine.StagedWindow{App: window.App, Title: window.Title}
		if err := driver.ToggleFloat(t.Context(), window.ID); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(filepath.Dir(env.Paths.WindowSlots), "staged-windows.json")
	if err := effects.WriteStagedWindows(path, ids); err != nil {
		t.Fatal(err)
	}
	if err := Apply(t.Context(), testProfile(t), driver, env); err != nil {
		t.Fatal(err)
	}
	for id := range ids {
		window, found, err := driver.QueryWindow(t.Context(), id)
		if err != nil || !found || window.Floating {
			t.Errorf("window %d not recovered: %+v, %v", id, window, err)
		}
	}
}

func TestReusedStagedIDRemainsUserFloating(t *testing.T) {
	env := commandEnv(t)
	path := filepath.Join(filepath.Dir(env.Paths.WindowSlots), "staged-windows.json")
	if err := os.WriteFile(path, []byte(`{"1":{"app":"Other","title":"old"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if ledger, err := effects.ReadStagedWindows(path); err != nil || len(ledger) != 1 {
		t.Fatalf("identity ledger not loaded: %v, %v", ledger, err)
	}
	driver := fake.New(fake.Seed{
		Displays: []fake.SeedDisplay{{Idx: 1, Frame: displayFrame(1728, 1117)}},
		Spaces:   []fake.SeedSpace{{DisplayIdx: 1, Label: "laptop"}},
		Windows:  []fake.SeedWindow{{ID: 1, App: "Arc", SpaceIndex: 1, Floating: true}},
	})
	if _, err := Laptop(t.Context(), testProfile(t), driver, env); err != nil {
		t.Fatal(err)
	}
	window, found, err := driver.QueryWindow(t.Context(), 1)
	if err != nil || !found || !window.Floating {
		t.Fatalf("reused window was tiled: %+v, %v", window, err)
	}
	ledger, err := effects.ReadStagedWindows(path)
	if err != nil || len(ledger) != 0 {
		t.Fatalf("reused ID retained: %v, %v", ledger, err)
	}
}
