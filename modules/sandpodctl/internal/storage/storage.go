// Package storage ports modules/host-storage/scripts.
package storage

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"sandpodctl/internal/platform"
)

func timeStamp() string { return time.Now().Format("20060102-150405") }

// EnableProjectQuota ports enable-project-quota.sh.
func EnableProjectQuota(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "Usage: enable-project-quota (no args)")
		return 2
	}
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	fstab := "/etc/fstab"
	raw, err := os.ReadFile(fstab)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	uuidLine := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if regexp.MustCompile(`^\s*UUID=\S+\s+/\s+xfs\s`).MatchString(line) {
			uuidLine = line
			break
		}
	}
	if uuidLine == "" {
		fmt.Fprintf(os.Stderr, "No xfs / entry in %s.\n", fstab)
		return 1
	}
	if regexp.MustCompile(`^\s*UUID=\S+\s+/\s+xfs\s+\S*pquota`).MatchString(string(raw)) {
		fmt.Println("fstab already has pquota for /.")
	} else {
		// Backup then append ,pquota to the / options field.
		bak := fmt.Sprintf("%s.bak.%s", fstab, timeStamp())
		_ = platform.CopyFile(fstab, bak)
		re := regexp.MustCompile(`(?m)^(UUID=\S+\s+/\s+xfs\s+)(\S+)`)
		updated := re.ReplaceAllStringFunc(string(raw), func(m string) string {
			sub := re.FindStringSubmatch(m)
			if len(sub) != 3 {
				return m
			}
			opts := sub[2]
			if strings.Contains(strings.Split(opts, ",")[0]+",", "pquota,") || containsOpt(opts, "pquota") {
				return m
			}
			_ = opts
			return sub[1] + sub[2] + ",pquota"
		})
		// Replace only first occurrence (bash python did count=1).
		_ = updated
		lines := strings.Split(string(raw), "\n")
		n := 0
		for i, l := range lines {
			if n == 0 && regexp.MustCompile(`^UUID=\S+\s+/\s+xfs\s+`).MatchString(l) {
				lines[i] = addPquota(l)
				n = 1
			}
		}
		_ = os.WriteFile(fstab, []byte(strings.Join(lines, "\n")), 0644)
		_ = exec.Command("systemctl", "daemon-reload").Run()
		fmt.Printf("Persisted pquota in %s.\n", fstab)
	}
	if platform.Have("grubby") {
		out, _ := exec.Command("grubby", "--info=DEFAULT").Output()
		if strings.Contains(string(out), "rootflags=.*pquota") || regexp.MustCompile(`rootflags=.*pquota`).MatchString(string(out)) {
			fmt.Println("Kernel cmdline already has rootflags=pquota.")
		} else {
			c := exec.Command("grubby", "--update-kernel=ALL", "--args=rootflags=pquota")
			c.Stdout, c.Stderr = os.Stdout, os.Stderr
			_ = c.Run()
			fmt.Println("Added rootflags=pquota to all boot entries.")
		}
	} else {
		fmt.Fprintln(os.Stderr, "WARNING: grubby not found - add rootflags=pquota to the kernel cmdline manually.")
	}
	mounts, _ := os.ReadFile("/proc/mounts")
	active := false
	for _, l := range strings.Split(string(mounts), "\n") {
		if regexp.MustCompile(`\s/\sxfs`).MatchString(" "+l) && regexp.MustCompile(`pquota|prjquota`).MatchString(l) {
			active = true
		}
	}
	if active {
		fmt.Println("ACTIVE: / is mounted with project quota accounting.")
		c := exec.Command("xfs_quota", "-x", "-c", "state -p", "/")
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		_ = c.Run()
	} else {
		fmt.Println("PENDING: / is not (yet) mounted with pquota.")
		fmt.Println("Reboot to activate, then re-run this command to verify.")
	}
	return 0
}

func containsOpt(opts, want string) bool {
	for _, o := range strings.Split(opts, ",") {
		if o == want {
			return true
		}
	}
	return false
}

func addPquota(line string) string {
	re := regexp.MustCompile(`^(UUID=\S+\s+/\s+xfs\s+)(\S+)`)
	m := re.FindStringSubmatch(line)
	if m == nil {
		return line
	}
	if containsOpt(m[2], "pquota") {
		return line
	}
	return m[1] + m[2] + ",pquota"
}

// QuotaReport ports quota-report.sh.
func QuotaReport(args []string) int {
	registry := "/etc/sandpod/customers.tsv"
	for i := 0; i < len(args); i++ {
		if args[i] == "--registry" {
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "Missing --registry value.")
				return 2
			}
			registry = args[i+1]
			i++
		} else {
			fmt.Fprintf(os.Stderr, "Unknown: %s\n", args[i])
			return 2
		}
	}
	mounts, _ := os.ReadFile("/proc/mounts")
	// bash checks `grep -P '\s/\sxfs' /proc/mounts | grep -qE 'pquota|prjquota'`
	off := true
	for _, l := range strings.Split(string(mounts), "\n") {
		if strings.Contains(" "+l+" ", " / xfs ") && (strings.Contains(l, "pquota") || strings.Contains(l, "prjquota")) {
			off = false
		}
	}
	_ = off
	// When accounting is off, fall back to du like the script.
	hasQuota := false
	for _, l := range strings.Split(string(mounts), "\n") {
		if regexp.MustCompile(`\s/\sxfs`).MatchString(" "+l) && regexp.MustCompile(`pquota|prjquota`).MatchString(l) {
			hasQuota = true
		}
	}
	if !hasQuota {
		fmt.Println("Project quota accounting is OFF (see host-storage enable-project-quota.sh).")
		fmt.Println("Falling back to du (slow, no enforcement):")
		raw, _ := os.ReadFile(registry)
		for _, line := range strings.Split(string(raw), "\n") {
			t := strings.TrimSpace(line)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			parts := strings.Split(line, "\t")
			if len(parts) < 2 {
				continue
			}
			inst, user := parts[0], parts[1]
			home, _, err := platform.LookupAccount(user)
			if err != nil || home == "" {
				fmt.Printf("%s(%s): no home\n", user, inst)
				continue
			}
			if _, err := os.Stat(home); err != nil {
				fmt.Printf("%s(%s): no home\n", user, inst)
				continue
			}
			out, _ := exec.Command("du", "-sh", home).Output()
			size := strings.Fields(string(out))
			s := ""
			if len(size) > 0 {
				s = size[0]
			}
			fmt.Printf("%s(%s): %s\n", user, inst, s)
		}
		return 0
	}
	// Accounting on: parse /etc/projid then xfs_quota report. With a canonical
	// projid file xfs_quota prints project names; with a broken one it prints
	// `#<id>`. Keep whichever key the report actually uses.
	out, err := exec.Command("xfs_quota", "-x", "-c", "report -p -h", "/").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	keep := map[string]bool{}
	if raw, err := os.ReadFile("/etc/projid"); err == nil {
		for _, l := range strings.Split(string(raw), "\n") {
			if name, id, ok := parseProjidLine(l); ok {
				keep[name] = true
				keep["#"+id] = true
			}
		}
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) > 0 {
		fmt.Println(lines[0])
	}
	if len(lines) > 1 {
		fmt.Println(lines[1])
	}
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) == 0 {
			continue
		}
		if keep[f[0]] {
			fmt.Println(l)
		}
	}
	return 0
}

// ReapplyQuotas ports reapply-quotas.sh.
func ReapplyQuotas(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "Usage: reapply-quotas (no args)")
		return 2
	}
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	mounts, _ := os.ReadFile("/proc/mounts")
	has := false
	for _, l := range strings.Split(string(mounts), "\n") {
		if regexp.MustCompile(`\s/\sxfs`).MatchString(" "+l) && regexp.MustCompile(`pquota|prjquota`).MatchString(l) {
			has = true
		}
	}
	if !has {
		fmt.Fprintln(os.Stderr, "PENDING: / has no pquota accounting; limits cannot apply yet.")
		fmt.Fprintln(os.Stderr, "Run enable-project-quota + reboot first.")
		return 3
	}
	users := map[string]bool{}
	if raw, err := os.ReadFile("/etc/sandpod/customers.tsv"); err == nil {
		for _, l := range strings.Split(string(raw), "\n") {
			t := strings.TrimSpace(l)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			parts := strings.Split(l, "\t")
			if len(parts) >= 2 && parts[1] != "" {
				users[parts[1]] = true
			}
		}
	}
	if raw, err := os.ReadFile("/etc/projid"); err == nil {
		for _, l := range strings.Split(string(raw), "\n") {
			if name, _, ok := parseProjidLine(l); ok {
				users[name] = true
			}
		}
	}
	var list []string
	for u := range users {
		list = append(list, u)
	}
	sort.Strings(list)
	if len(list) == 0 {
		fmt.Println("No customers found; nothing to do.")
		return 0
	}
	fail := false
	for _, u := range list {
		if code := SetCustomerQuota([]string{u}); code != 0 {
			fmt.Fprintf(os.Stderr, "FAILED: %s\n", u)
			fail = true
		}
	}
	if fail {
		return 1
	}
	return 0
}

// SetCustomerQuota ports set-customer-quota.sh.
func SetCustomerQuota(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: set-customer-quota <service-user> [--hard-gb 10] [--soft-gb 8] [--hard-inodes N] [--soft-inodes N]")
		return 2
	}
	svc := args[0]
	hardGB, softGB := "10", "8"
	hardIn, softIn := "0", "0"
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--hard-gb", "--soft-gb", "--hard-inodes", "--soft-inodes":
			if i+1 >= len(rest) {
				fmt.Fprintf(os.Stderr, "Missing value for %s.\n", rest[i])
				return 2
			}
			switch rest[i] {
			case "--hard-gb":
				hardGB = rest[i+1]
			case "--soft-gb":
				softGB = rest[i+1]
			case "--hard-inodes":
				hardIn = rest[i+1]
			case "--soft-inodes":
				softIn = rest[i+1]
			}
			i++
		default:
			fmt.Fprintf(os.Stderr, "Unknown arg: %s\n", rest[i])
			return 2
		}
	}
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	if _, _, err := platform.LookupAccount(svc); err != nil {
		fmt.Fprintln(os.Stderr, "Unknown user.")
		return 1
	}
	numRe := regexp.MustCompile(`^[0-9]+$`)
	if !numRe.MatchString(hardGB) || !numRe.MatchString(softGB) {
		fmt.Fprintln(os.Stderr, "Quotas must be whole GB.")
		return 2
	}
	h, _ := strconv.Atoi(hardGB)
	s, _ := strconv.Atoi(softGB)
	if s > h {
		fmt.Fprintln(os.Stderr, "soft must be <= hard.")
		return 2
	}
	if !numRe.MatchString(hardIn) || !numRe.MatchString(softIn) {
		fmt.Fprintln(os.Stderr, "Inode limits must be counts (0 = unset).")
		return 2
	}
	hi, _ := strconv.Atoi(hardIn)
	si, _ := strconv.Atoi(softIn)
	if hi > 0 && si > 0 && si > hi {
		fmt.Fprintln(os.Stderr, "soft-inodes must be <= hard-inodes.")
		return 2
	}
	home, _, err := platform.LookupAccount(svc)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Unknown user.")
		return 1
	}
	_, uid, _ := platform.LookupAccount(svc)
	projid := strconv.Itoa(uid)
	if home == "" || home == "/" || !strings.HasPrefix(home, "/home/") {
		fmt.Fprintf(os.Stderr, "Refusing: unexpected home '%s' for %s.\n", home, svc)
		return 1
	}
	if _, err := os.Stat(home); err != nil {
		fmt.Fprintln(os.Stderr, "No home dir.")
		return 1
	}
	// /etc/projid + /etc/projects use the projid(5)/projects(5) layout:
	// /etc/projid = <name>:<id>, /etc/projects = <id>:<path>. Legacy
	// <id>:<name> / <name>:<path> entries are migrated in place.
	_ = touch("/etc/projid")
	_ = touch("/etc/projects")
	rewriteCanonical("/etc/projid", dropProjidEntry(svc, projid), svc+":"+projid)
	rewriteCanonical("/etc/projects", dropProjectsEntry(svc, projid, home), projid+":"+home)
	// Tag the tree.
	tag := exec.Command("xfs_quota", "-x", "-c", "project -s -p "+home+" "+projid, "/")
	tagOut, _ := tag.CombinedOutput()
	for _, l := range strings.Split(string(tagOut), "\n") {
		if strings.Contains(l, "skipping special file") {
			continue
		}
		if strings.TrimSpace(l) != "" {
			fmt.Println(l)
		}
	}
	statOut, _ := exec.Command("xfs_io", "-c", "stat", home).Output()
	tagVal := ""
	if m := regexp.MustCompile(`fsxattr\.projid = ([0-9]*)`).FindStringSubmatch(string(statOut)); m != nil {
		tagVal = m[1]
	}
	if tagVal != projid {
		fmt.Fprintf(os.Stderr, "WARNING: project tag missing on %s (got '%s'). Quota not applied.\n", home, orQ(tagVal))
		return 1
	}
	mounts, _ := os.ReadFile("/proc/mounts")
	has := false
	for _, l := range strings.Split(string(mounts), "\n") {
		if regexp.MustCompile(`\s/\sxfs`).MatchString(" "+l) && regexp.MustCompile(`pquota|prjquota`).MatchString(l) {
			has = true
		}
	}
	if !has {
		fmt.Println("PENDING: / has no pquota accounting (reboot first - see enable-project-quota).")
		fmt.Printf("Project %s (id %s) tagged at %s; limits apply on next run after reboot:\n", svc, projid, home)
		fmt.Printf("  sandpodctl set-customer-quota %s --hard-gb %s --soft-gb %s --hard-inodes %s --soft-inodes %s\n", svc, hardGB, softGB, hardIn, softIn)
		return 3
	}
	limitArgs := fmt.Sprintf("bhard=%sg bsoft=%sg", hardGB, softGB)
	if hi > 0 {
		limitArgs += fmt.Sprintf(" ihard=%d", hi)
	}
	if si > 0 {
		limitArgs += fmt.Sprintf(" isoft=%d", si)
	}
	lc := exec.Command("xfs_quota", "-x", "-c", "limit -p "+limitArgs+" "+projid, "/")
	lc.Stdout, lc.Stderr = os.Stdout, os.Stderr
	if err := lc.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Setting limits failed.")
		return 1
	}
	fmt.Printf("Quota %s: soft=%sG hard=%sG inodes soft=%s hard=%s on %s\n", svc, softGB, hardGB, softIn, hardIn, home)
	qc := exec.Command("xfs_quota", "-x", "-c", "quota -N -p "+projid, "/")
	qc.Stdout = os.Stdout
	_ = qc.Run()
	return 0
}

// projIDRe matches a numeric project id; projNameRe matches a SandPod
// service-user project name.
var (
	projIDRe   = regexp.MustCompile(`^[0-9]+$`)
	projNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// parseProjidLine accepts the canonical projid(5) layout `<name>:<id>` and the
// legacy SandPod layout `<id>:<name>`. Both are migrated to canonical on the
// next set-customer-quota run.
func parseProjidLine(line string) (name, id string, ok bool) {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") {
		return "", "", false
	}
	i := strings.Index(t, ":")
	if i <= 0 || i >= len(t)-1 {
		return "", "", false
	}
	a, b := strings.TrimSpace(t[:i]), strings.TrimSpace(t[i+1:])
	if projNameRe.MatchString(a) && projIDRe.MatchString(b) {
		return a, b, true
	}
	if projIDRe.MatchString(a) && projNameRe.MatchString(b) {
		return b, a, true
	}
	return "", "", false
}

// dropProjidEntry removes any line describing this project, in either layout.
func dropProjidEntry(svc, id string) func(string) bool {
	return func(line string) bool {
		name, pid, ok := parseProjidLine(line)
		return ok && (name == svc || pid == id)
	}
}

// dropProjectsEntry removes the project path in either layout (`<id>:<path>`
// canonically, `<name>:<path>` for legacy files) plus stale paths for the
// account. `projects(5)` keys on the path, so a name left behind would keep a
// second project entry alive.
func dropProjectsEntry(svc, id, home string) func(string) bool {
	return func(line string) bool {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			return false
		}
		i := strings.Index(t, ":")
		if i <= 0 || i >= len(t)-1 {
			return false
		}
		a, b := strings.TrimSpace(t[:i]), strings.TrimSpace(t[i+1:])
		if b == home {
			return true
		}
		return a == svc || a == id
	}
}

// mergeCanonical drops stale entries, appends the canonical line and keeps the
// file sorted. It is pure so both the writer and tests can use it.
func mergeCanonical(lines []string, drop func(string) bool, add string) []string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) == "" || drop(l) {
			continue
		}
		out = append(out, l)
	}
	out = append(out, add)
	sort.Strings(out)
	return out
}

func rewriteCanonical(path string, drop func(string) bool, add string) {
	raw, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	merged := mergeCanonical(lines, drop, add)
	_ = os.WriteFile(path, []byte(strings.Join(merged, "\n")+"\n"), 0644)
}

func touch(p string) error {
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDONLY, 0644)
	if err != nil {
		return err
	}
	return f.Close()
}

func orQ(s string) string {
	if s == "" {
		return "?"
	}
	return s
}
