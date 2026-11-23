"""P3 tests: async jobs — locks, fail-fast validation, creds-once, nginx chain."""
import os
import stat
import sys
import time

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))

FIX = "/tmp/sapi-p3-fixture"


def _write(path, body):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as fh:
        fh.write(body)
    os.chmod(path, os.stat(path).st_mode | stat.S_IXUSR | stat.S_IXGRP)


def setup_module(_):
    M = os.path.join(FIX, "mods")
    for d in ("customer-provisioning/scripts", "monitoring-backup/scripts",
              "customer-apps/scripts", "shared-runtimes/scripts",
              "host-mariadb/scripts", "host-nginx/scripts"):
        os.makedirs(os.path.join(M, d), exist_ok=True)
    P = os.path.join(M, "customer-provisioning/scripts")
    _write(os.path.join(P, "provision-customer.sh"),
           '#!/usr/bin/env bash\nsleep 1.2\necho "Provisioned $1"\n')
    _write(os.path.join(P, "remove-customer.sh"),
           '#!/usr/bin/env bash\necho "removed $1 $*"\n')
    _write(os.path.join(P, "add-ssh-key.sh"),
           '#!/usr/bin/env bash\necho "key $3"\n')
    _write(os.path.join(M, "customer-apps/scripts/list-sites.sh"),
           '#!/usr/bin/env bash\necho \'{"defaults":{},"php_sites":[],"port_apps":[]}\'\n')
    B = os.path.join(M, "monitoring-backup/scripts")
    _write(os.path.join(B, "backup-customer.sh"),
           '#!/usr/bin/env bash\necho "/backups/$1-20261123.tar.gz"\n')
    _write(os.path.join(B, "rollout-php-patch.sh"),
           '#!/usr/bin/env bash\nsleep 2\necho "rollout $*"\n')
    _write(os.path.join(M, "host-mariadb/scripts/create-site-db.sh"),
           '#!/usr/bin/env bash\necho "DB_HOST=h\nDB_NAME=$1\nDB_USER=$2\nDB_PASS=s3cr3t-$1"\n')
    N = os.path.join(M, "host-nginx/scripts")
    _write(os.path.join(N, "generate-site-conf.sh"),
           f'#!/usr/bin/env bash\necho gen >> {FIX}/order.log\necho gen-ok\n')
    _write(os.path.join(N, "reload-host-nginx.sh"),
           f'#!/usr/bin/env bash\necho reload >> {FIX}/order.log\necho reload-ok\n')
    if os.path.exists(os.path.join(FIX, "order.log")):
        os.remove(os.path.join(FIX, "order.log"))
    with open(os.path.join(FIX, "customers.tsv"), "w") as fh:
        fh.write("001\tsvc-c001\tcustomer-001\t18001\t22201\timg:pilot\n")
    os.environ["SANDPOD_SCRIPT_DIR"] = M
    os.environ["SANDPOD_REGISTRY"] = os.path.join(FIX, "customers.tsv")
    # Hermetic auth: never depend on the host's real token file (see test_api).
    os.environ["SANDPOD_TOKEN_FILE"] = os.path.join(FIX, "panel-api.token.absent")


def _client():
    from fastapi.testclient import TestClient
    import app as panel_app
    panel_app.jobs.clear()
    return TestClient(panel_app.app)


def _wait_done(c, jid, timeout=8):
    for _ in range(int(timeout * 2)):
        j = c.get(f"/v1/jobs/{jid}").json()
        if j["status"] in ("done", "failed"):
            return j
        time.sleep(0.5)
    raise AssertionError(f"job {jid} not finished")


def test_p3_provision_validation_fail_fast():
    c = _client()
    assert c.post("/v1/customers", json={"instance": "002", "cpus": "xx"}).status_code == 400
    assert c.post("/v1/customers", json={"instance": "002", "memory": "0"}).status_code == 400
    assert c.post("/v1/customers", json={"instance": "002", "ioWeight": 5}).status_code == 400
    r = c.post("/v1/customers",
               json={"instance": "002", "webPort": 18002, "sshPort": 18002})
    assert r.status_code == 400
    r = c.post("/v1/customers?dry_run=true", json={"instance": "002", "cpus": "1.0"})
    assert r.json()["dryRun"] is True and "--cpus" in r.json()["cmd"]


def test_p3_double_provision_conflict():
    c = _client()
    r1 = c.post("/v1/customers", json={"instance": "007"})
    assert r1.status_code == 202
    r2 = c.post("/v1/customers", json={"instance": "007"})
    assert r2.status_code == 409  # instance busy
    j = _wait_done(c, r1.json()["jobId"])
    assert j["status"] == "done" and "Provisioned 007" in j["stdout"]


def test_p3_remove_guard_and_backup_path():
    c = _client()
    r = c.delete("/v1/customers/001?delete_user=true")
    assert r.status_code == 400  # --delete-user requires --delete-data
    r = c.post("/v1/customers/001/backups")
    assert r.status_code == 202
    j = _wait_done(c, r.json()["jobId"])
    assert j["status"] == "done" and j["stdout"].strip().endswith(".tar.gz")


def test_p3_db_creds_once_and_list_redacted():
    c = _client()
    r = c.post("/v1/dbs", json={"dbName": "shopdb", "dbUser": "shopu"})
    assert r.status_code == 202
    j = _wait_done(c, r.json()["jobId"])
    assert "DB_PASS=s3cr3t-shopdb" in j["stdout"]  # full creds exactly once
    lst = c.get("/v1/jobs").json()
    row = next(x for x in lst if x["jobId"] == r.json()["jobId"])
    assert "stdout" not in row and "DB_PASS" not in str(row)


def test_p3_nginx_chain_order_and_rollout_guard():
    c = _client()
    if os.path.exists(os.path.join(FIX, "order.log")):
        os.remove(os.path.join(FIX, "order.log"))
    r = c.post("/v1/host-nginx/reload")
    assert r.status_code == 202
    j = _wait_done(c, r.json()["jobId"])
    assert j["status"] == "done"
    order = open(os.path.join(FIX, "order.log")).read().split()
    assert order == ["gen", "reload"]  # gen before reload, one job
    assert c.post("/v1/rollouts/php", json={"batchSize": 0}).status_code == 400
    r1 = c.post("/v1/rollouts/php", json={"batchSize": 1, "sleepSeconds": 0})
    assert r1.status_code == 202
    r2 = c.post("/v1/rollouts/app", json={"batchSize": 1})
    assert r2.status_code == 409  # fleet busy
