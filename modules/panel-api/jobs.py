"""In-memory async jobs. Daemon restart drops history — acceptable for prototype.

Long ops (provision/remove/backup/db/rollout/nginx) run here so HTTP
doesn't time out. Registry races are already guarded by flock inside the
scripts; this layer serialises duplicate provision per instance (409)
and chains gen+reload for host-nginx.

Secrets: job stdout may contain DB creds (create-site-db prints once).
We NEVER log stdout/stderr to the journal — audit() only sees
label/instance/exit. Panel must store creds in its vault; history is
in-memory and pruned.
"""
import re
import threading
import time
import uuid
try:
    from .sh import run_script
except ImportError:
    from sh import run_script  # type: ignore

_JOBS: dict = {}
_LOCK = threading.Lock()
_INSTANCE_BUSY: dict = {}  # instance -> jobId (queued/running)
_MAX_JOBS = 200
# Self-protection (scope: daemon-local, not per-site rate-limit/DDoS —
# that belongs to the edge team): bound concurrent queued/running jobs so
# one panel loop cannot fork unbounded threads/processes on the host.
_MAX_CONCURRENT = 10


def active_count() -> int:
    with _LOCK:
        return sum(1 for j in _JOBS.values()
                   if j.get("status") in ("queued", "running"))


def can_queue() -> bool:
    """True if a new async job may be queued without exceeding the cap."""
    return active_count() < _MAX_CONCURRENT


def _prune():
    if len(_JOBS) <= _MAX_JOBS:
        return
    done = [jid for jid, j in _JOBS.items() if j["status"] in ("done", "failed")]
    done.sort(key=lambda jid: _JOBS[jid].get("finishedAt", 0))
    for jid in done[: len(_JOBS) - _MAX_JOBS]:
        inst = _JOBS[jid].get("instance")
        if inst and _INSTANCE_BUSY.get(inst) == jid:
            continue  # still referenced as busy; keep
        del _JOBS[jid]


def _finish(jid, code, so, se):
    with _LOCK:
        j = _JOBS.get(jid)
        if not j:
            return
        j.update({"status": "done" if code == 0 else "failed",
                  "exitCode": code,
                  "stdout": so[-8000:], "stderr": se[-8000:],
                  "finishedAt": time.time()})
        inst = j.get("instance")
        if inst and _INSTANCE_BUSY.get(inst) == jid:
            del _INSTANCE_BUSY[inst]
        _prune()


def submit(label: str, script: str, args: list, timeout: int = 1800,
           instance: str | None = None) -> str:
    jid = uuid.uuid4().hex[:12]
    with _LOCK:
        _JOBS[jid] = {"jobId": jid, "label": label, "status": "queued",
                      "exitCode": None, "stdout": "", "stderr": "",
                      "instance": instance, "createdAt": time.time()}
        if instance:
            _INSTANCE_BUSY[instance] = jid

    def _run():
        with _LOCK:
            if jid in _JOBS:
                _JOBS[jid]["status"] = "running"
        code, so, se = run_script(script, args, timeout=timeout)
        _finish(jid, code, so, se)

    threading.Thread(target=_run, daemon=True).start()
    return jid


def submit_chain(label: str, steps: list[tuple[str, list]], timeout: int = 1800,
                 instance: str | None = None) -> str:
    """Run steps sequentially in ONE job; stop on first non-zero exit."""
    jid = uuid.uuid4().hex[:12]
    with _LOCK:
        _JOBS[jid] = {"jobId": jid, "label": label, "status": "queued",
                      "exitCode": None, "stdout": "", "stderr": "",
                      "instance": instance, "createdAt": time.time()}
        if instance:
            _INSTANCE_BUSY[instance] = jid

    def _run():
        with _LOCK:
            if jid in _JOBS:
                _JOBS[jid]["status"] = "running"
        out_s, out_e, code = [], [], 0
        for script, args in steps:
            c, so, se = run_script(script, args, timeout=timeout)
            out_s.append(f"### {script} exit={c}\n{so}")
            out_e.append(se)
            code = c
            if c != 0:
                break
        _finish(jid, code, "".join(out_s)[-8000:], "".join(out_e)[-8000:])

    threading.Thread(target=_run, daemon=True).start()
    return jid


def try_acquire(instance: str) -> bool:
    """Reserve instance if no queued/running job. Returns True + reserves.
    submit() with same instance overwrites the reservation with the jobId.
    dry_run paths must NOT call this."""
    with _LOCK:
        jid = _INSTANCE_BUSY.get(instance)
        if not jid:
            _INSTANCE_BUSY[instance] = "claimed"
            return True
        if jid == "claimed":
            return False
        j = _JOBS.get(jid)
        if not j or j["status"] in ("done", "failed"):
            _INSTANCE_BUSY[instance] = "claimed"
            return True
        return False


def release(instance: str):
    with _LOCK:
        if _INSTANCE_BUSY.get(instance) == "claimed":
            del _INSTANCE_BUSY[instance]


def get(jid: str):
    with _LOCK:
        j = _JOBS.get(jid)
        return dict(j) if j else None


def list_jobs():
    """Summaries for polling. DB-job stdout (creds) only via get(jobId)."""
    with _LOCK:
        return [{"jobId": j["jobId"], "label": j.get("label"),
                 "status": j["status"], "exitCode": j["exitCode"],
                 "instance": j.get("instance")}
                for j in _JOBS.values()]


def redact_preview(text: str) -> str:
    """Mask DB_PASS-like secrets if a preview is ever logged."""
    return re.sub(r"(?i)(pass\w*\s*[:=]\s*)(\S+)", r"\1***", text or "")[:200]


def clear():
    """Test hook only."""
    with _LOCK:
        _JOBS.clear()
        _INSTANCE_BUSY.clear()
