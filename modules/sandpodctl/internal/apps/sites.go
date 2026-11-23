package apps

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"sandpodctl/internal/platform"
)

// AddSite ports add-site.sh.
func AddSite(args []string) int {
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: add-site <service-user> <container> <domain> [--php 8.3] [--primary]")
		return 2
	}
	svc, cont, domain := args[0], args[1], strings.ToLower(args[2])
	phpMinor, primary := "8.3", false
	rest := args[3:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--php":
			if i+1 >= len(rest) {
				fmt.Fprintln(os.Stderr, "Missing --php value.")
				return 2
			}
			phpMinor = rest[i+1]
			i++
		case "--primary":
			primary = true
		default:
			fmt.Fprintf(os.Stderr, "Unknown arg: %s\n", rest[i])
			return 2
		}
	}
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	ctx, code := Load(svc, cont)
	if code != 0 {
		return code
	}
	if !platform.ValidDomain(domain) {
		fmt.Fprintf(os.Stderr, "Invalid domain: %s\n", domain)
		return 2
	}
	if !platform.ValidMinor(phpMinor) {
		fmt.Fprintf(os.Stderr, "Unsupported PHP minor (8.2-8.6): %s\n", phpMinor)
		return 2
	}
	platform.EOLWarnForMinor(phpMinor)
	backend, _ := platform.BackendForMinor(phpMinor)

	var siteRoot string
	if primary {
		siteRoot = "/srv/customer/public_html"
	} else {
		dir := ctx.Home + "/data/" + domain
		if err := platform.MkdirOwn(0755, svc, svc, dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		idx := dir + "/index.php"
		if _, err := os.Stat(idx); err != nil {
			content := fmt.Sprintf("<?php\necho \"OK from %s\\n\";\n", domain)
			if err := os.WriteFile(idx, []byte(content), 0644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			_ = platform.Chown(idx, svc, svc)
		}
		siteRoot = "/srv/customer/" + domain
	}

	if err := platform.CopyFile(ctx.NginxConf, ctx.NginxConf+".bak"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	portConflict := false
	if _, err := os.Stat(ctx.SitesDir + "/" + domain + ".conf"); err == nil {
		portConflict = true
	}
	if _, err := os.Stat(ctx.SupervisorDir + "/" + domain + ".conf"); err == nil {
		portConflict = true
	}
	if portConflict {
		_ = os.Remove(ctx.SitesDir + "/" + domain + ".conf")
		_ = os.Remove(ctx.SupervisorDir + "/" + domain + ".conf")
	}
	if err := platform.MapSet(ctx.NginxConf, "site_root", domain, siteRoot); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.Rename(ctx.NginxConf+".bak", ctx.NginxConf)
		return 1
	}
	if err := platform.MapSet(ctx.NginxConf, "php_fpm_backend", domain, backend); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.Rename(ctx.NginxConf+".bak", ctx.NginxConf)
		return 1
	}
	if err := ctx.NginxTestReload(); err != nil {
		fmt.Fprintln(os.Stderr, "Reload failed, restoring backup.")
		_ = os.Rename(ctx.NginxConf+".bak", ctx.NginxConf)
		return 1
	}
	if portConflict {
		if err := ctx.SupervisorRereadUpdate(); err != nil {
			fmt.Fprintln(os.Stderr, "WARNING: nginx reloaded but supervisor update failed.")
			return 1
		}
	}
	_ = os.Remove(ctx.NginxConf + ".bak")
	fmt.Printf("Site %s: root=%s php=%s (%s)\n", domain, siteRoot, phpMinor, backend)
	return 0
}

// RemoveSite ports remove-site.sh.
func RemoveSite(args []string) int {
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: remove-site <service-user> <container> <domain> [--delete-data]")
		return 2
	}
	svc, cont, domain := args[0], args[1], strings.ToLower(args[2])
	delData := false
	for _, a := range args[3:] {
		if a == "--delete-data" {
			delData = true
		} else {
			fmt.Fprintf(os.Stderr, "Unknown arg: %s\n", a)
			return 2
		}
	}
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	ctx, code := Load(svc, cont)
	if code != 0 {
		return code
	}
	if !platform.ValidDomain(domain) {
		fmt.Fprintf(os.Stderr, "Invalid domain: %s\n", domain)
		return 2
	}
	if err := platform.CopyFile(ctx.NginxConf, ctx.NginxConf+".bak"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	portConflict := false
	if _, err := os.Stat(ctx.SitesDir + "/" + domain + ".conf"); err == nil {
		portConflict = true
	}
	if _, err := os.Stat(ctx.SupervisorDir + "/" + domain + ".conf"); err == nil {
		portConflict = true
	}
	if portConflict {
		_ = os.Remove(ctx.SitesDir + "/" + domain + ".conf")
		_ = os.Remove(ctx.SupervisorDir + "/" + domain + ".conf")
	}
	_ = platform.MapDel(ctx.NginxConf, "site_root", domain)
	_ = platform.MapDel(ctx.NginxConf, "php_fpm_backend", domain)
	if err := ctx.NginxTestReload(); err != nil {
		fmt.Fprintln(os.Stderr, "Reload failed, restoring backup.")
		_ = os.Rename(ctx.NginxConf+".bak", ctx.NginxConf)
		return 1
	}
	if portConflict {
		if err := ctx.SupervisorRereadUpdate(); err != nil {
			fmt.Fprintln(os.Stderr, "WARNING: nginx reloaded but supervisor update failed.")
			return 1
		}
	}
	_ = os.Remove(ctx.NginxConf + ".bak")
	if delData {
		_ = os.RemoveAll(ctx.Home + "/data/" + domain)
		fmt.Printf("Deleted data for %s.\n", domain)
	}
	fmt.Printf("Removed site %s (maps + port-app configs deleted).\n", domain)
	return 0
}

// SetPHPVersion ports set-php-version.sh.
func SetPHPVersion(args []string) int {
	if len(args) != 4 {
		fmt.Fprintln(os.Stderr, "Usage: set-php-version <service-user> <container> <domain|default> <minor: 8.2-8.6>")
		return 2
	}
	svc, cont, domain, minor := args[0], args[1], strings.ToLower(args[2]), args[3]
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	ctx, code := Load(svc, cont)
	if code != 0 {
		return code
	}
	if domain != "default" {
		if !platform.ValidDomain(domain) {
			fmt.Fprintf(os.Stderr, "Invalid domain: %s\n", domain)
			return 2
		}
		if _, err := os.Stat(ctx.SitesDir + "/" + domain + ".conf"); err == nil {
			fmt.Fprintf(os.Stderr, "%s is a port app (proxy_pass); PHP version does not apply.\n", domain)
			fmt.Fprintln(os.Stderr, "Use remove-port-app to drop it or add-site to convert it to PHP.")
			return 1
		}
		if _, err := os.Stat(ctx.SupervisorDir + "/" + domain + ".conf"); err == nil {
			fmt.Fprintf(os.Stderr, "%s is a port app (proxy_pass); PHP version does not apply.\n", domain)
			fmt.Fprintln(os.Stderr, "Use remove-port-app to drop it or add-site to convert it to PHP.")
			return 1
		}
	}
	if !platform.ValidMinor(minor) {
		fmt.Fprintf(os.Stderr, "Unsupported PHP minor (8.2-8.6): %s\n", minor)
		return 2
	}
	platform.EOLWarnForMinor(minor)
	backend, _ := platform.BackendForMinor(minor)
	if err := platform.CopyFile(ctx.NginxConf, ctx.NginxConf+".bak"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := platform.MapSet(ctx.NginxConf, "php_fpm_backend", domain, backend); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.Rename(ctx.NginxConf+".bak", ctx.NginxConf)
		return 1
	}
	if err := ctx.NginxTestReload(); err != nil {
		fmt.Fprintln(os.Stderr, "Reload failed, restoring backup.")
		_ = os.Rename(ctx.NginxConf+".bak", ctx.NginxConf)
		return 1
	}
	_ = os.Remove(ctx.NginxConf + ".bak")
	fmt.Printf("%s: php=%s (%s)\n", domain, minor, backend)
	return 0
}

// ListSites ports list-sites.sh.
func ListSites(args []string) int {
	jsonOut := false
	var pos []string
	for _, a := range args {
		if a == "--json" {
			jsonOut = true
		} else {
			pos = append(pos, a)
		}
	}
	if len(pos) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: list-sites <service-user> <container> [--json]")
		return 2
	}
	ctx, code := Load(pos[0], pos[1])
	if code != 0 {
		return code
	}
	raw, err := os.ReadFile(ctx.NginxConf)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	src := string(raw)
	if jsonOut {
		roots := platform.ParseMap(src, "site_root")
		backends := platform.ParseMap(src, "php_fpm_backend")
		domSet := map[string]bool{}
		for d := range roots {
			if d != "default" {
				domSet[d] = true
			}
		}
		for d := range backends {
			if d != "default" {
				domSet[d] = true
			}
		}
		var domains []string
		for d := range domSet {
			domains = append(domains, d)
		}
		sort.Strings(domains)
		type phpSite struct {
			Domain  string  `json:"domain"`
			Root    *string `json:"root"`
			Backend *string `json:"backend"`
		}
		var phpSites []phpSite
		for _, d := range domains {
			var r, b *string
			if v, ok := roots[d]; ok {
				vv := v
				r = &vv
			}
			if v, ok := backends[d]; ok {
				vv := v
				b = &vv
			}
			phpSites = append(phpSites, phpSite{Domain: d, Root: r, Backend: b})
		}
		if phpSites == nil {
			phpSites = []phpSite{}
		}
		type portApp struct {
			Domain    string  `json:"domain"`
			Port      *int    `json:"port"`
			Target    *string `json:"target"`
			Program   *string `json:"program"`
			Command   *string `json:"command"`
			Directory *string `json:"directory"`
		}
		var apps []portApp
		confs, _ := filepath.Glob(filepath.Join(ctx.SitesDir, "*.conf"))
		sort.Strings(confs)
		proxyRe := regexp.MustCompile(`proxy_pass\s+(http://127\.0\.0\.1:(\d+))`)
		progRe := regexp.MustCompile(`(?m)^\[program:(.+?)\]`)
		cmdRe := regexp.MustCompile(`(?m)^command\s*=\s*(.+)$`)
		dirRe := regexp.MustCompile(`(?m)^directory\s*=\s*(.+)$`)
		for _, conf := range confs {
			domain := strings.TrimSuffix(filepath.Base(conf), ".conf")
			csrc, _ := os.ReadFile(conf)
			var target *string
			var port *int
			if m := proxyRe.FindStringSubmatch(string(csrc)); m != nil {
				t, p := m[1], 0
				fmt.Sscanf(m[2], "%d", &p)
				target, port = &t, &p
			}
			var prog, cmd, dir *string
			if ssrc, err := os.ReadFile(filepath.Join(ctx.SupervisorDir, domain+".conf")); err == nil {
				if m := progRe.FindStringSubmatch(string(ssrc)); m != nil {
					v := strings.TrimSpace(m[1])
					prog = &v
				}
				if m := cmdRe.FindStringSubmatch(string(ssrc)); m != nil {
					v := strings.TrimSpace(m[1])
					cmd = &v
				}
				if m := dirRe.FindStringSubmatch(string(ssrc)); m != nil {
					v := strings.TrimSpace(m[1])
					dir = &v
				}
			}
			apps = append(apps, portApp{Domain: domain, Port: port, Target: target, Program: prog, Command: cmd, Directory: dir})
		}
		if apps == nil {
			apps = []portApp{}
		}
		def := map[string]*string{}
		for k, kk := range map[string]string{"site_root": "site_root", "php_fpm_backend": "php_fpm_backend"} {
			_ = k
			_ = kk
		}
		var sr, be *string
		if v, ok := roots["default"]; ok {
			vv := v
			sr = &vv
		}
		if v, ok := backends["default"]; ok {
			vv := v
			be = &vv
		}
		def["site_root"] = sr
		def["php_fpm_backend"] = be
		out := map[string]any{"defaults": def, "php_sites": phpSites, "port_apps": apps}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return 0
	}
	for _, v := range []string{"site_root", "php_fpm_backend"} {
		fmt.Printf("$%s:\n", v)
		m := platform.ParseMap(src, v)
		if len(m) == 0 {
			fmt.Println("  (map missing)")
			continue
		}
		var keys []string
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  %s  %s;\n", k, m[k])
		}
	}
	fmt.Println("port_apps:")
	confs, _ := filepath.Glob(filepath.Join(ctx.SitesDir, "*.conf"))
	sort.Strings(confs)
	if len(confs) == 0 {
		fmt.Println("  (none)")
		return 0
	}
	proxyRe := regexp.MustCompile(`proxy_pass\s+(http://127\.0\.0\.1:\d+)`)
	progRe := regexp.MustCompile(`(?m)^\[program:(.+?)\]`)
	for _, conf := range confs {
		domain := strings.TrimSuffix(filepath.Base(conf), ".conf")
		csrc, _ := os.ReadFile(conf)
		port := "(proxy target unknown)"
		if m := proxyRe.FindStringSubmatch(string(csrc)); m != nil {
			port = m[1]
		}
		prog := "(no supervisor conf)"
		if ssrc, err := os.ReadFile(filepath.Join(ctx.SupervisorDir, domain+".conf")); err == nil {
			if m := progRe.FindStringSubmatch(string(ssrc)); m != nil {
				prog = m[1]
			}
		}
		fmt.Printf("  %s -> %s [%s]\n", domain, port, prog)
	}
	return 0
}
