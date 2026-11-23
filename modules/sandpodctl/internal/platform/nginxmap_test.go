package platform

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleConf = `http {
    map $host $site_root {
        default  /srv/customer/public_html;
    }
    map $host $php_fpm_backend {
        default  127.0.0.1:9000;
    }
    include /etc/sandpod-config/sites/*.conf;
}
`

func TestMapSetDel(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "nginx.conf")
	if err := os.WriteFile(p, []byte(sampleConf), 0644); err != nil {
		t.Fatal(err)
	}
	if err := MapSet(p, "site_root", "shop.example.com", "/srv/customer/shop.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := MapSet(p, "php_fpm_backend", "shop.example.com", "127.0.0.1:9084"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	m := ParseMap(string(raw), "php_fpm_backend")
	if m["shop.example.com"] != "127.0.0.1:9084" || m["default"] != "127.0.0.1:9000" {
		t.Fatalf("mapset wrong: %v", m)
	}
	if err := MapDel(p, "site_root", "shop.example.com"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(p)
	if _, ok := ParseMap(string(raw), "site_root")["shop.example.com"]; ok {
		t.Fatal("mapdel did not delete")
	}
	if err := MapDel(p, "site_root", "default"); err == nil {
		t.Fatal("mapdel must refuse default")
	}
}

func TestEnsureSitesInclude(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "nginx.conf")
	base := "http {\n    map $host $x {\n        default 1;\n    }\n}\n"
	if err := os.WriteFile(p, []byte(base), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSitesInclude(p); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if !contains(string(raw), "include /etc/sandpod-config/sites/*.conf;") {
		t.Fatal("include not added")
	}
	// idempotent
	if err := EnsureSitesInclude(p); err != nil {
		t.Fatal(err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
