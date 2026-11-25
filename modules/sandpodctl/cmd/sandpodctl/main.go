// Command sandpodctl is the single Go binary for all SandPod operator commands.
//
// Each subcommand keeps the argv contract of the bash scripts it replaced:
//
//	sandpodctl provision-customer 002 ...
//	sandpodctl add-site svc-c001 customer-001 shop.example.com --php 8.3
//
// Busybox mode: when invoked via a symlink named <cmd>[.sh], it dispatches
// to that subcommand.
package main

import (
	"fmt"
	"os"

	"sandpodctl/internal/apps"
	"sandpodctl/internal/fleet"
	"sandpodctl/internal/mariadb"
	"sandpodctl/internal/nginx"
	"sandpodctl/internal/platform"
	"sandpodctl/internal/provisioning"
	"sandpodctl/internal/runtimes"
	"sandpodctl/internal/storage"
)

type cmd struct {
	name  string
	usage string
	run   func([]string) int
}

var commands = []cmd{
	// customer-provisioning
	{"provision-customer", "provision-customer <instance> [flags...]", provisioning.Provision},
	{"remove-customer", "remove-customer <instance> [flags...]", provisioning.RemoveCustomer},
	{"add-ssh-key", "add-ssh-key <service-user> <container> <pubkey-file>", provisioning.AddSSHKey},
	{"health-check", "health-check <web-port> [base-host]", provisioning.HealthCheck},
	// customer-apps
	{"add-site", "add-site <service-user> <container> <domain> [--php 8.3] [--primary]", apps.AddSite},
	{"remove-site", "remove-site <service-user> <container> <domain> [--delete-data]", apps.RemoveSite},
	{"list-sites", "list-sites <service-user> <container> [--json]", apps.ListSites},
	{"set-php-version", "set-php-version <service-user> <container> <domain|default> <minor>", apps.SetPHPVersion},
	{"add-port-app", "add-port-app <service-user> <container> <domain> --port P --command C [--primary]", apps.AddPortApp},
	{"remove-port-app", "remove-port-app <service-user> <container> <domain> [--delete-data]", apps.RemovePortApp},
	// shared-runtimes
	{"install-php-runtimes", "install-php-runtimes", runtimes.InstallPHPRuntimes},
	{"update-php-runtimes", "update-php-runtimes", runtimes.UpdatePHPRuntimes},
	{"reload-customer-php", "reload-customer-php <service-user> <container> <minor>", runtimes.ReloadCustomerPHP},
	{"install-node-runtimes", "install-node-runtimes [flags...]", runtimes.InstallNodeRuntimes},
	{"update-node-runtimes", "update-node-runtimes [flags...]", runtimes.UpdateNodeRuntimes},
	{"install-python-runtimes", "install-python-runtimes [flags...]", runtimes.InstallPythonRuntimes},
	{"update-python-runtimes", "update-python-runtimes [flags...]", runtimes.UpdatePythonRuntimes},
	{"reload-customer-app", "reload-customer-app <service-user> <container> <domain>", runtimes.ReloadCustomerApp},
	// host-nginx
	{"generate-site-conf", "generate-site-conf [--prune]", nginx.GenerateSiteConf},
	{"reload-host-nginx", "reload-host-nginx", nginx.ReloadHostNginx},
	// host-mariadb
	{"create-site-db", "create-site-db <db> <user> [--host ...]", mariadb.CreateSiteDB},
	{"backup-site-db", "backup-site-db <db> [--dir ...]", mariadb.BackupSiteDB},
	{"restore-site-db", "restore-site-db <db> <dump.sql.gz>", mariadb.RestoreSiteDB},
	// host-storage
	{"enable-project-quota", "enable-project-quota", storage.EnableProjectQuota},
	{"quota-report", "quota-report [--registry ...]", storage.QuotaReport},
	{"reapply-quotas", "reapply-quotas", storage.ReapplyQuotas},
	{"set-customer-quota", "set-customer-quota <service-user> [--hard-gb N ...]", storage.SetCustomerQuota},
	// monitoring-backup
	{"backup-customer", "backup-customer <instance> [flags...]", fleet.BackupCustomer},
	{"resource-report", "resource-report [--registry ...] [--json]", fleet.ResourceReport},
	{"rollout-php-patch", "rollout-php-patch [flags...]", fleet.RolloutPHPPatch},
	{"rollout-app-patch", "rollout-app-patch [flags...]", fleet.RolloutAppPatch},
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: sandpodctl <command> [args...]")
	fmt.Fprintln(os.Stderr, "Commands:")
	for _, c := range commands {
		fmt.Fprintf(os.Stderr, "  %-24s %s\n", c.name, c.usage)
	}
}

func main() {
	// Busybox/symlink mode: sandpodctl invoked as <cmd>[.sh].
	if exe := platform.ExeName(os.Args[0]); exe != "sandpodctl" && exe != "main" {
		for _, c := range commands {
			if exe == c.name {
				os.Exit(c.run(os.Args[1:]))
			}
		}
	}
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	name := os.Args[1]
	if name == "-h" || name == "--help" || name == "help" {
		usage()
		os.Exit(0)
	}
	for _, c := range commands {
		if name == c.name {
			os.Exit(c.run(os.Args[2:]))
		}
	}
	fmt.Fprintf(os.Stderr, "Unknown command: %s\n", name)
	usage()
	os.Exit(2)
}
