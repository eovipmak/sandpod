package platform

import (
	"fmt"
	"os/exec"
	"strings"
)

// SELinuxEnforcing mirrors `getenforce` == Enforcing (false when absent).
func SELinuxEnforcing() bool {
	if !Have("getenforce") {
		return false
	}
	out, err := exec.Command("getenforce").Output()
	return err == nil && strings.TrimSpace(string(out)) == "Enforcing"
}

// LabelContainerFiles sets container_file_t on each path pattern (semanage
// fcontext -a, falling back to -m when the rule already exists).
func LabelContainerFiles(patterns ...string) error {
	if !Have("semanage") {
		return fmt.Errorf("install policycoreutils-python-utils to label shared runtime paths")
	}
	for _, pat := range patterns {
		if exec.Command("semanage", "fcontext", "-a", "-t", "container_file_t", pat).Run() != nil {
			_ = exec.Command("semanage", "fcontext", "-m", "-t", "container_file_t", pat).Run()
		}
	}
	return nil
}

// Restorecon applies the policy labels to paths (-RF when recursive).
func Restorecon(recursive bool, paths ...string) error {
	flags := "-F"
	if recursive {
		flags = "-RF"
	}
	return Run("restorecon", append([]string{flags}, paths...)...)
}

// Arch returns `uname -m` (callers map it to their own release naming).
func Arch() string {
	out, _ := exec.Command("uname", "-m").Output()
	return strings.TrimSpace(string(out))
}

// Fetch downloads url to dest with curl -fsSL.
func Fetch(url, dest string) error {
	return Run("curl", "-fsSL", url, "-o", dest)
}
