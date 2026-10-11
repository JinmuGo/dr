package state

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrLocked is the error Lock returns while another process holds the lock.
var ErrLocked = errors.New("another dr has this worktree open")

// Lock takes the lock on the file lock in dir, the state directory, without waiting, making
// dir when it is missing, so that only one dr at a time writes the worktree's files. While
// another process holds the lock, Lock fails with ErrLocked. The lock is held until unlock
// is called or the process ends. Where the system cannot lock files, Lock takes no lock and
// unlock does nothing.
func Lock(dir string) (unlock func() error, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return lock(filepath.Join(dir, "lock"))
}
