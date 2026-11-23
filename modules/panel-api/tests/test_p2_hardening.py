"""P2 hardening tests: auth/validation/timeout fixes (repo-only, fixture).

Covers Part 2 of plans/gaps-audit.md: provision format checks, ssh-key
whitelist, rollout int-safety, 404 shape, resources miss -> 404,
jobs.acquire atomicity, submit failure cleanup, server.py guards.
"""
import os
import stat
import sys

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))

FIX = "/tmp/sapi-p2-fixture"


def _write(path, body):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as fh:
        fh.write(body)
    os.chmod(path, os.stat(path).st_mode | stat.S_IXUSR | stat.S_IXGRP)


def setup_module(_):
    M = os.path.join(FIX, "mods")
    os.makedirs(M, exist_ok=True)
    _write(os.path.join(M, "sandpodctl"), '''#!/usr/bin/env bash
# Fixture fake for the Go sandpodctl binary: $1 = subcommand.
cmd="$1"; shift
case "$cmd" in
  resource-report) echo '[{"instance":"001"}]' ;;
  *) echo "unknown command: $cmd" >&2; exit 2 ;;
esac
''')
    with open(os.path.join(FIX, "customers.tsv"), "w") as fh:
        fh.write("001\tsvc-c001\tcustomer-001\t18001\t22201\timg:pilot\n")
    os.environ["SANDPOD_SANDPODCTL"] = os.path.join(M, "sandpodctl")
    os.environ.pop("SANDPOD_BACKEND", None)
    os.environ.pop("SANDPOD_SCRIPT_DIR", None)
    os.environ["SANDPOD_REGISTRY"] = os.path.join(FIX, "customers.tsv")
    os.environ["SANDPOD_TOKEN_FILE"] = os.path.join(FIX, "panel-api.token.absent")


def _client():
    from fastapi.testclient import TestClient
    import app as panel_app
    panel_app.jobs.clear()
    return TestClient(panel_app.app)


def test_provision_format_validation_400():
    c = _client()
    # bad service user (mirror lib.sh pattern)
    r = c.post("/v1/customers", json={"instance": "002", "serviceUser": "Bad User!"})
    assert r.status_code == 400
    # quota upper bound (must be 1-1000, fail-fast before queue)
    r = c.post("/v1/customers", json={"instance": "002", "quotaGb": 9999})
    assert r.status_code == 400
    # blank image tag refused
    r = c.post("/v1/customers", json={"instance": "002", "image": "   "})
    assert r.status_code == 400
    # sane values still pass validation (dry_run shows cmd, no exec)
    r = c.post("/v1/customers?dry_run=true",
               json={"instance": "002", "serviceUser": "svc-c002", "quotaGb": 10})
    assert r.status_code == 202 and r.json()["dryRun"] is True


def test_ssh_key_whitelist():
    c = _client()
    assert c.post("/v1/customers/001/ssh-keys",
                  json={"publicKey": "not-a-key"}).status_code == 422
    assert c.post("/v1/customers/001/ssh-keys",
                  json={"publicKey": "ssh-rsa not-base64!!!"}).status_code == 422
    # multiline smuggling refused
    assert c.post("/v1/customers/001/ssh-keys",
                  json={"publicKey": "ssh-ed25519 AAAA\nBBBB"}).status_code == 422
    # well-formed key passes validation (dry_run, no exec, no leak)
    good = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqtestkey user@host"
    r = c.post("/v1/customers/001/ssh-keys?dry_run=true",
               json={"publicKey": good})
    assert r.status_code == 202 and r.json()["dryRun"] is True


def test_rollout_int_safety():
    c = _client()
    assert c.post("/v1/rollouts/php", json={"batchSize": 0}).status_code == 400
    # non-integer shapes are pydantic 422 (documented), never 500
    assert c.post("/v1/rollouts/php", json={"batchSize": "abc"}).status_code == 422
    assert c.post("/v1/rollouts/php", json={"batchSize": None}).status_code == 422


def test_404_shape_and_resources_miss():
    c = _client()
    r = c.get("/v1/customers/999/sites")
    assert r.status_code == 404
    assert r.json()["detail"]["error"] == "unknown instance"
    assert r.json()["detail"]["exitCode"] == 2
    assert c.get("/v1/jobs/nope").status_code == 404
    # report JSON without this instance -> 404, not {} 200
    import app as panel_app
    orig = panel_app.run_script
    panel_app.run_script = lambda n, a, timeout=120: (0, "[]", "")
    try:
        r = c.get("/v1/customers/001/resources")
        assert r.status_code == 404
    finally:
        panel_app.run_script = orig


def test_jobs_acquire_atomic_and_cleanup():
    import jobs
    jobs.clear()
    assert jobs.acquire("001") == "ok"
    assert jobs.acquire("001") == "busy"  # same instance re-entry
    jobs.release("001")
    assert jobs.acquire("001") == "ok"
    jobs.release("001")
    # global cap: 10 queued entries -> full, not ok
    try:
        for i in range(jobs._MAX_CONCURRENT):
            jobs._JOBS[f"fake{i}"] = {"jobId": f"fake{i}", "status": "queued"}
        assert jobs.acquire("002") == "full"
    finally:
        jobs.clear()
    # submit Thread failure must not leave stale reservation (no perm 409)
    import threading
    real_thread = threading.Thread

    class Boom:
        def __init__(self, *a, **k):
            pass

        def start(self):
            raise RuntimeError("no threads")

    threading.Thread = Boom
    try:
        jobs.acquire("009")
        try:
            jobs.submit("x", "y", [], instance="009")
            assert False, "submit should raise"
        except RuntimeError:
            pass
        assert jobs.acquire("009") == "ok"  # not stuck
        jobs.release("009")
    finally:
        threading.Thread = real_thread
        jobs.clear()


def test_server_fallback_guards_present():
    import os as _os
    api = _os.path.join(_os.path.dirname(__file__), "..")
    srv = open(_os.path.join(api, "server.py")).read()
    assert "parse_qs" in srv  # DELETE query flags, not body
    assert "MAX_BODY_BYTES" in srv  # body cap, not unbounded read
    assert "_too_large" in srv and "_bad_json" in srv
    assert "timeout=7200" in srv  # rollout needs fleet timeout, not 1800
    app_src = open(_os.path.join(api, "app.py")).read()
    assert "acquire_or_409" in app_src  # atomic cap+instance, no TOCTOU
    assert "jobs.acquire" in app_src
