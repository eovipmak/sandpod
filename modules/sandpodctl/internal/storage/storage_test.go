package storage

import (
	"reflect"
	"testing"
)

func TestParseProjidLine(t *testing.T) {
	cases := []struct {
		line, name, id string
		ok             bool
	}{
		{"svc-c001:20001", "svc-c001", "20001", true},
		{"20001:svc-c001", "svc-c001", "20001", true},
		{" svc-c002:20002 ", "svc-c002", "20002", true},
		{"", "", "", false},
		{"# comment", "", "", false},
		{"no-colon", "", "", false},
		{"svc-c001:", "", "", false},
		{":20001", "", "", false},
		{"name:notanid", "", "", false},
		{"bad name:20001", "", "", false},
	}
	for _, c := range cases {
		name, id, ok := parseProjidLine(c.line)
		if ok != c.ok || name != c.name || id != c.id {
			t.Errorf("parseProjidLine(%q) = (%q,%q,%v), want (%q,%q,%v)", c.line, name, id, ok, c.name, c.id, c.ok)
		}
	}
}

func TestMergeCanonicalProjidMigratesLegacy(t *testing.T) {
	in := []string{
		"# SandPod projects",
		"20001:svc-c001",
		"20002:svc-c002",
	}
	got := mergeCanonical(in, dropProjidEntry("svc-c001", "20001"), "svc-c001:20001")
	want := []string{
		"# SandPod projects",
		"20002:svc-c002",
		"svc-c001:20001",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged = %#v, want %#v", got, want)
	}
}

func TestMergeCanonicalProjidIsIdempotent(t *testing.T) {
	in := []string{"# header", "svc-c001:20001", "svc-c002:20002"}
	got := mergeCanonical(in, dropProjidEntry("svc-c001", "20001"), "svc-c001:20001")
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("merged = %#v, want %#v", got, in)
	}
}

func TestMergeCanonicalProjectsMigratesLegacy(t *testing.T) {
	in := []string{
		"svc-c001:/home/svc-c001",
		"svc-c002:/home/svc-c002",
	}
	got := mergeCanonical(in, dropProjectsEntry("svc-c001", "20001", "/home/svc-c001"), "20001:/home/svc-c001")
	want := []string{
		"20001:/home/svc-c001",
		"svc-c002:/home/svc-c002",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged = %#v, want %#v", got, want)
	}
}

func TestMergeCanonicalDropsStalePathAndDuplicates(t *testing.T) {
	in := []string{
		"20001:/home/svc-c001-old",
		"20001:/home/svc-c001",
		"20002:/home/svc-c002",
	}
	got := mergeCanonical(in, dropProjectsEntry("svc-c001", "20001", "/home/svc-c001"), "20001:/home/svc-c001")
	want := []string{
		"20001:/home/svc-c001",
		"20002:/home/svc-c002",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged = %#v, want %#v", got, want)
	}
}
