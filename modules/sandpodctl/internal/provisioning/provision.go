package provisioning

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"sandpodctl/internal/platform"
	"sandpodctl/internal/storage"
)

// Provision ports provision-customer.sh (rootless account + Quadlet + data/config).
func Provision(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: provision-customer <instance-id> [--service-user svc-c002] [--web-port 18002]")
		return 2
	}
	inst := args[0]
	if !platform.ValidInstance3(inst) {
		fmt.Fprintln(os.Stderr, "instance-id must be 3 digits (e.g. 002).")
		return 2
	}
	svc, webPort, sshPort, sshKey, image, imageTar, imageSourceUser := "", "", "", "", "localhost/sandpod/customer-runtime:node-shared", "", ""
	skipTransfer := false
	quotaGB := "10"
	hardInodes, softInodes := "0", "0"
	registry := "/etc/sandpod/customers.tsv"
	cpus, memory, ioWeight := "1.0", "1g", "100"
	skipStart, force := false, false
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--service-user", "--web-port", "--ssh-port", "--ssh-key", "--image", "--image-tar",
			"--image-source-user", "--quota-gb", "--hard-inodes", "--soft-inodes", "--registry",
			"--cpus", "--memory", "--io-weight":
			if i+1 >= len(rest) {
				fmt.Fprintf(os.Stderr, "Missing value for %s.\n", rest[i])
				return 2
			}
			v := rest[i+1]
			switch rest[i] {
			case "--service-user":
				svc = v
			case "--web-port":
				webPort = v
			case "--ssh-port":
				sshPort = v
			case "--ssh-key":
				sshKey = v
			case "--image":
				image = v
			case "--image-tar":
				imageTar = v
			case "--image-source-user":
				imageSourceUser = v
			case "--quota-gb":
				quotaGB = v
			case "--hard-inodes":
				hardInodes = v
			case "--soft-inodes":
				softInodes = v
			case "--registry":
				registry = v
			case "--cpus":
				cpus = v
			case "--memory":
				memory = v
			case "--io-weight":
				ioWeight = v
			}
			i++
		case "--skip-image-transfer":
			skipTransfer = true
		case "--skip-start":
			skipStart = true
		case "--force":
			force = true
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, "Usage: provision-customer <instance-id> [--service-user svc-c002] ...")
			return 0
		default:
			fmt.Fprintf(os.Stderr, "Unknown arg: %s\n", rest[i])
			return 2
		}
	}
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	if svc == "" {
		svc = "svc-c" + inst
	}
	if !platform.ValidServiceUser(svc) {
		fmt.Fprintln(os.Stderr, "Invalid service user.")
		return 2
	}
	if !platform.ValidImage(image) {
		fmt.Fprintln(os.Stderr, "Invalid --image tag.")
		return 2
	}
	qgb, err := strconv.Atoi(quotaGB)
	if err != nil || qgb <= 0 || !regexp.MustCompile(`^[0-9]+$`).MatchString(quotaGB) {
		fmt.Fprintln(os.Stderr, "Invalid --quota-gb (positive integer).")
		return 2
	}
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(hardInodes) || !regexp.MustCompile(`^[0-9]+$`).MatchString(softInodes) {
		fmt.Fprintln(os.Stderr, "Inode limits must be counts (0 = unset).")
		return 2
	}
	if !regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`).MatchString(cpus) {
		fmt.Fprintln(os.Stderr, "Invalid --cpus (e.g. 0.5, 1.0, 2).")
		return 2
	}
	if !regexp.MustCompile(`^[0-9]+[kKmMgG]?$`).MatchString(memory) {
		fmt.Fprintln(os.Stderr, "Invalid --memory (e.g. 512m, 1g, 2g).")
		return 2
	}
	ioN, _ := strconv.Atoi(ioWeight)
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(ioWeight) || ioN < 10 || ioN > 1000 {
		fmt.Fprintln(os.Stderr, "Invalid --io-weight (10-1000).")
		return 2
	}
	var cpuF float64
	fmt.Sscanf(cpus, "%f", &cpuF)
	cpuPct := int(cpuF * 100)
	if cpuPct < 10 || cpuPct > 1600 {
		fmt.Fprintln(os.Stderr, "Invalid --cpus (0.1-16).")
		return 2
	}
	memBytes, ok := platform.MemToBytes(memory)
	if !ok || memBytes <= 0 {
		fmt.Fprintln(os.Stderr, "Invalid --memory.")
		return 2
	}
	memHigh := memBytes * 9 / 10
	memSys := strings.ToUpper(memory)

	exe, _ := os.Executable()
	repoRoot := repoRootFromExe(exe)
	templateSrc := filepath.Join(repoRoot, "modules/customer-container/quadlet/customer@.container")
	nginxSrc := filepath.Join(repoRoot, "modules/customer-container/config/nginx.conf")
	indexSrc := filepath.Join(repoRoot, "modules/customer-container/config/default-index.html")
	pilotPHP := filepath.Join(repoRoot, "modules/customer-container/examples/pilot-site/index.php")
	for _, p := range []string{templateSrc, nginxSrc} {
		if _, err := os.Stat(p); err != nil {
			fmt.Fprintf(os.Stderr, "Missing template: %s\n", p)
			return 1
		}
	}

	// Existing ports from registry + live drop-in.
	existingWeb, existingSSH := readRegistryPorts(registry, inst)
	guessHome := guessHome(svc)
	if data, err := os.ReadFile(filepath.Join(guessHome, ".config/containers/systemd/customer@"+inst+".container.d/10-instance.conf")); err == nil {
		if m := regexp.MustCompile(`PublishPort=127\.0\.0\.1:([0-9]+)\(:80|:80\)`).FindStringSubmatch(string(data)); m != nil {
			existingWeb = m[1]
		}
		if m := regexp.MustCompile(`PublishPort=127\.0\.0\.1:([0-9]+):22`).FindStringSubmatch(string(data)); m != nil {
			existingSSH = m[1]
		}
		// bash uses grep -oP 'PublishPort=127\.0\.0\.1:\K[0-9]+(?=:80)'; approximate above.
		_ = existingWeb
		_ = existingSSH
	}
	// Re-parse with simpler regex to be safe.
	if data, err := os.ReadFile(filepath.Join(guessHome, ".config/containers/systemd/customer@"+inst+".container.d/10-instance.conf")); err == nil {
		re := regexp.MustCompile(`PublishPort=127\.0\.0\.1:([0-9]+):(80|22)`)
		for _, m := range re.FindAllStringSubmatch(string(data), -1) {
			if m[2] == "80" {
				existingWeb = m[1]
			} else {
				existingSSH = m[1]
			}
		}
	}
	if webPort == "" {
		if existingWeb != "" {
			webPort = existingWeb
		}
	}
	if sshPort == "" {
		if existingSSH != "" {
			sshPort = existingSSH
		}
	}
	n, _ := strconv.Atoi(inst)
	if webPort == "" {
		webPort = strconv.Itoa(18001)
		if m := n + 18000; m >= 18001 {
			webPort = strconv.Itoa(m)
		}
		for portInUse(webPort) || registryHasPort(registry, webPort) {
			p, _ := strconv.Atoi(webPort)
			webPort = strconv.Itoa(p + 1)
		}
	}
	if sshPort == "" {
		sshPort = strconv.Itoa(22201)
		if m := n + 22200; m >= 22201 {
			sshPort = strconv.Itoa(m)
		}
		for portInUse(sshPort) || registryHasPort(registry, sshPort) {
			p, _ := strconv.Atoi(sshPort)
			sshPort = strconv.Itoa(p + 1)
		}
	}
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(webPort) || !regexp.MustCompile(`^[0-9]+$`).MatchString(sshPort) {
		fmt.Fprintln(os.Stderr, "Ports must be numeric.")
		return 2
	}
	if webPort == sshPort {
		fmt.Fprintln(os.Stderr, "Web and SSH ports must differ.")
		return 2
	}
	if !force {
		if portInUse(webPort) {
			fmt.Fprintf(os.Stderr, "Web port %s is in use. Pass --force to override.\n", webPort)
			return 1
		}
		if portInUse(sshPort) {
			fmt.Fprintf(os.Stderr, "SSH port %s is in use. Pass --force to override.\n", sshPort)
			return 1
		}
	}
	if sshKey != "" {
		if _, err := os.Stat(sshKey); err != nil {
			fmt.Fprintf(os.Stderr, "SSH key not found: %s\n", sshKey)
			return 2
		}
	}

	container := "customer-" + inst
	fmt.Printf("Provisioning instance=%s user=%s container=%s web=127.0.0.1:%s ssh=127.0.0.1:%s\n", inst, svc, container, webPort, sshPort)

	// 1. Service account.
	if _, _, err := platform.LookupAccount(svc); err != nil {
		if err := platform.Run("useradd", "--create-home", "--shell", "/sbin/nologin", svc); err != nil {
			fmt.Fprintf(os.Stderr, "useradd failed: %v\n", err)
			return 1
		}
		fmt.Printf("Created user %s\n", svc)
	} else {
		fmt.Printf("User %s exists, reusing.\n", svc)
	}
	home, uid, err := platform.LookupAccount(svc)
	if err != nil || home == "" {
		fmt.Fprintf(os.Stderr, "Cannot resolve home for %s\n", svc)
		return 1
	}
	_ = exec.Command("loginctl", "enable-linger", svc).Run()

	// 3. Quadlet units.
	unitDir := home + "/.config/containers/systemd"
	dropinDir := filepath.Join(unitDir, "customer@"+inst+".container.d")
	if err := platform.MkdirOwn(0755, "", "", home+"/.config", home+"/.config/containers", unitDir, dropinDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = platform.Chown(home+"/.config", svc, svc)
	tpl, err := os.ReadFile(templateSrc)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = os.WriteFile(filepath.Join(unitDir, "customer@.container"), tpl, 0644)
	_ = platform.Chown(filepath.Join(unitDir, "customer@.container"), svc, svc)
	_ = os.Remove(filepath.Join(unitDir, "customer@"+inst+".container"))
	if err := os.Symlink("customer@.container", filepath.Join(unitDir, "customer@"+inst+".container")); err != nil && !os.IsExist(err) {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = platform.Chown(filepath.Join(unitDir, "customer@"+inst+".container"), svc, svc)
	inst10 := fmt.Sprintf("[Container]\nImage=%s\nPublishPort=127.0.0.1:%s:80\nPublishPort=127.0.0.1:%s:22\n", image, webPort, sshPort)
	if err := os.WriteFile(filepath.Join(dropinDir, "10-instance.conf"), []byte(inst10), 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = platform.Chown(filepath.Join(dropinDir, "10-instance.conf"), svc, svc)
	res20 := fmt.Sprintf(`# Pinned at provision time by provision-customer
# (--cpus %s --memory %s --io-weight %s).
[Container]
Memory=%s
PidsLimit=512
PodmanArgs=--memory-swap=%s --cpus=%s --blkio-weight=%s --memory-swappiness=0

[Service]
CPUQuota=%d%%
CPUWeight=100
MemoryHigh=%d
MemoryMax=%s
MemorySwapMax=0
TasksMax=1024
IOWeight=%s
`, cpus, memory, ioWeight, memory, memory, cpus, ioWeight, cpuPct, memHigh, memSys, ioWeight)
	if err := os.WriteFile(filepath.Join(dropinDir, "20-resources.conf"), []byte(res20), 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = platform.Chown(filepath.Join(dropinDir, "20-resources.conf"), svc, svc)

	// 4. Data + config.
	if err := platform.MkdirOwn(0755, svc, svc, home+"/data", home+"/data/public_html", home+"/config", home+"/config/sites", home+"/config/supervisor"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if _, err := os.Stat(home + "/config/nginx.conf"); err != nil {
		if data, err := os.ReadFile(nginxSrc); err == nil {
			_ = os.WriteFile(home+"/config/nginx.conf", data, 0644)
			_ = platform.Chown(home+"/config/nginx.conf", svc, svc)
			fmt.Println("Installed config/nginx.conf template")
		}
	} else {
		fmt.Println("config/nginx.conf exists, keeping.")
	}
	if _, err := os.Stat(home + "/data/public_html/index.html"); err != nil {
		if data, err := os.ReadFile(indexSrc); err == nil {
			_ = os.WriteFile(home+"/data/public_html/index.html", data, 0644)
			_ = platform.Chown(home+"/data/public_html/index.html", svc, svc)
		}
	}
	if _, err := os.Stat(home + "/data/public_html/index.php"); err != nil {
		if data, err := os.ReadFile(pilotPHP); err == nil {
			_ = os.WriteFile(home+"/data/public_html/index.php", data, 0644)
			_ = platform.Chown(home+"/data/public_html/index.php", svc, svc)
		}
	}

	// 5. Image + start.
	if !skipTransfer {
		ensureImage(svc, home, uid, image, imageTar, imageSourceUser, registry)
	}
	if err := platform.RunAsUser(svc, home, uid, "systemctl", "--user", "daemon-reload"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !skipStart {
		if err := platform.RunAsUser(svc, home, uid, "systemctl", "--user", "restart", "customer@"+inst+".service"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		_ = platform.RunAsUser(svc, home, uid, "systemctl", "--user", "enable", "customer@"+inst+".service")
		fmt.Printf("Started customer@%s.service\n", inst)
	} else {
		fmt.Println("Skipped start (--skip-start).")
	}

	// 6. SSH key.
	if sshKey != "" && skipStart {
		fmt.Fprintln(os.Stderr, "WARNING: --ssh-key ignored with --skip-start; run add-ssh-key afterwards.")
	}
	if sshKey != "" && !skipStart {
		if code := AddSSHKey([]string{svc, container, sshKey}); code != 0 {
			return code
		}
	}

	// 7. Registry with lock.
	_ = os.MkdirAll(filepath.Dir(registry), 0755)
	if _, err := os.Stat(registry); err != nil {
		_ = os.WriteFile(registry, []byte("#instance\tservice_user\tcontainer\tweb_port\tssh_port\timage\n"), 0644)
	}
	regErr := withRegistryLock(registry, func() error {
		raw, _ := os.ReadFile(registry)
		var others []string
		for _, line := range strings.Split(string(raw), "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.Split(line, "\t")
			if len(parts) > 0 && parts[0] == inst {
				continue
			}
			others = append(others, line)
		}
		othersText := strings.Join(others, "\n")
		for registryTextHasPort(othersText, webPort) {
			p, _ := strconv.Atoi(webPort)
			webPort = strconv.Itoa(p + 1)
		}
		for registryTextHasPort(othersText, sshPort) {
			p, _ := strconv.Atoi(sshPort)
			sshPort = strconv.Itoa(p + 1)
		}
		oldW, oldS := "", ""
		if data, err := os.ReadFile(filepath.Join(dropinDir, "10-instance.conf")); err == nil {
			re := regexp.MustCompile(`PublishPort=127\.0\.0\.1:([0-9]+):(80|22)`)
			for _, m := range re.FindAllStringSubmatch(string(data), -1) {
				if m[2] == "80" {
					oldW = m[1]
				} else {
					oldS = m[1]
				}
			}
		}
		inst10b := fmt.Sprintf("[Container]\nImage=%s\nPublishPort=127.0.0.1:%s:80\nPublishPort=127.0.0.1:%s:22\n", image, webPort, sshPort)
		_ = os.WriteFile(filepath.Join(dropinDir, "10-instance.conf"), []byte(inst10b), 0644)
		_ = platform.Chown(filepath.Join(dropinDir, "10-instance.conf"), svc, svc)
		if oldW+oldS != webPort+sshPort && !skipStart {
			fmt.Fprintf(os.Stderr, "Ports bumped under lock (%s/%s -> %s/%s); restarting unit ...\n", orDash(oldW), orDash(oldS), webPort, sshPort)
			_ = platform.RunAsUser(svc, home, uid, "systemctl", "--user", "daemon-reload")
			_ = platform.RunAsUser(svc, home, uid, "systemctl", "--user", "restart", "customer@"+inst+".service")
		}
		var b strings.Builder
		for _, o := range others {
			b.WriteString(o + "\n")
		}
		b.WriteString(fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\n", inst, svc, container, webPort, sshPort, image))
		tmp := registry + ".tmp"
		if err := os.WriteFile(tmp, []byte(b.String()), 0644); err != nil {
			return err
		}
		return os.Rename(tmp, registry)
	})
	if regErr != nil {
		fmt.Fprintln(os.Stderr, regErr)
		return 1
	}

	// 8. Disk quota (best-effort).
	if qgb > 0 {
		softGB := qgb * 8 / 10
		if softGB < 1 {
			softGB = 1
		}
		qargs := []string{svc, "--hard-gb", quotaGB, "--soft-gb", strconv.Itoa(softGB)}
		if hi, _ := strconv.Atoi(hardInodes); hi > 0 {
			qargs = append(qargs, "--hard-inodes", hardInodes)
		}
		if si, _ := strconv.Atoi(softInodes); si > 0 {
			qargs = append(qargs, "--soft-inodes", softInodes)
		}
		if storage.SetCustomerQuota(qargs) != 0 {
			fmt.Fprintln(os.Stderr, "NOTE: quota not enforced yet (see host-storage README).")
		}
	}

	fmt.Println("Done. Verify with:")
	fmt.Printf("  sandpodctl health-check %s\n", webPort)
	return 0
}

func orDash(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

func repoRootFromExe(exe string) string {
	// Binary lives at modules/sandpodctl/sandpodctl (or ./sandpodctl in dev).
	// Walk up until a dir containing modules/customer-container exists.
	dir := filepath.Dir(exe)
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "modules/customer-container")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	if cwd, err := os.Getwd(); err == nil {
		if _, err := os.Stat(filepath.Join(cwd, "modules/customer-container")); err == nil {
			return cwd
		}
	}
	return filepath.Dir(exe)
}

func readRegistryPorts(registry, inst string) (web, ssh string) {
	raw, err := os.ReadFile(registry)
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) >= 5 && parts[0] == inst {
			return parts[3], parts[4]
		}
	}
	return "", ""
}

func registryHasPort(registry, port string) bool {
	raw, err := os.ReadFile(registry)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		for _, f := range strings.Split(line, "\t") {
			if f == port {
				return true
			}
		}
		// bash also greps "\t${PORT}\t" via -P; tab-split covers it.
	}
	return false
}

func registryTextHasPort(text, port string) bool {
	for _, line := range strings.Split(text, "\n") {
		for _, f := range strings.Split(line, "\t") {
			if f == port {
				return true
			}
		}
	}
	return false
}

func guessHome(svc string) string {
	if h, _, err := platform.LookupAccount(svc); err == nil && h != "" {
		return h
	}
	return "/home/" + svc
}
