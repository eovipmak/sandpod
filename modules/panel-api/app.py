"""SandPod panel-api (FastAPI). Mechanism only — wraps the sandpodctl Go binary
(backend: sandpodctl, argv như script bash cũ).

Run as root on host, loopback-only. Panel (same host) calls 127.0.0.1:8741.
Auth: Bearer token file (0600). /v1/health is public for systemd/readiness.

Debug helpers:
  ?dry_run=1   -> validate + return cmd[] that WOULD run, no exec (works for
                  every mutating endpoint; panel devs don't need root data).
  X-Request-ID -> echoed back + in JSON error; logged with method/path/exit.
"""
import json
import logging
import os
import re
import time
import uuid
from typing import Literal

from fastapi import Depends, FastAPI, Header, HTTPException, Request
from fastapi.responses import JSONResponse
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer
from pydantic import BaseModel, Field, field_validator

try:  # package import (uvicorn panel-api.app:app)
    from . import jobs
    from .sh import build_cmd, exit_to_http, run_script
except ImportError:  # top-level import (pytest with sys.path)
    import jobs  # type: ignore[no-redef]
    from sh import build_cmd, exit_to_http, run_script  # type: ignore

log = logging.getLogger("sandpod.panel-api")
VERSION = "4.0.0"
TOKEN_FILE = os.environ.get("SANDPOD_TOKEN_FILE", "/etc/sandpod/panel-api.token")
REGISTRY = os.environ.get("SANDPOD_REGISTRY", "/etc/sandpod/customers.tsv")
# Self-protection: bound request bodies daemon-locally (loopback-only, one
# client). Per-site rate-limit/DDoS/WAF belongs to the edge team (out of
# scope per scope.md §3), so we only cap what the daemon itself parses.
MAX_BODY_BYTES = int(os.environ.get("SANDPOD_MAX_BODY_BYTES", "1048576"))


def _token_path() -> str:
    """Resolved at call time so tests can point it at a fixture via env."""
    return os.environ.get("SANDPOD_TOKEN_FILE", "/etc/sandpod/panel-api.token")


def token_file_mode_ok(path: str | None = None) -> bool:
    """Token file must be mode 0600 when present (creds protection)."""
    try:
        return (os.stat(path or _token_path()).st_mode & 0o777) == 0o600
    except OSError:
        return False


app = FastAPI(title="SandPod Panel API", version=VERSION,
              docs_url="/docs", openapi_url="/openapi.json")
auth_scheme = HTTPBearer(auto_error=False)


def load_token() -> str:
    try:
        with open(_token_path()) as fh:
            return fh.read().strip()
    except OSError:
        return ""  # dev/fixture without token; production must provision one


if os.environ.get("SANDPOD_REQUIRE_AUTH") == "1":
    # Fail-closed at boot: systemd also enforces this via ExecStartPre so a
    # mis-provisioned host never serves unauthenticated. Dev/fixture (env
    # unset) keeps the old fail-open behaviour for repo-only tests.
    if not load_token() or not token_file_mode_ok():
        raise RuntimeError(
            f"token file {TOKEN_FILE} missing or not mode 0600 "
            "(SANDPOD_REQUIRE_AUTH=1, fail-closed)")


def require_auth(cred: HTTPAuthorizationCredentials | None = Depends(auth_scheme)):
    want = load_token()
    strict = os.environ.get("SANDPOD_REQUIRE_AUTH") == "1"
    if not want:
        # Fail-closed when the daemon was told to require auth (production):
        # a missing/empty token file must deny, not serve open. Dev/fixture
        # without the env keeps the old fail-open behaviour.
        if strict:
            raise HTTPException(status_code=500, detail={
                "error": "token file missing (fail-closed)",
                "exitCode": 1, "stderr": ""})
        return
    # Fail-closed per request: a token file chmodded away from 0600 after
    # boot must stop being honoured.
    if strict and not token_file_mode_ok():
        raise HTTPException(status_code=500, detail={
            "error": "token file misconfigured (must be mode 0600)",
            "exitCode": 1, "stderr": ""})
    if not cred or cred.scheme.lower() != "bearer" or cred.credentials != want:
        raise HTTPException(status_code=401, detail="unauthorized")


def audit(request_id: str, method: str, path: str, code: int, detail: str = ""):
    log.info(json.dumps({"rid": request_id, "m": method, "p": path,
                         "code": code, "detail": detail[:200]}))


@app.middleware("http")
async def request_id_mw(request: Request, call_next):
    rid = request.headers.get("X-Request-ID") or uuid.uuid4().hex[:12]
    t0 = time.time()
    # Body-size cap (daemon self-protection; loopback-only so this is not a
    # WAF/rate-limit substitute — see scope.md §3). Header check is cheap;
    # chunked bodies without a trustworthy length are read (Starlette caches
    # the body, so downstream handlers still see it) and measured.
    try:
        cl = request.headers.get("content-length")
        if cl is not None and int(cl) > MAX_BODY_BYTES:
            return JSONResponse(status_code=413,
                                content={"error": "request body too large",
                                         "exitCode": 2, "stderr": "",
                                         "requestId": rid})
        te = request.headers.get("transfer-encoding", "")
        if "chunked" in te.lower() and cl is None:
            body = await request.body()
            if len(body) > MAX_BODY_BYTES:
                return JSONResponse(status_code=413,
                                    content={"error": "request body too large",
                                             "exitCode": 2, "stderr": "",
                                             "requestId": rid})
    except ValueError:
        pass
    try:
        resp = await call_next(request)
    except Exception:
        log.exception(json.dumps({"rid": rid, "p": request.url.path, "err": "unhandled"}))
        return JSONResponse(status_code=500,
                            content={"error": "internal", "exitCode": 1,
                                     "stderr": "", "requestId": rid})
    resp.headers["X-Request-ID"] = rid
    audit(rid, request.method, request.url.path, resp.status_code,
          f"{(time.time() - t0) * 1000:.0f}ms")
    return resp


def acquire_or_409(instance: str | None, busy_msg: str):
    """Atomic cap + per-instance reservation. Raises 409 with Spec v4 shape.

    'busy' -> instance already has a queued/running job; 'full' -> global
    cap reached. On 'ok' the instance is reserved ("claimed"); the caller
    must submit the job (submit overwrites with the jobId) without an
    intervening await that could throw (submit itself releases on failure).
    """
    st = jobs.acquire(instance)
    if st == "ok":
        return
    if st == "busy":
        raise HTTPException(status_code=409, detail={
            "error": busy_msg, "exitCode": 1, "stderr": ""})
    raise HTTPException(status_code=409, detail={
        "error": f"too many jobs running (cap {jobs._MAX_CONCURRENT}), "
                 "retry after polling GET /v1/jobs",
        "exitCode": 1, "stderr": ""})


# ---------- models (mirror openapi.yaml) ----------

class ProvisionRequest(BaseModel):
    instance: str = Field(pattern=r"^[0-9]{3}$")
    serviceUser: str | None = None
    webPort: int | None = None
    sshPort: int | None = None
    image: str | None = None
    quotaGb: int | None = None
    cpus: str | None = None
    memory: str | None = None
    ioWeight: int | None = None


class AddSiteRequest(BaseModel):
    domain: str
    php: Literal["8.2", "8.3", "8.4", "8.5", "8.6"] = "8.3"
    primary: bool = False


class SetPhpRequest(BaseModel):
    php: Literal["8.2", "8.3", "8.4", "8.5", "8.6"]


class DeleteOpts(BaseModel):
    deleteData: bool = False


class RemoveCustomerOpts(BaseModel):
    deleteData: bool = False
    deleteUser: bool = False
    deleteRegistryEntry: bool = False


class AddPortAppRequest(BaseModel):
    domain: str
    port: int  # validated in handler -> 400 (plan: valid_port lỗi → 400)
    command: str
    primary: bool = False


class ReloadRequest(BaseModel):
    target: str  # php-8.3 | app-<domain>

    @field_validator("target")
    @classmethod
    def check_target(cls, v: str):
        if v.startswith("php-") or v.startswith("app-"):
            return v
        raise ValueError("target must be php-8.x or app-<domain>")


class SshKeyRequest(BaseModel):
    publicKey: str

    @field_validator("publicKey")
    @classmethod
    def check_key(cls, v: str):
        # Whitelist known key types + base64 payload (panel owns who gets
        # access; API only rejects obvious garbage before exec → 422 shape
        # error here, script stays authoritative).
        if (len(v) < 4096 and "\n" not in v and re.match(
                r"^ssh-(ed25519|rsa|ecdsa-sha2-nistp256|ecdsa-sha2-nistp384|"
                r"ecdsa-sha2-nistp521|dss) [A-Za-z0-9+/=]+( .*)?$", v)):
            return v
        raise ValueError("bad publicKey")


class DbRequest(BaseModel):
    dbName: str = Field(pattern=r"^[A-Za-z0-9_]+$", max_length=64)
    dbUser: str = Field(pattern=r"^[A-Za-z0-9_]+$", max_length=64)


class RolloutRequest(BaseModel):
    batchSize: int = 5
    sleepSeconds: int = 10
    skipUpdate: bool = False


class Job(BaseModel):
    jobId: str
    status: str
    exitCode: int | None = None
    stdout: str = ""
    stderr: str = ""


class Ok(BaseModel):
    ok: bool
    stdout: str = ""
    stderr: str = ""
    warnings: list[str] = []
    requestId: str = ""


# ---------- P2 helpers: validation + warnings, single source ----------

DOMAIN_RE = re.compile(
    r"^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$")
PHP_MINORS = ("8.2", "8.3", "8.4", "8.5", "8.6")


def check_domain(raw: str) -> str:
    """Mirror lib.sh valid_domain. Script stays authoritative; this gives
    fast 400 without exec. Returns lowercased domain."""
    d = (raw or "").lower()
    if not DOMAIN_RE.match(d):
        raise HTTPException(status_code=400, detail={
            "error": f"invalid domain: {raw}", "exitCode": 2, "stderr": ""})
    return d


def check_port(port: int) -> int:
    if not isinstance(port, int) or not (3000 <= port <= 3999):
        raise HTTPException(status_code=400, detail={
            "error": f"invalid port {port} (want 3000-3999)",
            "exitCode": 2, "stderr": ""})
    return port


def check_command(cmd: str) -> str:
    if not (cmd or "").strip():
        raise HTTPException(status_code=400, detail={
            "error": "missing --command", "exitCode": 2, "stderr": ""})
    if "\n" in cmd:
        raise HTTPException(status_code=400, detail={
            "error": "command must be a single line", "exitCode": 2, "stderr": ""})
    # Shell metachars are allowed on purpose: the command runs as `customer`
    # inside the container (never as root on the host) and the panel may
    # legitimately wrap it (e.g. `/bin/sh -l -c '...'`). Only bound length.
    if len(cmd) > 8192:
        raise HTTPException(status_code=400, detail={
            "error": "command too long (max 8192)", "exitCode": 2, "stderr": ""})
    return cmd


def warnings_from(stderr: str) -> list[str]:
    """EOL/policy warns passthrough as structured field, never an error."""
    out = []
    for line in (stderr or "").splitlines():
        if "WARNING" in line:
            out.append(line.strip()[:500])
    return out


def ok_result(stdout: str, stderr: str, request: Request) -> Ok:
    return Ok(ok=True, stdout=stdout, stderr=stderr,
             warnings=warnings_from(stderr),
             requestId=request.headers.get("X-Request-ID", ""))


def raise_script_error(op: str, code: int, stdout: str, stderr: str):
    raise HTTPException(status_code=exit_to_http(code), detail={
        "error": f"{op} failed", "exitCode": code,
        "stdout": stdout, "stderr": stderr,
        "warnings": warnings_from(stderr)})


# ---------- helpers ----------

def get_customers() -> list[dict]:
    out = []
    try:
        with open(os.environ.get("SANDPOD_REGISTRY", REGISTRY)) as fh:
            for line in fh:
                line = line.rstrip("\n")
                if not line or line.lstrip().startswith("#"):
                    continue
                parts = line.split("\t")
                if len(parts) < 6:
                    continue
                inst, user, cont, web, ssh, img = parts[:6]
                out.append({"instance": inst, "serviceUser": user,
                            "container": cont,
                            "webPort": int(web) if web.isdigit() else web,
                            "sshPort": int(ssh) if ssh.isdigit() else ssh,
                            "image": img})
    except OSError:
        pass
    return out


def lookup(instance: str) -> dict:
    for c in get_customers():
        if c["instance"] == instance:
            return c
    raise HTTPException(status_code=404, detail={
        "error": "unknown instance", "exitCode": 2, "stderr": ""})


def dry(cmd: list, request: Request):
    """?dry_run=1: return cmd without exec. Returns dict or None."""
    from fastapi import Request as _R  # noqa (type hint only)
    return {"dryRun": True, "cmd": cmd,
            "requestId": request.headers.get("X-Request-ID", "")}


def script_or_500(name: str, args: list, request: Request, dry_run: bool):
    """Legacy helper: dry-run short-circuit, else None (caller runs once)."""
    if dry_run:
        return dry(build_cmd(name, args), request)
    return None


def validate_provision(req: ProvisionRequest) -> str | None:
    """Mirror provision-customer.sh validation for fail-fast 400 (no queue)."""
    if req.serviceUser is not None and str(req.serviceUser) != "":
        if not re.match(r"^[a-z_][a-z0-9_-]*\$?$",
                        str(req.serviceUser)):
            return (f"invalid --service-user {req.serviceUser} "
                    "(mirror lib.sh: lowercase, digits/_/-)")
    if req.image is not None and str(req.image).strip() == "":
        return "invalid --image (must be non-empty tag)"
    if req.cpus is not None:
        if not re.match(r"^[0-9]+(\.[0-9]+)?$", str(req.cpus)):
            return f"invalid --cpus {req.cpus} (e.g. 0.5, 1.0, 2)"
        try:
            pct = int(float(str(req.cpus)) * 100)
        except ValueError:
            return f"invalid --cpus {req.cpus}"
        if not (10 <= pct <= 1600):
            return f"invalid --cpus {req.cpus} (want 0.1-16)"
    if req.memory is not None:
        if not re.match(r"^[0-9]+[kKmMgG]?$", str(req.memory)):
            return f"invalid --memory {req.memory} (e.g. 512m, 1g, 2g)"
        if int(re.sub(r"[^0-9]", "", str(req.memory)) or 0) <= 0:
            return f"invalid --memory {req.memory}"
    if req.ioWeight is not None:
        try:
            w = int(req.ioWeight)  # type: ignore[arg-type]
        except (TypeError, ValueError):
            return f"invalid --io-weight {req.ioWeight} (want 10-1000)"
        if not (10 <= w <= 1000):
            return f"invalid --io-weight {req.ioWeight} (want 10-1000)"
    if req.quotaGb is not None:
        try:
            q = int(req.quotaGb)  # type: ignore[arg-type]
        except (TypeError, ValueError):
            return f"invalid --quota-gb {req.quotaGb}"
        if not (1 <= q <= 1000):
            return f"invalid --quota-gb {req.quotaGb} (want 1-1000)"
    for p, name in ((req.webPort, "web-port"), (req.sshPort, "ssh-port")):
        if p is not None:
            try:
                pi = int(p)  # type: ignore[arg-type]
            except (TypeError, ValueError):
                return f"invalid --{name} {p} (want numeric)"
            if not (1 <= pi <= 65535):
                return f"invalid --{name} {p} (want 1-65535)"
    if (req.webPort is not None and req.sshPort is not None
            and int(req.webPort) == int(req.sshPort)):
        return "web and ssh ports must differ"
    return None


# ---------- routes ----------

@app.get("/v1/health")
def health():
    return {"ok": True, "version": VERSION}


@app.get("/v1/customers", dependencies=[Depends(require_auth)])
def list_customers():
    return get_customers()


@app.post("/v1/customers", status_code=202, dependencies=[Depends(require_auth)])
def provision(req: ProvisionRequest, request: Request, dry_run: bool = False):
    err = validate_provision(req)
    if err:
        raise HTTPException(status_code=400, detail={
            "error": err, "exitCode": 2, "stderr": ""})
    args = [req.instance]
    for val, flag in ((req.serviceUser, "--service-user"), (req.webPort, "--web-port"),
                      (req.sshPort, "--ssh-port"), (req.image, "--image"),
                      (req.quotaGb, "--quota-gb"), (req.cpus, "--cpus"),
                      (req.memory, "--memory"), (req.ioWeight, "--io-weight")):
        if val not in (None, ""):
            args += [flag, str(val)]
    if dry_run:
        return {"dryRun": True, "cmd": build_cmd("provision", args)}
    acquire_or_409(req.instance, f"instance {req.instance} busy (job running)")
    return Job(jobId=jobs.submit("provision", "provision", args,
                                 instance=req.instance),
               status="queued")


@app.delete("/v1/customers/{iid}", status_code=202, dependencies=[Depends(require_auth)])
def remove_customer(iid: str, dry_run: bool = False,
                    delete_data: bool = False, delete_user: bool = False,
                    delete_registry_entry: bool = False):
    """Query flags (DELETE-with-body is unreliable across clients)."""
    if not iid.isdigit() or len(iid) != 3:
        raise HTTPException(status_code=400, detail={
            "error": "instance must be 3 digits", "exitCode": 2, "stderr": ""})
    if delete_user and not delete_data:
        raise HTTPException(status_code=400, detail={
            "error": "--delete-user requires --delete-data (refused)",
            "exitCode": 2, "stderr": ""})
    args = [iid]
    if delete_data:
        args.append("--delete-data")
    if delete_user:
        args.append("--delete-user")
    if delete_registry_entry:
        args.append("--delete-registry-entry")
    if dry_run:
        return {"dryRun": True, "cmd": build_cmd("remove", args)}
    acquire_or_409(iid, f"instance {iid} busy (job running)")
    return Job(jobId=jobs.submit("remove", "remove", args, instance=iid),
               status="queued")


@app.get("/v1/customers/{iid}/sites", dependencies=[Depends(require_auth)])
def list_sites(iid: str):
    c = lookup(iid)
    code, so, se = run_script("list_sites", [c["serviceUser"], c["container"], "--json"])
    if code != 0:
        raise HTTPException(status_code=exit_to_http(code),
                            detail={"error": "list-sites failed", "exitCode": code, "stderr": se})
    try:
        return json.loads(so or "{}")
    except ValueError:
        raise HTTPException(status_code=500, detail={
            "error": "bad script JSON", "exitCode": 1, "stderr": ""})


@app.post("/v1/customers/{iid}/sites", dependencies=[Depends(require_auth)])
def add_site(iid: str, req: AddSiteRequest, request: Request, dry_run: bool = False):
    c = lookup(iid)
    domain = check_domain(req.domain)
    args = [c["serviceUser"], c["container"], domain, "--php", req.php]
    if req.primary:
        args.append("--primary")
    r = script_or_500("add_site", args, request, dry_run)
    if r:
        return r
    code, so, se = run_script("add_site", args)
    if code != 0:
        raise_script_error("add-site", code, so, se)
    return ok_result(so, se, request)


@app.patch("/v1/customers/{iid}/sites/{domain}", dependencies=[Depends(require_auth)])
def set_php(iid: str, domain: str, req: SetPhpRequest, request: Request,
            dry_run: bool = False):
    c = lookup(iid)
    d = check_domain(domain)
    args = [c["serviceUser"], c["container"], d, req.php]
    r = script_or_500("set_php", args, request, dry_run)
    if r:
        return r
    code, so, se = run_script("set_php", args)
    if code != 0:
        raise_script_error("set-php", code, so, se)
    return ok_result(so, se, request)


@app.delete("/v1/customers/{iid}/sites/{domain}", dependencies=[Depends(require_auth)])
def delete_site(iid: str, domain: str, request: Request, dry_run: bool = False,
                delete_data: bool = False):
    """?delete_data=true appends --delete-data (query, not body)."""
    c = lookup(iid)
    d = check_domain(domain)
    args = [c["serviceUser"], c["container"], d]
    if delete_data:
        args.append("--delete-data")
    r = script_or_500("remove_site", args, request, dry_run)
    if r:
        return r
    code, so, se = run_script("remove_site", args)
    if code != 0:
        raise_script_error("remove-site", code, so, se)
    return ok_result(so, se, request)


@app.post("/v1/customers/{iid}/port-apps", dependencies=[Depends(require_auth)])
def add_port_app(iid: str, req: AddPortAppRequest, request: Request,
                 dry_run: bool = False):
    c = lookup(iid)
    d = check_domain(req.domain)
    p = check_port(req.port)
    cmd = check_command(req.command)
    args = [c["serviceUser"], c["container"], d,
            "--port", str(p), "--command", cmd]
    if req.primary:
        args.append("--primary")
    r = script_or_500("add_port", args, request, dry_run)
    if r:
        return r
    code, so, se = run_script("add_port", args)
    if code != 0:
        raise_script_error("add-port-app", code, so, se)
    return ok_result(so, se, request)


@app.delete("/v1/customers/{iid}/port-apps/{domain}",
            dependencies=[Depends(require_auth)])
def delete_port_app(iid: str, domain: str, request: Request, dry_run: bool = False,
                    delete_data: bool = False):
    c = lookup(iid)
    d = check_domain(domain)
    args = [c["serviceUser"], c["container"], d]
    if delete_data:
        args.append("--delete-data")
    r = script_or_500("remove_port", args, request, dry_run)
    if r:
        return r
    code, so, se = run_script("remove_port", args)
    if code != 0:
        raise_script_error("remove-port-app", code, so, se)
    return ok_result(so, se, request)


@app.post("/v1/customers/{iid}/reload", dependencies=[Depends(require_auth)])
def reload(iid: str, req: ReloadRequest, request: Request, dry_run: bool = False):
    c = lookup(iid)
    if req.target.startswith("php-"):
        minor = req.target[4:]
        if minor not in PHP_MINORS:
            raise HTTPException(status_code=400, detail={
                "error": f"unsupported php minor {minor}", "exitCode": 2, "stderr": ""})
        name, args = "reload_php", [c["serviceUser"], c["container"], minor]
    else:
        d = check_domain(req.target[4:])
        name, args = "reload_app", [c["serviceUser"], c["container"], d]
    r = script_or_500(name, args, request, dry_run)
    if r:
        return r
    code, so, se = run_script(name, args)
    if code != 0:
        raise_script_error("reload", code, so, se)
    return ok_result(so, se, request)


@app.get("/v1/customers/{iid}/resources", dependencies=[Depends(require_auth)])
def resources(iid: str):
    lookup(iid)
    code, so, se = run_script("report", ["--registry",
                             os.environ.get("SANDPOD_REGISTRY", REGISTRY), "--json"])
    if code != 0:
        raise HTTPException(status_code=exit_to_http(code), detail={
            "error": "report failed", "exitCode": code, "stderr": se})
    try:
        arr = json.loads(so or "[]")
    except ValueError:
        raise HTTPException(status_code=500, detail={
            "error": "bad report JSON", "exitCode": 1, "stderr": ""})
    hit = next((r for r in arr if r.get("instance") == iid), None)
    if hit is None:
        raise HTTPException(status_code=404, detail={
            "error": "no report for instance", "exitCode": 1, "stderr": se})
    return hit


@app.post("/v1/customers/{iid}/ssh-keys", status_code=202,
          dependencies=[Depends(require_auth)])
def add_ssh_key(iid: str, req: SshKeyRequest, dry_run: bool = False):
    import tempfile
    lookup(iid)
    if dry_run:
        return {"dryRun": True,
                "cmd": build_cmd("add_key", ["<user>", "<container>", "<pubkey-file>"])}
    acquire_or_409(f"ssh-{iid}", f"instance {iid} busy (job running)")
    c = lookup(iid)
    kf = None
    try:
        with tempfile.NamedTemporaryFile("w", suffix=".pub", delete=False) as fh:
            fh.write(req.publicKey + "\n")
            kf = fh.name
        return Job(jobId=jobs.submit("ssh-key", "add_key",
                                     [c["serviceUser"], c["container"], kf],
                                     instance=f"ssh-{iid}",
                                     cleanup=[kf]),
                   status="queued")
    except Exception:
        # No stale reservation or temp key on failure (no permanent 409,
        # no /tmp/*.pub leak: the file is unlinked after the job finishes,
        # or here if submit itself throws).
        if kf is not None:
            try:
                os.unlink(kf)
            except OSError:
                pass
        jobs.release(f"ssh-{iid}")
        raise


@app.post("/v1/customers/{iid}/backups", status_code=202,
          dependencies=[Depends(require_auth)])
def backup(iid: str, dry_run: bool = False):
    lookup(iid)
    if dry_run:
        return {"dryRun": True, "cmd": build_cmd("backup", [iid])}
    acquire_or_409(f"backup-{iid}", f"backup for {iid} already running")
    # sandpodctl backup-customer prints the tar path on stdout (last line).
    return Job(jobId=jobs.submit("backup", "backup", [iid],
                                 instance=f"backup-{iid}"),
               status="queued")


@app.post("/v1/dbs", status_code=202, dependencies=[Depends(require_auth)])
def create_db(req: DbRequest, dry_run: bool = False):
    if dry_run:
        return {"dryRun": True, "cmd": build_cmd("mkdb", [req.dbName, req.dbUser])}
    acquire_or_409(None, "")
    # Creds are in job stdout EXACTLY once; in-memory only, never logged.
    # Panel must store them in its vault on first poll, then treat as secret.
    return Job(jobId=jobs.submit("mkdb", "mkdb", [req.dbName, req.dbUser]),
               status="queued")


@app.post("/v1/host-nginx/reload", status_code=202,
          dependencies=[Depends(require_auth)])
def reload_nginx(dry_run: bool = False):
    steps = [("gen_nginx", []), ("reload_nginx", [])]
    if dry_run:
        return {"dryRun": True,
                "cmds": [build_cmd(n, a) for n, a in steps]}
    acquire_or_409("host-nginx", "host-nginx reload already running")
    return Job(jobId=jobs.submit_chain("host-nginx", steps,
                                       instance="host-nginx"),
               status="queued")


def _rollout(kind: str, req: RolloutRequest, dry_run: bool):
    try:
        batch = int(req.batchSize) if req.batchSize is not None else 5
        sleep_s = int(req.sleepSeconds) if req.sleepSeconds is not None else 10
    except (TypeError, ValueError):
        raise HTTPException(status_code=400, detail={
            "error": "batchSize/sleepSeconds must be integers",
            "exitCode": 2, "stderr": ""})
    if batch < 1:
        raise HTTPException(status_code=400, detail={
            "error": "batchSize must be >= 1", "exitCode": 2, "stderr": ""})
    if sleep_s < 0:
        raise HTTPException(status_code=400, detail={
            "error": "sleepSeconds must be >= 0", "exitCode": 2, "stderr": ""})
    args = ["--batch-size", str(batch), "--sleep-seconds", str(sleep_s)]
    if req.skipUpdate:
        args.append("--skip-update")
    name = "rollout_php" if kind == "php" else "rollout_app"
    if dry_run:
        return {"dryRun": True, "cmd": build_cmd(name, args)}
    acquire_or_409("fleet",
                   "fleet rollout already running (fix forward, re-run after)")
    return Job(jobId=jobs.submit("rollout", name, args, instance="fleet",
                                 timeout=7200),
               status="queued")


@app.post("/v1/rollouts/php", status_code=202, dependencies=[Depends(require_auth)])
def rollout_php(req: RolloutRequest, dry_run: bool = False):
    return _rollout("php", req, dry_run)


@app.post("/v1/rollouts/app", status_code=202, dependencies=[Depends(require_auth)])
def rollout_app(req: RolloutRequest, dry_run: bool = False):
    return _rollout("app", req, dry_run)


@app.get("/v1/jobs", dependencies=[Depends(require_auth)])
def list_jobs():
    """Summaries only (no stdout — DB creds stay in get(jobId) until pruned)."""
    return jobs.list_jobs()


@app.get("/v1/jobs/{jid}", dependencies=[Depends(require_auth)])
def get_job(jid: str):
    j = jobs.get(jid)
    if not j:
        raise HTTPException(status_code=404, detail={
            "error": "unknown job", "exitCode": 2, "stderr": ""})
    return j


@app.get("/openapi-sync-check", dependencies=[Depends(require_auth)])
def sync_check():
    """Dev hook: paths FastAPI serve vs openapi.yaml contract (debug only)."""
    served = sorted({r.path for r in app.routes if getattr(r, "path", "").startswith("/v1")})
    return {"served": served, "version": VERSION}
