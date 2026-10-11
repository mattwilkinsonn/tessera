package engine

import (
	"testing"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/config/configtest"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"github.com/samber/mo"
)

func claimWindow(id int, app, title string, display int, minimized, floating bool) wm.Window {
	return wm.Window{ID: id, App: app, Title: title, DisplayIdx: display, SpaceID: "s1", Minimized: minimized, Floating: floating}
}

func engineTestProfile(t *testing.T) *config.Profile {
	t.Helper()
	profile := configtest.Profile()
	if err := config.Validate(profile); err != nil {
		t.Fatal(err)
	}
	return profile
}

func TestClaimSet(t *testing.T) {
	t.Run("pass 1 prefers a candidate on target display", testClaimSetPrefersTargetDisplay)
	t.Run("no candidate on preferred display falls through", testClaimSetFallsBackFromDisplay)
	t.Run("preferred candidate already claimed falls through", testClaimSetSkipsClaimedCandidate)
	t.Run("distinct claims exhaust candidates", testClaimSetExhaustsCandidates)
	t.Run("no matching and absent name", testClaimSetRejectsUnknownNames)
	t.Run("minimized and floating candidates are ignored", testClaimSetIgnoresUnavailableWindows)
	t.Run("spawn fallback claims unclassified blank app window only", testClaimSetSpawnFallbackSkipsClassified)
	t.Run("spawn fallback prefers blank window on requested display", testClaimSetSpawnFallbackPrefersDisplay)
	t.Run("does not fallback without spawn command", testClaimSetDoesNotFallbackWithoutSpawn)
	t.Run("claim many preserves name order and distinct ids", testClaimSetManyPreservesOrder)
	t.Run("reset clears claimed ids", testClaimSetReset)
}

func testClaimSetPrefersTargetDisplay(t *testing.T) {
	claims := NewClaimSet(engineTestProfile(t))
	windows := []wm.Window{claimWindow(1, "Arc", "", 2, false, false), claimWindow(2, "Arc", "", 3, false, false)}
	if got, ok := claims.Claim(windows, "arc", mo.Some(3)); !ok || got != 2 {
		t.Fatalf("Claim() = %d, %t; want 2, true", got, ok)
	}
}

func testClaimSetFallsBackFromDisplay(t *testing.T) {
	claims := NewClaimSet(engineTestProfile(t))
	windows := []wm.Window{claimWindow(1, "Arc", "", 1, false, false), claimWindow(2, "Arc", "", 1, false, false)}
	if got, ok := claims.Claim(windows, "arc", mo.Some(9)); !ok || got != 1 {
		t.Fatalf("Claim() = %d, %t; want 1, true", got, ok)
	}
}

func testClaimSetSkipsClaimedCandidate(t *testing.T) {
	claims := NewClaimSet(engineTestProfile(t))
	windows := []wm.Window{claimWindow(1, "Arc", "", 3, false, false), claimWindow(2, "Arc", "", 5, false, false)}
	first, firstOK := claims.Claim(windows, "arc", mo.Some(3))
	if !firstOK || first != 1 {
		t.Fatalf("first Claim() = %d, %t; want 1, true", first, firstOK)
	}
	if got, ok := claims.Claim(windows, "arc", mo.Some(3)); !ok || got != 2 {
		t.Fatalf("second Claim() = %d, %t; want 2, true", got, ok)
	}
}

func testClaimSetExhaustsCandidates(t *testing.T) {
	claims := NewClaimSet(engineTestProfile(t))
	windows := []wm.Window{claimWindow(1, "Arc", "", 1, false, false), claimWindow(2, "Arc", "", 1, false, false)}
	first, firstOK := claims.Claim(windows, "arc", mo.None[int]())
	second, secondOK := claims.Claim(windows, "arc", mo.None[int]())
	if !firstOK || !secondOK || first == second {
		t.Fatalf("distinct claims = %d/%t and %d/%t", first, firstOK, second, secondOK)
	}
	if _, ok := claims.Claim(windows, "arc", mo.None[int]()); ok {
		t.Fatal("third claim succeeded after candidates exhausted")
	}
}

func testClaimSetRejectsUnknownNames(t *testing.T) {
	claims := NewClaimSet(engineTestProfile(t))
	if _, ok := claims.Claim([]wm.Window{claimWindow(1, "Ghostty", "pc", 1, false, false)}, "arc", mo.None[int]()); ok {
		t.Fatal("unmatched window claimed")
	}
	if _, ok := claims.Claim([]wm.Window{claimWindow(1, "Arc", "", 1, false, false)}, "no-such-name", mo.None[int]()); ok {
		t.Fatal("unknown name claimed a window")
	}
}

func testClaimSetIgnoresUnavailableWindows(t *testing.T) {
	claims := NewClaimSet(engineTestProfile(t))
	windows := []wm.Window{
		claimWindow(1, "Arc", "", 1, true, false),
		claimWindow(2, "Arc", "", 1, false, true),
		claimWindow(3, "Arc", "", 1, false, false),
	}
	if got, ok := claims.Claim(windows, "arc", mo.None[int]()); !ok || got != 3 {
		t.Fatalf("Claim() = %d, %t; want 3, true", got, ok)
	}
}

func testClaimSetSpawnFallbackSkipsClassified(t *testing.T) {
	profile := engineTestProfile(t)
	spec := profile.Windows["ghostty-wave"]
	spec.Spawn = []string{"open", "-na", "Ghostty"}
	profile.Windows["ghostty-wave"] = spec
	claims := NewClaimSet(profile)
	classified := claimWindow(1, "Ghostty", "mbp", 1, false, false)
	if _, ok := claims.Claim([]wm.Window{classified}, "ghostty-wave", mo.None[int]()); ok {
		t.Fatal("fallback claimed window classified by another spec")
	}
	blank := claimWindow(2, "Ghostty", "", 1, false, false)
	if got, ok := claims.Claim([]wm.Window{classified, blank}, "ghostty-wave", mo.None[int]()); !ok || got != 2 {
		t.Fatalf("fallback Claim() = %d, %t; want 2, true", got, ok)
	}
}

func testClaimSetSpawnFallbackPrefersDisplay(t *testing.T) {
	profile := engineTestProfile(t)
	spec := profile.Windows["ghostty-wave"]
	spec.Spawn = []string{"open", "-na", "Ghostty"}
	profile.Windows["ghostty-wave"] = spec
	claims := NewClaimSet(profile)
	windows := []wm.Window{claimWindow(1, "Ghostty", "", 1, false, false), claimWindow(2, "Ghostty", "", 3, false, false)}
	if got, ok := claims.Claim(windows, "ghostty-wave", mo.Some(3)); !ok || got != 2 {
		t.Fatalf("fallback Claim() = %d, %t; want 2, true", got, ok)
	}
}

func testClaimSetDoesNotFallbackWithoutSpawn(t *testing.T) {
	claims := NewClaimSet(engineTestProfile(t))
	if _, ok := claims.Claim([]wm.Window{claimWindow(1, "Ghostty", "", 1, false, false)}, "ghostty-wave", mo.None[int]()); ok {
		t.Fatal("claim unexpectedly used fallback without spawn command")
	}
}

func testClaimSetManyPreservesOrder(t *testing.T) {
	claims := NewClaimSet(engineTestProfile(t))
	windows := []wm.Window{claimWindow(1, "Arc", "", 1, false, false), claimWindow(2, "Ghostty", "pc | x", 1, false, false), claimWindow(3, "Spotify", "", 1, false, false)}
	got := claims.ClaimMany(windows, []string{"arc", "discord", "ghostty-wave", "spotify"}, mo.None[int]())
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("ClaimMany() = %v, want [1 2 3]", got)
	}
}

func testClaimSetReset(t *testing.T) {
	claims := NewClaimSet(engineTestProfile(t))
	windows := []wm.Window{claimWindow(1, "Arc", "", 1, false, false)}
	if _, ok := claims.Claim(windows, "arc", mo.None[int]()); !ok {
		t.Fatal("first claim failed")
	}
	if _, ok := claims.Claim(windows, "arc", mo.None[int]()); ok {
		t.Fatal("second claim succeeded before reset")
	}
	claims.Reset()
	if got, ok := claims.Claim(windows, "arc", mo.None[int]()); !ok || got != 1 {
		t.Fatalf("Claim() after Reset() = %d, %t; want 1, true", got, ok)
	}
}
