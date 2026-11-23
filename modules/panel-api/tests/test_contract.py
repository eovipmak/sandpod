"""Repo-only contract tests: openapi paths map to existing scripts, server imports."""
import os
import re

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
API = os.path.join(REPO, "modules", "panel-api")


def test_openapi_paths_cover_scripts():
    src = open(os.path.join(API, "openapi.yaml")).read()
    for p in ["/health", "/customers", "/customers/{id}/sites",
              "/customers/{id}/port-apps", "/dbs", "/rollouts/php",
              "/rollouts/app", "/jobs/{jobId}"]:
        assert p in src, f"missing path {p}"


def test_server_maps_only_existing_scripts():
    src = open(os.path.join(API, "server.py")).read()
    src += open(os.path.join(API, "sh.py")).read()
    mods = os.path.join(REPO, "modules")
    for m in re.findall(r'"([\w-]+/scripts/[\w-]+\.sh)"', src):
        assert os.path.isfile(os.path.join(mods, m)), f"script missing: {m}"


def test_backend_subcommands_exist_in_sandpodctl():
    """Every sh.py SUBCOMMANDS value must be a real sandpodctl subcommand."""
    import re as _re
    sh_src = open(os.path.join(API, "sh.py")).read()
    main_src = open(os.path.join(REPO, "modules", "sandpodctl",
                                 "cmd", "sandpodctl", "main.go")).read()
    subs = _re.findall(r'"([\w-]+)":\s*"([\w-]+)"', sh_src.split("SUBCOMMANDS")[1].split("}")[0])
    assert subs, "SUBCOMMANDS map empty"
    for _logical, sub in subs:
        assert f'"{sub}"' in main_src, f"subcommand missing in Go dispatcher: {sub}"


def test_openapi_covers_remove_customer():
    src = open(os.path.join(API, "openapi.yaml")).read()
    assert "/customers/{id}" in src, "missing DELETE /customers/{id} (remove-customer.sh)"
    assert "remove-customer" in src or "Remove customer" in src


def test_delete_uses_query_not_body():
    src = open(os.path.join(API, "openapi.yaml")).read()
    assert "delete_data" in src, "DELETE must document ?delete_data query flag"
    assert "deleteData" not in src, "DELETE must not use body deleteData (query only)"


def test_server_uses_v1_prefix_and_query():
    srv = open(os.path.join(API, "server.py")).read()
    for p in ['"/v1/dbs"', '"/v1/rollouts/php"', '"/v1/rollouts/app"',
              '"/v1/host-nginx/reload"']:
        assert p in srv, f"server.py fallback missing {p} (must match app.py /v1 prefix)"
    assert '"/dbs"' not in srv.replace('"/v1/dbs"', ''), "stale non-/v1 /dbs route"
    assert "deleteData" not in srv, "server.py DELETE must use query flags, not body deleteData"


def test_no_panel_policy_in_api():
    src = open(os.path.join(API, "server.py")).read().lower()
    for kw in ["billing", "acme", "certbot", "listen 443"]:
        assert kw not in src, f"policy leak: {kw}"
