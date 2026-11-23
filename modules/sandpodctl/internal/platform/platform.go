// Package platform holds OS-abstraction helpers shared by all commands.
//
// Linux-first: production paths (runuser, podman, systemctl, XFS tools)
// live here behind small functions so a future port only replaces this
// package. Everything else stays OS-independent (stdlib only).
package platform

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// IsLinux reports whether we run on the supported production OS.
func IsLinux() bool { return runtime.GOOS == "linux" }

// RequireRoot mirrors `[[ $EUID -eq 0 ]] || exit 1`.
// On non-Linux dev machines it returns an error instead of exiting so
// callers can surface "Run as root." with exit code 1 like the scripts.
func RequireRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("Run as root.")
	}
	return nil
}

// MustRoot prints to stderr and returns 1 when not root.
func MustRoot() int {
	if err := RequireRoot(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	return -1
}

// LookupAccount resolves a service user to home dir + numeric uid,
// mirroring `getent passwd $user | cut -d: -f6` + `id -u $user`.
func LookupAccount(name string) (home string, uid int, err error) {
	if u, uerr := user.Lookup(name); uerr == nil {
		uid64, _ := strconv.Atoi(u.Uid)
		return u.HomeDir, uid64, nil
	}
	// Fallback to getent (covers nss setups os/user may miss).
	out, oerr := exec.Command("getent", "passwd", name).Output()
	if oerr != nil {
		return "", 0, fmt.Errorf("Unknown user %s.", name)
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ":")
	if len(parts) < 6 || parts[5] == "" {
		return "", 0, fmt.Errorf("Cannot resolve home for %s", name)
	}
	uidOut, uerr := exec.Command("id", "-u", name).Output()
	if uerr != nil {
		return "", 0, fmt.Errorf("Unknown user %s.", name)
	}
	uid64, _ := strconv.Atoi(strings.TrimSpace(string(uidOut)))
	return parts[5], uid64, nil
}

// UserManagerUp mirrors `[ -d /run/user/$uid ]`.
func UserManagerUp(uid int) bool {
	st, err := os.Stat(fmt.Sprintf("/run/user/%d", uid))
	return err == nil && st.IsDir()
}

// Run executes a program, wiring stdout/stderr through.
func Run(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	return c.Run()
}

// Output runs a program and returns combined stdout.
func Output(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

// RunAsUser mirrors:
//
//	runuser -u $user -- env HOME=$home XDG_RUNTIME_DIR=/run/user/$uid \
//	    DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$uid/bus <prog> <args...>
func RunAsUser(serviceUser, home string, uid int, prog string, args ...string) error {
	full := []string{"-u", serviceUser, "--", "env",
		"HOME=" + home,
		fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", uid),
		fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", uid),
		prog,
	}
	full = append(full, args...)
	return Run("runuser", full...)
}

// OutputAsUser is RunAsUser returning stdout.
func OutputAsUser(serviceUser, home string, uid int, prog string, args ...string) (string, error) {
	full := []string{"-u", serviceUser, "--", "env",
		"HOME=" + home,
		fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", uid),
		fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", uid),
		prog,
	}
	full = append(full, args...)
	out, err := exec.Command("runuser", full...).Output()
	return string(out), err
}

// ContainerExec mirrors lib.sh container_exec (podman exec as the account).
func ContainerExec(serviceUser, container string, args ...string) error {
	home, uid, err := LookupAccount(serviceUser)
	if err != nil {
		return err
	}
	podArgs := append([]string{"exec", container}, args...)
	return RunAsUser(serviceUser, home, uid, "podman", podArgs...)
}

// UserSystemctl runs `systemctl --user` as the account; bestEffort mirrors
// `|| true` used by remove-customer.sh.
func UserSystemctl(serviceUser string, bestEffort bool, args ...string) error {
	home, uid, err := LookupAccount(serviceUser)
	if err != nil {
		return err
	}
	sysArgs := append([]string{"--user"}, args...)
	err = RunAsUser(serviceUser, home, uid, "systemctl", sysArgs...)
	if err != nil && bestEffort {
		return nil
	}
	return err
}

// UserPodman runs `podman` as the account.
func UserPodman(serviceUser string, args ...string) error {
	home, uid, err := LookupAccount(serviceUser)
	if err != nil {
		return err
	}
	return RunAsUser(serviceUser, home, uid, "podman", args...)
}

// CopyFile mirrors `cp -a src dst` for regular files (content + mode 0644 default kept).
func CopyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, st.Mode().Perm())
}

// InstallFile writes data with mode and chowns to user:group names (best-effort chown).
func InstallFile(path string, data []byte, mode os.FileMode, owner, group string) error {
	if err := os.WriteFile(path, data, mode); err != nil {
		return err
	}
	return Chown(path, owner, group)
}

// Chown looks up names and chowns; no-op when names are empty.
func Chown(path, owner, group string) error {
	if owner == "" && group == "" {
		return nil
	}
	uid, gid := -1, -1
	if owner != "" {
		if u, err := user.Lookup(owner); err == nil {
			uid, _ = strconv.Atoi(u.Uid)
		} else if n, err := strconv.Atoi(owner); err == nil {
			uid = n
		}
	}
	if group != "" {
		if g, err := user.LookupGroup(group); err == nil {
			gid, _ = strconv.Atoi(g.Gid)
		} else if n, err := strconv.Atoi(group); err == nil {
			gid = n
		}
	}
	if uid == -1 && gid == -1 {
		return nil
	}
	return os.Chown(path, uid, gid)
}

// MkdirOwn mirrors `install -d -o user -g user -m mode dir...`.
func MkdirOwn(mode os.FileMode, owner, group string, dirs ...string) error {
	for _, d := range dirs {
		if err := os.MkdirAll(d, mode); err != nil {
			return err
		}
		if err := os.Chmod(d, mode); err != nil {
			return err
		}
		if err := Chown(d, owner, group); err != nil {
			return err
		}
	}
	return nil
}

// ExeName returns the dispatcher-relevant name: argv[0] basename with
// optional .sh suffix stripped (busybox/symlink mode).
func ExeName(argv0 string) string {
	b := filepath.Base(argv0)
	return strings.TrimSuffix(b, ".sh")
}

// Have reports whether a binary is on PATH.
func Have(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
