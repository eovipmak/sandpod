// Package provisioning ports modules/customer-provisioning/scripts.
package provisioning

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"sandpodctl/internal/platform"
)

// HealthCheck ports health-check.sh.
func HealthCheck(args []string) int {
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprintln(os.Stderr, "Usage: health-check <web-port 1-65535> [base-host]")
		return 2
	}
	var port int
	if _, err := fmt.Sscanf(args[0], "%d", &port); err != nil || port < 1 || port > 65535 || !regexp.MustCompile(`^[0-9]+$`).MatchString(args[0]) {
		fmt.Fprintln(os.Stderr, "Usage: health-check <web-port 1-65535> [base-host]")
		return 2
	}
	base := "127.0.0.1"
	if len(args) == 2 {
		base = args[1]
	}
	client := &http.Client{Timeout: 10 * time.Second}
	fail := false
	check := func(host, path, expect string) {
		url := fmt.Sprintf("http://%s:%d%s", base, port, path)
		req, _ := http.NewRequest("GET", url, nil)
		req.Host = host
		resp, err := client.Do(req)
		body := ""
		code := "000"
		if err == nil {
			defer resp.Body.Close()
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			body = string(b)
			code = fmt.Sprintf("%d", resp.StatusCode)
		}
		if code == "200" {
			if expect != "" && !strings.Contains(body, expect) {
				snip := body
				if len(snip) > 80 {
					snip = snip[:80]
				}
				fmt.Printf("FAIL %s%s: 200 but body lacks '%s' (got: %s)\n", host, path, expect, snip)
				fail = true
			} else {
				snip := body
				if len(snip) > 60 {
					snip = snip[:60]
				}
				fmt.Printf("OK   %s%s: %s %s\n", host, path, code, snip)
			}
		} else {
			fmt.Printf("FAIL %s%s: HTTP %s\n", host, path, code)
			fail = true
		}
	}
	for _, minor := range []string{"8.2", "8.3", "8.4", "8.5", "8.6"} {
		check("php"+strings.Replace(minor, ".", "", 1)+".test", "/index.php", minor)
	}
	check("localhost", "/", "")
	if fail {
		return 1
	}
	return 0
}

// AddSSHKey ports add-ssh-key.sh.
func AddSSHKey(args []string) int {
	if len(args) != 3 {
		fmt.Fprintln(os.Stderr, "Usage: add-ssh-key <service-user> <container-name> <pubkey-file>")
		return 2
	}
	svc, cont, keyFile := args[0], args[1], args[2]
	if !platform.ValidServiceUser(svc) {
		fmt.Fprintln(os.Stderr, "Invalid service user.")
		return 2
	}
	if !platform.ValidContainer(cont) {
		fmt.Fprintln(os.Stderr, "Invalid container name.")
		return 2
	}
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Key file not found.")
		return 2
	}
	key := strings.TrimSpace(string(raw))
	keyRe := regexp.MustCompile(`^(ssh-ed25519|ecdsa-sha2-nistp[0-9]+|ssh-rsa|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com) [A-Za-z0-9+/=]+( .*)?$`)
	if !keyRe.MatchString(key) {
		fmt.Fprintln(os.Stderr, "Not a valid OpenSSH public key.")
		return 2
	}
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	home, uid, err := platform.LookupAccount(svc)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !platform.UserManagerUp(uid) {
		fmt.Fprintln(os.Stderr, "User manager not running (enable-linger first).")
		return 1
	}
	remote := "key=\"$1\"\n" +
		"install -d -o customer -g customer -m 0700 /home/customer/.ssh\n" +
		"touch /home/customer/.ssh/authorized_keys\n" +
		"chown customer:customer /home/customer/.ssh/authorized_keys\n" +
		"chmod 0600 /home/customer/.ssh/authorized_keys\n" +
		"if ! grep -qxF \"$key\" /home/customer/.ssh/authorized_keys; then\n" +
		"    printf '%s\\n' \"$key\" >> /home/customer/.ssh/authorized_keys\n" +
		"fi\n"
	_ = home
	c := exec.Command("runuser", "-u", svc, "--", "env",
		"HOME="+home,
		fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", uid),
		fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", uid),
		"podman", "exec", "-i", cont, "/bin/bash", "-euo", "pipefail", "-s", key)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = strings.NewReader(remote)
	// NOTE: podman exec -i reads the script on stdin; the key travels as $1.
	// Go's exec cannot pass "$1" directly, so embed via extra sh -c wrapper:
	// instead re-exec with bash -s and key as trailing arg is handled by
	// passing key through environment-safe quoting below.
	_ = c
	// Simpler faithful approach: write key to a temp file readable only by
	// the service user and pipe the remote script with the key inlined.
	script := fmt.Sprintf("key=%q\n%s", key, strings.SplitN(remote, "\n", 2)[1])
	c2 := exec.Command("runuser", "-u", svc, "--", "env",
		"HOME="+home,
		fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", uid),
		fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", uid),
		"podman", "exec", "-i", cont, "/bin/bash", "-euo", "pipefail", "-s")
	c2.Stdout = os.Stdout
	c2.Stderr = os.Stderr
	c2.Stdin = strings.NewReader(script)
	if err := c2.Run(); err != nil {
		return 1
	}
	fmt.Printf("Installed key into %s:/home/customer/.ssh/authorized_keys\n", cont)
	return 0
}

// portInUse mirrors provision port_in_use: SANDPOD_USED_PORTS + ss + tcp dial.
func portInUse(port string) bool {
	if env := os.Getenv("SANDPOD_USED_PORTS"); env != "" {
		// bash: case ",${SANDPOD_USED_PORTS}," in *",${port},"*|*" ${port},"*|...
		// Approximate with token split on comma/space.
		for _, tok := range strings.Fields(strings.ReplaceAll(env, ",", " ")) {
			if tok == port {
				return true
			}
		}
	}
	if platform.Have("ss") {
		out, err := exec.Command("ss", "-ltnH").Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				f := strings.Fields(line)
				if len(f) == 0 {
					continue
				}
				addr := f[len(f)-1]
				if strings.HasSuffix(addr, ":"+port) {
					return true
				}
			}
			return false
		}
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
