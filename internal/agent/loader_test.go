package agent

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoaderReuseAndOwnership(t *testing.T) {
	const path = ".claude/projects/spike/a.jsonl"
	home := fakeHome(t, map[string]string{path: fixture(t, "claude-"+original+".jsonl")})
	var loader Loader
	edits, problems := loader.Load(home, since, []string{spike})
	if len(edits) != 3 || len(problems) != 0 {
		t.Fatalf("got %d edits and %v problems, want 3 edits and no problems", len(edits), problems)
	}
	entry := loader.entries[filepath.Join(home, path)]
	if entry == nil {
		t.Fatal("unchanged log was not cached")
	}
	want := edits[0].Prompt
	edits[0].Prompt = "caller changed this"
	for i := range edits {
		if edits[i].Before != nil {
			*edits[i].Before = "caller changed this"
		}
	}
	again, _ := loader.Load(home, since, []string{spike})
	if loader.entries[filepath.Join(home, path)] != entry {
		t.Error("unchanged log was parsed again")
	}
	if again[0].Prompt != want {
		t.Errorf("got prompt %q, want %q", again[0].Prompt, want)
	}
	for _, edit := range again {
		if edit.Before != nil && *edit.Before == "caller changed this" {
			t.Error("returned Before changed the cache")
		}
	}
}

func TestLoaderScope(t *testing.T) {
	const path = ".claude/projects/spike/a.jsonl"
	data := fixture(t, "claude-"+original+".jsonl")
	home := fakeHome(t, map[string]string{path: data})
	other := fakeHome(t, map[string]string{path: data})
	var loader Loader
	for _, tc := range []struct {
		name, home string
		since      time.Time
		roots      []string
		want       int
	}{
		{"initial", home, since, []string{spike, "/other"}, 3},
		{"ordered roots", home, since, []string{"/other", spike}, 3},
		{"narrow since", home, time.Date(2026, 10, 9, 12, 20, 50, 0, time.UTC), []string{"/other", spike}, 2},
		{"widen since", home, since, []string{"/other", spike}, 3},
		{"home", other, since, []string{"/other", spike}, 3},
		{"unrelated roots", other, since, []string{"/unrelated"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := loader.entries[filepath.Join(tc.home, path)]
			edits, problems := loader.Load(tc.home, tc.since, tc.roots)
			if len(edits) != tc.want || len(problems) != 0 {
				t.Fatalf("got %d edits and %v problems, want %d edits and no problems", len(edits), problems, tc.want)
			}
			if entry := loader.entries[filepath.Join(tc.home, path)]; entry == nil || entry == previous {
				t.Error("scope change did not replace the cached entry")
			}
		})
	}
}

func TestLoaderFileChanges(t *testing.T) {
	for _, change := range []string{"overwrite", "replacement", "delete", "unreadable"} {
		t.Run(change, func(t *testing.T) {
			const relative = ".claude/projects/spike/a.jsonl"
			data := fixture(t, "claude-"+original+".jsonl")
			home := fakeHome(t, map[string]string{relative: data})
			path := filepath.Join(home, relative)
			var loader Loader
			_, _ = loader.Load(home, since, []string{spike})
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			changed := strings.ReplaceAll(data, original, fork)
			switch change {
			case "overwrite":
				err = os.WriteFile(path, []byte(changed), 0o644)
			case "replacement":
				replacement := filepath.Join(home, "replacement")
				if err = os.WriteFile(replacement, []byte(changed), 0o644); err == nil {
					err = os.Rename(replacement, path)
				}
			case "delete", "unreadable":
				err = os.Remove(path)
				if err == nil && change == "unreadable" {
					err = os.Symlink(filepath.Join(home, "missing"), path)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if change == "overwrite" || change == "replacement" {
				if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
			}
			edits, problems := loader.Load(home, since, []string{spike})
			if change == "overwrite" || change == "replacement" {
				if len(edits) != 3 || len(problems) != 0 {
					t.Fatalf("got %d edits, %v problems", len(edits), problems)
				}
				for _, edit := range edits {
					if edit.Session != fork {
						t.Errorf("got session %q, want %q", edit.Session, fork)
					}
				}
			} else {
				if len(edits) != 0 || len(loader.entries) != 0 {
					t.Errorf("got %d edits and %d cached logs after %s", len(edits), len(loader.entries), change)
				}
				if change == "unreadable" && len(problems) != 1 {
					t.Errorf("got %v problems, want unreadable log", problems)
				}
			}
		})
	}
}

func TestLoaderForkChanges(t *testing.T) {
	const originalPath = ".claude/projects/spike/a.jsonl"
	const forkPath = ".claude/projects/spike/b.jsonl"
	home := fakeHome(t, map[string]string{
		originalPath: fixture(t, "claude-"+original+".jsonl"),
		forkPath:     fixture(t, "claude-"+fork+".jsonl"),
	})
	var loader Loader
	_, _ = loader.Load(home, since, []string{spike})
	entry := loader.entries[filepath.Join(home, forkPath)]
	if err := os.Remove(filepath.Join(home, originalPath)); err != nil {
		t.Fatal(err)
	}
	edits, _ := loader.Load(home, since, []string{spike})
	if len(edits) != 3 {
		t.Fatalf("got %d edits, want 3 copied edits after original removal", len(edits))
	}
	for _, edit := range edits {
		if edit.Session != fork {
			t.Errorf("got session %q, want %q", edit.Session, fork)
		}
	}
	if loader.entries[filepath.Join(home, forkPath)] != entry {
		t.Error("unchanged fork was parsed again")
	}
}

func TestSessionLogMutation(t *testing.T) {
	home := fakeHome(t, map[string]string{"a.jsonl": "{}\n"})
	path := filepath.Join(home, "a.jsonl")
	log := sessionLog{path: path, agent: agent{name: "test", parse: func([][]byte) []Edit {
		if err := os.WriteFile(path, []byte("{}\n{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return nil
	}}}
	log.read(time.Time{}, [][]byte{[]byte("{")})
	if log.cached != nil {
		t.Error("log changed during parsing was cached")
	}
}

func TestLoaderOverlapping(t *testing.T) {
	home := fakeHome(t, map[string]string{".claude/projects/spike/a.jsonl": fixture(t, "claude-"+original+".jsonl")})
	var loader Loader
	var group sync.WaitGroup
	for i := range 8 {
		group.Go(func() {
			roots, want := []string{spike}, 3
			if i%2 == 0 {
				roots, want = []string{"/unrelated"}, 0
			}
			edits, problems := loader.Load(home, since, roots)
			if len(edits) != want || len(problems) != 0 {
				t.Errorf("got %d edits and %v problems, want %d edits", len(edits), problems, want)
			}
		})
	}
	group.Wait()
}
