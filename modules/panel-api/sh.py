"""One place that knows how to call repo bash scripts. No policy here."""
import os
import subprocess

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))

SCRIPTS = {
    "provision": "customer-provisioning/scripts/provision-customer.sh",
    "remove": "customer-provisioning/scripts/remove-customer.sh",
    "add_key": "customer-provisioning/scripts/add-ssh-key.sh",
    "add_site": "customer-apps/scripts/add-site.sh",
    "set_php": "customer-apps/scripts/set-php-version.sh",
    "remove_site": "customer-apps/scripts/remove-site.sh",
    "list_sites": "customer-apps/scripts/list-sites.sh",
    "add_port": "customer-apps/scripts/add-port-app.sh",
    "remove_port": "customer-apps/scripts/remove-port-app.sh",
    "reload_php": "shared-runtimes/scripts/reload-customer-php.sh",
    "reload_app": "shared-runtimes/scripts/reload-customer-app.sh",
    "mkdb": "host-mariadb/scripts/create-site-db.sh",
    "gen_nginx": "host-nginx/scripts/generate-site-conf.sh",
    "reload_nginx": "host-nginx/scripts/reload-host-nginx.sh",
    "backup": "monitoring-backup/scripts/backup-customer.sh",
    "report": "monitoring-backup/scripts/resource-report.sh",
    "rollout_php": "monitoring-backup/scripts/rollout-php-patch.sh",
    "rollout_app": "monitoring-backup/scripts/rollout-app-patch.sh",
}


def script_path(name: str) -> str:
    base = os.environ.get("SANDPOD_SCRIPT_DIR", os.path.join(REPO_ROOT, "modules"))
    return os.path.join(base, SCRIPTS[name])


def build_cmd(name: str, args: list) -> list:
    return [script_path(name)] + [str(a) for a in args]


def run_script(name: str, args: list, timeout: int = 120) -> tuple[int, str, str]:
    """Returns (exit, stdout, stderr). Exit 0 ok / 2 bad args / 1 runtime."""
    try:
        p = subprocess.run(build_cmd(name, args), capture_output=True,
                           text=True, timeout=timeout)
        return p.returncode, p.stdout, p.stderr
    except FileNotFoundError as e:
        return 1, "", f"missing script: {e}"
    except subprocess.TimeoutExpired:
        return 1, "", "script timeout"


def exit_to_http(exit_code: int) -> int:
    if exit_code == 0:
        return 200
    if exit_code == 2:
        return 400
    return 500
