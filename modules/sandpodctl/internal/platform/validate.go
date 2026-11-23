package platform

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	reServiceUser = regexp.MustCompile(`^[a-z_][a-z0-9_-]*[$]?$`)
	reContainer   = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	reDomain      = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
	reMinor       = regexp.MustCompile(`^8\.[2-6]$`)
	reInstance3   = regexp.MustCompile(`^[0-9]{3}$`)
	reInstanceAny = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	reDBName      = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	reImage       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./:@-]*$`)
	reVersionXYZ  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	reHostPort    = regexp.MustCompile(`^[0-9]+$`)
	reMem         = regexp.MustCompile(`^[0-9]+[kKmMgG]?$`)
	reCPUs        = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)
	reSiteDomain  = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9_.-]*[A-Za-z0-9])?$`)
)

// Validators mirror the bash [[ =~ ]] guards; each prints nothing and lets
// the caller emit the exact script error text + exit code 2.
func ValidServiceUser(s string) bool { return reServiceUser.MatchString(s) }
func ValidContainer(s string) bool   { return reContainer.MatchString(s) }
func ValidDomain(s string) bool      { return reDomain.MatchString(strings.ToLower(s)) }
func ValidMinor(s string) bool       { return reMinor.MatchString(s) }
func ValidInstance3(s string) bool   { return reInstance3.MatchString(s) }
func ValidInstanceAny(s string) bool { return reInstanceAny.MatchString(s) }
func ValidDBName(s string) bool      { return reDBName.MatchString(s) }
func ValidImage(s string) bool       { return reImage.MatchString(s) }
func ValidVersionXYZ(s string) bool  { return reVersionXYZ.MatchString(s) }
func ValidSiteDomain(s string) bool  { return reSiteDomain.MatchString(s) }
func ValidPort3000(s string) bool {
	if !reHostPort.MatchString(s) {
		return false
	}
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n >= 3000 && n <= 3999
}

// Fail2 prints msg to stderr; callers return 2 afterwards.
func Fail2(msg string) {
	fmt.Fprintln(os.Stderr, msg)
}

// BackendForMinor mirrors lib.sh backend_for_minor.
func BackendForMinor(minor string) (string, bool) {
	switch minor {
	case "8.2":
		return "127.0.0.1:9082", true
	case "8.3":
		return "127.0.0.1:9000", true
	case "8.4":
		return "127.0.0.1:9084", true
	case "8.5":
		return "127.0.0.1:9085", true
	case "8.6":
		return "127.0.0.1:9086", true
	}
	return "", false
}

// EOLWarnForMinor mirrors lib.sh: warn on stderr, never fail.
func EOLWarnForMinor(minor string) {
	if minor == "8.2" {
		fmt.Fprintln(os.Stderr, "WARNING: PHP 8.2 security support ends Dec 2026; prefer 8.3+ for new sites.")
	}
}

// ProgramForDomain mirrors lib.sh program_for_domain.
func ProgramForDomain(domain string) string {
	d := strings.ToLower(domain)
	d = strings.ReplaceAll(d, ".", "-")
	return "app-" + d
}

// MemToBytes mirrors provision-customer.sh mem_to_bytes (plain number = bytes).
func MemToBytes(s string) (int64, bool) {
	if !reMem.MatchString(s) {
		return 0, false
	}
	num := s
	mult := int64(1)
	if c := s[len(s)-1]; c == 'k' || c == 'K' || c == 'm' || c == 'M' || c == 'g' || c == 'G' {
		num = s[:len(s)-1]
		switch c {
		case 'k', 'K':
			mult = 1024
		case 'm', 'M':
			mult = 1048576
		case 'g', 'G':
			mult = 1073741824
		}
	}
	var n int64
	if _, err := fmt.Sscanf(num, "%d", &n); err != nil {
		return 0, false
	}
	for _, r := range num {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	return n * mult, true
}
