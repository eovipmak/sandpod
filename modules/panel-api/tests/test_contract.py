"""Repo-only contract tests: openapi paths, backend subcommands, policy leaks."""
import os
import re

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
API = os.path.join(REPO, "modules", "panel-api")


def test_openapi_paths_present():
    src = open(os.path.join(API, "openapi.yaml")).read()
    for p in ["/health", "/customers", "/customers/{id}/sites",
              "/customers/{id}/port-apps", "/dbs", "/rollouts/php",
              "/rollouts/app", "/jobs/{jobId}"]:
        assert p in src, f"missing path {p}"


def test_no_legacy_script_paths_in_backend():
    """sh.py must call sandpodctl only; the bash scripts are gone."""
    src = open(os.path.join(API, "sh.py")).read()
    assert not re.search(r"\.sh\b", src), "sh.py still references a legacy .sh script"
    assert "SANDPOD_BACKEND" not in src, "scripts rollback backend was removed"


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
    assert "/customers/{id}" in src, "missing DELETE /customers/{id} (remove-customer)"
    assert "remove-customer" in src or "Remove customer" in src


def test_delete_uses_query_not_body():
    src = open(os.path.join(API, "openapi.yaml")).read()
    assert "delete_data" in src, "DELETE must document ?delete_data query flag"
    assert "deleteData" not in src, "DELETE must not use body deleteData (query only)"


def test_no_panel_policy_in_api():
    src = open(os.path.join(API, "app.py")).read().lower()
    for kw in ["billing", "acme", "certbot", "listen 443"]:
        assert kw not in src, f"policy leak: {kw}"
