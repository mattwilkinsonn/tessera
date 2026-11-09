package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeSelectAPI struct {
	replies  map[string][]byte
	failures map[string]error
}

func (f fakeSelectAPI) call(_ context.Context, args ...string) ([]byte, error) {
	if err := f.failures[args[0]]; err != nil {
		return nil, err
	}
	b, ok := f.replies[args[0]]
	if !ok {
		return nil, fmt.Errorf("unexpected endpoint %s", args[0])
	}
	return b, nil
}

func selectZIP(t *testing.T, sha string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, content := range map[string]string{assetName: "tarball", "provenance.json": fmt.Sprintf(`{"commit":"recorded","sha256":%q}`, sha)} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestSelectArtifact(t *testing.T) {
	tests := []struct {
		name     string
		change   func(map[string][]byte, map[string]error)
		hit      bool
		artifact int64
	}{
		{name: "hit", hit: true, artifact: 1},
		{name: "push hit", hit: true, artifact: 1, change: func(r map[string][]byte, _ map[string]error) {
			r["run1"] = bytes.Replace(r["run1"], []byte("pull_request"), []byte("push"), 1)
			r["run1"] = bytes.Replace(r["run1"], []byte(`"head_sha":"head"`), []byte(`"head_sha":"recorded"`), 1)
		}},
		{name: "no artifacts", change: func(r map[string][]byte, _ map[string]error) { r["list"] = []byte(`{"artifacts":[]}`) }},
		{name: "failed conclusion", change: func(r map[string][]byte, _ map[string]error) {
			r["run1"] = bytes.Replace(r["run1"], []byte(`"success"`), []byte(`"failure"`), 1)
		}},
		{name: "cancelled", change: func(r map[string][]byte, _ map[string]error) {
			r["run1"] = bytes.Replace(r["run1"], []byte(`"success"`), []byte(`"cancelled"`), 1)
		}},
		{name: "wrong workflow path", change: func(r map[string][]byte, _ map[string]error) {
			r["run1"] = bytes.Replace(r["run1"], []byte("ci.yml"), []byte("other.yml"), 1)
		}},
		{name: "fork head_repository", change: func(r map[string][]byte, _ map[string]error) {
			r["run1"] = bytes.Replace(r["run1"], []byte(`"head_repository":{"id":7}`), []byte(`"head_repository":{"id":8}`), 1)
		}},
		{name: "missing head_repository", change: func(r map[string][]byte, _ map[string]error) {
			r["run1"] = bytes.Replace(r["run1"], []byte(`,"head_repository":{"id":7}`), nil, 1)
		}},
		{name: "event workflow_dispatch", change: func(r map[string][]byte, _ map[string]error) {
			r["run1"] = bytes.Replace(r["run1"], []byte("pull_request"), []byte("workflow_dispatch"), 1)
		}},
		{name: "digest mismatch", change: func(r map[string][]byte, _ map[string]error) { r["badDigest"] = []byte("yes") }},
		{name: "tree mismatch", change: func(r map[string][]byte, _ map[string]error) {
			r["commit"] = []byte(`{"tree":{"sha":"other"},"parents":[{"sha":"head"}]}`)
		}},
		{name: "push commit differs", change: func(r map[string][]byte, _ map[string]error) {
			r["run1"] = bytes.Replace(r["run1"], []byte("pull_request"), []byte("push"), 1)
		}},
		{name: "pull request missing parent", change: func(r map[string][]byte, _ map[string]error) {
			r["commit"] = []byte(`{"tree":{"sha":"tree"},"parents":[{"sha":"other"}]}`)
		}},
		{name: "410 then older hit", hit: true, artifact: 2, change: func(r map[string][]byte, e map[string]error) {
			r["list"] = []byte(`{"artifacts":[{"id":1,"name":"tess-darwin-arm64-tree","created_at":"2026-01-02T00:00:00Z","workflow_run":{"id":1}},{"id":2,"name":"tess-darwin-arm64-tree","created_at":"2026-01-01T00:00:00Z","workflow_run":{"id":2}}]}`)
			e["zip1"] = errors.New("410 gone")
		}},
		{name: "all candidates fail", change: func(_ map[string][]byte, e map[string]error) { e["zip1"] = errors.New("gone") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := map[string][]byte{
				"list":   []byte(`{"artifacts":[{"id":1,"name":"tess-darwin-arm64-tree","workflow_run":{"id":1}}]}`),
				"run1":   []byte(`{"path":".github/workflows/ci.yml","status":"completed","conclusion":"success","event":"pull_request","head_sha":"head","repository":{"id":7},"head_repository":{"id":7}}`),
				"commit": []byte(`{"tree":{"sha":"tree"},"parents":[{"sha":"head"}]}`),
			}
			e := map[string]error{}
			if tt.change != nil {
				tt.change(r, e)
			}
			sha := digest([]byte("tarball"))
			if _, ok := r["badDigest"]; ok {
				sha = "bad"
			}
			z := selectZIP(t, sha)
			base := "repos/owner/repo"
			f := fakeSelectAPI{replies: map[string][]byte{base: []byte(`{"id":7}`), base + "/actions/artifacts?name=tess-darwin-arm64-tree&per_page=100&page=1": r["list"], base + "/actions/runs/1": r["run1"], base + "/actions/runs/2": r["run1"], base + "/actions/artifacts/1/zip": z, base + "/actions/artifacts/2/zip": z, base + "/git/commits/recorded": r["commit"]}, failures: map[string]error{}}
			if err := e["zip1"]; err != nil {
				f.failures[base+"/actions/artifacts/1/zip"] = err
			}
			dest := filepath.Join(t.TempDir(), "out")
			hit, err := selectArtifact(t.Context(), f, "owner/repo", "tree", "main", dest)
			if err != nil || hit != tt.hit {
				t.Fatalf("hit=%v err=%v, want %v", hit, err, tt.hit)
			}
			if !hit {
				return
			}
			b, err := os.ReadFile(filepath.Join(dest, assetName))
			if err != nil || string(b) != "tarball" {
				t.Fatalf("tarball %q: %v", b, err)
			}
			b, err = os.ReadFile(filepath.Join(dest, "provenance.json"))
			if err != nil {
				t.Fatal(err)
			}
			var p Provenance
			if err := json.Unmarshal(b, &p); err != nil {
				t.Fatal(err)
			}
			if p.Commit != "main" || p.SHA256 != digest([]byte("tarball")) || p.SourceArtifact != tt.artifact || p.SourceRun != tt.artifact {
				t.Fatalf("provenance %+v", p)
			}
		})
	}
}

func TestSelectRepositoryError(t *testing.T) {
	for _, reply := range []string{"{}", "invalid"} {
		t.Run(strings.ReplaceAll(reply, "{}", "missing"), func(t *testing.T) {
			f := fakeSelectAPI{replies: map[string][]byte{"repos/owner/repo": []byte(reply)}}
			if _, err := selectArtifact(t.Context(), f, "owner/repo", "tree", "main", t.TempDir()); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
