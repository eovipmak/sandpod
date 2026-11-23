//go:build linux || darwin || freebsd

package provisioning

import (
	"os"
	"syscall"
)

// withRegistryLock mirrors `flock -x 9` around registry edits (Linux).
// On other GOOS it runs fn without locking (dev only).
func withRegistryLock(registry string, fn func() error) error {
	lockPath := registry + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := flockExclusive(f); err != nil {
		return err
	}
	defer funlock(f)
	return fn()
}

func flockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

func funlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
