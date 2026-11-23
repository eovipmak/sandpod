package provisioning

import (
	"os"
	"strings"
)

// rewriteRegistryDropInstance removes lines starting with "inst\t".
func rewriteRegistryDropInstance(registry, inst string) error {
	raw, err := os.ReadFile(registry)
	if err != nil {
		return err
	}
	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, inst+"\t") {
			continue
		}
		kept = append(kept, line)
	}
	out := strings.Join(kept, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	tmp := registry + ".tmp"
	if err := os.WriteFile(tmp, []byte(out), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, registry)
}
