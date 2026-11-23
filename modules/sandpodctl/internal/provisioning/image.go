package provisioning

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"sandpodctl/internal/platform"
)

// ensureImage mirrors the rootless per-account image transfer in provision-customer.sh.
func ensureImage(svc, home string, uid int, image, imageTar, imageSourceUser, registry string) {
	runAs := func(prog string, args ...string) error {
		return platform.RunAsUser(svc, home, uid, prog, args...)
	}
	if imageExistsAs(svc, home, uid, image) {
		fmt.Printf("Image %s already present for %s\n", image, svc)
		return
	}
	if imageTar != "" {
		if _, err := os.Stat(imageTar); err != nil {
			fmt.Fprintf(os.Stderr, "Image tar not found: %s\n", imageTar)
			return
		}
		fmt.Printf("Loading image from %s ...\n", imageTar)
		_ = runAs("podman", "load", "-i", imageTar)
		return
	}
	var donors []string
	if imageSourceUser != "" {
		donors = append(donors, imageSourceUser)
	}
	if raw, err := os.ReadFile(registry); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimRight(line, "\r")
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.Split(line, "\t")
			if len(parts) < 2 {
				continue
			}
			d := parts[1]
			if d != "" && d != svc {
				donors = append(donors, d)
			}
		}
	}
	donors = append(donors, "svc-c001")
	seen := map[string]bool{}
	var uniq []string
	for _, d := range donors {
		if !seen[d] {
			seen[d] = true
			uniq = append(uniq, d)
		}
	}
	for _, donor := range uniq {
		if donor == svc {
			continue
		}
		if exec.Command("id", donor).Run() != nil {
			continue
		}
		dhome, duid, err := platform.LookupAccount(donor)
		if err != nil {
			continue
		}
		if _, err := os.Stat(fmt.Sprintf("/run/user/%d", duid)); err != nil {
			continue
		}
		if !imageExistsAs(donor, dhome, duid, image) {
			continue
		}
		fmt.Printf("Transferring %s from %s to %s ...\n", image, donor, svc)
		tmp, err := os.CreateTemp("/var/tmp", "sandpod-image-*.tar")
		if err != nil {
			continue
		}
		tmpPath := tmp.Name()
		tmp.Close()
		_ = os.Chown(tmpPath, duid, duid)
		_ = os.Chmod(tmpPath, 0600)
		save := exec.Command("runuser", "-u", donor, "--", "env",
			"HOME="+dhome, fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", duid),
			"podman", "save", "-o", tmpPath, image)
		save.Stdout = os.Stdout
		save.Stderr = os.Stderr
		if err := save.Run(); err != nil {
			_ = os.Remove(tmpPath)
			continue
		}
		_ = runAs("podman", "load", "-i", tmpPath)
		_ = os.Remove(tmpPath)
		return
	}
	fmt.Fprintf(os.Stderr, "WARNING: image %s not found locally; unit start will try to pull it.\n", image)
}

func imageExistsAs(svc, home string, uid int, image string) bool {
	c := exec.Command("runuser", "-u", svc, "--", "env",
		"HOME="+home, fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", uid),
		"podman", "image", "exists", image)
	return c.Run() == nil
}
