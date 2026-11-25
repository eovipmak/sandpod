package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An instance may own several domains (first is canonical, rest are aliases).
// They are joined with spaces for `server_name`; that must not trip the
// metacharacter guard and drop the whole instance.
func TestGenerateSiteConfMultiDomain(t *testing.T) {
	dir := t.TempDir()
	customers := filepath.Join(dir, "customers.tsv")
	sites := filepath.Join(dir, "sites.tsv")
	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(customers,
		[]byte("001\tsvc-c001\tcustomer-001\t18001\t22201\timg:pilot\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sites,
		[]byte("001\tshop.example.com\n001\twww.shop.example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CUSTOMERS", customers)
	t.Setenv("REGISTRY", sites)
	t.Setenv("OUT_DIR", out)

	if code := GenerateSiteConf(nil); code != 0 {
		t.Fatalf("GenerateSiteConf = %d, want 0", code)
	}
	raw, err := os.ReadFile(filepath.Join(out, "sandpod-001.conf"))
	if err != nil {
		t.Fatalf("config not generated: %v", err)
	}
	want := "server_name shop.example.com www.shop.example.com;"
	if !strings.Contains(string(raw), want) {
		t.Fatalf("generated config missing %q:\n%s", want, raw)
	}
}
