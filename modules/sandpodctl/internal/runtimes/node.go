package runtimes

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"sandpodctl/internal/platform"
)

// shared helpers for node/python installers
func needTool(name string) error {
	if !platform.Have(name) {
		return fmt.Errorf("Missing required tool: %s", name)
	}
	return nil
}

func selinuxLabel(prefix string) error {
	if !platform.SELinuxEnforcing() {
		return nil
	}
	if err := platform.LabelContainerFiles(prefix + `(/.*)?`); err != nil {
		return err
	}
	return platform.Restorecon(true, prefix)
}

// InstallNodeRuntimes ports install-node-runtimes.sh.
func InstallNodeRuntimes(args []string) int {
	node22, node24, node26 := "22.19.0", "24.13.0", "26.2.0"
	prefix := "/opt/sandpod/node"
	useFNM := true
	fnmVer := "1.38.0"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--node22-ver", "--node24-ver", "--node26-ver", "--prefix", "--fnm-ver":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Missing value for %s.\n", args[i])
				return 2
			}
			v := args[i+1]
			switch args[i] {
			case "--node22-ver":
				node22 = v
			case "--node24-ver":
				node24 = v
			case "--node26-ver":
				node26 = v
			case "--prefix":
				prefix = v
			case "--fnm-ver":
				fnmVer = v
			}
			i++
		case "--no-fnm":
			useFNM = false
		default:
			fmt.Fprintf(os.Stderr, "Unknown arg: %s\n", args[i])
			return 2
		}
	}
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	if c := checkPrefix(prefix); c != -1 {
		return c
	}
	for _, v := range []string{node22, node24, node26, fnmVer} {
		if !platform.ValidVersionXYZ(v) {
			fmt.Fprintf(os.Stderr, "Bad version: %s (want X.Y.Z).\n", v)
			return 2
		}
	}
	// libatomic check
	if out, _ := exec.Command("ldconfig", "-p").Output(); !strings.Contains(string(out), "libatomic") {
		if platform.Have("dnf") {
			fmt.Println("libatomic not found, installing via dnf ...")
			c := exec.Command("dnf", "install", "-y", "libatomic")
			c.Stdout, c.Stderr = os.Stdout, os.Stderr
			if err := c.Run(); err != nil {
				fmt.Fprintln(os.Stderr, "WARNING: libatomic install failed; node26 may not start.")
			}
		} else {
			fmt.Fprintln(os.Stderr, "WARNING: libatomic not found and no dnf to install it; node26 may not start.")
		}
	}
	verify := func(major string) error {
		c := exec.Command(filepath.Join(prefix, major, "bin", "node"), "--version")
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s/bin/node does not run (missing libatomic? try: dnf install -y libatomic).\n", filepath.Join(prefix, major))
			return err
		}
		return nil
	}
	fnmDir := filepath.Join(prefix, ".fnm")
	fnmBin := "/usr/local/bin/fnm"
	installFNM := func() error {
		if _, err := os.Stat(fnmBin); err == nil {
			out, _ := exec.Command(fnmBin, "--version").Output()
			fmt.Printf("fnm present: %s", out)
			return nil
		}
		for _, t := range []string{"curl", "unzip"} {
			if err := needTool(t); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return err
			}
		}
		arch := platform.Arch()
		switch arch {
		case "x86_64":
			arch = "x64"
		case "aarch64":
			arch = "arm64"
		default:
			fmt.Fprintf(os.Stderr, "Unsupported arch for fnm: %s (use --no-fnm).\n", arch)
			return fmt.Errorf("bad arch")
		}
		tmp, err := os.MkdirTemp("/var/tmp", "sandpod-fnm-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		fmt.Printf("Fetching fnm v%s ...\n", fnmVer)
		url := fmt.Sprintf("https://github.com/Schniz/fnm/releases/download/v%s/fnm-linux.zip", fnmVer)
		dl := exec.Command("curl", "-fsSL", url, "-o", filepath.Join(tmp, "fnm.zip"))
		dl.Stdout, dl.Stderr = os.Stdout, os.Stderr
		if err := dl.Run(); err != nil {
			return err
		}
		un := exec.Command("unzip", "-o", "-q", filepath.Join(tmp, "fnm.zip"), "-d", tmp)
		un.Stdout, un.Stderr = os.Stdout, os.Stderr
		if err := un.Run(); err != nil {
			return err
		}
		ins := exec.Command("install", "-m", "0755", filepath.Join(tmp, "fnm"), fnmBin)
		ins.Stdout, ins.Stderr = os.Stdout, os.Stderr
		if err := ins.Run(); err != nil {
			return err
		}
		out, _ := exec.Command(fnmBin, "--version").Output()
		fmt.Printf("Installed fnm: %s", out)
		return nil
	}
	fnmInstallOne := func(major, ver string) error {
		dest := filepath.Join(prefix, major)
		if st, err := os.Stat(filepath.Join(dest, "bin", "node")); err == nil && !st.IsDir() {
			if out, err := exec.Command(filepath.Join(dest, "bin", "node"), "--version").Output(); err == nil {
				if strings.TrimSpace(string(out)) == "v"+ver {
					fmt.Printf("node%s present: %s at %s\n", major, strings.TrimSpace(string(out)), dest)
					return nil
				}
				fmt.Printf("node%s at %s, want v%s - reinstalling ...\n", major, strings.TrimSpace(string(out)), ver)
			}
		}
		fmt.Printf("fnm installing node v%s (major %s) ...\n", ver, major)
		os.Setenv("FNM_DIR", fnmDir)
		c := exec.Command(fnmBin, "install", ver)
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		c.Env = append(os.Environ(), "FNM_DIR="+fnmDir)
		if err := c.Run(); err != nil {
			return err
		}
		src := filepath.Join(fnmDir, "node-versions", "v"+ver, "installation")
		if _, err := os.Stat(filepath.Join(src, "bin", "node")); err != nil {
			fmt.Fprintf(os.Stderr, "fnm install missing: %s/bin/node\n", src)
			return fmt.Errorf("missing")
		}
		_ = os.Remove(dest)
		if err := os.Symlink(src, dest); err != nil {
			return err
		}
		got, err := exec.Command(filepath.Join(dest, "bin", "node"), "--version").Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s/bin/node does not run (missing libatomic?).\n", dest)
			return err
		}
		link, _ := os.Readlink(dest)
		fmt.Printf("node%s: %s -> %s\n", major, strings.TrimSpace(string(got)), link)
		return nil
	}
	tarballInstallOne := func(major, ver string) error {
		dest := filepath.Join(prefix, major)
		if out, err := exec.Command(filepath.Join(dest, "bin", "node"), "--version").Output(); err == nil && strings.TrimSpace(string(out)) == "v"+ver {
			fmt.Printf("node%s present: v%s at %s\n", major, ver, dest)
			return nil
		}
		for _, t := range []string{"curl", "tar", "xz"} {
			if err := needTool(t); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return err
			}
		}
		url := fmt.Sprintf("https://nodejs.org/dist/v%s/node-v%s-linux-x64.tar.xz", ver, ver)
		tmp, err := os.MkdirTemp("/var/tmp", "sandpod-node-"+major+"-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		fmt.Printf("Fetching node v%s -> %s ...\n", ver, dest)
		if err := platform.Fetch(url, filepath.Join(tmp, "node.tar.xz")); err != nil {
			return err
		}
		un := exec.Command("tar", "-xJf", filepath.Join(tmp, "node.tar.xz"), "-C", tmp)
		un.Stdout, un.Stderr = os.Stdout, os.Stderr
		if err := un.Run(); err != nil {
			return err
		}
		fresh := filepath.Join(tmp, "node-v"+ver+"-linux-x64", "bin", "node")
		if _, err := os.Stat(fresh); err != nil {
			fmt.Fprintln(os.Stderr, "ERROR: extracted tree missing bin/node.")
			return err
		}
		if err := exec.Command(fresh, "--version").Run(); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: fresh node v%s does not run.\n", ver)
			return err
		}
		_ = os.RemoveAll(dest)
		_ = os.MkdirAll(dest, 0755)
		cp := exec.Command("cp", "-a", filepath.Join(tmp, "node-v"+ver+"-linux-x64")+"/.", dest+"/")
		cp.Stdout, cp.Stderr = os.Stdout, os.Stderr
		if err := cp.Run(); err != nil {
			return err
		}
		got, err := exec.Command(filepath.Join(dest, "bin", "node"), "--version").Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s/bin/node does not run (missing libatomic?).\n", dest)
			return err
		}
		fmt.Printf("Installed node%s: %s\n", major, strings.TrimSpace(string(got)))
		return nil
	}

	_ = os.MkdirAll(prefix, 0755)
	os.Setenv("FNM_DIR", fnmDir)
	if useFNM {
		if err := installFNM(); err != nil {
			return 1
		}
		_ = os.MkdirAll(fnmDir, 0755)
		for _, mv := range [][2]string{{"22", node22}, {"24", node24}, {"26", node26}} {
			if err := fnmInstallOne(mv[0], mv[1]); err != nil {
				return 1
			}
		}
	} else {
		for _, mv := range [][2]string{{"22", node22}, {"24", node24}, {"26", node26}} {
			if err := tarballInstallOne(mv[0], mv[1]); err != nil {
				return 1
			}
		}
	}
	_ = os.Remove(filepath.Join(prefix, "default"))
	_ = os.Symlink("22", filepath.Join(prefix, "default"))
	if err := selinuxLabel(prefix); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("---")
	for _, m := range []string{"22", "24", "26"} {
		if err := verify(m); err != nil {
			return 1
		}
	}
	for _, m := range []string{"22", "24", "26"} {
		out, _ := exec.Command(filepath.Join(prefix, m, "bin", "node"), "--version").Output()
		fmt.Println(strings.TrimSpace(string(out)))
	}
	fmt.Println("Next: mount /opt/sandpod/node read-only into customer containers")
	return 0
}

// UpdateNodeRuntimes ports update-node-runtimes.sh (delegates to installer).
func UpdateNodeRuntimes(args []string) int {
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	prefix := "/opt/sandpod/node"
	var pass []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--prefix" {
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "Missing --prefix value.")
				return 2
			}
			prefix = args[i+1]
			pass = append(pass, args[i], args[i+1])
			i++
		} else {
			pass = append(pass, args[i])
		}
	}
	if c := checkPrefix(prefix); c != -1 {
		return c
	}
	code := InstallNodeRuntimes(pass)
	if code != 0 {
		return code
	}
	if platform.SELinuxEnforcing() {
		_ = platform.Restorecon(true, prefix)
	}
	fmt.Println("Node trees are updated. Running port apps still use their loaded runtime.")
	fmt.Println("Restart each affected port app in every customer container")
	fmt.Println("(reload-customer-app per domain, or rollout-app-patch in batches).")
	return 0
}
