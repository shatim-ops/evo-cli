"""Candidate kubectl and Helm paths inside the Kubernetes cluster recorded by this run."""

import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import tempfile
import time
from uuid import UUID

import common


LABEL = "p90-run"
NS = "default"
PREFIX = "k8s-object:"
UID = re.compile(r"[a-zA-Z0-9][a-zA-Z0-9.-]{0,127}")


def require(ok, why):
    if not ok:
        raise RuntimeError("Kubernetes workloads: " + why)


def tool(cid, tool_name, *, write=False, **args):
    flags = ["--yes"] if write else []
    argv = ["tools", "call", tool_name, "--json", "--arg", "cluster_id=" + cid]
    for key, value in args.items():
        argv.extend(["--arg", key + "=" + value])
    return common.call_evo(*flags, *argv, timeout=130)


def mcp(cid, name, **args):
    messages = [
        {"id": 1, "method": "initialize", "params": {"protocolVersion": "2025-06-18",
         "capabilities": {}, "clientInfo": {"name": "p90", "version": "1"}}},
        {"method": "notifications/initialized"},
        {"id": 2, "method": "tools/call", "params": {"name": name, "arguments": dict(args, cluster_id=cid)}},
    ]
    wire = "".join(json.dumps(dict(line, jsonrpc="2.0")) + "\n" for line in messages)
    try:
        reply = subprocess.run([os.environ["EVO_BIN"], "--project-id", os.environ["CLOUDRU_PROJECT_ID"],
                                "mcp", "--toolsets", "k8s"], input=wire, text=True, capture_output=True,
                               timeout=75, check=True)
        calls = [m["result"] for m in map(json.loads, reply.stdout.splitlines()) if m.get("id") == 2]
        require(len(calls) == 1 and not calls[0].get("isError"), "MCP call refused")
        return json.loads(calls[0]["content"][0]["text"])
    except (ValueError, OSError, KeyError, TypeError, IndexError, subprocess.SubprocessError):
        raise RuntimeError("Kubernetes workloads: MCP result unavailable") from None


def kube(cid, args, *, write=False, via_mcp=False):
    result = (mcp(cid, "k8s_kubectl", args=args) if via_mcp else
              tool(cid, "k8s_kubectl", args=args, write=write))
    require(result.get("status") == "ok" and isinstance(result.get("output"), str), "kubectl read/write incomplete")
    # CombinedOutput may start with kubectl server warnings. Strip only those lines.
    lines = result["output"].splitlines()
    while lines and lines[0].startswith("Warning:"):
        lines.pop(0)
    return "\n".join(lines).strip()


def decode(s):
    try:
        return json.loads(s)
    except ValueError:
        raise RuntimeError("Kubernetes workloads: invalid Kubernetes JSON") from None


def observe_nodes(cid):
    result = decode(kube(cid, "get nodes -o json", via_mcp=True))
    nodes = result.get("items")
    require(isinstance(nodes, list) and nodes and not result.get("metadata", {}).get("continue"), "incomplete node list")
    entries = []
    for node in nodes:
        uid = node.get("metadata", {}).get("uid", "")
        require(UID.fullmatch(uid), "missing Kubernetes node UID")
        provider = node.get("spec", {}).get("providerID", "")
        require(isinstance(provider, str), "invalid provider ID")
        ready = any(c.get("type") == "Ready" and c.get("status") == "True" for c in node.get("status", {}).get("conditions", []))
        entries.append({"uid": uid, "provider_id": provider, "ready": ready})
    require(len({n["uid"] for n in entries}) == len(entries), "duplicate node UID")
    return entries


def manifest(name, marker, keep=False):
    meta = {"name": name, "namespace": NS, "labels": {LABEL: marker}}
    if keep:
        # Helm removes its own revision. The run deletes this chart object with a UID precondition.
        meta["annotations"] = {"helm.sh/resource-policy": "keep"}
    return {"apiVersion": "v1", "kind": "ConfigMap", "metadata": meta, "data": {"source": marker}}


def read_map(cid, name):
    text = kube(cid, "get configmap " + name + " -n default --ignore-not-found -o json")
    return decode(text) if text else None


def checked_map(obj, rec):
    meta = (obj or {}).get("metadata", {})
    uid = meta.get("uid", "")
    require(UID.fullmatch(uid) and meta.get("namespace") == NS and meta.get("name") == rec["name"] and
            meta.get("labels", {}).get(LABEL) == rec["marker"] and obj.get("data", {}).get("source") == rec["marker"],
            "ConfigMap has a different identity")
    return uid


def revisions(cid, release):
    # Project metadata only: no release payload, credentials or values enter the driver or its ledger.
    out = kube(cid, "get secrets -n default -l owner=helm,name=" + release +
               " -o=jsonpath-as-json={.items[*].metadata}")
    rows = decode(out)
    require(isinstance(rows, list), "incomplete Helm revision list")
    for row in rows:
        require(isinstance(row, dict) and UID.fullmatch(row.get("uid", "")) and row.get("namespace") == NS
                and row.get("labels", {}).get("owner") == "helm" and row["labels"].get("name") == release,
                "Helm metadata belongs to another release")
    return rows


def revision_uid(rows, name):
    require(len(rows) == 1 and rows[0]["name"] == "sh.helm.release.v1." + name + ".v1" and
            rows[0]["labels"].get("version") == "1", "another Helm revision or incomplete record")
    return rows[0]["uid"]


def record(which, **fields):
    common.note("k8s-object", PREFIX + which, **fields)


def private_dir():
    directory = tempfile.TemporaryDirectory(dir=os.environ["RUNNER_TEMP"], prefix="p90-obj-")
    require(not any(c.isspace() for c in directory.name), "runner path unsuitable for kubectl")
    return directory


def exercise(cid):
    run = os.environ["P90_SOURCE_RUN"]
    require(re.fullmatch("[0-9]{1,24}", run), "invalid run identity")
    ledger = common.ledger()
    require(all(PREFIX + x not in ledger for x in ("apply", "helm")), "creation already attempted")
    marker = secrets.token_hex(16)
    with private_dir() as d:
        directory = Path(d)
        for which in ("apply", "helm"):
            name = f"p90-{which}-{run}"
            rec = {"cluster": cid, "name": name, "marker": marker}
            require(read_map(cid, name) is None, "ConfigMap already exists")
            if which == "helm":
                require(not revisions(cid, name), "release already exists")
            record(which, state="intent", **rec)
            obj = manifest(name, marker, keep=which == "helm")
            if which == "apply":
                # Atomic Kubernetes create establishes the run's UID even if someone else raced
                # the preview. The subsequent candidate apply is constrained by that UID and version.
                path = directory / "cm.json"
                path.write_text(json.dumps(obj))
                created = decode(kube(cid, "create -f " + str(path) + " -o json", write=True))
                uid = checked_map(created, rec)
                record(which, uid=uid)
                rv = created["metadata"].get("resourceVersion")
                require(isinstance(rv, str) and rv, "missing ConfigMap resource version")
                obj["metadata"].update(uid=uid, resourceVersion=rv)
                obj["data"]["applied"] = "true"
                applied = tool(cid, "k8s_apply", write=True, manifest=json.dumps(obj), namespace=NS)
                require(applied.get("status") == "applied", "ConfigMap apply incomplete")
                seen = read_map(cid, name)
                require(checked_map(seen, rec) == uid and seen["data"].get("applied") == "true", "apply readback differs")
            else:
                chart = directory / "chart"
                (chart / "templates").mkdir(parents=True)
                (chart / "Chart.yaml").write_text("apiVersion: v2\nname: p90\nversion: 0.1.0\n")
                (chart / "templates" / "cm.yaml").write_text(json.dumps(obj))
                result = mcp(cid, "k8s_helm", action="install", namespace=NS, release=name, chart=str(chart), confirmed=True)
                require(result.get("status") in ("ok", "deploying"), "Helm install incomplete")
                if result["status"] == "deploying":
                    step = result.get("next_args", {})
                    require(result.get("next_tool") == "k8s_helm" and step.get("action") == "status" and
                            step.get("release") == name and step.get("namespace") == NS and step.get("cluster_id") == cid,
                            "MCP next step points outside the owned release")
                seen = read_map(cid, name)
                uid = checked_map(seen, rec)
                require(seen["metadata"].get("annotations", {}).get("helm.sh/resource-policy") == "keep", "chart policy differs")
                record(which, uid=uid, revision=revision_uid(revisions(cid, name), name))
                listed = tool(cid, "k8s_helm", action="list")
                require(listed.get("status") == "ok" and any(line.split()[:2] == [name, NS]
                        for line in listed.get("output", "").splitlines()), "Helm list readback differs")
                status = mcp(cid, "k8s_helm", action="status", namespace=NS, release=name)
                require(status.get("status") == "ok" and "STATUS: deployed" in status.get("output", "").splitlines(),
                        "Helm status is not deployed")
            record(which, state="observed")
    exercise_addon(cid)


def delete_map(cid, rec):
    """Kubernetes checks the recorded UID at the DELETE itself, including across cleanup retries."""
    with private_dir() as d:
        path = Path(d) / "delete.json"
        path.write_text(json.dumps({"apiVersion": "v1", "kind": "DeleteOptions", "preconditions": {"uid": rec["uid"]}}))
        kube(cid, "delete --raw /api/v1/namespaces/default/configmaps/" + rec["name"] + " -f " + str(path), write=True)
    require(read_map(cid, rec["name"]) is None, "ConfigMap removal not confirmed")


def cleanup(cid):
    cleanup_addon(cid)
    records = common.ledger()
    for which in ("helm", "apply"):
        rec = records.get(PREFIX + which)
        if rec is None:
            continue
        name = f"p90-{which}-{os.environ['P90_SOURCE_RUN']}"
        require(rec.get("kind") == "k8s-object" and rec.get("cluster") == cid and rec.get("name") == name and
                re.fullmatch("[0-9a-f]{32}", rec.get("marker", "")), "foreign workload receipt")
        if rec.get("state") == "gone":
            continue
        require(UID.fullmatch(rec.get("uid", "")), "no observed UID after create; readback unresolved")
        obj = read_map(cid, name)
        if obj is not None:
            require(checked_map(obj, rec) == rec["uid"], "replacement ConfigMap cannot be cleaned")
        if which == "helm":
            require(UID.fullmatch(rec.get("revision", "")), "missing Helm revision UID")
            rows = revisions(cid, name)
            if rows:
                require(revision_uid(rows, name) == rec["revision"], "replacement Helm revision cannot be cleaned")
                require(obj is not None and obj["metadata"].get("annotations", {}).get("helm.sh/resource-policy") == "keep",
                        "chart object diverged before uninstall")
                result = tool(cid, "k8s_helm", write=True, action="uninstall", namespace=NS, release=name)
                require(result.get("status") == "ok", "Helm uninstall incomplete")
                require(not revisions(cid, name), "Helm revision removal not confirmed")
                obj = read_map(cid, name)
        if obj is not None:
            require(checked_map(obj, rec) == rec["uid"], "ConfigMap identity changed during cleanup")
            delete_map(cid, rec)
        record(which, state="gone")


ADDON = "cert-manager"
ADDON_KEY = "k8s-object:addon"
CR_GROUPS = ("cert-manager.io", "acme.cert-manager.io")
COMPONENTS = {"cert-manager", "cert-manager-cainjector", "cert-manager-webhook"}


def addon_note(**fields):
    common.note("k8s-addon", ADDON_KEY, **fields)


def as_uuid(value):
    try:
        return isinstance(value, str) and UUID(value).int != 0 and str(UUID(value)) == value
    except ValueError:
        return False


def registry(cid):
    query = {"clusterId": cid, "projectId": os.environ["CLOUDRU_PROJECT_ID"], "pageSize":100}
    ids, tokens = {}, set()
    for _ in range(100):
        code, page = common.mk8s_get("/v3/clusterAddons", query)
        require(code == 200 and isinstance(page, dict), "addon list unreadable")
        fields = [k for k in ("clusterAddons", "items") if k in page]
        require(len(fields) <= 1 and (fields or set(page) <= {"nextPageToken"}), "ambiguous addon list")
        rows = page[fields[0]] if fields else []  # protobuf omits an empty repeated field.
        require(isinstance(rows,list), "invalid addon list")
        for row in rows:
            rid = row.get("id") if isinstance(row,dict) else None
            require(as_uuid(rid) and rid not in ids and row.get("clusterId",cid) == cid and
                    isinstance(row.get("name") or row.get("addonName"), str), "invalid addon list identity")
            ids[rid] = row
        token = page.get("nextPageToken", "")
        require(isinstance(token,str) and token not in tokens, "incomplete addon pagination")
        if not token:
            return ids
        tokens.add(token)
        query["pageToken"] = token
    raise RuntimeError("Kubernetes workloads: addon pagination limit")


def namespace_meta(cid):
    raw = kube(cid, "get namespace cert-manager --ignore-not-found -o json")
    if not raw:
        return None
    meta = decode(raw).get("metadata", {})
    require(meta.get("name") == ADDON and UID.fullmatch(meta.get("uid", "")), "namespace identity missing")
    return meta


def kitems(cid, query):
    listing = decode(kube(cid, "get " + query + " -o json"))
    require(isinstance(listing.get("items"),list) and not listing.get("metadata", {}).get("continue"),
            "incomplete Kubernetes object list")
    return listing["items"]


def addon_crds(cid):
    # Never remove a CRD that had another installation or now has user-created resources.
    out = {}
    for crd in kitems(cid, "customresourcedefinitions.apiextensions.k8s.io"):
        spec, meta = crd.get("spec",{}), crd.get("metadata",{})
        if spec.get("group") not in CR_GROUPS:
            continue
        name, uid = meta.get("name"), meta.get("uid","")
        require(isinstance(name,str) and re.fullmatch(r"[a-z][a-z0-9-]*",spec.get("names",{}).get("plural",""))
                and name == spec["names"]["plural"]+"."+spec["group"] and UID.fullmatch(uid)
                and name not in out, "invalid certificate CRD")
        out[name] = uid
    return out


def addon_pods(cid):
    rows = []
    for pod in kitems(cid, "pods -n cert-manager"):
        meta = pod.get("metadata", {})
        uid, name = meta.get("uid", ""), meta.get("name","")
        labels, owners = meta.get("labels",{}), meta.get("ownerReferences",[])
        require(meta.get("namespace") == ADDON and UID.fullmatch(uid) and len(owners) == 1 and
                UID.fullmatch(owners[0].get("uid","")) and labels.get("app.kubernetes.io/instance") == ADDON,
                "foreign or unrecognized pod in addon namespace")
        if owners[0].get("kind") == "Job":
            require(name.startswith("cert-manager-startupapicheck-"), "unrecognized addon job")
            rows.append({"uid":uid, "name":name, "owner_uid":owners[0]["uid"], "component":"job", "ready":False})
            continue
        comp = next((c for c in sorted(COMPONENTS,key=len,reverse=True) if name.startswith(c+"-") and
                     re.fullmatch(r"[a-z0-9]+-[a-z0-9]+",name[len(c)+1:])), None)
        require(comp and owners[0].get("kind") == "ReplicaSet" and meta.get("labels",{}).get("pod-template-hash") and
                owners[0].get("name") == comp+"-"+meta["labels"]["pod-template-hash"], "foreign addon pod controller")
        state = pod.get("status",{})
        ready = state.get("phase") == "Running" and any(c.get("type") == "Ready" and c.get("status") == "True" for c in state.get("conditions",[]))
        rows.append({"name":name, "uid":uid, "owner_uid":owners[0]["uid"], "component":comp, "ready":ready})
    require(len({p["uid"] for p in rows}) == len(rows), "duplicate addon pod UID")
    return rows


def poll_addon(seconds, read, reason):
    stop = time.monotonic() + seconds
    while time.monotonic() < stop:
        result = read()
        if result:
            return result
        time.sleep(min(12, max(0,stop-time.monotonic())))
    raise RuntimeError("Kubernetes workloads: " + reason)


def addon_id(rec):
    if rec.get("uid"):
        require(as_uuid(rec["uid"]), "invalid addon ID")
        return rec["uid"]
    require(as_uuid(rec.get("operation")), "no direct addon create receipt")
    code, op = common.mk8s_get("/v3/operations/" + rec["operation"], {"project_id": os.environ["CLOUDRU_PROJECT_ID"]})
    require(code == 200 and isinstance(op,dict) and op.get("id") == rec["operation"] and not op.get("error"),
            "addon operation not verified")
    rid = op.get("resourceId")
    if not rid:
        return None
    require(as_uuid(rid), "invalid operation resource ID")
    addon_note(uid=rid)
    return rid


def addon_card(cid, rid, version):
    code, body = common.mk8s_get("/v3/clusterAddons/" + rid, {"projectId": os.environ["CLOUDRU_PROJECT_ID"]})
    if code == 404:
        msg = str(body.get("message", "")).lower() if isinstance(body,dict) else ""
        require(isinstance(body,dict) and body.get("code") in (5,"NOT_FOUND",404) and
                ("addon" in msg or "add-on" in msg or "add on" in msg) and "not found" in msg, "unclassified addon absence")
        return None
    require(code == 200 and isinstance(body,dict) and body.get("id") == rid and body.get("clusterId") == cid and
            (body.get("name") or body.get("addonName")) == ADDON and body.get("appVersion") == version,
            "addon GET differs from creation receipt")
    return body


def preflight_addon():
    rel = common.call_evo("k8s","addon-versions",ADDON,"--json",timeout=40)
    choices = [r for r in rel.get("releases",[]) if r.get("latest") is True]
    require(rel.get("addon") == ADDON and rel.get("billable") is False and len(choices) == 1 and
            choices[0].get("version") == rel.get("latest") and re.fullmatch(r"v?[0-9]+(?:\.[0-9]+){2}",str(rel.get("latest",""))) and
            choices[0].get("ready_workers") in (0,1) and choices[0].get("namespace") in ("",ADDON),
            "reviewed free cert-manager release unavailable")
    return rel["latest"]


def exercise_addon(cid):
    require(ADDON_KEY not in common.ledger(), "addon already attempted")
    version = preflight_addon()
    require(not any((row.get("name") or row.get("addonName")) == ADDON for row in registry(cid).values())
            and namespace_meta(cid) is None and not addon_crds(cid), "previous cert-manager installation exists")
    addon_note(state="intent",cluster=cid,version=version)
    created = tool(cid, "k8s_add_addon", write=True, name=ADDON, version=version, resolve_mode="NONE")
    require(created.get("status") == "installing" and created.get("cluster_id") == cid and
            created.get("addon") == ADDON and created.get("version") == version,
            "unexpected addon create response")
    rid, op = created.get("cluster_addon_id"), created.get("operation_id")
    require((rid is None or as_uuid(rid)) and (op is None or as_uuid(op)) and (rid or op), "missing addon create identifiers")
    addon_note(state="creating", uid=rid, operation=op)
    rid = poll_addon(120, lambda: addon_id(common.ledger()[ADDON_KEY]), "addon ID not materialized")
    def ready():
        if addon_card(cid, rid, version) is None or rid not in registry(cid):
            return False
        ns = namespace_meta(cid)
        if ns is None:
            return False
        rec = common.ledger()[ADDON_KEY]
        require(rec.get("namespace_uid",ns["uid"]) == ns["uid"], "addon namespace replaced")
        pods, crds = addon_pods(cid), addon_crds(cid)
        require(all(crds.get(name) == uid for name,uid in rec.get("crds",{}).items()),
                "certificate CRD changed during install")
        # Namespace and child UIDs are recorded before readiness; recovery can check failed installs.
        state = {"namespace_uid":ns["uid"],"pods":pods,"crds":crds}
        if any(rec.get(key) != val for key,val in state.items()):
            addon_note(**state)
        components = [p for p in pods if p["component"] != "job"]
        return crds and {p["component"] for p in components} == COMPONENTS and all(p["ready"] for p in components)
    poll_addon(25*60, ready, "cert-manager controller, webhook and CA injector did not all reach Ready")
    addon_note(state="ready")


def cleanup_addon(cid):
    rec = common.ledger().get(ADDON_KEY)
    if not rec:
        return
    require(rec.get("kind") == "k8s-addon" and rec.get("cluster") == cid, "foreign addon receipt")
    if rec.get("state") == "gone":
        return
    rid = poll_addon(120, lambda: addon_id(rec), "addon resource ID unknown")
    def removed():
        if addon_card(cid, rid, rec["version"]) is not None or rid in registry(cid):
            return False
        # Before any observed installation, a temporarily empty GET is not proof.
        if rec.get("state") != "delete-intent" and not rec.get("namespace_uid"):
            return False
        ns = namespace_meta(cid)
        if ns:
            require(ns["uid"] == rec.get("namespace_uid"), "namespace changed after uninstall")
            if addon_pods(cid):
                return False
        return not addon_crds(cid)
    if not removed() and rec.get("state") != "delete-intent":
        require(addon_card(cid, rid, rec["version"]) is not None and rid in registry(cid), "missing owned addon before delete")
        ns = namespace_meta(cid)
        if ns:
            require(rec.get("namespace_uid") == ns["uid"], "unknown addon namespace identity")
            known = {p["uid"]: p for p in rec.get("pods",[])}
            for pod in addon_pods(cid):
                saved = known.get(pod["uid"])
                require(saved and all(pod[f] == saved[f] for f in ("name","owner_uid","component")),
                        "unrecorded or replacement addon pod blocks cascade")
        crds = addon_crds(cid)
        require(all(rec.get("crds",{}).get(name) == uid for name,uid in crds.items()), "unrecorded or replaced certificate CRD")
        for name in crds:
            # Return only UID values, not a certificate request's CSR/certificate data.
            instances = decode(kube(cid, "get " + name + " -A -o=jsonpath-as-json={.items[*].metadata.uid}"))
            require(instances == [], "an existing certificate object blocks cascading removal")
        addon_note(state="delete-intent")
        code, result = common.mk8s_delete_addon(rid, os.environ["CLOUDRU_PROJECT_ID"])
        require(code in (200,202,204), "addon delete not confirmed")
        if isinstance(result,dict) and as_uuid(result.get("id")):
            addon_note(delete_operation=result["id"])
        rec = common.ledger()[ADDON_KEY]
    poll_addon(10*60, removed, "addon ID, pods or CRDs still present")
    addon_note(state="gone", namespace_after_uninstall="present" if namespace_meta(cid) else "absent")
