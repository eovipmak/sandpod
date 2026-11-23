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

// InstallPythonRuntimes ports install-python-runtimes.sh.
func InstallPythonRuntimes(args []string) int {
	py312, py313, py314 := "3.12.7", "3.13.7", "3.14.0"
	releaseTag := "20251014"
	prefix := "/opt/sandpod/python"
	useUV := true
	uvVer := "0.10.0"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--py312-ver", "--py313-ver", "--py314-ver", "--release-tag", "--prefix", "--uv-ver":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Missing value for %s.\n", args[i])
				return 2
			}
			v := args[i+1]
			switch args[i] {
			case "--py312-ver":
				py312 = v
			case "--py313-ver":
				py313 = v
			case "--py314-ver":
				py314 = v
			case "--release-tag":
				releaseTag = v
			case "--prefix":
				prefix = v
			case "--uv-ver":
				uvVer = v
			}
			i++
		case "--no-uv":
			useUV = false
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
	for _, v := range []string{py312, py313, py314, uvVer} {
		if !platform.ValidVersionXYZ(v) {
			fmt.Fprintf(os.Stderr, "Bad version: %s (want X.Y.Z).\n", v)
			return 2
		}
	}
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(releaseTag) {
		fmt.Fprintf(os.Stderr, "Bad --release-tag: %s.\n", releaseTag)
		return 2
	}
	archOut, _ := exec.Command("uname", "-m").Output()
	var target string
	switch strings.TrimSpace(string(archOut)) {
	case "x86_64":
		target = "x86_64-unknown-linux-gnu"
	case "aarch64":
		target = "aarch64-unknown-linux-gnu"
	default:
		fmt.Fprintf(os.Stderr, "Unsupported arch: %s\n", strings.TrimSpace(string(archOut)))
		return 1
	}
	uvDir := filepath.Join(prefix, ".uv")
	uvBin := "/usr/local/bin/uv"
	installUV := func() error {
		if _, err := os.Stat(uvBin); err == nil {
			out, _ := exec.Command(uvBin, "--version").Output()
			fmt.Printf("uv present: %s", out)
			return nil
		}
		for _, t := range []string{"curl", "tar"} {
			if err := needTool(t); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return err
			}
		}
		tmp, err := os.MkdirTemp("/var/tmp", "sandpod-uv-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		fmt.Printf("Fetching uv v%s ...\n", uvVer)
		url := fmt.Sprintf("https://github.com/astral-sh/uv/releases/download/%s/uv-%s.tar.gz", uvVer, target)
		dl := exec.Command("curl", "-fsSL", url, "-o", filepath.Join(tmp, "uv.tar.gz"))
		dl.Stdout, dl.Stderr = os.Stdout, os.Stderr
		if err := dl.Run(); err != nil {
			return err
		}
		un := exec.Command("tar", "-xzf", filepath.Join(tmp, "uv.tar.gz"), "-C", tmp)
		un.Stdout, un.Stderr = os.Stdout, os.Stderr
		if err := un.Run(); err != nil {
			return err
		}
		ins := exec.Command("install", "-m", "0755", filepath.Join(tmp, "uv-"+target, "uv"), uvBin)
		ins.Stdout, ins.Stderr = os.Stdout, os.Stderr
		if err := ins.Run(); err != nil {
			return err
		}
		out, _ := exec.Command(uvBin, "--version").Output()
		fmt.Printf("Installed uv: %s", out)
		return nil
	}
	curVer := func(minor, dest string) string {
		out, err := exec.Command(filepath.Join(dest, "bin", "python"+minor), "--version").Output()
		if err != nil {
			return ""
		}
		f := strings.Fields(string(out))
		if len(f) == 2 {
			return f[1]
		}
		return ""
	}
	uvInstallOne := func(ver string) error {
		minor := ver[:strings.LastIndex(ver, ".")]
		dest := filepath.Join(prefix, minor)
		if _, err := os.Stat(filepath.Join(dest, "bin", "python"+minor)); err == nil {
			if cur := curVer(minor, dest); cur == ver {
				fmt.Printf("python%s present: %s at %s\n", minor, cur, dest)
				return nil
			} else {
				fmt.Printf("python%s at %s, want %s - reinstalling ...\n", minor, cur, ver)
			}
		}
		fmt.Printf("uv installing CPython %s ...\n", ver)
		c := exec.Command(uvBin, "python", "install", ver)
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		c.Env = append(os.Environ(), "UV_PYTHON_INSTALL_DIR="+uvDir)
		if err := c.Run(); err != nil {
			return err
		}
		fc := exec.Command(uvBin, "python", "find", ver, "--python-preference", "only-managed")
		fc.Env = append(os.Environ(), "UV_PYTHON_INSTALL_DIR="+uvDir)
		binOut, err := fc.Output()
		if err != nil {
			return err
		}
		root := filepath.Dir(filepath.Dir(strings.TrimSpace(string(binOut))))
		if _, err := os.Stat(filepath.Join(root, "bin", "python"+minor)); err != nil {
			fmt.Fprintf(os.Stderr, "uv install missing: %s/bin/python%s\n", root, minor)
			return fmt.Errorf("missing")
		}
		_ = os.Remove(dest)
		if err := os.Symlink(root, dest); err != nil {
			return err
		}
		out, _ := exec.Command(filepath.Join(dest, "bin", "python"+minor), "--version").Output()
		link, _ := os.Readlink(dest)
		fmt.Printf("python%s: %s -> %s\n", minor, strings.TrimSpace(string(out)), link)
		return nil
	}
	tarballInstallOne := func(ver string) error {
		minor := ver[:strings.LastIndex(ver, ".")]
		dest := filepath.Join(prefix, minor)
		if _, err := os.Stat(filepath.Join(dest, "bin", "python"+minor)); err == nil {
			if cur := curVer(minor, dest); cur == ver {
				fmt.Printf("python%s present: %s at %s\n", minor, cur, dest)
				return nil
			}
			fmt.Printf("python%s at %s, want %s - reinstalling ...\n", minor, curVer(minor, dest), ver)
		}
		for _, t := range []string{"curl", "tar"} {
			if err := needTool(t); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return err
			}
		}
		url := fmt.Sprintf("https://github.com/astral-sh/python-build-standalone/releases/download/%s/cpython-%s+%s-%s-install_only.tar.gz", releaseTag, ver, releaseTag, target)
		tmp, err := os.MkdirTemp("/var/tmp", "sandpod-python-"+minor+"-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		fmt.Printf("Fetching CPython %s -> %s ...\n", ver, dest)
		dl := exec.Command("curl", "-fsSL", url, "-o", filepath.Join(tmp, "python.tar.gz"))
		dl.Stdout, dl.Stderr = os.Stdout, os.Stderr
		if err := dl.Run(); err != nil {
			return err
		}
		un := exec.Command("tar", "-xzf", filepath.Join(tmp, "python.tar.gz"), "-C", tmp)
		un.Stdout, un.Stderr = os.Stdout, os.Stderr
		if err := un.Run(); err != nil {
			return err
		}
		fresh := filepath.Join(tmp, "python", "bin", "python"+minor)
		if _, err := os.Stat(fresh); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: extracted tree missing bin/python%s.\n", minor)
			return err
		}
		if err := exec.Command(fresh, "--version").Run(); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: fresh python %s does not run.\n", ver)
			return err
		}
		_ = os.RemoveAll(dest)
		_ = os.MkdirAll(dest, 0755)
		cp := exec.Command("cp", "-a", filepath.Join(tmp, "python")+"/.", dest+"/")
		cp.Stdout, cp.Stderr = os.Stdout, os.Stderr
		if err := cp.Run(); err != nil {
			return err
		}
		out, _ := exec.Command(filepath.Join(dest, "bin", "python"+minor), "--version").Output()
		fmt.Printf("Installed python%s: %s\n", minor, strings.TrimSpace(string(out)))
		return nil
	}

	os.Setenv("UV_PYTHON_INSTALL_DIR", uvDir)
	_ = os.MkdirAll(prefix, 0755)
	if useUV {
		if err := installUV(); err != nil {
			return 1
		}
		_ = os.MkdirAll(uvDir, 0755)
		for _, v := range []string{py312, py313, py314} {
			if err := uvInstallOne(v); err != nil {
				return 1
			}
		}
	} else {
		for _, v := range []string{py312, py313, py314} {
			if err := tarballInstallOne(v); err != nil {
				return 1
			}
		}
	}
	_ = os.Remove(filepath.Join(prefix, "default"))
	_ = os.Symlink("3.12", filepath.Join(prefix, "default"))
	if err := selinuxLabel(prefix); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("---")
	for _, m := range []string{"3.12", "3.13", "3.14"} {
		out, err := exec.Command(filepath.Join(prefix, m, "bin", "python"+m), "--version").Output()
		if err != nil {
			return 1
		}
		fmt.Println(strings.TrimSpace(string(out)))
	}
	fmt.Println("Next: mount /opt/sandpod/python read-only into customer containers")
	return 0
}

// UpdatePythonRuntimes ports update-python-runtimes.sh.
func UpdatePythonRuntimes(args []string) int {
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	prefix := "/opt/sandpod/python"
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
	if code := InstallPythonRuntimes(pass); code != 0 {
		return code
	}
	if enforcing() {
		c := exec.Command("restorecon", "-RF", prefix)
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		_ = c.Run()
	}
	fmt.Println("Python trees are updated. Running port apps still use their loaded runtime.")
	fmt.Println("Restart each affected port app in every customer container")
	fmt.Println("(reload-customer-app per domain, or rollout-app-patch in batches).")
	return 0
}
