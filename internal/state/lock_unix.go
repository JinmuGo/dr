//go:build unix && !aix && (illumos || !solaris)

package state

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// lock takes an exclusive flock on the file at path, making the file when it is missing.
// Closing the file, which unlock and the end of the process do, releases it.
func lock(path string) (unlock func() error, err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close() // the error that matters is err
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return f.Close, nil
}
