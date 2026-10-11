package state_test

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/JinmuGo/dr/internal/state"
)

// names returns the names of the entries of dir, sorted.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestSaveLoad(t *testing.T) {
	// The directory is made when missing, and each save replaces the file through a
	// temporary file that is renamed over it, so no other file is left beside it.
	dir := filepath.Join(t.TempDir(), "git", "dr")
	path := filepath.Join(dir, "viewed.json")
	for _, v := range []state.Viewed{
		{"a.go": "1111", "d/b.go": "2222"},
		{"a.go": "3333"},
		{},
	} {
		if err := state.Save(path, v); err != nil {
			t.Fatal(err)
		}
		got, err := state.Load(path)
		if err != nil || !maps.Equal(got, v) {
			t.Errorf("Load after Save(%v): got %v, %v", v, got, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]string
		if err := json.Unmarshal(data, &object); err != nil || !maps.Equal(object, v) {
			t.Errorf("Save(%v) wrote %q, want a JSON object of paths and hashes", v, data)
		}
		if got := names(t, dir); !slices.Equal(got, []string{"viewed.json"}) {
			t.Errorf("Save(%v) left %q in its directory, want only viewed.json", v, got)
		}
	}
}

func TestLoad(t *testing.T) {
	// A missing file holds no files. A broken one holds none either, and Load says why.
	dir := t.TempDir()
	for _, tc := range []struct {
		name, data string
		ok         bool
	}{
		{name: "missing", ok: true},
		{name: "null", data: "null", ok: true},
		{name: "empty", data: ""},
		{name: "cut", data: `{"a.go": "11`},
		{name: "list", data: `["a.go"]`},
		{name: "number", data: `{"a.go": 1}`},
	} {
		path := filepath.Join(dir, tc.name+".json")
		if tc.name != "missing" {
			if err := os.WriteFile(path, []byte(tc.data), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		v, err := state.Load(path)
		if v == nil || len(v) > 0 || (err == nil) != tc.ok {
			t.Errorf("%s: got %v, %v, want no files and an error %t", tc.name, v, err, !tc.ok)
		}
	}
	if v, err := state.Load(dir); v == nil || len(v) > 0 || err == nil {
		t.Errorf("a directory: got %v, %v, want no files and an error", v, err)
	}
}

func TestCheck(t *testing.T) {
	// A file stays viewed while its diff has the hash it had when it was marked.
	v := state.Viewed{"a.go": "1111"}
	for _, tc := range []struct {
		path, hash      string
		viewed, changed bool
	}{
		{path: "a.go", hash: "1111", viewed: true},
		{path: "a.go", hash: "2222", changed: true},
		{path: "b.go", hash: "1111"},
	} {
		if viewed, changed := v.Check(tc.path, tc.hash); viewed != tc.viewed || changed != tc.changed {
			t.Errorf("Check(%q, %q): got viewed %t, changed %t, want %t, %t", tc.path, tc.hash, viewed, changed, tc.viewed, tc.changed)
		}
	}
}

func TestKeep(t *testing.T) {
	// Files of another diff stay in the file when the files of this one change.
	path := filepath.Join(t.TempDir(), "viewed.json")
	if err := state.Save(path, state.Viewed{"a.go": "1111", "gone.go": "9999"}); err != nil {
		t.Fatal(err)
	}
	v, err := state.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	delete(v, "a.go")
	v["b.go"] = "2222"
	if err := state.Save(path, v); err != nil {
		t.Fatal(err)
	}
	want := state.Viewed{"b.go": "2222", "gone.go": "9999"}
	if got, err := state.Load(path); err != nil || !maps.Equal(got, want) {
		t.Errorf("got %v, %v, want %v", got, err, want)
	}
}

func TestSaveError(t *testing.T) {
	// A save that fails leaves no temporary file behind.
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(filepath.Join(file, "dr", "viewed.json"), state.Viewed{}); err == nil {
		t.Error("Save under a file: got no error")
	}
	taken := filepath.Join(dir, "taken")
	if err := os.MkdirAll(filepath.Join(taken, "in"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(taken, state.Viewed{"a.go": "1111"}); err == nil {
		t.Error("Save over a directory: got no error")
	}
	if got := names(t, dir); !slices.Equal(got, []string{"file", "taken"}) {
		t.Errorf("got %q after the failed saves, want only file and taken", got)
	}
}
