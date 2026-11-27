"""Private-zone D65/D66 on the release candidate, using the original run’s resource IDs."""
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time

import common
from core import exact_id


ZONE = "dns-zone"
RECORD = "dns-record"
COLLECTION = {ZONE: "/v1/privateZones", RECORD: "/v1/privateRecordsSole"}
MARKER = "p90-private-" + common.CANDIDATE[:12]
VALUE = "198.18.255.10"


def need(test, reason):
    if not test:
        raise RuntimeError("private DNS: " + reason)


def project():
    return exact_id(os.environ["CLOUDRU_PROJECT_ID"])


def domain():
    run = os.environ["P90_SOURCE_RUN"]
    need(re.fullmatch(r"[1-9][0-9]{0,19}", run), "invalid original run")
    return "evo-p90-" + run + ".test."


def row(key):
    return common.ledger().get("network." + key, {})


def mark(key, **fields):
    common.note("network-" + key, "network." + key, **fields)


def preflight():
    project()
    domain()
    need(Path(os.environ["P90_NET_BIN"]).is_file(), "reviewed candidate entry point unavailable")
    return preview_vpc()


def tool(tool_name, args, *, negative_case=False, dry=False):
    argv = [os.environ["EVO_BIN"], "--project-id", project()]
    if dry or negative_case:
        argv.append("--dry-run")
    argv.extend(["tools", "call", tool_name, "--json"])
    for key, value in args.items():
        argv.extend(["--arg", f"{key}={value}"])
    proc = subprocess.run(argv, capture_output=True, timeout=90)
    need(proc.returncode == (1 if negative_case else 0), "unexpected candidate exit")
    # JSON error output can be on stderr for a CLI refusal. Never print either.
    for data in (proc.stdout, proc.stderr):
        for line in [data, *data.splitlines()]:
            try:
                out = json.loads(line)
            except (ValueError, UnicodeError):
                continue
            if isinstance(out, dict):
                if negative_case and isinstance(out.get("error"), dict):
                    return {"reason": out["error"].get("reason"), "error": out["error"].get("message")}
                return out
    raise RuntimeError("private DNS: incomplete candidate result")


def preview_vpc():
    out = tool("dns_zone_create", {"type":"private", "domain":domain(), "description":MARKER}, dry=True)
    details = (out.get("preview") or {}).get("details", {}).get("preview", {})
    params = {p["key"]:p["value"] for p in details.get("params", [])}
    need(out.get("status") == "dry_run" and (out.get("preview") or {}).get("action") == "dns_zone_create"
         and details.get("project_id") == project() and params.get("visibility") == "private",
         "candidate did not resolve private DNS in this project")
    return exact_id(params.get("vpc_id"))


def inventory(key, zone=None):
    result, cursors = {}, set()
    q = {"project_id":project()} if key == ZONE else {"private_zone_id":exact_id(zone)}
    field = "zones" if key == ZONE else "records"
    for _ in range(30):
        code, body = common.service("GET", "dns", COLLECTION[key], query=q)
        need(code == 200 and isinstance(body, dict) and isinstance(body.get(field), list), "incomplete inventory")
        for item in body[field]:
            need(isinstance(item, dict), "invalid inventory entry")
            ident = exact_id(item.get("id"))
            need(ident not in result, "duplicate DNS identity")
            result[ident] = item
        need(not any(body.get(f) for f in ("next_page_token", "has_more", "next")), "unknown continuation")
        token = body.get("nextPageToken", "")
        need(isinstance(token, str), "malformed continuation")
        if not token:
            return result
        need(body[field] and token not in cursors, "inventory failed to advance")
        cursors.add(token)
        q["pageToken"] = token
    raise RuntimeError("private DNS: page bound reached")


def negative(vpc_id):
    own = row("vpc")
    need(own.get("returned_directly") is True and own["id"] == exact_id(vpc_id) and own.get("project") == project(),
         "negative case lacks own VPC receipt")
    no_zone = "nodns-" + domain()
    listed = inventory(ZONE)
    need(all(no_zone not in (x.get("domain"), x.get("name")) for x in listed.values()), "negative domain occupied")
    out = tool("dns_zone_create", {"domain":no_zone, "type":"private", "vpc_id":vpc_id}, negative_case=True)
    msg = str(out.get("error", "")).lower()
    need(out.get("reason") == "USAGE" and vpc_id in msg
         and ("no dns server" in msg or "нет dns-сервера" in msg), "D65 refusal not established")
    need(all(no_zone not in (x.get("domain"), x.get("name")) for x in inventory(ZONE).values()),
         "unexpected private zone after refusal")
    mark("dns-negative", refused=True)


def fence(key, name, args, method, path, body=None):
    spec = dict(Key=key, Host="dns.api.cloud.ru", Path=path, Method=method, Project=project(),
                Shape="vpcop", Query={}, Expected=body, GroupID="")
    with tempfile.NamedTemporaryFile(mode="w", dir=os.environ["RUNNER_TEMP"]) as af:
        json.dump(args, af)
        af.flush()
        p = subprocess.run([os.environ["P90_NET_BIN"], "--yes", "--project-id", project(), "tools", "call", name,
                            "--json", "--args-file", af.name], env=dict(os.environ, P90_NET_CALL=json.dumps(spec)),
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=180)
    need(p.returncode == 0, "guarded candidate mutation incomplete")


def operation(op, expected=None):
    op = exact_id(op)
    for i in range(30):
        code, obj = common.service("GET", "dns", "/v1/operations/"+op)
        need(code == 200 and isinstance(obj, dict) and obj.get("id") == op and type(obj.get("done")) is bool
             and not obj.get("error"), "operation unavailable or failed")
        ids = {exact_id(obj[f]) for f in ("resourceId","resource_id") if obj.get(f)}
        need(len(ids) <= 1 and (not ids or not expected or ids == {expected}), "operation changed resource")
        if obj["done"]:
            return next(iter(ids), expected)
        if i < 29:
            time.sleep(5)
    raise RuntimeError("private DNS: operation still pending")


def owned(key):
    r = row(key)
    need(r.get("returned_directly") is True and r.get("project") == project(), "direct create receipt missing")
    previous = exact_id(r["id"]) if r.get("id") else None
    rid = exact_id(operation(r.get("operation"), previous))
    if not previous:
        mark(key, id=rid)
    return rid


def scope(key, obj, rid):
    need(isinstance(obj, dict) and obj.get("id") == rid, "addressed card differs")
    for f in ("project_id", "projectId"):
        need(obj.get(f) in (None, project()), "foreign DNS project")
    if key == ZONE:
        need(obj.get("domain") == domain() and obj.get("name") == domain() and
             obj.get("description") == MARKER and isinstance(obj.get("linkedVpcIds"), list), "zone ownership differs")
    else:
        need(obj.get("privateZoneId", obj.get("private_zone_id")) == exact_id(row(ZONE).get("id"))
             and str(obj.get("name", "")).rstrip(".") in ("www","www."+domain().rstrip("."))
             and str(obj.get("type", "")).upper() == "A" and obj.get("values") == [VALUE] and obj.get("ttl") == 300,
             "record ownership differs")
    return obj


def card(key, rid, zone=None, *, allow_gone=False):
    st, obj = common.service("GET", "dns", COLLECTION[key]+"/"+rid)
    if allow_gone and st == 404 and isinstance(obj, dict) and obj.get("code") in (5,"5","NOT_FOUND"):
        message = str(obj.get("message","")).lower()
        need("not found" in message and ("zone" if key == ZONE else "record") in message and
             "route" not in message and "endpoint" not in message and rid not in inventory(key, zone),
             "DNS disappearance not established")
        return None
    need(st == 200, "DNS card unavailable")
    return scope(key, obj, rid)


def create(key, vpc, zid=None):
    need(not row(key), "previous create receipt")
    if key == ZONE:
        body = {"domain":domain(), "name":domain(), "description":MARKER, "project_id":project(), "linkedVpcIds":[vpc]}
        args = {"type":"private", "domain":domain(), "description":MARKER, "vpc_id":vpc}
        # Project default was selected in dry-run. The write names that exact ID.
        mark(key, project=project(), approved_vpc=vpc, intent=True)
    else:
        args = {"type":"private","zone_id":zid,"name":"www","record_type":"A","value":VALUE,"ttl":300}
        body = {"private_zone_id":zid,"name":"www","type":"a","values":[VALUE],"ttl":300}
        mark(key, project=project(), zone_id=zid, intent=True)
    fence(key, "dns_zone_create" if key == ZONE else "dns_record_create", args, "POST", COLLECTION[key], body)
    return owned(key)


def static_records(zid):
    baseline = inventory(RECORD, zid)
    for rid, obj in baseline.items():
        need(str(obj.get("type", "")).upper() in ("SOA","NS") and obj.get("name") in ("@","",domain(),domain().rstrip("."))
             and obj.get("privateZoneId",obj.get("private_zone_id")) == zid, "unexpected record in new private zone")
    return sorted(baseline)


def run():
    vpc = preflight()
    need(all(domain() not in (x.get("domain"),x.get("name")) for x in inventory(ZONE).values()), "domain occupied")
    zid = create(ZONE, vpc)
    z = card(ZONE, zid)
    need(z["linkedVpcIds"] == [vpc], "expected one exclusively inspected link")
    mark(ZONE, initial_records=static_records(zid), link_verified=True)
    need(tool("dns_zone_get", {"id":zid, "type":"private"}).get("id") == zid, "candidate zone read incomplete")
    rid = create(RECORD, vpc, zid)
    card(RECORD, rid)
    need(tool("dns_record_get", {"record_id":rid,"type":"private"}).get("id") == rid and
         rid in inventory(RECORD,zid), "candidate record read incomplete")
    remove(RECORD, rid, zid)
    out = tool("dns_zone_delete", {"id":zid, "type":"private"}, negative_case=True)
    msg = str(out.get("error","")).lower()
    need(out.get("reason") == "USAGE" and vpc in msg and ("linked" in msg or "связан" in msg), "D66 refusal not established")
    mark(ZONE, d66_refused=True)
    cleanup()


def remove(key, rid, zone=None):
    r = row(key)
    if not r.get("delete_intent"):
        need(card(key, rid, zone) is not None, "owned target unavailable")
        if key == ZONE:
            need(r.get("unlinked") and static_records(rid) == r.get("initial_records"), "record set changed")
        mark(key, delete_intent=True)
        name = "dns_zone_delete" if key == ZONE else "dns_record_delete"
        args = {"type":"private", "id" if key == ZONE else "record_id":rid}
        fence(key, name, args, "DELETE", COLLECTION[key]+"/"+rid)
    r = row(key)
    need(r.get("delete_dispatched") is True, "original delete never confirmed")
    if r.get("delete_operation"):
        operation(r["delete_operation"],rid)
    for i in range(76):
        if card(key,rid,zone,allow_gone=True) is None:
            mark(key, gone=True)
            return
        if i < 75:
            time.sleep(20)
    raise RuntimeError("private DNS: addressed delete still pending")


def cleanup():
    if not row(ZONE) or row(ZONE).get("gone"):
        return
    zid = owned(ZONE)
    if row(RECORD) and not row(RECORD).get("gone"):
        remove(RECORD, owned(RECORD), zid)
    r = row(ZONE)
    if not r.get("delete_intent"):
        z = card(ZONE,zid)
        vpc = exact_id(r.get("approved_vpc"))
        links = z["linkedVpcIds"]
        need(links == [vpc] or r.get("unlink_intent") and links == [], "foreign, missing, or ambiguous VPC link")
        # Only the new zone's apex NS/SOA records may remain. Reuse their
        # recorded IDs when available and verify them again before deletion.
        baseline = static_records(zid)
        if "initial_records" not in r:
            mark(ZONE, initial_records=baseline)
        else:
            need(r["initial_records"] == baseline, "zone acquired other records")
        if links and not r.get("unlink_intent"):
            mark(ZONE, unlink_intent=True)
            st, body = common.service("PATCH","dns",COLLECTION[ZONE]+"/"+zid, body={"linkedVpcIds":[]})
            need(st in (200,202,204) and (body is None or isinstance(body, dict)), "unlink not acknowledged")
            if body:
                mark(ZONE, unlink_op=exact_id(body.get("id")))
        if row(ZONE).get("unlink_op"):
            operation(row(ZONE)["unlink_op"],zid)
        need(card(ZONE,zid)["linkedVpcIds"] == [], "original VPC link still present")
        mark(ZONE, unlinked=True)
    remove(ZONE,zid)
