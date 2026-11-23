"""P4 tests: daemon self-protection + fail-closed auth + op fail-paths.

Repo-only, fixture-based (no host). Covers plans/gaps-audit.md Part 4:
401/0600/missing-token, public health+docs, global cap 409, body 413,
job prune bound, ssh-key tmp cleanup, sync fail shapes, server.py parity.
"""
import os
import stat
import sys
import time

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))

FIX = "/tmp/sapi-p4-fixture"
TOKEN = os.path.join(FIX, "panel-api.token")
GOOD_KEY = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqtestkey user@host"


def _write(path, body, mode=0o755):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as fh:
        fh.write(body)
    os.chmod(path, mode)


def setup_module(_):
    M = os.path.join(FIX, "mods")
    os.makedirs(M, exist_ok=True)
    _write(os.path.join(M, "sandpodctl"), '''#!/usr/bin/env bash
# Fixture fake for the Go sandpodctl binary: $1 = subcommand.
cmd="$1"; shift
case "$cmd" in
  list-sites) echo '{"defaults":{},"php_sites":[],"port_apps":[]}' ;;
  resource-report) echo '[{"instance":"001"}]' ;;
  add-ssh-key) echo "key installed for $1" ;;
  *) echo "unknown command: $cmd" >&2; exit 2 ;;
esac
''')
    with open(os.path.join(FIX, "customers.tsv"), "w") as fh:
        fh.write("001\tsvc-c001\tcustomer-001\t18001\t22201\timg:pilot\n")
    with open(TOKEN, "w") as fh:
        fh.write("secret-test-token\n")
    os.chmod(TOKEN, 0o600)
    os.environ["SANDPOD_SANDPODCTL"] = os.path.join(M, "sandpodctl")
    os.environ.pop("SANDPOD_BACKEND", None)
    os.environ.pop("SANDPOD_SCRIPT_DIR", None)
    os.environ["SANDPOD_REGISTRY"] = os.path.join(FIX, "customers.tsv")
    os.environ["SANDPOD_TOKEN_FILE"] = TOKEN
    os.environ.pop("SANDPOD_REQUIRE_AUTH", None)


def _client():
    from fastapi.testclient import TestClient
    import app as panel_app
    panel_app.jobs.clear()
    return TestClient(panel_app.app)


def _auth():
    return {"Authorization": "Bearer secret-test-token"}


def test_public_health_docs_no_auth_but_api_locked(monkeypatch):
    monkeypatch.setenv("SANDPOD_REQUIRE_AUTH", "1")
    monkeypatch.setenv("SANDPOD_TOKEN_FILE", TOKEN)
    c = _client()
    assert c.get("/v1/health").status_code == 200  # public readiness
    assert c.get("/docs").status_code == 200  # public swagger
    assert c.get("/v1/customers").status_code == 401  # no header
    r = c.get("/v1/customers",
              headers={"Authorization": "Bearer secret-test-token"})
    assert r.status_code == 200 and r.json()[0]["instance"] == "001"
    assert c.get("/v1/customers",
                 headers={"Authorization": "Bearer wrong"}).status_code == 401


def test_fail_closed_missing_or_bad_mode_token(monkeypatch):
    import app as panel_app
    monkeypatch.setenv("SANDPOD_REQUIRE_AUTH", "1")
    monkeypatch.setenv("SANDPOD_TOKEN_FILE", os.path.join(FIX, "absent.token"))
    c = _client()
    # missing file must deny (500), never serve open
    assert c.get("/v1/customers").status_code == 500
    bad = os.path.join(FIX, "bad.token")
    with open(bad, "w") as fh:
        fh.write("x\n")
    os.chmod(bad, 0o644)
    monkeypatch.setenv("SANDPOD_TOKEN_FILE", bad)
    assert _client().get("/v1/customers",
                         headers={"Authorization": "Bearer x"}).status_code == 500
    # fail-open preserved when the env is unset (repo-only dev/tests)
    monkeypatch.delenv("SANDPOD_REQUIRE_AUTH")
    assert panel_app.token_file_mode_ok(TOKEN) is True
    assert panel_app.token_file_mode_ok(bad) is False


def test_global_cap_409():
    import app as panel_app
    c = _client()
    try:
        for i in range(panel_app.jobs._MAX_CONCURRENT):
            panel_app.jobs._JOBS[f"cap{i}"] = {
                "jobId": f"cap{i}", "status": "queued"}
        r = c.post("/v1/customers", json={"instance": "003"},
                   headers=_auth())
        assert r.status_code == 409
        assert r.json()["detail"]["exitCode"] == 1
    finally:
        panel_app.jobs.clear()


def test_body_cap_413(monkeypatch):
    import app as panel_app
    monkeypatch.setattr(panel_app, "MAX_BODY_BYTES", 10)
    c = _client()
    r = c.post("/v1/customers/001/sites",
               json={"domain": "x.example.com", "php": "8.3"})
    assert r.status_code == 413
    assert r.json()["exitCode"] == 2


def test_prune_bound():
    import jobs
    jobs.clear()
    try:
        for i in range(jobs._MAX_JOBS + 5):
            jobs._JOBS[f"p{i}"] = {"jobId": f"p{i}", "status": "done",
                                   "finishedAt": float(i)}
        jobs._prune()
        assert len(jobs._JOBS) <= jobs._MAX_JOBS
    finally:
        jobs.clear()


def test_ssh_key_tmp_cleaned_after_job():
    import tempfile
    c = _client()
    recorded = []
    real = tempfile.NamedTemporaryFile

    class Rec:
        def __init__(self, *a, **k):
            self._f = real(*a, **k)
            recorded.append(self._f.name)

        def __enter__(self):
            return self._f.__enter__()

        def __exit__(self, *a):
            return self._f.__exit__(*a)

    tempfile.NamedTemporaryFile = Rec
    try:
        r = c.post("/v1/customers/001/ssh-keys",
                   json={"publicKey": GOOD_KEY}, headers=_auth())
        assert r.status_code == 202
        jid = r.json()["jobId"]
        for _ in range(200):
            j = c.get(f"/v1/jobs/{jid}", headers=_auth()).json()
            if j["status"] in ("done", "failed"):
                break
            time.sleep(0.05)
        assert recorded, "expected a temp pubkey file"
        assert not os.path.exists(recorded[0]), "temp key must be unlinked"
        # job summaries never carry stdout (no creds/key material in lists)
        assert "stdout" not in c.get("/v1/jobs", headers=_auth()).json()[0]
    finally:
        tempfile.NamedTemporaryFile = real


def test_sync_fail_shapes():
    import app as panel_app
    c = _client()
    orig = panel_app.run_script
    panel_app.run_script = lambda n, a, timeout=120: (2, "", "bad input")
    try:
        r = c.get("/v1/customers/001/sites", headers=_auth())
        assert r.status_code == 400
        assert r.json()["detail"]["exitCode"] == 2
    finally:
        panel_app.run_script = orig
    panel_app.run_script = lambda n, a, timeout=120: (0, "not-json", "")
    try:
        assert c.get("/v1/customers/001/sites",
                       headers=_auth()).status_code == 500
        assert c.get("/v1/customers/001/resources",
                     headers=_auth()).status_code == 500
    finally:
        panel_app.run_script = orig


def test_server_parity_strings():
    api = os.path.join(os.path.dirname(__file__), "..")
    srv = open(os.path.join(api, "server.py")).read()
    for p in ('"/v1/dbs"', '"/v1/host-nginx/reload"',
              '"/v1/rollouts/php"', '"/v1/rollouts/app"'):
        assert p in srv, f"fallback must serve {p} (match app.py /v1 prefix)"
    assert "delete_data" in srv  # DELETE via query flags, not body
    assert "deleteData" not in srv
    assert "413" in srv and "_too_large" in srv
