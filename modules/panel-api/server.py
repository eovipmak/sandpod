#!/usr/bin/env python3
"""SandPod panel-api daemon (stdlib only, run as root, loopback-only).

Thin wrap over modules/*/scripts/*.sh. No policy, no TLS issuance.
Sync ops run scripts inline; long ops (provision/remove/backup/rollout/db)
run as background jobs, poll via GET /v1/jobs/{id}.

Env:
  SANDPOD_TOKEN_FILE   bearer token file (0600), default /etc/sandpod/panel-api.token
  SANDPOD_REGISTRY      default /etc/sandpod/customers.tsv
  SANDPOD_SCRIPT_DIR    repo scripts root (for fixture tests)
  SANDPOD_PORT          default 8741 (always binds 127.0.0.1)
"""
import json
import os
import re
import subprocess
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlparse

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
SCRIPT = {
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
    "gen_nginx": "host-nginx/scripts/generate-site-conf.sh",
    "reload_nginx": "host-nginx/scripts/reload-host-nginx.sh",
    "backup": "monitoring-backup/scripts/backup-customer.sh",
    "report": "monitoring-backup/scripts/resource-report.sh",
    "rollout_php": "monitoring-backup/scripts/rollout-php-patch.sh",
    "rollout_app": "monitoring-backup/scripts/rollout-app-patch.sh",
}
JOBS = {}
JOBS_LOCK = threading.Lock()
INSTANCE_LOCKS = {}
INSTANCE_LOCKS_GUARD = threading.Lock()


def script_path(name):
    base = os.environ.get("SANDPOD_SCRIPT_DIR", os.path.join(REPO_ROOT, "modules"))
    return os.path.join(base, SCRIPT[name])


def run_script(name, args, timeout=120):
    """Run a repo script. Returns (exit, stdout, stderr)."""
    cmd = [script_path(name)] + [str(a) for a in args]
    try:
        p = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
        return p.returncode, p.stdout, p.stderr
    except FileNotFoundError as e:
        return 1, "", f"missing script: {e}"
    except subprocess.TimeoutExpired:
        return 1, "", "script timeout"


def exit_to_http(exit_code):
    if exit_code == 0:
        return 200
    if exit_code == 2:
        return 400
    return 500


def load_token():
    f = os.environ.get("SANDPOD_TOKEN_FILE", "/etc/sandpod/panel-api.token")
    try:
        with open(f) as fh:
            return fh.read().strip()
    except OSError:
        return ""


def registry_customers():
    reg = os.environ.get("SANDPOD_REGISTRY", "/etc/sandpod/customers.tsv")
    out = []
    try:
        with open(reg) as fh:
            for line in fh:
                line = line.rstrip("\n")
                if not line or line.lstrip().startswith("#"):
                    continue
                parts = line.split("\t")
                if len(parts) < 6:
                    continue
                inst, user, cont, web, ssh, img = parts[:6]
                out.append({"instance": inst, "serviceUser": user,
                            "container": cont, "webPort": int(web) if web.isdigit() else web,
                            "sshPort": int(ssh) if ssh.isdigit() else ssh, "image": img})
    except OSError:
        pass
    return out


def lookup_customer(inst):
    for c in registry_customers():
        if c["instance"] == inst:
            return c
    return None


def submit_job(label, name, args, timeout=1800):
    jid = uuid.uuid4().hex[:12]
    with JOBS_LOCK:
        JOBS[jid] = {"jobId": jid, "label": label, "status": "queued",
                     "exitCode": None, "stdout": "", "stderr": ""}
    def _run():
        with JOBS_LOCK:
            JOBS[jid]["status"] = "running"
        code, so, se = run_script(name, args, timeout=timeout)
        with JOBS_LOCK:
            JOBS[jid].update({"status": "done" if code == 0 else "failed",
                              "exitCode": code, "stdout": so[-8000:], "stderr": se[-8000:]})
    threading.Thread(target=_run, daemon=True).start()
    return jid


class Handler(BaseHTTPRequestHandler):
    server_version = "sandpod-panel-api/4.0"

    def log_message(self, fmt, *args):
        pass  # audit via journal stdout in _audit

    def _audit(self, code, detail=""):
        print(json.dumps({"ts": time.strftime("%FT%TZ", time.gmtime()),
                          "m": self.command, "p": self.path,
                          "code": code, "detail": detail[:200]}), flush=True)

    def _send(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _auth_ok(self):
        if self.path == "/v1/health":
            return True
        want = load_token()
        if not want:
            return True  # no token provisioned (dev/fixture); production must provision one
        got = self.headers.get("Authorization", "")
        return got == f"Bearer {want}"

    def _body(self):
        try:
            n = int(self.headers.get("Content-Length", 0))
        except ValueError:
            n = 0
        if not n:
            return {}
        try:
            return json.loads(self.rfile.read(n) or b"{}")
        except (ValueError, OSError):
            return {}

    def do_GET(self):
        if not self._auth_ok():
            self._send(401, {"error": "unauthorized", "exitCode": 1, "stderr": ""})
            return
        u = urlparse(self.path)
        if u.path == "/v1/health":
            self._send(200, {"ok": True, "version": "4.0.0"})
            return
        if u.path == "/v1/customers":
            self._send(200, registry_customers())
            return
        m = re.match(r"^/v1/customers/([0-9]{3})/sites$", u.path)
        if m:
            c = lookup_customer(m.group(1))
            if not c:
                self._send(404, {"error": "unknown instance"})
                return
            code, so, se = run_script("list_sites", [c["serviceUser"], c["container"], "--json"])
            if code != 0:
                self._send(exit_to_http(code), {"error": "list-sites failed", "exitCode": code, "stderr": se})
                return
            try:
                self._send(200, json.loads(so or "{}"))
            except ValueError:
                self._send(500, {"error": "bad script JSON", "exitCode": 1, "stderr": so})
            return
        m = re.match(r"^/v1/customers/([0-9]{3})/resources$", u.path)
        if m:
            c = lookup_customer(m.group(1))
            if not c:
                self._send(404, {"error": "unknown instance"})
                return
            code, so, se = run_script("report", ["--registry",
                                     os.environ.get("SANDPOD_REGISTRY", "/etc/sandpod/customers.tsv"), "--json"])
            if code != 0:
                self._send(exit_to_http(code), {"error": "report failed", "exitCode": code, "stderr": se})
                return
            try:
                arr = json.loads(so or "[]")
            except ValueError:
                self._send(500, {"error": "bad report JSON", "exitCode": 1, "stderr": so})
                return
            self._send(200, next((r for r in arr if r.get("instance") == m.group(1)), {}))
            return
        m = re.match(r"^/v1/jobs/([0-9a-f]+)$", u.path)
        if m:
            with JOBS_LOCK:
                j = JOBS.get(m.group(1))
            if not j:
                self._send(404, {"error": "unknown job"})
                return
            self._send(200, j)
            return
        self._send(404, {"error": "not found"})

    def do_POST(self):
        if not self._auth_ok():
            self._send(401, {"error": "unauthorized", "exitCode": 1, "stderr": ""})
            return
        u = urlparse(self.path)
        data = self._body()
        if u.path == "/v1/customers":
            inst = str(data.get("instance", ""))
            if not re.match(r"^[0-9]{3}$", inst):
                self._send(400, {"error": "instance must be 3 digits", "exitCode": 2, "stderr": ""})
                return
            args = [inst]
            for k, f in (("serviceUser", "--service-user"), ("webPort", "--web-port"),
                         ("sshPort", "--ssh-port"), ("image", "--image"),
                         ("quotaGb", "--quota-gb"), ("cpus", "--cpus"),
                         ("memory", "--memory"), ("ioWeight", "--io-weight")):
                if data.get(k) not in (None, ""):
                    args += [f, str(data[k])]
            jid = submit_job("provision", "provision", args)
            self._send(202, {"jobId": jid, "status": "queued", "exitCode": None, "stdout": "", "stderr": ""})
            return
        m = re.match(r"^/v1/customers/([0-9]{3})/sites$", u.path)
        if m:
            c = lookup_customer(m.group(1))
            if not c:
                self._send(404, {"error": "unknown instance"})
                return
            domain = str(data.get("domain", "")).lower()
            php = str(data.get("php", "8.3"))
            args = [c["serviceUser"], c["container"], domain, "--php", php]
            if data.get("primary"):
                args.append("--primary")
            code, so, se = run_script("add_site", args)
            self._send(exit_to_http(code) if code else 200,
                       {"ok": code == 0, "stdout": so, "stderr": se} if code
                       else {"ok": True, "stdout": so, "stderr": se})
            return
        m = re.match(r"^/v1/customers/([0-9]{3})/port-apps$", u.path)
        if m:
            c = lookup_customer(m.group(1))
            if not c:
                self._send(404, {"error": "unknown instance"})
                return
            try:
                port = int(data.get("port", 0))
            except (ValueError, TypeError):
                port = 0
            if not (3000 <= port <= 3999):
                self._send(400, {"error": "port must be 3000-3999 (panel-allocated)", "exitCode": 2, "stderr": ""})
                return
            args = [c["serviceUser"], c["container"], str(data.get("domain", "")).lower(),
                    "--port", str(port), "--command", str(data.get("command", ""))]
            if data.get("primary"):
                args.append("--primary")
            code, so, se = run_script("add_port", args)
            self._send(exit_to_http(code) if code else 200,
                       {"ok": code == 0, "stdout": so, "stderr": se} if code
                       else {"ok": True, "stdout": so, "stderr": se})
            return
        m = re.match(r"^/v1/customers/([0-9]{3})/reload$", u.path)
        if m:
            c = lookup_customer(m.group(1))
            if not c:
                self._send(404, {"error": "unknown instance"})
                return
            target = str(data.get("target", ""))
            if target.startswith("php-"):
                code, so, se = run_script("reload_php", [c["serviceUser"], c["container"], target[4:]])
            elif target.startswith("app-"):
                code, so, se = run_script("reload_app", [c["serviceUser"], c["container"], target[4:]])
            else:
                self._send(400, {"error": "target must be php-8.x or app-<domain>", "exitCode": 2, "stderr": ""})
                return
            self._send(exit_to_http(code) if code else 200,
                       {"ok": code == 0, "stdout": so, "stderr": se} if code
                       else {"ok": True, "stdout": so, "stderr": se})
            return
        m = re.match(r"^/v1/customers/([0-9]{3})/(ssh-keys|backups)$", u.path)
        if m:
            c = lookup_customer(m.group(1))
            if not c:
                self._send(404, {"error": "unknown instance"})
                return
            if m.group(2) == "backups":
                jid = submit_job("backup", "backup", [m.group(1)])
            else:
                key = data.get("publicKey", "")
                if not (key.startswith("ssh-") and len(key) < 4096):
                    self._send(400, {"error": "bad publicKey", "exitCode": 2, "stderr": ""})
                    return
                kf = f"/tmp/sandpod-key-{uuid.uuid4().hex}.pub"
                with open(kf, "w") as fh:
                    fh.write(key + "\n")
                jid = submit_job("ssh-key", "add_key", [c["serviceUser"], c["container"], kf])
            self._send(202, {"jobId": jid, "status": "queued", "exitCode": None, "stdout": "", "stderr": ""})
            return
        if u.path == "/dbs":
            if not re.match(r"^[A-Za-z0-9_]+$", str(data.get("dbName", ""))) or \
               not re.match(r"^[A-Za-z0-9_]+$", str(data.get("dbUser", ""))):
                self._send(400, {"error": "dbName/dbUser must be [A-Za-z0-9_]", "exitCode": 2, "stderr": ""})
                return
            jid = submit_job("mkdb", "mkdb", [data["dbName"], data["dbUser"]])
            self._send(202, {"jobId": jid, "status": "queued", "exitCode": None, "stdout": "", "stderr": ""})
            return
        if u.path in ("/rollouts/php", "/rollouts/app"):
            args = ["--batch-size", str(data.get("batchSize", 5)),
                    "--sleep-seconds", str(data.get("sleepSeconds", 10))]
            if data.get("skipUpdate"):
                args.append("--skip-update")
            jid = submit_job("rollout", "rollout_php" if u.path.endswith("/php") else "rollout_app", args)
            self._send(202, {"jobId": jid, "status": "queued", "exitCode": None, "stdout": "", "stderr": ""})
            return
        if u.path == "/host-nginx/reload":
            jid = submit_job("nginx", "reload_nginx", [])
            self._send(202, {"jobId": jid, "status": "queued", "exitCode": None, "stdout": "", "stderr": ""})
            return
        self._send(404, {"error": "not found"})

    def do_PATCH(self):
        if not self._auth_ok():
            self._send(401, {"error": "unauthorized", "exitCode": 1, "stderr": ""})
            return
        m = re.match(r"^/v1/customers/([0-9]{3})/sites/(.+)$", urlparse(self.path).path)
        if not m:
            self._send(404, {"error": "not found"})
            return
        c = lookup_customer(m.group(1))
        if not c:
            self._send(404, {"error": "unknown instance"})
            return
        data = self._body()
        domain = m.group(2).lower()
        if data.get("php") not in ("8.2", "8.3", "8.4", "8.5", "8.6"):
            self._send(400, {"error": "php must be 8.2-8.6", "exitCode": 2, "stderr": ""})
            return
        code, so, se = run_script("set_php", [c["serviceUser"], c["container"], domain, data["php"]])
        self._send(exit_to_http(code) if code else 200,
                   {"ok": code == 0, "stdout": so, "stderr": se} if code
                   else {"ok": True, "stdout": so, "stderr": se})

    def do_DELETE(self):
        if not self._auth_ok():
            self._send(401, {"error": "unauthorized", "exitCode": 1, "stderr": ""})
            return
        u = urlparse(self.path)
        m = re.match(r"^/v1/customers/([0-9]{3})$", u.path)
        if m:
            data = self._body()
            args = [m.group(1)]
            if data.get("deleteData"):
                args.append("--delete-data")
            if data.get("deleteUser"):
                args.append("--delete-user")
            if data.get("deleteRegistryEntry"):
                args.append("--delete-registry-entry")
            jid = submit_job("remove", "remove", args)
            self._send(202, {"jobId": jid, "status": "queued", "exitCode": None, "stdout": "", "stderr": ""})
            return
        m = re.match(r"^/v1/customers/([0-9]{3})/(sites|port-apps)/(.+)$", u.path)
        if m:
            c = lookup_customer(m.group(1))
            if not c:
                self._send(404, {"error": "unknown instance"})
                return
            data = self._body()
            name = "remove_site" if m.group(2) == "sites" else "remove_port"
            args = [c["serviceUser"], c["container"], m.group(3).lower()]
            if data.get("deleteData"):
                args.append("--delete-data")
            code, so, se = run_script(name, args)
            self._send(exit_to_http(code) if code else 200,
                       {"ok": code == 0, "stdout": so, "stderr": se} if code
                       else {"ok": True, "stdout": so, "stderr": se})
            return
        self._send(404, {"error": "not found"})


def main():
    port = int(os.environ.get("SANDPOD_PORT", "8741"))
    srv = HTTPServer(("127.0.0.1", port), Handler)
    print(f"sandpod-panel-api listening on 127.0.0.1:{port}", flush=True)
    srv.serve_forever()


if __name__ == "__main__":
    main()
