package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
)

type publishOptions struct {
	repo, tag, sha, tarball, fetchInto, notes string
	prerelease, generateNotes                 bool
}
type release struct {
	ID     int64          `json:"id"`
	Tag    string         `json:"tag_name"`
	Target string         `json:"target_commitish"`
	Draft  bool           `json:"draft"`
	Assets []releaseAsset `json:"assets"`
}
type releaseAsset struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

func publish(ctx context.Context, gh api, o publishOptions) error {
	r, found, err := findRelease(ctx, gh, o)
	if err != nil {
		return err
	}
	if !found {
		if o.tarball == "" {
			return errors.New("no published asset to fetch")
		}
		r, err = createDraft(ctx, gh, o)
		if err != nil {
			return err
		}
	}
	if err := checkReleaseTarget(ctx, gh, o, r); err != nil {
		return err
	}
	var asset *releaseAsset
	for i := range r.Assets {
		if r.Assets[i].Name == assetName {
			asset = &r.Assets[i]
			break
		}
	}
	if o.tarball == "" {
		if r.Draft || asset == nil || o.fetchInto == "" {
			return errors.New("no published asset to fetch")
		}
		data, err := downloadReleaseAsset(ctx, gh, o, asset)
		if err != nil {
			return err
		}
		return os.WriteFile(o.fetchInto, data, 0o600)
	}
	data, err := os.ReadFile(o.tarball)
	if err != nil {
		return err
	}
	if err := reconcileAsset(ctx, gh, o, r, asset, data); err != nil {
		return err
	}
	if r.Draft {
		return finishDraft(ctx, gh, o, r)
	}
	return nil
}

func findRelease(ctx context.Context, gh api, o publishOptions) (*release, bool, error) {
	for page := 1; ; page++ {
		var releases []release
		endpoint := fmt.Sprintf("repos/%s/releases?per_page=100&page=%d", o.repo, page)
		if err := apiJSON(ctx, gh, endpoint, &releases); err != nil {
			return nil, false, err
		}
		for i := range releases {
			if releases[i].Tag == o.tag {
				return &releases[i], true, nil
			}
		}
		if len(releases) < 100 {
			return nil, false, nil
		}
	}
}

func releaseFlags(o publishOptions) []string {
	args := []string{"-F", fmt.Sprintf("prerelease=%t", o.prerelease)}
	if o.prerelease {
		args = append(args, "-f", "body="+o.notes, "-f", "make_latest=false")
	}
	return args
}

func createDraft(ctx context.Context, gh api, o publishOptions) (*release, error) {
	args := []string{"repos/" + o.repo + "/releases", "-X", "POST", "-f", "tag_name=" + o.tag, "-f", "target_commitish=" + o.sha, "-F", "draft=true"}
	args = append(args, releaseFlags(o)...)
	if o.generateNotes {
		args = append(args, "-F", "generate_release_notes=true")
	}
	b, err := gh.call(ctx, args...)
	if err != nil {
		return nil, err
	}
	var r release
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func checkReleaseTarget(ctx context.Context, gh api, o publishOptions, r *release) error {
	if r.Draft {
		if r.Target != o.sha {
			return fmt.Errorf("draft target %s differs from %s", r.Target, o.sha)
		}
		return nil
	}
	var ref struct {
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	if err := apiJSON(ctx, gh, "repos/"+o.repo+"/git/ref/tags/"+url.PathEscape(o.tag), &ref); err != nil {
		return err
	}
	for ref.Object.Type == "tag" {
		if err := apiJSON(ctx, gh, "repos/"+o.repo+"/git/tags/"+ref.Object.SHA, &ref); err != nil {
			return err
		}
	}
	if ref.Object.SHA != o.sha {
		return fmt.Errorf("tag target %s differs from %s", ref.Object.SHA, o.sha)
	}
	return nil
}

func downloadReleaseAsset(ctx context.Context, gh api, o publishOptions, asset *releaseAsset) ([]byte, error) {
	data, err := gh.call(ctx, fmt.Sprintf("repos/%s/releases/assets/%d", o.repo, asset.ID), "-H", "Accept: application/octet-stream")
	if err != nil {
		return nil, err
	}
	if asset.Digest != "" && asset.Digest != "sha256:"+digest(data) {
		return nil, fmt.Errorf("asset digest %s differs from downloaded sha256:%s", asset.Digest, digest(data))
	}
	return data, nil
}

func reconcileAsset(ctx context.Context, gh api, o publishOptions, r *release, asset *releaseAsset, local []byte) error {
	if asset != nil {
		existing, err := downloadReleaseAsset(ctx, gh, o, asset)
		if err != nil {
			return err
		}
		oldDigest, newDigest := digest(existing), digest(local)
		if oldDigest == newDigest {
			return nil
		}
		if !r.Draft {
			return fmt.Errorf("published asset digest %s differs from %s", oldDigest, newDigest)
		}
		if _, err := gh.call(ctx, fmt.Sprintf("repos/%s/releases/assets/%d", o.repo, asset.ID), "-X", "DELETE"); err != nil {
			return err
		}
	}
	endpoint := fmt.Sprintf("https://uploads.github.com/repos/%s/releases/%d/assets?name=%s", o.repo, r.ID, url.QueryEscape(assetName))
	_, err := gh.call(ctx, endpoint, "-X", "POST", "-H", "Content-Type: application/gzip", "--input", o.tarball)
	return err
}

func finishDraft(ctx context.Context, gh api, o publishOptions, r *release) error {
	args := make([]string, 0, 11)
	args = append(args, fmt.Sprintf("repos/%s/releases/%d", o.repo, r.ID), "-X", "PATCH", "-F", "draft=false")
	args = append(args, releaseFlags(o)...)
	_, err := gh.call(ctx, args...)
	return err
}
