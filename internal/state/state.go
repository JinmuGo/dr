// Package state keeps what dr remembers of a worktree from one run to the next. It keeps it
// in the state directory, DirName in the worktree's git directory, so nothing of it shows in
// the worktree and removing the worktree removes it. The directory holds viewed.json, the
// files marked viewed; log.jsonl, the questions and answers, the comments, the fix requests
// and the fixes dr applied and undid; lock, which one dr at a time holds to apply fixes; and
// sessions/, where dr starts the sessions of agents that let it choose where.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DirName is the name of the state directory in the worktree's git directory, as
// git.Path takes it.
const DirName = "dr"

// LogPath returns the path of the log in dir, the state directory.
func LogPath(dir string) string {
	return filepath.Join(dir, "log.jsonl")
}

// ViewedPath returns the path of the viewed files in dir, the state directory.
func ViewedPath(dir string) string {
	return filepath.Join(dir, "viewed.json")
}

// Viewed maps the path of each file marked viewed to the hash of the diff it showed then,
// as review.File.Hash makes it. It keeps the files of other diffs too, such as those dr
// showed against another base.
type Viewed map[string]string

// Check reports whether the file at path is viewed, its diff still having the hash hash,
// and whether it was marked viewed but its diff changed since.
func (v Viewed) Check(path, hash string) (viewed, changed bool) {
	saved, ok := v[path]
	return ok && saved == hash, ok && saved != hash
}

// Load reads the viewed files from the file at path, a JSON object of paths and hashes. A
// missing file holds none. A file that cannot be read or holds anything else gives none and
// the error. The map it returns is never nil.
func Load(path string) (Viewed, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Viewed{}, nil
	}
	if err != nil {
		return Viewed{}, err
	}
	var v Viewed
	if err := json.Unmarshal(data, &v); err != nil {
		return Viewed{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if v == nil { // the file holds null
		v = Viewed{}
	}
	return v, nil
}

// Save writes v to the file at path, making its directory when it is missing. It writes a
// temporary file beside it and renames that over it, so the file is never half written.
func Save(path string, v Viewed) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name()) // the error that matters is err
	}
	return err
}
