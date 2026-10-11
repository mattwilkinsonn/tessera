package config

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"encoding/json/v2"
)

//go:embed default.json
var embedded embed.FS

func Parse(path string, src []byte) (*Profile, error) {
	var profile Profile
	if err := json.Unmarshal(src, &profile, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("profile %s: %w", path, err)
	}
	if err := Validate(&profile); err != nil {
		return nil, fmt.Errorf("profile %s: %w", path, err)
	}
	return &profile, nil
}

func Load(ctx context.Context, env func(string) string, home string) (*Profile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if env == nil {
		return nil, errors.New("profile environment lookup is nil")
	}
	if path := env("TESSERA_PROFILE"); path != "" {
		if strings.HasSuffix(path, ".ts") {
			return nil, fmt.Errorf("profile %s: TypeScript profiles are not loaded; render the profile to profile.json", path)
		}
		return loadFile(path)
	}

	configHome := env("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	profilePath := filepath.Join(configHome, "tessera", "profile.json")
	profile, err := loadIfPresent(profilePath)
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return profile, err
	}

	typescriptPath := filepath.Join(configHome, "tessera", "profile.ts")
	if _, err := os.Stat(typescriptPath); err == nil {
		return nil, fmt.Errorf("profile %s: TypeScript profiles are not loaded; render the profile to profile.json", typescriptPath)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("checking profile %s: %w", typescriptPath, err)
	}

	data, err := embedded.ReadFile("default.json")
	if err != nil {
		return nil, fmt.Errorf("reading embedded default profile: %w", err)
	}
	return Parse("embedded default.json", data)
}

func loadFile(path string) (*Profile, error) {
	//nolint:gosec // Profile paths are selected explicitly by the user.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading profile %s: %w", path, err)
	}
	return Parse(path, data)
}

func loadIfPresent(path string) (*Profile, error) {
	//nolint:gosec // Profile paths come from the user's config directory.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(path, data)
}
