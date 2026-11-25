// Package fleet ports modules/monitoring-backup/scripts.
package fleet

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"sandpodctl/internal/platform"
)

// BackupCustomer ports backup-customer.sh.
func BackupCustomer(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: backup-customer <instance-id> [--service-user ...] [--dir ...] [--keep 7]")
		return 2
	}
	inst := args[0]
	if !platform.ValidInstanceAny(inst) {
		fmt.Fprintln(os.Stderr, "Bad instance id.")
		return 2
	}
	svc := "svc-c" + inst
	dir := "/var/backups/sandpod"
	keep := 7
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--service-user", "--dir", "--keep":
			if i+1 >= len(rest) {
				fmt.Fprintf(os.Stderr, "Missing value for %s.\n", rest[i])
				return 2
			}
			switch rest[i] {
			case "--service-user":
				svc = rest[i+1]
			case "--dir":
				dir = rest[i+1]
			case "--keep":
				n, err := strconv.Atoi(rest[i+1])
				if err != nil {
					fmt.Fprintln(os.Stderr, "--keep must be a positive integer.")
					return 2
				}
				keep = n
			}
			i++
		default:
			fmt.Fprintf(os.Stderr, "Unknown: %s\n", rest[i])
			return 2
		}
	}
	if keep < 1 {
		fmt.Fprintln(os.Stderr, "--keep must be a positive integer.")
		return 2
	}
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	home, _, err := platform.LookupAccount(svc)
	if err != nil || home == "" {
		fmt.Fprintf(os.Stderr, "No data dir for %s.\n", svc)
		return 1
	}
	if _, err := os.Stat(home + "/data"); err != nil {
		fmt.Fprintf(os.Stderr, "No data dir for %s.\n", svc)
		return 1
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = os.Chmod(dir, 0700)
	ts := time.Now().Format("20060102-150405")
	out := filepath.Join(dir, fmt.Sprintf("%s-%s.tar.gz", inst, ts))
	tar := exec.Command("tar", "-czpf", out, "-C", home, "data", "config")
	tar.Stdout, tar.Stderr = os.Stdout, os.Stderr
	if err := tar.Run(); err != nil {
		return 1
	}
	_ = os.Chmod(out, 0600)
	fmt.Println(out)
	matches, _ := filepath.Glob(filepath.Join(dir, inst+"-*.tar.gz"))
	sort.Strings(matches)
	if len(matches) > keep {
		for _, f := range matches[:len(matches)-keep] {
			_ = os.Remove(f)
			fmt.Printf("Pruned %s\n", f)
		}
	}
	return 0
}

// ---- resource-report ----

type xfsEntry struct {
	Used, Soft, Hard string
}

func loadXFSReportFile(path string, blocks, inodes map[string]xfsEntry, byIDBlocks, byIDInodes map[string]string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !regexp.MustCompile(`^\s*#[0-9]+\s`).MatchString(line) {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		id, name, used, soft, hard := f[0], f[1], f[2], f[3], ""
		if len(f) >= 5 {
			hard = f[4]
		}
		if !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(name) {
			continue
		}
		if blocks != nil {
			blocks[name] = xfsEntry{used, soft, hard}
			byIDBlocks[strings.TrimPrefix(id, "#")] = name
		} else {
			inodes[name] = xfsEntry{used, soft, hard}
			byIDInodes[strings.TrimPrefix(id, "#")] = name
		}
	}
}

func mountsHaveQuota() bool {
	raw, _ := os.ReadFile("/proc/mounts")
	hasXFS := regexp.MustCompile(`\s/\sxfs`).MatchString(" " + string(raw))
	hasQ := strings.Contains(string(raw), "pquota") || strings.Contains(string(raw), "prjquota")
	return hasXFS && hasQ
}

func diskFor(user, uid string, r map[string]xfsEntry, byID map[string]string) string {
	name := ""
	if _, ok := r[user]; ok {
		name = user
	} else if uid != "" {
		if n, ok := byID[uid]; ok {
			name = n
		}
	}
	if name == "" {
		return "quota-off"
	}
	e := r[name]
	return fmt.Sprintf("%s used / %s soft / %s hard", e.Used, e.Soft, e.Hard)
}

func inodesFor(user, uid string, r map[string]xfsEntry, byID map[string]string) string {
	name := ""
	if _, ok := r[user]; ok {
		name = user
	} else if uid != "" {
		if n, ok := byID[uid]; ok {
			name = n
		}
	}
	if name == "" {
		return "-"
	}
	e := r[name]
	return fmt.Sprintf("%s used / %s soft / %s hard", e.Used, e.Soft, e.Hard)
}

// ResourceReport ports resource-report.sh.
func ResourceReport(args []string) int {
	registry := "/etc/sandpod/customers.tsv"
	xfsReport, xfsInodeReport := "", ""
	jsonOut := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--registry", "--xfs-report-file", "--xfs-inode-report-file":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Missing value for %s.\n", args[i])
				return 2
			}
			switch args[i] {
			case "--registry":
				registry = args[i+1]
			case "--xfs-report-file":
				xfsReport = args[i+1]
			case "--xfs-inode-report-file":
				xfsInodeReport = args[i+1]
			}
			i++
		case "--json":
			jsonOut = true
		default:
			fmt.Fprintf(os.Stderr, "Unknown: %s\n", args[i])
			return 2
		}
	}
	if _, err := os.Stat(registry); err != nil {
		fmt.Fprintf(os.Stderr, "Missing registry: %s\n", registry)
		return 1
	}
	blocks := map[string]xfsEntry{}
	inodes := map[string]xfsEntry{}
	byIDB := map[string]string{}
	byIDI := map[string]string{}
	if xfsReport != "" {
		loadXFSReportFile(xfsReport, blocks, nil, byIDB, nil)
	} else if mountsHaveQuota() {
		tmp, err := os.CreateTemp("", "sandpod-xfs-report-*")
		if err == nil {
			tmp.Close()
			c := exec.Command("xfs_quota", "-x", "-c", "report -p -b -h", "/")
			out, err := c.Output()
			if err == nil {
				_ = os.WriteFile(tmp.Name(), out, 0600)
				loadXFSReportFile(tmp.Name(), blocks, nil, byIDB, nil)
			}
			_ = os.Remove(tmp.Name())
		}
	}
	if xfsInodeReport != "" {
		loadXFSReportFile(xfsInodeReport, nil, inodes, nil, byIDI)
	} else if mountsHaveQuota() {
		tmp, err := os.CreateTemp("", "sandpod-xfs-inodes-*")
		if err == nil {
			tmp.Close()
			c := exec.Command("xfs_quota", "-x", "-c", "report -p -i -h", "/")
			out, err := c.Output()
			if err == nil {
				_ = os.WriteFile(tmp.Name(), out, 0600)
				loadXFSReportFile(tmp.Name(), nil, inodes, nil, byIDI)
			}
			_ = os.Remove(tmp.Name())
		}
	}
	type row struct {
		inst, user, cont string
	}
	var rows []row
	if raw, err := os.ReadFile(registry); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			t := strings.TrimSpace(line)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			parts := strings.Split(line, "\t")
			if len(parts) < 3 {
				continue
			}
			rows = append(rows, row{parts[0], parts[1], parts[2]})
		}
	}
	statsFor := func(user, cont string) string {
		home, uid, err := platform.LookupAccount(user)
		if err != nil {
			return "no such user"
		}
		if !platform.UserManagerUp(uid) {
			return "user manager down"
		}
		out, err := platform.OutputAsUser(user, home, uid, "podman", "stats", "--no-stream", "--format", "{{.CPUPerc}} {{.MemUsage}}", cont)
		if err != nil {
			return "stopped stopped"
		}
		return strings.TrimSpace(out)
	}
	uidFor := func(user string) string {
		_, uid, err := platform.LookupAccount(user)
		if err != nil {
			return ""
		}
		return strconv.Itoa(uid)
	}
	if jsonOut {
		type item struct {
			Instance  string `json:"instance"`
			User      string `json:"user"`
			Container string `json:"container"`
			Stats     string `json:"stats"`
			Disk      string `json:"disk"`
			Inodes    string `json:"inodes"`
		}
		var items []item
		for _, r := range rows {
			uid := uidFor(r.user)
			st := statsFor(r.user, r.cont)
			if uid == "" {
				st = "no such user"
			}
			items = append(items, item{r.inst, r.user, r.cont, st, diskFor(r.user, uid, blocks, byIDB), inodesFor(r.user, uid, inodes, byIDI)})
		}
		if items == nil {
			items = []item{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "")
		_ = enc.Encode(items)
		return 0
	}
	fmt.Printf("%-8s %-12s %-14s %-28s %-32s %s\n", "INSTANCE", "USER", "CONTAINER", "STATS", "DISK", "INODES")
	for _, r := range rows {
		uid := uidFor(r.user)
		st := statsFor(r.user, r.cont)
		if uid == "" {
			st = "no such user"
		}
		fmt.Printf("%-8s %-12s %-14s %-28s %-32s %s\n", r.inst, r.user, r.cont, st, diskFor(r.user, uid, blocks, byIDB), inodesFor(r.user, uid, inodes, byIDI))
	}
	fmt.Println("Limits (template): 1 CPU (quota 100%) / 1 GiB RAM, swap 0 / 512 pids (TasksMax 1024) / IOWeight 100. Disk quota: host-storage (soft 8G / hard 10G per account).")
	return 0
}
