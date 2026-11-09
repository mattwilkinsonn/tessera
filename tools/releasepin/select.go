package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

type artifact struct {
	ID      int64     `json:"id"`
	Name    string    `json:"name"`
	Expired bool      `json:"expired"`
	Created time.Time `json:"created_at"`
	Run     struct {
		ID int64 `json:"id"`
	} `json:"workflow_run"`
}

type workflowRun struct {
	Path       string `json:"path"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Event      string `json:"event"`
	HeadSHA    string `json:"head_sha"`
	Repository struct {
		ID int64 `json:"id"`
	} `json:"repository"`
	HeadRepository struct {
		ID int64 `json:"id"`
	} `json:"head_repository"`
}

func apiJSON(ctx context.Context, gh api, endpoint string, v any) error {
	b, err := gh.call(ctx, endpoint)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func selectArtifact(ctx context.Context, gh api, repo, tree, commit, dest string) (bool, error) {
	base := "repos/" + repo
	var repository struct {
		ID int64 `json:"id"`
	}
	if err := apiJSON(ctx, gh, base, &repository); err != nil {
		return false, fmt.Errorf("reading repository: %w", err)
	}
	if repository.ID == 0 {
		return false, errors.New("repository id is missing")
	}
	candidates := []artifact{}
	for page := 1; ; page++ {
		var list struct {
			Artifacts []artifact `json:"artifacts"`
		}
		endpoint := fmt.Sprintf("%s/actions/artifacts?name=tess-darwin-arm64-%s&per_page=100&page=%d", base, tree, page)
		if err := apiJSON(ctx, gh, endpoint, &list); err != nil {
			fmt.Fprintf(os.Stderr, "listing artifacts: %v\n", err)
			break
		}
		candidates = append(candidates, list.Artifacts...)
		if len(list.Artifacts) < 100 {
			break
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Created.After(candidates[j].Created) })
	for _, a := range candidates {
		if a.Expired || a.ID == 0 || a.Run.ID == 0 || a.Name != "tess-darwin-arm64-"+tree {
			continue
		}
		tar, ok := candidate(ctx, gh, base, tree, dest, repository.ID, a)
		if !ok {
			continue
		}
		root, err := os.OpenRoot(filepath.Dir(dest))
		if err != nil {
			return false, err
		}
		err = root.MkdirAll(filepath.Base(dest), 0o750)
		if err == nil {
			err = root.WriteFile(filepath.Join(filepath.Base(dest), assetName), tar, 0o600)
		}
		closeErr := root.Close()
		if err != nil {
			return false, err
		}
		if closeErr != nil {
			return false, closeErr
		}
		p := Provenance{Commit: commit, SHA256: digest(tar), SourceRun: a.Run.ID, SourceArtifact: a.ID}
		if err := writeJSON(filepath.Join(dest, "provenance.json"), p); err != nil {
			return false, err
		}
		fmt.Fprintf(os.Stderr, "reused run %d artifact %d\n", a.Run.ID, a.ID)
		return true, nil
	}
	return false, nil
}

func candidate(ctx context.Context, gh api, base, tree, dest string, repoID int64, a artifact) ([]byte, bool) {
	var run workflowRun
	if err := apiJSON(ctx, gh, fmt.Sprintf("%s/actions/runs/%d", base, a.Run.ID), &run); err != nil {
		return nil, false
	}
	if !validRun(run, repoID) {
		return nil, false
	}
	tar, p, ok := artifactContents(ctx, gh, base, dest, a.ID)
	if !ok || !validCommit(ctx, gh, base, tree, p.Commit, run) {
		return nil, false
	}
	return tar, true
}

func validRun(run workflowRun, repoID int64) bool {
	return run.Path == ".github/workflows/ci.yml" && run.Status == "completed" &&
		run.Conclusion == "success" && run.HeadSHA != "" &&
		run.Repository.ID == repoID && run.HeadRepository.ID == repoID &&
		(run.Event == "push" || run.Event == "pull_request")
}

func artifactContents(ctx context.Context, gh api, base, dest string, id int64) ([]byte, Provenance, bool) {
	var p Provenance
	raw, err := gh.call(ctx, fmt.Sprintf("%s/actions/artifacts/%d/zip", base, id))
	if err != nil {
		return nil, p, false
	}
	f, err := os.CreateTemp(filepath.Dir(dest), "releasepin-*.zip")
	if err != nil {
		return nil, p, false
	}
	defer func() {
		root, err := os.OpenRoot(filepath.Dir(f.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "opening artifact directory: %v\n", err)
			return
		}
		if err := root.Remove(filepath.Base(f.Name())); err != nil {
			fmt.Fprintf(os.Stderr, "removing artifact zip: %v\n", err)
		}
		if err := root.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "closing artifact directory: %v\n", err)
		}
	}()
	if _, err = f.Write(raw); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "closing artifact zip: %v\n", closeErr)
		}
		return nil, p, false
	}
	if err = f.Close(); err != nil {
		return nil, p, false
	}
	z, err := zip.OpenReader(f.Name())
	if err != nil {
		return nil, p, false
	}
	defer func() {
		if err := z.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "closing artifact zip reader: %v\n", err)
		}
	}()
	var tar, provenance []byte
	for _, file := range z.File {
		if file.Name != assetName && file.Name != "provenance.json" {
			continue
		}
		r, err := file.Open()
		if err != nil {
			return nil, p, false
		}
		b, readErr := io.ReadAll(r)
		closeErr := r.Close()
		if readErr != nil || closeErr != nil {
			return nil, p, false
		}
		if file.Name == assetName {
			if tar != nil {
				return nil, p, false
			}
			tar = b
		} else {
			if provenance != nil {
				return nil, p, false
			}
			provenance = b
		}
	}
	if tar == nil || json.Unmarshal(provenance, &p) != nil || !commitSHA.MatchString(p.Commit) || !strings.EqualFold(p.SHA256, digest(tar)) {
		return nil, p, false
	}
	return tar, p, true
}

func validCommit(ctx context.Context, gh api, base, tree, commit string, run workflowRun) bool {
	var c struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if err := apiJSON(ctx, gh, base+"/git/commits/"+commit, &c); err != nil || c.Tree.SHA != tree {
		return false
	}
	if run.Event == "push" {
		return commit == run.HeadSHA
	}
	for _, parent := range c.Parents {
		if parent.SHA == run.HeadSHA {
			return true
		}
	}
	return false
}
