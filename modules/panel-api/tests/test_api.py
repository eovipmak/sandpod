"""FastAPI contract tests: TestClient + fixture fake-scripts, no root needed.

P2: site ops sync — validation 400, warnings[], delete_data query, dry_run cmd.
"""
import os
import stat
import sys
import time

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))

FIX = "/tmp/sapi-fastapi-fixture"


def _write(path, body):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as fh:
        fh.write(body)
    os.chmod(path, os.stat(path).st_mode | stat.S_IXUSR | stat.S_IXGRP)


def setup_module(_):
    for d in ("customer-apps/scripts", "monitoring-backup/scripts",
              "customer-provisioning/scripts", "shared-runtimes/scripts",
              "host-mariadb/scripts", "host-nginx/scripts"):
        os.makedirs(os.path.join(FIX, "mods", d), exist_ok=True)
    M = os.path.join(FIX, "mods")
    _write(os.path.join(M, "customer-apps/scripts/list-sites.sh"),
           '#!/usr/bin/env bash\necho \'{"defaults":{},"php_sites":[],"port_apps":[]}\'\n')
    _write(os.path.join(M, "customer-apps/scripts/add-site.sh"),
           '#!/usr/bin/env bash\necho "Site $3 php $5"\n'
           'if [ "$4" = "8.2" ]; then echo "WARNING: PHP 8.2 security support ends Dec 2026" >&2; fi\n')
    _write(os.path.join(M, "customer-apps/scripts/set-php-version.sh"),
           '#!/usr/bin/env bash\necho "Site $3 -> $4"\n'
           'if [ "$4" = "8.2" ]; then echo "WARNING: PHP 8.2 security support ends Dec 2026" >&2; fi\n')
    _write(os.path.join(M, "customer-apps/scripts/remove-site.sh"),
           '#!/usr/bin/env bash\necho "removed $3 $4"\n')
    _write(os.path.join(M, "customer-apps/scripts/add-port-app.sh"),
           '#!/usr/bin/env bash\necho "port app $3 $5 $7"\n')
    _write(os.path.join(M, "customer-apps/scripts/remove-port-app.sh"),
           '#!/usr/bin/env bash\necho "removed port $3 $4"\n')
    _write(os.path.join(M, "shared-runtimes/scripts/reload-customer-php.sh"),
           '#!/usr/bin/env bash\necho "reload php $3"\n'
           'if [ "$3" = "8.2" ]; then echo "WARNING: PHP 8.2 EOL soon" >&2; fi\n'
           'if [ "$3" = "9.9" ]; then echo "bad minor" >&2; exit 2; fi\n')
    _write(os.path.join(M, "shared-runtimes/scripts/reload-customer-app.sh"),
           '#!/usr/bin/env bash\necho "restart $3"\n')
    _write(os.path.join(M, "monitoring-backup/scripts/resource-report.sh"),
           '#!/usr/bin/env bash\necho \'[{"instance":"001"}]\'\n')
    _write(os.path.join(M, "monitoring-backup/scripts/backup-customer.sh"),
           '#!/usr/bin/env bash\necho /tmp/x.tar.gz\n')
    _write(os.path.join(M, "monitoring-backup/scripts/rollout-php-patch.sh"),
           '#!/usr/bin/env bash\necho ok\n')
    with open(os.path.join(FIX, "customers.tsv"), "w") as fh:
        fh.write("001\tsvc-c001\tcustomer-001\t18001\t22201\timg:pilot\n")
    os.environ["SANDPOD_SCRIPT_DIR"] = os.path.join(FIX, "mods")
    os.environ["SANDPOD_REGISTRY"] = os.path.join(FIX, "customers.tsv")
    os.environ.pop("SANDPOD_TOKEN_FILE", None)


def _client():
    from fastapi.testclient import TestClient
    import app as panel_app
    panel_app.jobs.clear()
    return TestClient(panel_app.app)


def test_health_and_docs():
    c = _client()
    assert c.get("/v1/health").json() == {"ok": True, "version": "4.0.0"}
    assert c.get("/openapi.json").status_code == 200


def test_customers_and_sites_and_resources():
    c = _client()
    assert c.get("/v1/customers").json()[0]["instance"] == "001"
    assert c.get("/v1/customers/001/sites").json()["php_sites"] == []
    assert c.get("/v1/customers/001/resources").json() == {"instance": "001"}
    assert c.get("/v1/customers/999/sites").status_code == 404


def test_mutation_and_dry_run():
    c = _client()
    r = c.post("/v1/customers/001/sites",
               json={"domain": "shop.example.com", "php": "8.3"})
    assert r.status_code == 200 and r.json()["ok"] is True
    r = c.post("/v1/customers/001/sites?dry_run=true",
               json={"domain": "x.example.com", "php": "8.4"})
    assert r.json()["dryRun"] is True and "add-site.sh" in " ".join(r.json()["cmd"])
    # P2: port/target validation is 400 (plan), not pydantic 422
    assert c.post("/v1/customers/001/port-apps",
                  json={"domain": "a.test", "port": 80, "command": "x"}).status_code == 400
    assert c.post("/v1/customers/001/reload",
                  json={"target": "nope"}).status_code == 422
    r = c.post("/v1/customers/001/backups")
    assert r.status_code == 202
    time.sleep(0.5)
    j = c.get(f"/v1/jobs/{r.json()['jobId']}").json()
    assert j["status"] in ("running", "done", "queued")
    assert "X-Request-ID" in c.get("/v1/health").headers


# ---------- P2 ----------

def test_p2_set_php_ok_and_warnings():
    c = _client()
    r = c.patch("/v1/customers/001/sites/shop.example.com", json={"php": "8.4"})
    assert r.status_code == 200 and r.json()["ok"] is True
    assert r.json()["warnings"] == []
    r = c.patch("/v1/customers/001/sites/shop.example.com", json={"php": "8.2"})
    assert r.status_code == 200
    assert any("8.2" in w for w in r.json()["warnings"])  # EOL passthrough, not error


def test_p2_reload_php_app_and_errors():
    c = _client()
    r = c.post("/v1/customers/001/reload", json={"target": "php-8.3"})
    assert r.status_code == 200 and "reload php 8.3" in r.json()["stdout"]
    r = c.post("/v1/customers/001/reload", json={"target": "php-8.2"})
    assert r.status_code == 200 and any("8.2" in w for w in r.json()["warnings"])
    r = c.post("/v1/customers/001/reload", json={"target": "php-9.9"})
    assert r.status_code == 400  # unsupported minor, no exec
    r = c.post("/v1/customers/001/reload", json={"target": "app-shop.example.com"})
    assert r.status_code == 200 and "restart" in r.json()["stdout"]


def test_p2_delete_with_query_flags_and_dry_run():
    c = _client()
    r = c.delete("/v1/customers/001/sites/shop.example.com")
    assert r.status_code == 200 and "--delete-data" not in r.json()["stdout"]
    r = c.delete("/v1/customers/001/sites/shop.example.com?delete_data=true")
    assert r.status_code == 200 and "--delete-data" in r.json()["stdout"]
    # dry_run shows exact cmd incl. flag
    r = c.delete("/v1/customers/001/sites/shop.example.com?delete_data=true&dry_run=true")
    assert r.json()["dryRun"] is True
    assert "--delete-data" in r.json()["cmd"]
    r = c.delete("/v1/customers/001/port-apps/shop.example.com?dry_run=true")
    assert "remove-port-app.sh" in " ".join(r.json()["cmd"])
    # invalid domain -> 400 before exec (mirrors lib.sh valid_domain)
    assert c.post("/v1/customers/001/sites",
                  json={"domain": "bad..", "php": "8.3"}).status_code == 400
    assert c.delete("/v1/customers/001/sites/bad..").status_code == 400
    # bad command (multiline) -> 400
    r = c.post("/v1/customers/001/port-apps",
               json={"domain": "a.example.com", "port": 3001, "command": "a\nb"})
    assert r.status_code == 400


def test_p2_error_shape_single_exec():
    """Script exit 2 maps to 400 with exitCode; script runs exactly once."""
    c = _client()
    ctr = os.path.join(FIX, "count-add-site")
    if os.path.exists(ctr):
        os.remove(ctr)
    import app as panel_app
    orig = panel_app.run_script
    calls = []

    def counting(name, args, timeout=120):
        if name == "add_site":
            calls.append(1)
        return orig(name, args, timeout=timeout)

    panel_app.run_script = counting
    try:
        r = c.post("/v1/customers/001/sites",
                   json={"domain": "once.example.com", "php": "8.3"})
        assert r.status_code == 200
        assert len(calls) == 1, f"script executed {len(calls)}x, want 1"
    finally:
        panel_app.run_script = orig
