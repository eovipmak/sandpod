// Package runtimes ports modules/shared-runtimes/scripts.
package runtimes

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"sandpodctl/internal/platform"
)

// ReloadCustomerPHP ports reload-customer-php.sh.
func ReloadCustomerPHP(args []string) int {
	if len(args) != 3 {
		fmt.Fprintln(os.Stderr, "Usage: reload-customer-php <service-user> <container-name> <php-minor>")
		return 2
	}
	svc, cont, minor := args[0], args[1], args[2]
	if !platform.ValidServiceUser(svc) {
		fmt.Fprintln(os.Stderr, "Invalid service account name.")
		return 2
	}
	if !platform.ValidContainer(cont) {
		fmt.Fprintln(os.Stderr, "Invalid container name.")
		return 2
	}
	if !platform.ValidMinor(minor) {
		fmt.Fprintln(os.Stderr, "Unsupported PHP minor version.")
		return 2
	}
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	home, uid, err := platform.LookupAccount(svc)
	if err != nil || home == "" {
		fmt.Fprintln(os.Stderr, "The service account needs an active lingering user manager.")
		return 1
	}
	if !platform.UserManagerUp(uid) {
		fmt.Fprintln(os.Stderr, "The service account needs an active lingering user manager.")
		return 1
	}
	c := exec.Command("runuser", "-u", svc, "--", "env",
		"HOME="+home,
		fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", uid),
		fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", uid),
		"/bin/bash", "-c",
		`cd "$HOME" && podman exec "$1" /usr/local/bin/sandpod-runtime-reload "$2"`,
		"--", cont, minor)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return 1
	}
	return 0
}

// ReloadCustomerApp ports reload-customer-app.sh (needs customer-apps lib for domain validation).
func ReloadCustomerApp(args []string) int {
	if len(args) != 3 {
		fmt.Fprintln(os.Stderr, "Usage: reload-customer-app <service-user> <container> <domain>")
		return 2
	}
	svc, cont := args[0], args[1]
	domain := strings.ToLower(args[2])
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	home, uid, err := platform.LookupAccount(svc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unknown user %s.\n", svc)
		return 1
	}
	if !platform.UserManagerUp(uid) {
		fmt.Fprintf(os.Stderr, "User manager for %s is down (enable-linger first).\n", svc)
		return 1
	}
	_ = home
	if !platform.ValidContainer(cont) {
		fmt.Fprintln(os.Stderr, "Invalid container name.")
		return 2
	}
	if !platform.ValidDomain(domain) {
		fmt.Fprintf(os.Stderr, "Invalid domain: %s\n", domain)
		return 2
	}
	prog := platform.ProgramForDomain(domain)
	if err := platform.RunAsUser(svc, home, uid, "podman", "exec", cont, "supervisorctl", "restart", prog); err != nil {
		return 1
	}
	if err := platform.RunAsUser(svc, home, uid, "podman", "exec", cont, "supervisorctl", "status", prog); err != nil {
		return 1
	}
	return 0
}

func enforcing() bool {
	if !platform.Have("getenforce") {
		return false
	}
	out, err := exec.Command("getenforce").Output()
	return err == nil && strings.TrimSpace(string(out)) == "Enforcing"
}

// InstallPHPRuntimes ports install-php-runtimes.sh.
func InstallPHPRuntimes(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "Usage: install-php-runtimes (no args)")
		return 2
	}
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	run := func(prog string, a ...string) error {
		c := exec.Command(prog, a...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	}
	if err := run("dnf", "install", "-y", "https://rpms.remirepo.net/enterprise/remi-release-10.rpm"); err != nil {
		return 1
	}
	base := []string{"install", "-y",
		"php82-php-cli", "php82-php-fpm", "php82-php-mbstring", "php82-php-mysqlnd", "php82-php-opcache", "php82-php-xml",
		"php83-php-cli", "php83-php-fpm", "php83-php-mbstring", "php83-php-mysqlnd", "php83-php-opcache", "php83-php-xml",
		"php84-php-cli", "php84-php-fpm", "php84-php-mbstring", "php84-php-mysqlnd", "php84-php-opcache", "php84-php-xml",
		"php85-php-cli", "php85-php-fpm", "php85-php-mbstring", "php85-php-mysqlnd", "php85-php-opcache", "php85-php-xml",
	}
	if err := run("dnf", base...); err != nil {
		return 1
	}
	if err := run("dnf", "install", "-y",
		"php86-php-cli", "php86-php-fpm", "php86-php-mbstring", "php86-php-mysqlnd", "php86-php-opcache", "php86-php-xml"); err != nil {
		fmt.Fprintln(os.Stderr, "WARNING: php86-* not available in Remi on this host; 8.2-8.5 installed, 8.6 skipped.")
		fmt.Fprintln(os.Stderr, "Re-run this command after Remi publishes php86 to enable the 8.6 route.")
	}
	if enforcing() {
		if !platform.Have("semanage") {
			fmt.Fprintln(os.Stderr, "Install policycoreutils-python-utils to label shared runtime paths.")
			return 1
		}
		for _, p := range []string{`/opt/remi(/.*)?`, `/etc/opt/remi(/.*)?`, `/usr/lib64/libonig\.so\.105`} {
			c := exec.Command("semanage", "fcontext", "-a", "-t", "container_file_t", p)
			if c.Run() != nil {
				_ = exec.Command("semanage", "fcontext", "-m", "-t", "container_file_t", p).Run()
			}
		}
		_ = run("restorecon", "-RF", "/opt/remi", "/etc/opt/remi", "/usr/lib64/libonig.so.105")
	}
	return 0
}

// UpdatePHPRuntimes ports update-php-runtimes.sh.
func UpdatePHPRuntimes(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "Usage: update-php-runtimes (no args)")
		return 2
	}
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	run := func(a ...string) error {
		c := exec.Command("dnf", a...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	}
	if err := run("upgrade", "-y", "php82-*", "php83-*", "php84-*", "php85-*", "oniguruma5php"); err != nil {
		return 1
	}
	if err := run("upgrade", "-y", "php86-*"); err != nil {
		fmt.Fprintln(os.Stderr, "WARNING: php86-* not available; 8.2-8.5 updated.")
	}
	if enforcing() {
		_ = run("restorecon", "-RF", "/opt/remi", "/etc/opt/remi", "/usr/lib64/libonig.so.105")
	}
	fmt.Println("PHP RPMs are updated. Running FPM workers still use their loaded runtime.")
	fmt.Println("Gracefully reload the affected PHP-FPM minor version in each customer container.")
	return 0
}

var reSysPrefix = regexp.MustCompile(`^/(usr|etc|var|home|root|boot|proc|sys|dev|run|tmp|bin|sbin|lib|lib64)$`)

func checkPrefix(prefix string) int {
	if !strings.HasPrefix(prefix, "/") {
		fmt.Fprintln(os.Stderr, "--prefix must be absolute.")
		return 2
	}
	if prefix == "/" || reSysPrefix.MatchString(filepath.Clean(prefix)) {
		fmt.Fprintf(os.Stderr, "Refusing --prefix %s (system path).\n", prefix)
		return 2
	}
	return -1
}
