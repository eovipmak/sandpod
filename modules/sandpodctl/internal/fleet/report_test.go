package fleet

import (
	"os"
	"path/filepath"
	"testing"
)

func writeReport(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "xfs-report")
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

// xfs_quota without a usable /etc/projid prints `#<id>` rows (and can also
// print a bare <id> row). The second column is used, not a project name.
func TestLoadXFSReportNumericIDs(t *testing.T) {
	p := writeReport(t, `Project quota on / (/dev/vda2)
                        Blocks
Project ID   Used   Soft   Hard Warn/Grace
---------- ---------------------------------
20001        9.8G      0      0  00 [------]
#20001       2.6G     8G    10G  00 [------]
#0           9.8G      0      0  00 [------]
`)
	blocks := map[string]xfsEntry{}
	byID := map[string]string{}
	loadXFSReportFile(p, blocks, nil, byID, nil)
	if got, want := diskFor("svc-c001", "20001", blocks, byID), "2.6G used / 8G soft / 10G hard"; got != want {
		t.Fatalf("disk = %q, want %q", got, want)
	}
	if got := diskFor("svc-c999", "999", blocks, byID); got != "quota-off" {
		t.Fatalf("unknown user disk = %q, want quota-off", got)
	}
}

// With a usable /etc/projid the first column is the project name.
func TestLoadXFSReportNamedProjects(t *testing.T) {
	p := writeReport(t, `Project quota on / (/dev/vda2)
                        Blocks
Project ID   Used   Soft   Hard Warn/Grace
---------- ---------------------------------
#0           9.8G      0      0  00 [------]
svc-c001     2.6G     8G    10G  00 [------]
`)
	blocks := map[string]xfsEntry{}
	byID := map[string]string{}
	loadXFSReportFile(p, blocks, nil, byID, nil)
	if got, want := diskFor("svc-c001", "20001", blocks, byID), "2.6G used / 8G soft / 10G hard"; got != want {
		t.Fatalf("disk = %q, want %q", got, want)
	}
}

func TestLoadXFSInodeReport(t *testing.T) {
	p := writeReport(t, `Project quota on / (/dev/vda2)
                        Inodes
Project ID   Used   Soft   Hard Warn/Grace
---------- ---------------------------------
#20001      54.6k      0      0  00 [------]
`)
	inodes := map[string]xfsEntry{}
	byID := map[string]string{}
	loadXFSReportFile(p, nil, inodes, nil, byID)
	if got, want := inodesFor("svc-c001", "20001", inodes, byID), "54.6k used / 0 soft / 0 hard"; got != want {
		t.Fatalf("inodes = %q, want %q", got, want)
	}
}
