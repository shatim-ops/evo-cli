"""Check Container Apps in the owner-isolated project approved through the workflow environment.

The candidate's DELETE is addressed by project and name. Verify the original
create receipt against the current app immediately before invoking the candidate.
"""

import http.client
import json
import os
import re
import subprocess
import tempfile
import time
from pathlib import Path
from urllib.parse import urlencode
from uuid import UUID

import common

MODES = ("mcp", "cli")
WINDOW = 30 * 60
IMAGE = re.compile(r"[a-z0-9][a-z0-9-]*\.cr\.cloud\.ru/(?:[a-z0-9][a-z0-9._-]*/)*[a-z0-9][a-z0-9._-]*@sha256:[0-9a-f]{64}")


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def uid(value):
    try:
        return isinstance(value, str) and UUID(value).int != 0 and str(UUID(value)) == value
    except ValueError:
        return False


def preflight():
    project, run_id = os.environ["CLOUDRU_PROJECT_ID"], os.environ["P90_SOURCE_RUN"]
    require(uid(project) and re.fullmatch(r"[1-9][0-9]{0,19}", run_id), "invalid CA scope")
    return project, run_id


def settings(image=None):
    project, run_id = preflight()
    if image is None:
        import registry
        image = registry.image()
    require(IMAGE.fullmatch(image), "existing reviewed registry digest required")
    require(os.path.isfile(os.environ["P90_CA_BIN"]) and os.access(os.environ["P90_CA_BIN"], os.X_OK),
            "reviewed candidate CA binary required")
    return project, run_id, image


def app_name(run_id, mode):
    return f"p90-ca-{run_id}-{mode}"


def marker(name):
    return f"{name}-{common.CANDIDATE[:12]}"


def verify_image(project, image):
    """Inspect a digest in an existing registry of this project; never push or pull layers."""
    registry = image.split(".", 1)[0]
    rows = common.call_evo("ar", "list", "--project-id", project, "--json", timeout=35)["registries"]
    matches = [row for row in rows if row.get("name") == registry and row.get("type") == "DOCKER"
               and row.get("status") == "ACTIVE" and uid(row.get("id"))]
    require(len(matches) == 1, "existing project registry not verified")
    with tempfile.TemporaryDirectory(dir=os.environ["RUNNER_TEMP"]) as config:
        # DOCKER_CONFIG also confines the temporary registry credential.
        import registry as owned_registry
        env = dict(owned_registry.local_env(), DOCKER_CONFIG=config)
        host = image.split("/", 1)[0]
        login = subprocess.run(["docker", "login", host, "-u", os.environ["CLOUDRU_KEY_ID"], "--password-stdin"],
                               env=env, input=os.environ["CLOUDRU_SECRET"].encode(), capture_output=True, timeout=25)
        require(login.returncode == 0, "registry authentication failed")
        result = subprocess.run(["docker", "manifest", "inspect", "--verbose", image],
                                env=env, capture_output=True, timeout=30)
        require(result.returncode == 0, "approved image unavailable")
        manifest = json.loads(result.stdout)
        descriptor = manifest.get("Descriptor", {}) if isinstance(manifest, dict) else {}
        platform = descriptor.get("platform", {})
        require(descriptor.get("digest") == image.rsplit("@", 1)[1] and platform.get("os") == "linux"
                and platform.get("architecture") == "amd64", "single reviewed linux/amd64 manifest required")


def ca_api(method, name, project, deadline, body=None):
    """One physical HTTP/1.1 request. No redirects, automatic retries or raw output."""
    require((method == "POST" and not name and body) or
            (method == "GET" and re.fullmatch(r"p90-ca-[1-9][0-9]{0,19}-(cli|mcp)", name) and body is None),
            "CA request outside reviewed surface")
    bearer = common.cloud_auth()
    remaining = deadline - time.time()
    require(remaining > 0, "CA deadline passed")
    connection = http.client.HTTPSConnection("containers.api.cloud.ru", timeout=min(15, remaining))
    path = "/v2/containers" + ("/" + name if name else "") + "?" + urlencode({"projectId": project})
    try:
        connection.request(method, path, None if body is None else json.dumps(body).encode(),
                           {"Authorization": "Bearer " + bearer, "Content-Type": "application/json",
                            "Accept-Encoding": "identity", "Connection": "close"})
        response = connection.getresponse()
        raw = response.read(524289)
        size = response.getheader("Content-Length")
        require(len(raw) <= 524288 and (size is None or size.isdecimal() and int(size) == len(raw))
                and response.getheader("Content-Range") is None
                and response.getheader("Content-Encoding", "identity").lower() == "identity", "incomplete CA response")
        return response.status, json.loads(raw, object_pairs_hook=common.unique_keys)
    except (OSError, ValueError, http.client.HTTPException):
        raise RuntimeError("CA response incomplete; outcome unknown") from None
    finally:
        connection.close()


def absent(status, value):
    if not isinstance(value, dict):
        return False
    message = value.get("message")
    if status == 499:
        return message == "Serverless with name is not found"
    return (status == 404 and value.get("code") in (None, 5, "NOT_FOUND")
            and message in ("container not found", "Serverless with name is not found"))


def identity(receipt, value):
    return (isinstance(value, dict) and value.get("id") == receipt["resource_id"]
            and value.get("projectId") == receipt["project"] and value.get("name") == receipt["name"]
            and value.get("description") == marker(receipt["name"]))


def input_body(receipt):
    name = receipt["name"]
    return {"projectId": receipt["project"], "name": name, "description": marker(name),
            "configuration": {"ingress": {"publiclyAccessible": True}, "privileged": False},
            "template": {"protocol": "http_1", "scaling": {"minInstanceCount": 0, "maxInstanceCount": 1},
                         "containers": [{"name": name, "image": receipt["image"], "containerPort": 8080,
                                         "resources": {"cpu": "0.1", "memory": "256Mi"}}]}}


def candidate(mode, operation, rec, wire, timeout):
    """Run the reviewed program with the ordinary CLI or JSON-RPC stdio frontend."""
    name = rec["name"]
    env = dict(os.environ, P90_CA_NAME=name, P90_CA_DEADLINE=str(rec["until"]), P90_CA_WIRE=wire)
    command = [os.environ["P90_CA_BIN"], "--project-id", rec["project"]]
    if mode == "cli":
        command.extend(["ca", operation, name, "--json"])
        if operation == "delete":
            command.extend(["--wait=true", "--yes"])
        data = None
    else:
        command.append("mcp")
        args = {"name": name}
        if operation == "delete":
            args.update(wait=True, confirmed=True)
        messages = [
            {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
                "protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "patch-e2e", "version": "1"}}},
            {"jsonrpc": "2.0", "method": "notifications/initialized"},
            {"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": "ca_" + operation, "arguments": args}},
        ]
        data = "".join(json.dumps(msg) + "\n" for msg in messages).encode()
    try:
        result = subprocess.run(command, input=data, capture_output=True, env=env, timeout=timeout, check=False)
        if mode == "cli":
            if result.returncode and operation == "get":
                return None   # Independent exact GET below must establish the actual outcome.
            require(result.returncode == 0, "candidate CLI refused the call")
            return json.loads(result.stdout)
        require(result.returncode == 0, "candidate MCP transport exited")
        replies = [json.loads(line) for line in result.stdout.splitlines() if line.strip()]
        calls = [r["result"] for r in replies if r.get("id") == 2 and "result" in r]
        require(len(calls) == 1, "missing candidate MCP response")
        if calls[0].get("isError") and operation == "get":
            return None
        require(not calls[0].get("isError"), "candidate MCP refused the call")
        return json.loads(calls[0]["content"][0]["text"])
    except (OSError, KeyError, IndexError, TypeError, ValueError, subprocess.TimeoutExpired):
        raise RuntimeError("candidate CA response unavailable") from None


def observe_created(rec):
    until = min(time.time() + 180, rec["until"])
    while time.time() < until:
        code, value = ca_api("GET", rec["name"], rec["project"], until)
        if code == 200:
            require(identity(rec, value), "CA GET does not match the direct create receipt")
            return
        require(absent(code, value), "CA creation could not be read")
        time.sleep(min(3, max(0, until - time.time())))
    raise RuntimeError("created CA identity did not become visible")


def note(key, **fields):
    common.note("container_app", key, **fields)


def check_receipt(key, rec):
    project, run_id, image = settings(rec.get("image"))
    mode = key.removeprefix("ca-")
    require(mode in MODES and rec.get("kind") == "container_app" and rec.get("project") == project
            and rec.get("name") == app_name(run_id, mode) and rec.get("image") == image
            and uid(rec.get("resource_id")) and uid(rec.get("create_operation"))
            and isinstance(rec.get("start"), int) and rec.get("until") == rec["start"] + WINDOW,
            "unresolved CA create receipt; no delete authorized")
    return mode


def read_after_delete(key, rec):
    # A timed-out delete is never resent, including after runner loss.
    until = time.time() + 120
    while time.time() < until:
        code, value = ca_api("GET", rec["name"], rec["project"], until)
        if absent(code, value):
            listing = common.call_evo("ca", "list", "--project-id", rec["project"], "--json",
                                      timeout=max(1, min(25, until - time.time())))
            rows = listing.get("containers")
            require(isinstance(rows, list) and all(isinstance(item, dict) and uid(item.get("id"))
                    and isinstance(item.get("name"), str) and item["name"]
                    and item["id"] != rec["resource_id"] and item["name"] != rec["name"] for item in rows),
                    "CA list does not confirm disappearance of this identity")
            note(key, status="gone", gone_at=int(time.time()), read_status=code, original_uid_absent=True)
            return
        require(code == 200 and identity(rec, value), "CA identity changed or unknown; possible residue")
        time.sleep(min(3, max(0, until - time.time())))
    raise RuntimeError("CA readback incomplete; possible residue")


def finish(key, rec):
    mode = check_receipt(key, rec)
    if rec["status"] == "gone":
        require(rec.get("original_uid_absent") is True, "incomplete CA disappearance receipt")
        return
    if rec["status"] in ("delete_intent", "delete_response"):
        read_after_delete(key, rec)
        return
    require(rec["status"] == "created", "unrecognized CA receipt")
    require(time.time() >= rec["start"], "CA cleanup window has not started")
    observe_created(rec)
    budget = 45 if mode == "mcp" else 120
    require(rec["until"] - time.time() > budget + 30, "insufficient CA cleanup window")
    note(key, status="delete_intent", surface=mode)
    with tempfile.TemporaryDirectory(dir=os.environ["RUNNER_TEMP"]) as directory:
        wire = str(Path(directory) / "delete-once")
        # A remote receipt write precedes this final check. A changed identity
        # leaves the intent visible for recovery without sending a DELETE.
        code, value = ca_api("GET", rec["name"], rec["project"], rec["until"])
        require(code == 200 and identity(rec, value), "CA identity changed before delete")
        result = candidate(mode, "delete", rec, wire, budget + 25)
        end = int(time.time() * 1000)
        rows = Path(wire).read_text(encoding="ascii").splitlines()
        require(len(rows) == 2 and re.fullmatch(r"attempt [0-9]{13}", rows[0])
                and re.fullmatch(r"200 [0-9]{13}", rows[1]), "one accepted CA DELETE was not observed")
        accepted = int(rows[1].split()[1])
        require(isinstance(result, dict) and result.get("name") == rec["name"]
                and result.get("status") in ("deleted", "deleting")
                and uid(result.get("operation_id")), "candidate CA delete result incomplete")
        continued = result["status"] == "deleting"
        require(not continued or (result.get("next_tool") == "ca_get"
                and result.get("next_args") == {"name": rec["name"]}), "CA continuation changed")
        require(0 <= end - accepted <= (budget + 5) * 1000, "CA wait exceeded its budget")
        note(key, status="delete_response", delete_operation=result["operation_id"], candidate_status=result["status"],
             physical_deletes=1, accepted_to_response_ms=end - accepted, continuation_offered=continued)
    if continued:
        get = candidate(mode, "get", rec, "", 25)
        require(get is None or identity(rec, get), "candidate CA continuation reached a different identity")
    read_after_delete(key, rec)


def create(key, mode, project, run_id, image):
    require(key not in common.ledger(), "CA creation already attempted")
    start = int(time.time())
    rec = {"kind": "container_app", "start": start, "until": start + WINDOW, "project": project,
           "name": app_name(run_id, mode), "image": image, "status": "create_intent"}
    code, existing = ca_api("GET", rec["name"], project, rec["until"])
    require(absent(code, existing), "unused CA identity unconfirmed")
    note(key, **{k: v for k, v in rec.items() if k != "kind"})
    code, op = ca_api("POST", "", project, rec["until"], input_body(rec))
    require(code == 200 and isinstance(op, dict) and not op.get("error"), "CA create outcome unknown")
    fields = {k: op[col] for k, col in (("create_operation", "id"), ("resource_id", "resourceId"))
              if uid(op.get(col))}
    note(key, status="created" if len(fields) == 2 else "create_response", **fields)
    require(len(fields) == 2, "direct CA create did not establish both operation and resource IDs")
    rec.update(fields, status="created")
    return rec


def run():
    import registry
    import ca_lifecycle
    project, run_id, image = settings(registry.prepare())
    verify_image(project, image)
    for mode in MODES:
        key = "ca-" + mode
        rec = create(key, mode, project, run_id, image)
        observe_created(rec)
        ca_lifecycle.exercise(rec)
        finish(key, rec)   # No second application until the first is confirmed gone.


def cleanup():
    receipts = common.ledger()
    for key, rec in receipts.items():
        if key in ("ca-cli", "ca-mcp") or rec.get("kind") == "container_app":
            finish(key, rec)
