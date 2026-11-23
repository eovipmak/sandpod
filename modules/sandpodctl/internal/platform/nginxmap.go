package platform

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Nginx map helpers: Go ports of the python3 snippets in customer-apps/lib.sh.
// They edit ~/config/nginx.conf in place.

// ensure map block lookup
var mapReTmpl = `(?s)(map\s+\$host\s+\$%s\s*\{(.*?)\})`

func findMap(src, v string) (fullStart, fullEnd, innerStart, innerEnd int, inner string, ok bool) {
	re := regexp.MustCompile(fmt.Sprintf(mapReTmpl, regexp.QuoteMeta(v)))
	loc := re.FindStringSubmatchIndex(src)
	if loc == nil {
		return 0, 0, 0, 0, "", false
	}
	return loc[0], loc[1], loc[4], loc[5], src[loc[4]:loc[5]], true
}

// MapSet mirrors lib.sh map_set (idempotent replace-or-insert).
func MapSet(confPath, v, key, value string) error {
	data, err := os.ReadFile(confPath)
	if err != nil {
		return err
	}
	src := string(data)
	fs, fe, is, ie, inner, ok := findMap(src, v)
	if !ok {
		return fmt.Errorf("map $%s not found in %s", v, confPath)
	}
	_ = fs
	_ = fe
	// replace existing `key value;` line
	lineRe := regexp.MustCompile(`(?m)^([ \t]+)` + regexp.QuoteMeta(key) + `[ \t]+[^;\n]+;\s*\n`)
	if lineRe.MatchString(inner) {
		inner = lineRe.ReplaceAllString(inner, `$1`+key+`  `+value+";\n")
	} else if key == "default" {
		defRe := regexp.MustCompile(`(?m)^([ \t]+)default[ \t]+[^;\n]+;\s*\n`)
		inner = defRe.ReplaceAllString(inner, `$1default  `+value+";\n")
	} else {
		anchor := regexp.MustCompile(`(?m)^([ \t]+)default[ \t]+[^;\n]+;\s*\n`).FindStringSubmatch(inner)
		if anchor != nil {
			indent := anchor[1]
			inner = strings.Replace(inner, anchor[0], anchor[0]+indent+key+`  `+value+";\n", 1)
		} else {
			inner = inner + "        " + key + "  " + value + ";\n"
		}
	}
	out := src[:is] + inner + src[ie:]
	return os.WriteFile(confPath, []byte(out), 0644)
}

// MapDel mirrors lib.sh map_del (no-op when absent, refuses default).
func MapDel(confPath, v, key string) error {
	if key == "default" {
		return fmt.Errorf("refusing to delete the default entry")
	}
	data, err := os.ReadFile(confPath)
	if err != nil {
		return err
	}
	src := string(data)
	_, _, is, ie, inner, ok := findMap(src, v)
	if !ok {
		return fmt.Errorf("map $%s not found in %s", v, confPath)
	}
	lineRe := regexp.MustCompile(`(?m)^[ \t]+` + regexp.QuoteMeta(key) + `[ \t]+[^;\n]+;\s*\n`)
	inner = lineRe.ReplaceAllString(inner, "")
	out := src[:is] + inner + src[ie:]
	return os.WriteFile(confPath, []byte(out), 0644)
}

// EnsureSitesInclude mirrors lib.sh ensure_nginx_sites_include.
func EnsureSitesInclude(confPath string) error {
	data, err := os.ReadFile(confPath)
	if err != nil {
		return err
	}
	src := string(data)
	incRe := regexp.MustCompile(`include\s+/etc/sandpod-config/sites/\*\.conf\s*;`)
	if incRe.MatchString(src) {
		return nil
	}
	stripped := strings.TrimRight(src, " \t\r\n")
	idx := strings.LastIndex(stripped, "}")
	if idx == -1 {
		return fmt.Errorf("no http closing brace found")
	}
	addition := "    include /etc/sandpod-config/sites/*.conf;\n"
	out := stripped[:idx] + addition + stripped[idx:] + "\n"
	if err := os.WriteFile(confPath, []byte(out), 0644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Added sites include to %s\n", confPath)
	return nil
}

// ParseMap extracts a `map $host $var` block into key->value.
func ParseMap(src, v string) map[string]string {
	out := map[string]string{}
	re := regexp.MustCompile(`(?s)map\s+\$host\s+\$` + regexp.QuoteMeta(v) + `\s*\{(.*?)\}`)
	m := re.FindStringSubmatch(src)
	if m == nil {
		return out
	}
	for _, line := range strings.Split(m[1], "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		t = strings.TrimSuffix(t, ";")
		parts := strings.Fields(t)
		if len(parts) >= 2 {
			out[parts[0]] = strings.Join(parts[1:], " ")
		}
	}
	return out
}
