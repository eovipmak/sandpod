package platform

import (
	"reflect"
	"testing"
)

func TestAsUserArgs(t *testing.T) {
	c := AsUser("svc-c001", "/home/svc-c001", 1234, "podman", "exec", "customer-001")
	want := []string{"runuser", "-u", "svc-c001", "--", "env",
		"HOME=/home/svc-c001",
		"XDG_RUNTIME_DIR=/run/user/1234",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1234/bus",
		"podman", "exec", "customer-001"}
	if !reflect.DeepEqual(c.Args, want) {
		t.Fatalf("AsUser args = %v\nwant %v", c.Args, want)
	}
	// runuser keeps the caller's cwd; AsUser must pin it to the account home
	// so callers running from root's home don't fail with "cannot chdir".
	if c.Dir != "/home/svc-c001" {
		t.Fatalf("AsUser Dir = %q, want /home/svc-c001", c.Dir)
	}
}
