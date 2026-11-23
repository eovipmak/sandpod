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
    mods = os.path.join(REPO, "modules")
    for m in re.findall(r'"([\w-]+/scripts/[\w-]+\.sh)"', src):
        assert os.path.isfile(os.path.join(mods, m)), f"script missing: {m}"


def test_no_panel_policy_in_api():
    src = open(os.path.join(API, "server.py")).read().lower()
    for kw in ["billing", "acme", "certbot", "listen 443"]:
        assert kw not in src, f"policy leak: {kw}"
