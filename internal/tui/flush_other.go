//go:build !darwin && !linux

package tui

// flushInput does nothing here: the rest of a key that comes in more than one read goes
// to dr.
func flushInput(uintptr) error { return nil }
