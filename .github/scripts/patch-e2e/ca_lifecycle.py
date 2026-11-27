"""Exercise the Container App identified by the direct creation receipt."""

import http.client
import os
import re
import time
from urllib.parse import urlsplit

import apps
import common


def require(ok, reason):
    if not ok:
        raise RuntimeError("CA lifecycle: " + reason)


def public_host(card, name):
    # Cloud.ru's Managed Container Apps guide gives *.containerapps.ru. The candidate's
    # live case reads configuration.ingress.publicUri (internalUri is not a public route).
    # https://cloud.ru/docs/container-apps-evolution/ug/topics/tutorials__deploy-frontend-app
    ingress = card.get("configuration", {}).get("ingress", {})
    require(ingress.get("publiclyAccessible") is True and not ingress.get("customDomains"),
            "reviewed public ingress required")
    uri = ingress.get("publicUri")
    if not uri:
        return None  # The service can report the owned identity before it issues its public URL.
    require(isinstance(uri, str) and uri.isascii() and all(33 <= ord(c) <= 126 for c in uri),
            "invalid public URI")
    u = urlsplit(uri if "://" in uri else "https://" + uri)
    # Authority must be identical to the validated hostname, so no userinfo, port or parser ambiguity.
    host = u.hostname or ""
    label = host.removesuffix(".containerapps.ru")
    require(u.scheme == "https" and u.netloc == host and host.endswith(".containerapps.ru") and
            re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", label) and
            (label == name or label.startswith(name + "-")) and u.path in ("", "/") and not u.query and not u.fragment,
            "unexpected public ingress address")
    return host


def ready(host, deadline):
    # No redirect handling, proxy, token, cookies, request body, or application data in logs.
    connection = http.client.HTTPSConnection(host, timeout=max(.1, min(8, deadline - time.time())))
    try:
        connection.request("GET", "/", headers={"Accept-Encoding": "identity", "Connection": "close"})
        res = connection.getresponse()
        if res.status in (404, 429, 502, 503, 504):
            return False
        require(res.status == 200 and res.getheader("Content-Encoding", "identity") == "identity" and
                res.getheader("Content-Range") is None, "unexpected readiness response")
        data = res.read(32)
        length = res.getheader("Content-Length")
        require((length is None or length.isdecimal() and int(length) == len(data))
                and data in (b"p90-ready", b"p90-ready\n"), "readiness body differs")
        return True
    except (OSError, http.client.HTTPException):
        return False
    finally:
        connection.close()


def exercise(rec):
    mode = next((m for m in apps.MODES if rec.get("name") == apps.app_name(os.environ["P90_SOURCE_RUN"], m)), None)
    require(mode, "unknown receipt scope")
    key = "ca-" + mode
    apps.check_receipt(key, rec)
    saved = common.ledger().get(key, {})
    require(rec.get("status") == "created" and all(saved.get(k) == rec.get(k) for k in
            ("status", "project", "name", "resource_id", "create_operation", "start", "until", "image")),
            "direct creation receipt missing or changed")
    smoke_key = "ca-lifecycle:" + mode
    require(smoke_key not in common.ledger(), "already attempted; reconcile existing receipt")
    # Leave time for the parent to issue ca_delete and confirm its readback.
    until = rec["until"] - 240

    def check_time():
        require(rec["start"] <= time.time() < until, "lifecycle window exhausted")

    def tool(action, write=False):
        check_time()
        flags = ["--yes"] if write else []
        return common.call_evo(*flags, "--project-id", rec["project"], "tools", "call", "ca_" + action,
                               "--arg", "name=" + rec["name"], "--json", timeout=max(1, min(35, until - time.time())))

    def card():
        value = tool("get")
        require(apps.identity(rec, value), "observed application identity changed")
        return value

    def record(phase, **state):
        common.note("ca-lifecycle", smoke_key, phase=phase, **state)

    def poll(seconds, read, message):
        end = min(time.time() + seconds, until)
        while time.time() < end:
            if read():
                return
            time.sleep(min(8, max(0, end - time.time())))
        raise RuntimeError("CA lifecycle: " + message)

    def healthy():
        host = public_host(card(), rec["name"])
        return bool(host) and ready(host, until)

    def action(verb):
        record(verb + "-intent")
        card()
        result = tool(verb, write=True)
        accepted = {"stop": "stopping", "start": "starting"}[verb]
        require(result.get("action") == verb and result.get("name") == rec["name"]
                and result.get("status") in ("ok", accepted), "service action not confirmed")
        if result["status"] == accepted:
            require(result.get("next_tool") == "ca_get" and result.get("next_args") == {"name": rec["name"]},
                    "service continuation differs")
        record(verb + "-accepted")

    check_time()
    record("started", **{k: rec[k] for k in ("resource_id", "create_operation")})
    poll(360, healthy, "first HTTP 200 not observed")
    record("http-ready", first_http=200)

    def logs():
        card()
        value = tool("logs")
        rows = value.get("logs")
        require(isinstance(rows, list) and all(isinstance(row, dict) for row in rows), "invalid application log response")
        matched = any(isinstance(row.get("message"), str) and "p90-ready" in row["message"].split() for row in rows)
        record("log-read", log_lines=len(rows), marker_observed=matched, log_truncated=value.get("truncated") is True)
        return matched

    poll(120, logs, "application log marker not observed")
    action("stop")
    poll(180, lambda: str(card().get("status", "")).upper() in ("STOPPED", "CONTAINER_STATUS_STOPPED"),
         "stopped state not observed")
    action("start")
    poll(300, healthy, "HTTP 200 after restart not observed")
    record("exercised", stopped=True, restart_http=200)
