package yabai

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattwilkinsonn/tessera/internal/commands"
	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/config/configtest"
	"github.com/mattwilkinsonn/tessera/internal/wm"
	"reflect"
)

func TestQueryWindow(t *testing.T) {
	root := t.TempDir()
	windows := filepath.Join(root, "windows.json")
	spaces := filepath.Join(root, "spaces.json")
	if err := os.WriteFile(windows, []byte(`[{"id":90,"app":"Code","role":"AXWindow","subrole":"AXStandardWindow","root-window":true,"level":0,"can-move":true,"can-resize":true,"has-ax-reference":true,"is-floating":true,"space":2}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spaces, []byte(`[{"id":47,"index":2}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TESS_YABAI_WINDOWS", windows)
	t.Setenv("TESS_YABAI_SPACES", spaces)
	t.Setenv("TESS_YABAI_LOG", filepath.Join(root, "argv.log"))
	t.Setenv("TESS_YABAI_EXIT_CODE", "")
	driver := New(os.Getenv("TESS_YABAI"))
	t.Run("classification and stable space ID", func(t *testing.T) {
		window, found, err := driver.QueryWindow(t.Context(), 90)
		if err != nil || !found {
			t.Fatalf("QueryWindow = %t, %v", found, err)
		}
		if window.SpaceID != wm.SpaceID("47") || !window.Floating || !window.Root || window.Role != "AXWindow" || window.Subrole != "AXStandardWindow" || !window.CanMove || !window.CanResize || window.Level != 0 {
			t.Fatalf("window = %+v", window)
		}
	})
	t.Run("missing ID", func(t *testing.T) {
		_, found, err := driver.QueryWindow(t.Context(), 999)
		if err != nil || found {
			t.Fatalf("missing = %t, %v", found, err)
		}
	})
	t.Run("query errors are not missing windows", func(t *testing.T) {
		t.Setenv("TESS_YABAI_WINDOWS_EXIT_CODE", "1")
		_, found, err := driver.QueryWindow(t.Context(), 90)
		if err == nil || found || !strings.Contains(err.Error(), "windows query failed") {
			t.Fatalf("failed query = %t, %v", found, err)
		}
	})
	t.Run("spaces query error", func(t *testing.T) {
		t.Setenv("TESS_YABAI_SPACES", filepath.Join(root, "missing.json"))
		_, found, err := driver.QueryWindow(t.Context(), 90)
		if err == nil || found {
			t.Fatalf("failed spaces = %t, %v", found, err)
		}
	})
	t.Run("canceled query", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, found, err := driver.QueryWindow(ctx, 90)
		if found || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled = %t, %v", found, err)
		}
	})
}

func TestRulesAddOnly(t *testing.T) {
	root := t.TempDir()
	windows := filepath.Join(root, "windows.json")
	spaces := filepath.Join(root, "spaces.json")
	displays := filepath.Join(root, "displays.json")
	log := filepath.Join(root, "argv.log")
	for path, data := range map[string]string{
		windows:  `[{"id":90,"app":"Code","space":1,"has-ax-reference":true,"is-floating":false,"frame":{"x":0,"y":0,"w":2500,"h":2000}}]`,
		spaces:   `[{"id":47,"index":1,"display":1,"type":"bsp"},{"id":48,"index":2,"display":2,"type":"stack"}]`,
		displays: `[{"index":1,"frame":{"w":5120},"spaces":[1]},{"index":2,"frame":{"w":1728},"spaces":[2]}]`,
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TESS_YABAI_WINDOWS", windows)
	t.Setenv("TESS_YABAI_SPACES", spaces)
	t.Setenv("TESS_YABAI_DISPLAYS", displays)
	t.Setenv("TESS_YABAI_LOG", log)
	t.Setenv("TESS_YABAI_EXIT_CODE", "")
	profile := configtest.Profile()
	if err := config.Validate(profile); err != nil {
		t.Fatal(err)
	}
	driver := New(os.Getenv("TESS_YABAI"))
	before, err := driver.QueryWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := commands.Rules(t.Context(), profile, driver); err != nil {
		t.Fatal(err)
	}
	after, err := driver.QueryWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rules changed populated BSP state")
	}
	argv, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(argv), "--apply") || strings.Contains(string(argv), "window\n90\n--") || strings.Contains(string(argv), "space=") || strings.Contains(string(argv), "display=") {
		t.Fatalf("rules applied effects to open windows: %s", argv)
	}
	if !strings.Contains(string(argv), "manage=off") {
		t.Fatalf("desk rule missing: %s", argv)
	}
}
