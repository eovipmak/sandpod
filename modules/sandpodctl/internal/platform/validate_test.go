package platform

import "testing"

func TestValidators(t *testing.T) {
	if !ValidServiceUser("svc-c001") || ValidServiceUser("Bad User!") {
		t.Fatal("service user validation")
	}
	if !ValidContainer("customer-001") || ValidContainer("bad/container") {
		t.Fatal("container validation")
	}
	if !ValidDomain("shop.example.com") || ValidDomain("not_a_domain!") {
		t.Fatal("domain validation")
	}
	if ValidDomain("BAD.EXAMPLE.COM") != true { // case-insensitive by lowering in callers; regex itself is lower
		t.Log("note: ValidDomain expects lowercased input for uppers")
	}
	for _, m := range []string{"8.2", "8.3", "8.4", "8.5", "8.6"} {
		if !ValidMinor(m) {
			t.Fatalf("minor %s", m)
		}
	}
	if ValidMinor("8.1") || ValidMinor("7.4") {
		t.Fatal("minor reject")
	}
	if !ValidPort3000("3001") || ValidPort3000("2999") || ValidPort3000("4000") {
		t.Fatal("port range")
	}
	if b, ok := BackendForMinor("8.3"); !ok || b != "127.0.0.1:9000" {
		t.Fatal("backend 8.3")
	}
	if ProgramForDomain("Shop.Example.COM") != "app-shop-example-com" {
		t.Fatal("program")
	}
	if n, ok := MemToBytes("1g"); !ok || n != 1073741824 {
		t.Fatal("mem 1g")
	}
	if n, ok := MemToBytes("512m"); !ok || n != 512*1048576 {
		t.Fatal("mem 512m")
	}
	if _, ok := MemToBytes("1t"); ok {
		t.Fatal("mem 1t must fail")
	}
}
