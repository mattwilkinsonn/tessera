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

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"
)

//go:embed schema.cue default.cue
var embedded embed.FS

func Parse(path string, src []byte) (*Profile, error) {
	schemaSource, err := embedded.ReadFile("schema.cue")
	if err != nil {
		return nil, fmt.Errorf("reading embedded profile schema: %w", err)
	}

	ctx := cuecontext.New()
	schema := ctx.CompileBytes(schemaSource, cue.Filename("schema.cue"))
	if err := schema.Err(); err != nil {
		return nil, fmt.Errorf("reading profile %s: %s", path, cueerrors.Details(err, nil))
	}

	value := ctx.CompileBytes(src, cue.Filename(path), cue.Scope(schema))
	if err := value.Err(); err != nil {
		return nil, fmt.Errorf("profile %s: %s", path, cueerrors.Details(err, nil))
	}

	profileValue := schema.LookupPath(cue.ParsePath("#Profile")).Unify(value)
	if err := profileValue.Validate(cue.Concrete(true)); err != nil {
		return nil, fmt.Errorf("profile %s: %s", path, cueerrors.Details(err, nil))
	}

	var profile Profile
	if err := profileValue.Decode(&profile); err != nil {
		return nil, fmt.Errorf("profile %s: %s", path, cueerrors.Details(err, nil))
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
			return nil, fmt.Errorf("profile %s: TypeScript profiles are not loaded; profiles now use profile.cue", path)
		}
		return loadFile(path)
	}

	configHome := env("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	directory := filepath.Join(configHome, "tessera")
	profilePath := filepath.Join(directory, "profile.cue")
	profile, err := loadIfPresent(profilePath)
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return profile, err
	}

	for _, extension := range []string{".ts", ".json"} {
		legacyPath := filepath.Join(directory, "profile"+extension)
		if _, err := os.Stat(legacyPath); err == nil {
			return nil, fmt.Errorf("profile %s: this profile format is no longer loaded; profiles now use profile.cue", legacyPath)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("checking profile %s: %w", legacyPath, err)
		}
	}

	data, err := embedded.ReadFile("default.cue")
	if err != nil {
		return nil, fmt.Errorf("reading embedded default profile: %w", err)
	}
	return Parse("embedded default.cue", data)
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
