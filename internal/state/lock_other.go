//go:build !unix || aix || (solaris && !illumos)

package state

// lock takes no lock: the system has no flock, as on Windows, AIX and Solaris. unlock does
// nothing.
func lock(string) (unlock func() error, err error) {
	return func() error { return nil }, nil
}
