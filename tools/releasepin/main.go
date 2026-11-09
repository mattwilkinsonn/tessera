package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec" //nolint:depguard // Release tooling invokes gh and git as its subprocess seam.
	"path/filepath"
	"strings"
)

const assetName = "tess-darwin-arm64.tar.gz"

var errNoChange = errors.New("nothing to do")

type Pin struct {
	Version string `json:"version"`
	Rev     string `json:"rev"`
	URL     string `json:"url"`
	Hash    string `json:"hash"`
}
type Provenance struct {
	Commit         string `json:"commit"`
	SHA256         string `json:"sha256"`
	SourceRun      int64  `json:"source_run,omitempty"`
	SourceArtifact int64  `json:"source_artifact,omitempty"`
}

func sriSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}
func digest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644) //nolint:gosec // Release metadata is a committed file.
}
func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, name, args...) //nolint:gosec // Only the tool's gh and git commands use this seam.
	var stderr bytes.Buffer
	c.Stderr = &stderr
	b, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", name, err, stderr.String())
	}
	return b, nil
}

type api interface {
	call(ctx context.Context, args ...string) ([]byte, error)
}
type ghAPI struct{}

func (ghAPI) call(ctx context.Context, args ...string) ([]byte, error) {
	return command(ctx, "gh", append([]string{"api"}, args...)...)
}
func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, ghAPI{}); err != nil {
		if errors.Is(err, errNoChange) {
			os.Exit(3)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string, out io.Writer, gh api) error {
	if len(args) == 0 {
		return errors.New("missing subcommand")
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	switch args[0] {
	case "provenance":
		tar := f.String("tarball", "", "tarball")
		commit := f.String("commit", "", "commit")
		dest := f.String("out", "", "output")
		sr := f.Int64("source-run", 0, "source run")
		sa := f.Int64("source-artifact", 0, "source artifact")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		b, err := os.ReadFile(*tar) //nolint:gosec // The tarball path is supplied by the CLI caller.
		if err != nil {
			return err
		}
		return writeJSON(*dest, Provenance{Commit: *commit, SHA256: digest(b), SourceRun: *sr, SourceArtifact: *sa})
	case "select":
		repo := f.String("repo", "", "repository")
		tree := f.String("tree", "", "tree")
		commit := f.String("commit", "", "commit")
		dest := f.String("out", "", "output")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		hit, err := selectArtifact(ctx, gh, *repo, *tree, *commit, *dest)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "hit=%t\n", hit)
		return err
	case "publish":
		o := publishOptions{}
		f.StringVar(&o.repo, "repo", "", "repository")
		f.StringVar(&o.tag, "tag", "", "tag")
		f.StringVar(&o.sha, "sha", "", "commit")
		f.StringVar(&o.tarball, "tarball", "", "tarball")
		f.StringVar(&o.fetchInto, "fetch-into", "", "download destination")
		f.BoolVar(&o.prerelease, "prerelease", false, "prerelease")
		f.StringVar(&o.notes, "notes", "", "notes")
		f.BoolVar(&o.generateNotes, "generate-notes", false, "generate notes")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		return publish(ctx, gh, o)
	case "owes":
		sha := f.String("sha", "", "commit")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		last, err := command(ctx, "git", "log", "-1", "--format=%H", *sha, "--", "VERSION")
		if err != nil {
			return err
		}
		v, err := command(ctx, "git", "show", *sha+":VERSION")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "stable=%t\nversion=%s\n", strings.TrimSpace(string(last)) == *sha, strings.TrimSpace(string(v)))
		return err
	case "pin":
		tar := f.String("tarball", "", "tarball")
		tag := f.String("tag", "", "tag")
		rev := f.String("rev", "", "revision")
		version := f.String("version", "", "version")
		repo := f.String("repo", "", "repository")
		dest := f.String("out", "", "output")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		return pin(ctx, *tar, *tag, *rev, *version, *repo, *dest)
	case "cask":
		tar := f.String("tarball", "", "tarball")
		version := f.String("version", "", "version")
		repo := f.String("repo", "", "repository")
		dest := f.String("out", "", "output")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		return cask(*tar, *version, *repo, *dest)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}
func assetURL(repo, tag string) string {
	return "https://github.com/" + repo + "/releases/download/" + tag + "/" + assetName
}
func pin(ctx context.Context, tar, tag, rev, version, repo, dest string) error {
	old, err := os.ReadFile(dest) //nolint:gosec // The output path is supplied by the CLI caller.
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(old) > 0 {
		var p Pin
		if err := json.Unmarshal(old, &p); err != nil {
			return err
		}
		if p.Rev == rev {
			return errNoChange
		}
		_, err := command(ctx, "git", "merge-base", "--is-ancestor", rev, p.Rev)
		if err == nil {
			return errNoChange
		}
		exit, ok := errors.AsType[*exec.ExitError](err)
		if !ok || exit.ExitCode() != 1 {
			return fmt.Errorf("checking ancestry: %w", err)
		}
	}
	b, err := os.ReadFile(tar) //nolint:gosec // The tarball path is supplied by the CLI caller.
	if err != nil {
		return err
	}
	p := Pin{Version: version, Rev: rev, URL: assetURL(repo, tag), Hash: sriSHA256(b)}
	encoded, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if bytes.Equal(old, encoded) {
		return errNoChange
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil { //nolint:gosec // The pin directory is committed and must be traversable.
		return err
	}
	return os.WriteFile(dest, encoded, 0o644) //nolint:gosec // The pin is a committed file.
}
