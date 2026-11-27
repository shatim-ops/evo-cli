"""Private DNS, Certificate Manager and Secret Manager checks for the patch."""

import base64
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import tempfile
import time
import uuid

import common


CERT = "misc-certificate"
SECRET = "misc-secret"
KINDS = {SECRET: "secret", CERT: "certificate"}
PATH = {SECRET: ("secretmanager", "/v1/secrets/"), CERT: ("certificatemanager", "/v1/certificate/")}


def preflight():
    import private_dns

    private_dns.preflight()
    # The published v1 API accepts an immutable secret UUID and an int32
    # version ID. The candidate's v2 path-based delete must never be used.
    ident(os.environ.get("CLOUDRU_PROJECT_ID"))
    require(shutil.which("openssl") is not None, "certificate generator unavailable")
    require(re.fullmatch(r"[1-9][0-9]{0,19}", os.environ.get("P90_SOURCE_RUN", "")), "invalid source run")


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def ident(value):
    require(isinstance(value, str), "missing resource ID")
    try:
        parsed = uuid.UUID(value)
    except ValueError:
        raise RuntimeError("invalid service resource ID") from None
    require(parsed.int and str(parsed) == value, "invalid service resource ID")
    return value


def tool(operation, *flags, arg_file=None, **params):
    argv = [*flags, "--project-id", os.environ["CLOUDRU_PROJECT_ID"], "tools", "call", operation, "--json"]
    if arg_file is not None:
        argv.extend(("--args-file", str(arg_file)))
    for name, value in params.items():
        argv.extend(("--arg", f"{name}={value}"))
    result = common.call_evo(*argv, timeout=45)
    require(isinstance(result, dict), "invalid candidate result")
    return result


def mark(key, **fields):
    common.note(KINDS[key], key, **fields)


def begin(key):
    require(key not in common.ledger(), "previous attempt exists; recover it first")
    run_id = os.environ["P90_SOURCE_RUN"]
    require(re.fullmatch(r"[1-9][0-9]{0,19}", run_id), "invalid source run")
    return "p90-" + run_id + "-" + secrets.token_hex(8)


def card(key, rid):
    host, path = PATH[key]
    status, body = common.service("GET", host, path + ident(rid))
    if status == 200:
        require(isinstance(body, dict) and body.get("id") == rid, "wrong or incomplete service card")
        for name in ("project_id", "projectId"):
            if name in body:
                require(body[name] == os.environ["CLOUDRU_PROJECT_ID"], "foreign project")
        return body
    # Classify only a resource-specific structured service error. A route,
    # opaque gateway response, denial or unreadable body cannot close a receipt.
    nouns = {SECRET: ["secret"], CERT: ["certificate"]}
    if status == 404 and isinstance(body, dict) and set(body) <= {"message", "code", "details"} and body.get("details") in (None, []):
        code = body.get("code")
        message = body.get("message")
        allowed = {text for noun in nouns[key] for text in
                   (noun + " not found", noun + " " + rid + " not found", noun + " does not exist")}
        if code in (5, "5", "NOT_FOUND") and isinstance(message, str) and message.strip().lower().rstrip(".") in allowed:
            return None
    raise RuntimeError("addressed read did not prove presence or absence")


def owned_card(key, rid, receipt):
    row = card(key, rid)
    if row is None:
        return None
    require(row.get("name") == receipt["name"] and row.get("description") == receipt["marker"], "ownership mismatch")
    return row


def run_certificate():
    name = begin(CERT)
    with tempfile.TemporaryDirectory() as folder:
        key, cert, argsfile = (Path(folder) / filename for filename in ("key.pem", "cert.pem", "arguments.json"))
        subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
                        "-subj", "/CN=" + name + ".invalid", "-keyout", str(key), "-out", str(cert)],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30)
        # This is newly generated disposable material. Keep it out of argv, logs and checkpoints.
        argsfile.touch(mode=0o600)
        argsfile.write_text(json.dumps({"name": name, "description": name, "server_certificate": cert.read_text(),
                                        "private_key": key.read_text()}))
        mark(CERT, name=name, marker=name, state="create-intent")
        out = tool("cert_import", "--yes", arg_file=argsfile)
    require(out.get("status") == "imported" and isinstance(out.get("certificate"), dict), "no direct certificate import receipt")
    cid = ident(out["certificate"].get("id"))
    mark(CERT, id=cid, state="created")
    require(owned_card(CERT, cid, common.ledger()[CERT]) is not None, "certificate not visible by ID")
    versions = tool("cert_versions", certificate_id=cid)
    require(isinstance(versions.get("versions"), list) and versions["versions"], "missing imported version")
    tool("cert_usage", id=cid)


def run():
    import private_dns

    preflight()
    private_dns.run()
    run_certificate()
    run_secret()


def absent_in_project(key, rid):
    out = tool("cert_list" if key == CERT else "secret_list")
    field = "certificates" if key == CERT else "secrets"
    items = out.get(field)
    if key == CERT:
        require(out.get("project") == os.environ["CLOUDRU_PROJECT_ID"], "wrong certificate inventory project")
        # Only the CLI's documented empty projection permits a null list.
        if field in out and items is None and type(out.get("count")) is int and out["count"] == 0:
            items = []
        require(isinstance(items, list) and type(out.get("count")) is int and len(items) == out["count"],
                "incomplete certificate inventory")
    require(isinstance(items, list) and not any(out.get(k) for k in ("truncated", "next_page_token", "nextPageToken")),
            "incomplete project inventory")
    ids = [ident(row.get("id")) for row in items if isinstance(row, dict)]
    require(len(ids) == len(items) == len(set(ids)), "incomplete inventory identity")
    return rid not in ids


def cleanup_one(key):
    book = common.ledger()
    rec = book.get(key)
    if rec is None or rec.get("state") == "gone":
        return
    require(rec.get("kind") == KINDS[key] and rec.get("id"),
            "create receipt has no trusted resource ID; possible remaining resource")
    rid = ident(rec["id"])
    if owned_card(key, rid, rec) is not None:
        if key == SECRET:
            remove_own_versions(rid, rec)
        if not rec.get("delete_started"):
            mark(key, delete_started=True)
            if key == SECRET:
                status, _ = common.service("DELETE", "secretmanager", "/v1/secrets/" + rid, query={"force": "false"})
                require(status in (200, 204), "secret deletion not acknowledged")
            else:
                # force=false makes the service reject deletion of an in-use certificate.
                tool("cert_delete", "--yes", id=rid, force="false")
    for poll in range(20):
        if card(key, rid) is None and absent_in_project(key, rid):
            mark(key, state="gone")
            return
        if poll < 19:
            time.sleep(5)
    raise RuntimeError("addressed deletion not confirmed; retain receipt")


def cleanup():
    import private_dns

    incomplete = []
    try:
        private_dns.cleanup()
    except Exception:
        incomplete.append("private-dns")
    for key in (CERT, SECRET):
        try:
            cleanup_one(key)
        except Exception:
            incomplete.append(key)
    if incomplete:
        raise RuntimeError("Unresolved private receipts for " + ", ".join(incomplete))

def version_id(value):
    require(type(value) is int and 0 < value <= 2147483647, "invalid secret version ID")
    return value


def versions_in(sid):
    # One secret with two expected versions. A full page (or extra continuation)
    # exceeds our scope: do not enumerate or touch further versions.
    status, body = common.service("GET", "secretmanager", "/v1/secrets/" + ident(sid) + "/versions",
                                  query={"page.limit": 3, "page.offset": 0})
    require(status == 200 and isinstance(body, dict) and set(body) == {"versions"}
            and isinstance(body["versions"], list) and len(body["versions"]) < 3, "unconfirmed version inventory")
    out = {}
    for version in body["versions"]:
        require(isinstance(version, dict) and version.get("secretId") == sid, "wrong version parent")
        vid = version_id(version.get("id"))
        state = version.get("state")
        require(state in ("ENABLED", "DISABLED", "DESTROYED") and vid not in out, "invalid version inventory")
        out[vid] = state
    return out


def run_secret():
    name = begin(SECRET)
    with tempfile.TemporaryDirectory() as folder:
        path = Path(folder) / "params.json"
        path.touch(mode=0o600)
        path.write_text(json.dumps({"path": name, "value": secrets.token_urlsafe(32), "description": name}))
        mark(SECRET, name=name, marker=name, state="create-intent")
        out = tool("secret_create", "--yes", arg_file=path)
    sec = out.get("secret")
    require(out.get("status") == "created" and isinstance(sec, dict), "no direct secret create receipt")
    sid = ident(sec.get("id"))
    mark(SECRET, id=sid, state="created")
    require(sec.get("path") == name and sec.get("projectId") == os.environ["CLOUDRU_PROJECT_ID"],
            "unexpected secret create scope")
    initial = sec.get("versions")
    require(isinstance(initial, list) and len(initial) == 1 and isinstance(initial[0], dict),
            "create response did not identify its first version")
    vid = version_id(initial[0].get("id"))
    mark(SECRET, versions=[vid])
    require(owned_card(SECRET, sid, common.ledger()[SECRET]) is not None and versions_in(sid) == {vid: "ENABLED"},
            "secret/version receipt mismatch")
    # v2 secret_add_version addresses a reusable path. Perform the equivalent
    # version addition on the exact owned v1 UUID. Never read a payload.
    mark(SECRET, version_pending=True)
    status, body = common.service("POST", "secretmanager", "/v1/secrets/" + sid + "/versions",
                                  body={"payload": {"data": base64.b64encode(secrets.token_bytes(32)).decode("ascii")}})
    require(status in (200, 201) and isinstance(body, dict) and body.get("secretId") == sid,
            "unknown added secret version")
    second = version_id(body.get("id"))
    require(second != vid, "duplicate added version")
    mark(SECRET, versions=[vid, second], version_pending=False)
    require(versions_in(sid) == {vid: "ENABLED", second: "ENABLED"}, "secret versions did not settle")


def remove_own_versions(sid, rec):
    versions = rec.get("versions")
    require(isinstance(versions, list) and 1 <= len(versions) <= 2 and not rec.get("version_pending"),
            "a created version has no direct receipt")
    permitted = {version_id(vid) for vid in versions}
    observed = versions_in(sid)
    require(set(observed) <= permitted, "unowned version prevents secret deletion")
    sent = set(rec.get("version_deletes", []))
    for vid, state in observed.items():
        if state == "DESTROYED" or vid in sent:
            continue
        path = f"/v1/secrets/{sid}/versions/{vid}"
        status, body = common.service("GET", "secretmanager", path)
        require(status == 200 and isinstance(body, dict) and body.get("secretId") == sid
                and version_id(body.get("id")) == vid and body.get("state") == state, "wrong version identity")
        sent.add(vid)
        mark(SECRET, version_deletes=sorted(sent))
        status, _ = common.service("DELETE", "secretmanager", path)
        require(status in (200, 204), "secret version delete not acknowledged")
    for poll in range(10):
        after = versions_in(sid)
        require(set(after) <= permitted, "unowned version prevents secret deletion")
        if all(state == "DESTROYED" for state in after.values()):
            return
        if poll < 9:
            time.sleep(3)
    raise RuntimeError("owned secret versions still live")
