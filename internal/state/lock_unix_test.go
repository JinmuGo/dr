//go:build unix && !aix && (illumos || !solaris)

package state_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/JinmuGo/dr/internal/state"
)

func TestLock(t *testing.T) {
	// The state directory is made when missing. A second Lock fails while the first is
	// held, and once it is released the lock can be taken again.
	dir := filepath.Join(t.TempDir(), "git", "dr")
	unlock, err := state.Lock(dir)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if _, err := state.Lock(dir); !errors.Is(err, state.ErrLocked) {
		t.Errorf("second Lock: got %v, want ErrLocked", err)
	}
	if err := unlock(); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	again, err := state.Lock(dir)
	if err != nil {
		t.Fatalf("Lock after unlock: %v", err)
	}
	if err := again(); err != nil {
		t.Errorf("unlock: %v", err)
	}
}
