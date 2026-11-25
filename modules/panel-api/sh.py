"""One place that knows how to call the operator backend. No policy here.

The backend is the Go binary `sandpodctl` (one subcommand per operation,
argv contract unchanged from the retired bash scripts).
"""
import os
import subprocess

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))

# Logical op -> sandpodctl subcommand.
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


def sandpodctl_bin() -> str:
    """Binary path: explicit env wins, else repo checkout, else /usr/local/bin."""
    env = os.environ.get("SANDPOD_SANDPODCTL")
    if env:
        return env
    repo_bin = os.path.join(REPO_ROOT, "modules", "sandpodctl", "sandpodctl")
    if os.path.isfile(repo_bin) and os.access(repo_bin, os.X_OK):
        return repo_bin
    return "/usr/local/bin/sandpodctl"


def build_cmd(name: str, args: list) -> list:
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
