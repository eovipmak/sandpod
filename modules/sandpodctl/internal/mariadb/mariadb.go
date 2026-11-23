// Package mariadb ports modules/host-mariadb/scripts.
package mariadb

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"sandpodctl/internal/platform"
)

// CreateSiteDB ports create-site-db.sh.
func CreateSiteDB(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: create-site-db <db-name> <db-user> [--host host.containers.internal]")
		return 2
	}
	db, user := args[0], args[1]
	host := "host.containers.internal"
	rest := args[2:]
	for i := 0; i < len(rest); i++ {
		if rest[i] == "--host" {
			if i+1 >= len(rest) {
				fmt.Fprintln(os.Stderr, "Missing --host value.")
				return 2
			}
			host = rest[i+1]
			i++
		} else {
			fmt.Fprintf(os.Stderr, "Unknown arg: %s\n", rest[i])
			return 2
		}
	}
	if !platform.ValidDBName(db) {
		fmt.Fprintln(os.Stderr, "DB name must be [A-Za-z0-9_].")
		return 2
	}
	if !platform.ValidDBName(user) {
		fmt.Fprintln(os.Stderr, "DB user must be [A-Za-z0-9_].")
		return 2
	}
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	var buf [18]byte
	if _, err := rand.Read(buf[:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	pass := base64.StdEncoding.EncodeToString(buf[:])
	sql := fmt.Sprintf(`CREATE DATABASE IF NOT EXISTS `+"`%s`"+` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS '%s'@'%%' IDENTIFIED BY '%s';
ALTER USER '%s'@'%%' IDENTIFIED BY '%s';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, INDEX, ALTER,
      CREATE TEMPORARY TABLES, LOCK TABLES, EXECUTE, REFERENCES
  ON `+"`%s`"+`.* TO '%s'@'%%';
FLUSH PRIVILEGES;
`, db, user, pass, user, pass, db, user)
	c := exec.Command("mariadb", "--protocol=socket")
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	c.Stdin = strings.NewReader(sql)
	if err := c.Run(); err != nil {
		return 1
	}
	fmt.Println("# Store these in the control panel vault - they are not saved on the host.")
	fmt.Printf("DB_HOST=%s\nDB_NAME=%s\nDB_USER=%s\nDB_PASS=%s\n", host, db, user, pass)
	fmt.Println("# Container test (after granting host firewall/bind, see README):")
	fmt.Println("#   mysql -h $DB_HOST -u $DB_USER -p $DB_NAME -e 'SELECT 1;'")
	return 0
}

// BackupSiteDB ports backup-site-db.sh. Returns output path on stdout.
func BackupSiteDB(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: backup-site-db <db-name> [--dir /var/backups/sandpod]")
		return 2
	}
	db := args[0]
	dir := "/var/backups/sandpod"
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		if rest[i] == "--dir" {
			if i+1 >= len(rest) {
				fmt.Fprintln(os.Stderr, "Missing --dir value.")
				return 2
			}
			dir = rest[i+1]
			i++
		} else {
			fmt.Fprintf(os.Stderr, "Unknown: %s\n", rest[i])
			return 2
		}
	}
	if !platform.ValidDBName(db) {
		fmt.Fprintln(os.Stderr, "Bad DB name.")
		return 2
	}
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	if !strings.HasPrefix(dir, "/") {
		fmt.Fprintln(os.Stderr, "--dir must be absolute.")
		return 2
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = os.Chmod(dir, 0700)
	ts := time.Now().Format("20060102-150405")
	out := filepath.Join(dir, fmt.Sprintf("%s-%s.sql.gz", db, ts))
	tmp := out + ".tmp"
	dump := exec.Command("mariadb-dump", "--protocol=socket", "--single-transaction", "--routines", "--triggers", db)
	gz := exec.Command("gzip", "-c")
	dumpOut, err := dump.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	gz.Stdin = dumpOut
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	gz.Stdout = f
	gz.Stderr = os.Stderr
	if err := gz.Start(); err != nil {
		f.Close()
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := dump.Run(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		fmt.Fprintf(os.Stderr, "Dump of %s failed.\n", db)
		return 1
	}
	_ = dumpOut.Close()
	if err := gz.Wait(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		fmt.Fprintf(os.Stderr, "Dump of %s failed.\n", db)
		return 1
	}
	f.Close()
	st, err := os.Stat(tmp)
	if err != nil || st.Size() == 0 {
		_ = os.Remove(tmp)
		fmt.Fprintf(os.Stderr, "Dump of %s is empty.\n", db)
		return 1
	}
	if err := exec.Command("gzip", "-t", tmp).Run(); err != nil {
		_ = os.Remove(tmp)
		fmt.Fprintf(os.Stderr, "Dump of %s failed integrity check.\n", db)
		return 1
	}
	if err := os.Rename(tmp, out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = os.Chmod(out, 0600)
	fmt.Println(out)
	return 0
}

// RestoreSiteDB ports restore-site-db.sh.
func RestoreSiteDB(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: restore-site-db <db-name> <dump.sql.gz>")
		return 2
	}
	db, dump := args[0], args[1]
	if !platform.ValidDBName(db) {
		fmt.Fprintln(os.Stderr, "Bad DB name.")
		return 2
	}
	if _, err := os.Stat(dump); err != nil {
		fmt.Fprintln(os.Stderr, "Dump not found.")
		return 2
	}
	if code := platform.MustRoot(); code != -1 {
		return code
	}
	if err := exec.Command("gzip", "-t", dump).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Dump is not valid gzip: %s\n", dump)
		return 2
	}
	if st, err := os.Stat(dump); err != nil || st.Size() == 0 {
		fmt.Fprintf(os.Stderr, "Dump is empty: %s\n", dump)
		return 2
	}
	// Safety net: pre-restore backup (best-effort).
	_ = BackupSiteDB([]string{db})
	mk := exec.Command("mariadb", "--protocol=socket", "-e",
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", db))
	mk.Stdout, mk.Stderr = os.Stdout, os.Stderr
	_ = mk.Run()
	zcat := exec.Command("gzip", "-dc", dump)
	imp := exec.Command("mariadb", "--protocol=socket", db)
	pipe, err := zcat.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	imp.Stdin = pipe
	imp.Stdout, imp.Stderr = os.Stdout, os.Stderr
	if err := imp.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := zcat.Run(); err != nil {
		_ = imp.Wait()
		return 1
	}
	_ = pipe.Close()
	if err := imp.Wait(); err != nil {
		return 1
	}
	fmt.Printf("Restored %s from %s\n", db, dump)
	return 0
}
