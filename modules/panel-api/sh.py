"""One place that knows how to call the operator backend. No policy here.

Default backend is the Go binary `sandpodctl` (one subcommand per legacy
script, same argv). Rollback: SANDPOD_BACKEND=scripts uses the legacy
modules/*/scripts/*.sh paths with identical arguments.
"""
import os
import subprocess

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))

# Logical op -> sandpodctl subcommand (1:1 with legacy scripts, same argv).
SUBCOMMANDS = {
    "provision": "provision-customer",
    "remove": "remove-customer",
    "add_key": "add-ssh-key",
    "health": "health-check",
    "add_site": "add-site",
    "set_php": "set-php-version",
    "remove_site": "remove-site",
    "list_sites": "list-sites",
    "add_port": "add-port-app",
    "remove_port": "remove-port-app",
    "reload_php": "reload-customer-php",
    "reload_app": "reload-customer-app",
    "mkdb": "create-site-db",
    "backup_db": "backup-site-db",
    "restore_db": "restore-site-db",
    "gen_nginx": "generate-site-conf",
    "reload_nginx": "reload-host-nginx",
    "backup": "backup-customer",
    "report": "resource-report",
    "rollout_php": "rollout-php-patch",
    "rollout_app": "rollout-app-patch",
    "quota_set": "set-customer-quota",
    "quota_report": "quota-report",
    "quota_reapply": "reapply-quotas",
    "quota_enable": "enable-project-quota",
}

# Legacy script paths (rollback only, SANDPOD_BACKEND=scripts).
SCRIPTS = {
    "provision": "customer-provisioning/scripts/provision-customer.sh",
    "remove": "customer-provisioning/scripts/remove-customer.sh",
    "add_key": "customer-provisioning/scripts/add-ssh-key.sh",
    "health": "customer-provisioning/scripts/health-check.sh",
    "add_site": "customer-apps/scripts/add-site.sh",
    "set_php": "customer-apps/scripts/set-php-version.sh",
    "remove_site": "customer-apps/scripts/remove-site.sh",
    "list_sites": "customer-apps/scripts/list-sites.sh",
    "add_port": "customer-apps/scripts/add-port-app.sh",
    "remove_port": "customer-apps/scripts/remove-port-app.sh",
    "reload_php": "shared-runtimes/scripts/reload-customer-php.sh",
    "reload_app": "shared-runtimes/scripts/reload-customer-app.sh",
    "mkdb": "host-mariadb/scripts/create-site-db.sh",
    "backup_db": "host-mariadb/scripts/backup-site-db.sh",
    "restore_db": "host-mariadb/scripts/restore-site-db.sh",
    "gen_nginx": "host-nginx/scripts/generate-site-conf.sh",
    "reload_nginx": "host-nginx/scripts/reload-host-nginx.sh",
    "backup": "monitoring-backup/scripts/backup-customer.sh",
    "report": "monitoring-backup/scripts/resource-report.sh",
    "rollout_php": "monitoring-backup/scripts/rollout-php-patch.sh",
    "rollout_app": "monitoring-backup/scripts/rollout-app-patch.sh",
    "quota_set": "host-storage/scripts/set-customer-quota.sh",
    "quota_report": "host-storage/scripts/quota-report.sh",
    "quota_reapply": "host-storage/scripts/reapply-quotas.sh",
    "quota_enable": "host-storage/scripts/enable-project-quota.sh",
}


def sandpodctl_bin() -> str:
    """Binary path: explicit env wins, else repo checkout, else /usr/local/bin."""
    env = os.environ.get("SANDPOD_SANDPODCTL")
    if env:
        return env
    repo_bin = os.path.join(REPO_ROOT, "modules", "sandpodctl", "sandpodctl")
    if os.path.isfile(repo_bin) and os.access(repo_bin, os.X_OK):
        return repo_bin
    return "/usr/local/bin/sandpodctl"


def script_path(name: str) -> str:
    base = os.environ.get("SANDPOD_SCRIPT_DIR", os.path.join(REPO_ROOT, "modules"))
    return os.path.join(base, SCRIPTS[name])


def build_cmd(name: str, args: list) -> list:
    if os.environ.get("SANDPOD_BACKEND") == "scripts":
        return [script_path(name)] + [str(a) for a in args]
    return [sandpodctl_bin(), SUBCOMMANDS[name]] + [str(a) for a in args]


def run_script(name: str, args: list, timeout: int = 120) -> tuple[int, str, str]:
    """Returns (exit, stdout, stderr). Exit 0 ok / 2 bad args / 1 runtime."""
    try:
        p = subprocess.run(build_cmd(name, args), capture_output=True,
                           text=True, timeout=timeout)
        return p.returncode, p.stdout, p.stderr
    except FileNotFoundError as e:
        return 1, "", f"missing backend: {e}"
    except subprocess.TimeoutExpired:
        return 1, "", "backend timeout"


def exit_to_http(exit_code: int) -> int:
    if exit_code == 0:
        return 200
    if exit_code == 2:
        return 400
    return 500
