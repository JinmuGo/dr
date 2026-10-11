// Package state keeps what dr remembers of a worktree from one run to the next: the files
// marked viewed, in viewed.json, and the questions asked about lines and their answers, in
// log.jsonl. dr keeps them in the worktree's git directory, so nothing of it shows in the
// worktree and removing the worktree removes it.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

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
