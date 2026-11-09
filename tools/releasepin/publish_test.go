package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakePublishAPI struct {
	release   *release
	target    string
	existing  []byte
	mutations []string
	requests  [][]string
	annotated bool
}

func (f *fakePublishAPI) call(_ context.Context, args ...string) ([]byte, error) {
	endpoint := args[0]
	method := "GET"
	for i := 1; i+1 < len(args); i++ {
		if args[i] == "-X" {
			method = args[i+1]
		}
	}
	if method != "GET" {
		f.mutations = append(f.mutations, method+" "+endpoint)
		f.requests = append(f.requests, append([]string(nil), args...))
		if method == "POST" && strings.HasSuffix(endpoint, "/releases") {
			return json.Marshal(release{ID: 1, Tag: "build", Target: "abc", Draft: true})
		}
		return []byte(`{}`), nil
	}
	switch {
	case strings.Contains(endpoint, "/releases?per_page="):
		if f.release == nil {
			return []byte(`[]`), nil
		}
		return json.Marshal([]release{*f.release})
	case strings.Contains(endpoint, "/git/ref/tags/"):
		if f.annotated {
			return []byte(`{"object":{"type":"tag","sha":"tag-object"}}`), nil
		}
		return []byte(fmt.Sprintf(`{"object":{"type":"commit","sha":%q}}`, f.target)), nil
	case strings.Contains(endpoint, "/git/tags/tag-object"):
		return []byte(fmt.Sprintf(`{"object":{"type":"commit","sha":%q}}`, f.target)), nil
	case strings.Contains(endpoint, "/releases/assets/"):
		return f.existing, nil
	default:
		return nil, fmt.Errorf("unexpected request: %v", args)
	}
}

func TestPublishRecovery(t *testing.T) {
	upload := "POST https://uploads.github.com/repos/owner/repo/releases/1/assets?name=" + assetName
	patch := "PATCH repos/owner/repo/releases/1"
	del := "DELETE repos/owner/repo/releases/assets/2"
	cases := []struct {
		name      string
		state     string
		asset     bool
		existing  string
		target    string
		fetch     bool
		wantErr   bool
		different bool
		annotated bool
		mutations []string
	}{
		{name: "no release", mutations: []string{"POST repos/owner/repo/releases", upload, patch}},
		{name: "draft no asset", state: "draft", mutations: []string{upload, patch}},
		{name: "draft different asset", state: "draft", asset: true, existing: "old", mutations: []string{del, upload, patch}},
		{name: "draft target mismatch", state: "draft", target: "wrong", wantErr: true},
		{name: "published equal asset", state: "published", asset: true, existing: "new"},
		{name: "published different asset", state: "published", asset: true, existing: "old", wantErr: true, different: true},
		{name: "published no asset", state: "published", mutations: []string{upload}},
		{name: "published tag mismatch", state: "published", target: "wrong", wantErr: true},
		{name: "published annotated tag", state: "published", asset: true, existing: "new", annotated: true},
		{name: "published annotated mismatch", state: "published", target: "wrong", annotated: true, wantErr: true},
		{name: "fetch success", state: "published", asset: true, existing: "new", fetch: true},
		{name: "fetch no asset", state: "published", fetch: true, wantErr: true},
		{name: "fetch draft", state: "draft", asset: true, existing: "new", fetch: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tar := filepath.Join(dir, "asset")
			if err := os.WriteFile(tar, []byte("new"), 0o600); err != nil {
				t.Fatal(err)
			}
			target := tc.target
			if target == "" {
				target = "abc"
			}
			f := &fakePublishAPI{target: target, existing: []byte(tc.existing), annotated: tc.annotated}
			if tc.state != "" {
				f.release = &release{ID: 1, Tag: "build", Target: target, Draft: tc.state == "draft"}
				if tc.asset {
					f.release.Assets = []releaseAsset{{ID: 2, Name: assetName, Digest: "sha256:" + digest(f.existing)}}
				}
			}
			o := publishOptions{repo: "owner/repo", tag: "build", sha: "abc", tarball: tar, prerelease: true, notes: "notes"}
			if tc.fetch {
				o.tarball = ""
				o.fetchInto = filepath.Join(dir, "fetched")
			}
			err := publish(t.Context(), f, o)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error %t", err, tc.wantErr)
			}
			if tc.different && (!strings.Contains(err.Error(), digest([]byte("old"))) || !strings.Contains(err.Error(), digest([]byte("new")))) {
				t.Fatalf("missing digests: %v", err)
			}
			if !reflect.DeepEqual(f.mutations, tc.mutations) {
				t.Fatalf("mutations = %v, want %v", f.mutations, tc.mutations)
			}
			if tc.fetch && !tc.wantErr {
				got, err := os.ReadFile(o.fetchInto)
				if err != nil || string(got) != tc.existing {
					t.Fatalf("fetch = %q, %v", got, err)
				}
			}
		})
	}
}

func TestPublishGeneratedNotes(t *testing.T) {
	dir := t.TempDir()
	tar := filepath.Join(dir, "asset")
	if err := os.WriteFile(tar, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fakePublishAPI{}
	err := publish(t.Context(), f, publishOptions{repo: "owner/repo", tag: "build", sha: "abc", tarball: tar, generateNotes: true})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"repos/owner/repo/releases", "-X", "POST", "-f", "tag_name=build", "-f", "target_commitish=abc", "-F", "draft=true", "-F", "prerelease=false", "-F", "generate_release_notes=true"},
		{"https://uploads.github.com/repos/owner/repo/releases/1/assets?name=" + assetName, "-X", "POST", "-H", "Content-Type: application/gzip", "--input", tar},
		{"repos/owner/repo/releases/1", "-X", "PATCH", "-F", "draft=false", "-F", "prerelease=false"},
	}
	if !reflect.DeepEqual(f.requests, want) {
		t.Fatalf("requests = %v, want %v", f.requests, want)
	}
}
