//go:build windows || plan9

package provisioning

// Fallback for OSes without flock: run without locking (dev only).
func withRegistryLock(registry string, fn func() error) error {
	_ = registry
	return fn()
}
