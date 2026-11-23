package provisioning

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"sandpodctl/internal/platform"
)

// RemoveCustomer ports remove-customer.sh.
func RemoveCustomer(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: remove-customer <instance-id> [--service-user ...] [--delete-data] [--delete-user]")
		return 2
	}
	inst := args[0]
	if !platform.ValidInstance3(inst) {
		fmt.Fprintln(os.Stderr, "instance-id must be 3 digits.")
		return 2
	}
	svc := "svc-c" + inst
	delData, delUser, delReg := false, false, false
	registry := "/etc/sandpod/customers.tsv"
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--service-user":
			if i+1 >= len(rest) {
				fmt.Fprintln(os.Stderr, "Missing --service-user value.")
				return 2
			}
			svc = rest[i+1]
			i++
		case "--delete-data":
			delData = true
		case "--delete-user":
			delUser = true
		case "--delete-registry-entry":
			delReg = true
		case "--registry":
			if i+1 >= len(rest) {
				fmt.Fprintln(os.Stderr, "Missing --registry value.")
				return 2
			}
			registry = rest[i+1]
			i++
		default:
			fmt.Fprintf(os.Stderr, "Unknown arg: %s\n", rest[i])
			return 2
		}
	}
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	home, _, err := platform.LookupAccount(svc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "User %s not found.\n", svc)
		return 1
	}
	if home == "" || home == "/" || !strings.HasPrefix(home, "/home/") {
		fmt.Fprintf(os.Stderr, "Refusing: unexpected home '%s' for %s.\n", home, svc)
		return 1
	}
	fmt.Printf("Stopping customer@%s.service ...\n", inst)
	_ = platform.UserSystemctl(svc, true, "stop", "customer@"+inst+".service")
	_ = platform.UserSystemctl(svc, true, "disable", "customer@"+inst+".service")

	unitDir := home + "/.config/containers/systemd"
	_ = os.Remove(filepath.Join(unitDir, "customer@"+inst+".container"))
	_ = os.RemoveAll(filepath.Join(unitDir, "customer@"+inst+".container.d"))
	if matches, _ := filepath.Glob(filepath.Join(unitDir, "customer@*.container")); len(matches) == 0 {
		_ = os.Remove(filepath.Join(unitDir, "customer@.container"))
	}
	_ = platform.UserSystemctl(svc, true, "daemon-reload")

	if delData {
		fmt.Printf("Deleting %s/data and %s/config ...\n", home, home)
		_ = os.RemoveAll(home + "/data")
		_ = os.RemoveAll(home + "/config")
	}
	if delReg {
		if _, err := os.Stat(registry); err == nil {
			_ = withRegistryLock(registry, func() error {
				return rewriteRegistryDropInstance(registry, inst)
			})
		}
	}
	if delUser {
		if !delData {
			fmt.Fprintln(os.Stderr, "Refusing --delete-user without --delete-data.")
			return 2
		}
		_ = exec.Command("loginctl", "disable-linger", svc).Run()
		if err := exec.Command("userdel", "-r", svc).Run(); err != nil {
			_ = exec.Command("userdel", svc).Run()
		}
		fmt.Printf("Deleted user %s\n", svc)
	}
	kept := "no"
	if !delData {
		kept = "yes"
	}
	fmt.Printf("Removed instance %s (data kept: %s).\n", inst, kept)
	return 0
}
