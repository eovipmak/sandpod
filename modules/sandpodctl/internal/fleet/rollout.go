package fleet

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"sandpodctl/internal/platform"
	"sandpodctl/internal/provisioning"
	"sandpodctl/internal/runtimes"
)

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type customer struct {
	inst, user, cont, web string
}

func loadCustomers(registry string) ([]customer, error) {
	raw, err := os.ReadFile(registry)
	if err != nil {
		return nil, err
	}
	var out []customer
	for _, line := range strings.Split(string(raw), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 5 {
			continue
		}
		out = append(out, customer{parts[0], parts[1], parts[2], parts[3]})
	}
	return out, nil
}

// RolloutPHPPatch ports rollout-php-patch.sh.
func RolloutPHPPatch(args []string) int {
	batch := 5
	sleepS := 10
	skipUpdate := false
	var minors []string
	registry := "/etc/sandpod/customers.tsv"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--batch-size", "--sleep-seconds", "--minor", "--registry":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Missing value for %s.\n", args[i])
				return 2
			}
			v := args[i+1]
			switch args[i] {
			case "--batch-size":
				n, err := strconv.Atoi(v)
				if err != nil {
					fmt.Fprintln(os.Stderr, "--batch-size must be a positive integer.")
					return 2
				}
				batch = n
			case "--sleep-seconds":
				n, err := strconv.Atoi(v)
				if err != nil {
					fmt.Fprintln(os.Stderr, "--sleep-seconds must be a non-negative integer.")
					return 2
				}
				sleepS = n
			case "--minor":
				minors = append(minors, v)
			case "--registry":
				registry = v
			}
			i++
		case "--skip-update":
			skipUpdate = true
		default:
			fmt.Fprintf(os.Stderr, "Unknown arg: %s\n", args[i])
			return 2
		}
	}
	if batch < 1 {
		fmt.Fprintln(os.Stderr, "--batch-size must be a positive integer.")
		return 2
	}
	if len(minors) == 0 {
		minors = []string{"8.2", "8.3", "8.4", "8.5", "8.6"}
	}
	for _, m := range minors {
		if !platform.ValidMinor(m) {
			fmt.Fprintf(os.Stderr, "Unsupported --minor: %s (want 8.2-8.6).\n", m)
			return 2
		}
	}
	if _, err := os.Stat(registry); err != nil {
		fmt.Fprintf(os.Stderr, "Missing %s.\n", registry)
		return 1
	}
	if !skipUpdate {
		fmt.Println("== Updating host PHP RPMs ==")
		if code := runtimes.UpdatePHPRuntimes(nil); code != 0 {
			return code
		}
	} else {
		fmt.Println("== Skipping host RPM update (--skip-update) ==")
	}
	customers, err := loadCustomers(registry)
	if err != nil || len(customers) == 0 {
		fmt.Fprintln(os.Stderr, "Registry empty.")
		return 1
	}
	fmt.Printf("== Rolling out to %d customers, batch-size %d, minors: %s ==\n", len(customers), batch, strings.Join(minors, " "))
	fail := false
	batchN := 0
	flush := func(members []customer) bool {
		if len(members) == 0 {
			return true
		}
		batchN++
		names := make([]string, len(members))
		for i, m := range members {
			names[i] = m.cont
		}
		fmt.Printf("--- Batch %d: %s ---\n", batchN, strings.Join(names, " "))
		for _, c := range members {
			for _, minor := range minors {
				fmt.Printf("Reload %s php%s ...\n", c.cont, minor)
				if code := runtimes.ReloadCustomerPHP([]string{c.user, c.cont, minor}); code != 0 {
					fmt.Fprintf(os.Stderr, "FAIL reload %s php%s\n", c.cont, minor)
					fail = true
				}
			}
			fmt.Printf("Health-check %s (port %s) ...\n", c.cont, c.web)
			if code := provisioning.HealthCheck([]string{c.web}); code != 0 {
				fmt.Fprintf(os.Stderr, "FAIL health %s\n", c.cont)
				fail = true
			}
		}
		if fail {
			fmt.Fprintf(os.Stderr, "Batch %d FAILED - stopping (fix forward, re-run with --skip-update).\n", batchN)
			return false
		}
		fmt.Printf("Batch %d OK. Sleeping %ds ...\n", batchN, sleepS)
		time.Sleep(time.Duration(sleepS) * time.Second)
		return true
	}
	var members []customer
	for _, c := range customers {
		members = append(members, c)
		if len(members) >= batch {
			if !flush(members) {
				return 1
			}
			members = nil
		}
	}
	if !flush(members) {
		return 1
	}
	fmt.Printf("Rollout complete, failures: %d\n", boolToInt(fail))
	if fail {
		return 1
	}
	return 0
}

// RolloutAppPatch ports rollout-app-patch.sh.
func RolloutAppPatch(args []string) int {
	batch := 5
	sleepS := 10
	skipNode, skipPy := false, false
	registry := "/etc/sandpod/customers.tsv"
	var nodeArgs, pyArgs []string
	collectExtra := func(flag string, dst *[]string, i *int) {
		*dst = append(*dst, args[*i+1])
		*i++
		_ = flag
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--batch-size":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "Missing --batch-size value.")
				return 2
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				fmt.Fprintln(os.Stderr, "--batch-size must be a positive integer.")
				return 2
			}
			batch = n
			i++
		case "--sleep-seconds":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "Missing --sleep-seconds value.")
				return 2
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				fmt.Fprintln(os.Stderr, "--sleep-seconds must be a non-negative integer.")
				return 2
			}
			sleepS = n
			i++
		case "--skip-node-update":
			skipNode = true
		case "--skip-python-update":
			skipPy = true
		case "--skip-update":
			skipNode, skipPy = true, true
		case "--registry":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "Missing --registry value.")
				return 2
			}
			registry = args[i+1]
			i++
		case "--node-args":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "Missing --node-args value.")
				return 2
			}
			collectExtra("--node-args", &nodeArgs, &i)
		case "--python-args":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "Missing --python-args value.")
				return 2
			}
			collectExtra("--python-args", &pyArgs, &i)
		default:
			fmt.Fprintf(os.Stderr, "Unknown arg: %s\n", args[i])
			return 2
		}
	}
	if batch < 1 {
		fmt.Fprintln(os.Stderr, "--batch-size must be a positive integer.")
		return 2
	}
	if _, err := os.Stat(registry); err != nil {
		fmt.Fprintf(os.Stderr, "Missing %s.\n", registry)
		return 1
	}
	if !skipNode {
		fmt.Println("== Updating host Node.js trees ==")
		if code := runtimes.UpdateNodeRuntimes(nodeArgs); code != 0 {
			return code
		}
	} else {
		fmt.Println("== Skipping host Node.js update ==")
	}
	if !skipPy {
		fmt.Println("== Updating host Python trees ==")
		if code := runtimes.UpdatePythonRuntimes(pyArgs); code != 0 {
			return code
		}
	} else {
		fmt.Println("== Skipping host Python update ==")
	}
	customers, err := loadCustomers(registry)
	if err != nil || len(customers) == 0 {
		fmt.Fprintln(os.Stderr, "Registry empty.")
		return 1
	}
	fmt.Printf("== Rolling out to %d customers, batch-size %d ==\n", len(customers), batch)
	fail := false
	batchN := 0
	flush := func(members []customer) bool {
		if len(members) == 0 {
			return true
		}
		batchN++
		names := make([]string, len(members))
		for i, m := range members {
			names[i] = m.cont
		}
		fmt.Printf("--- Batch %d: %s ---\n", batchN, strings.Join(names, " "))
		for _, c := range members {
			fmt.Printf("Port apps for %s ...\n", c.cont)
			domains, err := portAppDomains(c.user, c.cont)
			if err != nil {
				fmt.Fprintf(os.Stderr, "FAIL list-sites %s\n", c.cont)
				fail = true
				continue
			}
			if len(domains) == 0 {
				fmt.Printf("(no port apps for %s, skipping restarts)\n", c.cont)
			} else {
				for _, d := range domains {
					fmt.Printf("Restart %s %s ...\n", c.cont, d)
					if code := runtimes.ReloadCustomerApp([]string{c.user, c.cont, d}); code != 0 {
						fmt.Fprintf(os.Stderr, "FAIL restart %s %s\n", c.cont, d)
						fail = true
					}
				}
			}
			fmt.Printf("Health-check %s (port %s) ...\n", c.cont, c.web)
			if code := provisioning.HealthCheck([]string{c.web}); code != 0 {
				fmt.Fprintf(os.Stderr, "FAIL health %s\n", c.cont)
				fail = true
			}
		}
		if fail {
			fmt.Fprintf(os.Stderr, "Batch %d FAILED - stopping (fix forward, re-run with --skip-update).\n", batchN)
			return false
		}
		fmt.Printf("Batch %d OK. Sleeping %ds ...\n", batchN, sleepS)
		time.Sleep(time.Duration(sleepS) * time.Second)
		return true
	}
	var members []customer
	for _, c := range customers {
		members = append(members, c)
		if len(members) >= batch {
			if !flush(members) {
				return 1
			}
			members = nil
		}
	}
	if !flush(members) {
		return 1
	}
	fmt.Printf("Rollout complete, failures: %d\n", boolToInt(fail))
	if fail {
		return 1
	}
	return 0
}

// portAppDomains lists port-app domains via list-sites --json of the same binary.
func portAppDomains(user, cont string) ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	out, err := exec.Command(exe, "list-sites", user, cont, "--json").Output()
	if err != nil {
		return nil, err
	}
	var parsed struct {
		PortApps []struct {
			Domain string `json:"domain"`
		} `json:"port_apps"`
	}
	dec := jsonDecoder(out)
	if err := dec(&parsed); err != nil {
		return nil, err
	}
	var domains []string
	for _, a := range parsed.PortApps {
		if a.Domain != "" {
			domains = append(domains, a.Domain)
		}
	}
	sort.Strings(domains)
	return domains, nil
}

func jsonDecoder(b []byte) func(any) error {
	return func(v any) error {
		return jsonUnmarshal(b, v)
	}
}

func jsonUnmarshal(b []byte, v any) error {
	return json.Unmarshal(b, v)
}
