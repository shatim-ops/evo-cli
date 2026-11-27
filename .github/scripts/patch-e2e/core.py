"""Two bounded Compose lifecycles. All candidate traffic goes through the reviewed core binary."""
import base64
import contextlib
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import time
import urllib.request
import uuid

from common import compute, ledger, note

API = "/api/v1"
GROUPS = API + "/security-groups"
ZONE = "ru.AZ-2"
IMAGE = "ubuntu-22.04"
FLAVOR = "lowcost10-2-4"
SIZE = 20
NGINX = "docker.io/library/nginx@sha256:7fc0fb4af7bf006aeb31aea88069976060f8f0a0a86b667126742cc17f4e31ae"
PROJECT = "CLOUDRU_PROJECT_ID"
LIMIT = 100
ALL_IPV4 = "0.0.0.0/0"


def runner_cidr():
    value = os.environ.get("P90_RUNNER_CIDR", "")
    try:
        network = ipaddress.ip_network(value, strict=True)
    except ValueError:
        raise RuntimeError("Compute: configure the approved runner public IPv4/32 before dispatch") from None
    require(network.version == 4 and network.prefixlen == 32 and network.network_address.is_global and
            not network.network_address.is_multicast and value == str(network), "invalid approved runner public IPv4/32")
    return value


def require(condition, message):
    if not condition:
        raise RuntimeError("Compute: " + message)


def uid(value):
    try:
        return isinstance(value, str) and str(uuid.UUID(value)) == value and uuid.UUID(value).int != 0
    except (ValueError, AttributeError):
        return False


def exact_id(value):
    require(uid(value), "missing exact resource identity")
    return value


def row(cycle, key):
    return ledger().get(f"core.{cycle}.{key}", {})


def save(cycle, suffix, kind, **fields):
    note(kind, f"core.{cycle}.{suffix}", **fields)


def one(values, reason):
    require(len(values) == 1, reason)
    return values[0]


def related_id(card, field):
    direct = card.get(field + "_id")
    nested = card.get(field)
    indirect = nested.get("id") if isinstance(nested, dict) else nested if isinstance(nested, str) else None
    require(not direct or not indirect or direct == indirect, "conflicting resource scope")
    return direct or indirect


def get(path, query=None):
    status, body = compute("GET", path, query=query)
    require(status == 200, "scoped GET incomplete")
    return body


def page(data, offset, allow_bare=False):
    if allow_bare and isinstance(data, list):
        require(offset == 0, "format changed during enumeration")
        return data, len(data)
    require(isinstance(data, dict) and isinstance(data.get("items"), list), "unknown page shape")
    items, total = data["items"], data.get("total")
    require(type(total) is int and total >= offset + len(items), "missing or inconsistent page total")
    require(data.get("offset", offset) == offset and (items or total == offset), "incomplete page")
    for field in ("next_page_token", "nextPageToken", "next", "has_more", "hasNext"):
        require(data.get(field) in (None, "", False), "unexpected continuation protocol")
    return items, total


def catalog(name, project=None, *, paged=True):
    query = {"project_id": project} if project else {}
    got = {}
    initial_total = None
    for _ in range(20):
        offset = len(got)
        params = dict(query)
        if paged:
            params.update(offset=offset, limit=LIMIT)
        items, total = page(get(f"{API}/{name}", params), offset, allow_bare=not paged)
        if initial_total is None:
            initial_total = total
        require(total == initial_total, "catalog changed during enumeration")
        for item in items:
            require(isinstance(item, dict) and uid(item.get("id")) and item["id"] not in got, "ambiguous catalog identity")
            got[item["id"]] = item
        if len(got) == total:
            return list(got.values())
        require(paged, "unpaged catalog was incomplete")
    raise RuntimeError("Compute: catalog page bound reached")


def rules(group):
    body = get(f"{GROUPS}/{exact_id(group)}/rules", {"offset": 0, "limit": LIMIT})
    items, total = page(body, 0)
    require(total == len(items) <= LIMIT, "security group membership is incomplete")
    require(all(isinstance(r, dict) and uid(r.get("id")) for r in items), "malformed group member")
    require(len({r["id"] for r in items}) == len(items), "duplicate rule ID")
    return {r["id"]: r for r in items}


def subnet_for_zone(rows, zone):
    choices = [r for r in rows if related_id(r, "availability_zone") == zone and
               r.get("state", "") in ("", "created", "updating") and r.get("type") not in ("technical", "zone_technical")]
    require(choices, "no usable subnet in the requested zone")
    choices.sort(key=lambda r: (-(2 * bool(r.get("default")) + bool(r.get("routed_network"))), r.get("name", "").lower(), r["id"]))
    selected = choices[0]
    require(selected.get("routed_network") is True and selected.get("dns_servers") != [], "subnet cannot serve a public Compose VM")
    return selected["id"]


def check_image_access(detail):
    metadata = detail.get("image_metadata")
    require(isinstance(metadata, list) and all(isinstance(field, dict) for field in metadata),
            "image authentication template unreadable")
    require(any(field.get("name") == "public_key" for field in metadata), "image does not declare the deploy SSH key")
    # FillMetadata sends a random value for a linux_password field even beside a public key.
    # The reviewed VM POST accepts only this checkout's key and non-secret login/host metadata.
    for field in metadata:
        require(field.get("name") != "linux_password" and field.get("field_type") != "linux_password",
                "approved image requires an additional password in the VM request")


def preflight():
    cidr = runner_cidr()
    project = exact_id(os.environ.get(PROJECT))
    zone = one([r for r in catalog("availability-zones", paged=False) if r.get("name") == ZONE], "zone unavailable")
    require(zone.get("enabled") is not False, "zone is disabled")
    flavors = catalog("flavors")
    flavor = one([r for r in flavors if r.get("name") == FLAVOR], "approved flavor unavailable")
    require(flavor.get("cpu") == 2 and flavor.get("ram") == 4, "unexpected compute capacity")
    image = one([r for r in catalog("images", project) if r.get("name") == IMAGE], "approved image unavailable or ambiguous")
    detail = get(f"{API}/images/{image['id']}")
    require(isinstance(detail, dict) and detail.get("id") == image["id"] and detail.get("name") == IMAGE, "image details disagree")
    check_image_access(detail)
    for card in (image, detail):
        for field, limit in (("min_disk", SIZE), ("min_cpu", 2), ("min_ram", 4)):
            if field in card:
                require(type(card[field]) is int and 0 <= card[field] <= limit, "image exceeds budget")
    subnet = subnet_for_zone(catalog("subnets", project), zone["id"])
    disk = one([r for r in catalog("disk-types", project, paged=False) if r.get("name") == "SSD"], "approved SSD unavailable")
    group_candidates = catalog("security-groups", project)
    # Detect an unusable representation before creating a group (#251). This is a read of membership only.
    require(group_candidates, "no existing rule page for the pre-write contract check")
    rules(group_candidates[0]["id"])
    # Last run stopped before FIP POST (#253); positively establish read access to the project collection.
    catalog("floating-ips", project)
    catalog("vms", project)
    run = os.environ.get("GITHUB_RUN_ID", "")
    require(re.fullmatch(r"[1-9][0-9]{0,19}", run), "invalid run identity")
    require(Path(os.environ["P90_CORE_BIN"]).is_file(), "guarded candidate binary missing")
    return dict(project_id=project, zone_id=zone["id"], zone_name=ZONE,
                image_id=image["id"], image_name=IMAGE, flavor_id=flavor["id"], flavor_name=FLAVOR,
                subnet_id=subnet, disk_type_name=disk["name"], disk_cap_gb=SIZE, cidr=cidr)


def group_card(cycle, *, detached):
    g = row(cycle, "group")
    card = get(GROUPS + "/" + exact_id(g.get("id")))
    scope = row(cycle, "scope")
    require(isinstance(card, dict) and card.get("id") == g["id"] and card.get("name") == g["name"] and
            card.get("description") == g["name"] and not card.get("default") and
            related_id(card, "project") in (None, scope["project_id"]) and
            related_id(card, "availability_zone") in (None, scope["zone_id"]), "group identity changed")
    if detached:
        require(card.get("has_interfaces") is False, "cannot prove group is detached")
    return card


def direct_post(cycle, key, kind, path, body, query=None):
    require(not row(cycle, key).get("post_sent"), "previous create already dispatched")
    save(cycle, key, kind, post_sent=True, name=body.get("name", body.get("description", "")))
    status, result = compute("POST", path, query=query, body=body)
    require(status in (200, 201) and isinstance(result, dict) and uid(result.get("id")), "create returned no directly owned ID")
    resource = result["id"]
    save(cycle, key, kind, id=resource)
    if key == "group" and not counts_match(result, []):
        save(cycle, key, kind, initial_rules_uncertain=True)
        require(False, "group creation did not confirm zero initial rule counts")
    return resource


def baserule(cycle, key):
    name = row(cycle, "group")["name"]
    return {"direction": "ingress" if key == "ssh" else "egress", "ether_type": "IPv4",
            "ip_protocol": "tcp" if key == "ssh" else "any", "port_range": "22:22" if key == "ssh" else "",
            "remote_ip_prefix": row(cycle, "scope")["cidr"] if key == "ssh" else ALL_IPV4, "description": name + "-" + key}


def wait_for(fn, seconds=90, interval=3):
    until = time.monotonic() + seconds
    while True:
        if fn():
            return
        if time.monotonic() >= until:
            raise RuntimeError("Compute: readback timed out")
        time.sleep(interval)


def counts_match(card, values):
    for direction in ("ingress", "egress"):
        key = direction + "_rules_count"
        if key in card and (type(card[key]) is not int or card[key] != sum(r.get("direction") == direction for r in values)):
            return False
    return True


def owned_rule_set(cycle, group):
    require(not row(cycle, "group").get("initial_rules_uncertain"), "unverified initial group members")
    members = rules(group)
    card = group_card(cycle, detached=False)
    require(counts_match(card, members.values()), "membership counts disagree")
    records = {r["id"]: k for k in ("ssh", "egress", "app-rule") if (r := row(cycle, k)).get("id")}
    foreign = set(members) - set(records)
    if foreign:
        save(cycle, "group", "group", foreign_members_seen=True)
    require(not foreign and not row(cycle, "group").get("foreign_members_seen"), "unexpected member; group preserved")
    for rid, rule in members.items():
        key = records[rid]
        if key == "app-rule":
            scope = row(cycle, "scope")
            port = str(scope["port"])
            want_rule = {"description": row(cycle, key)["marker"], "ether_type": "IPv4", "direction": "ingress",
                         "ip_protocol": "tcp", "port_range": f"{port}:{port}", "remote_ip_prefix": scope["cidr"]}
        else:
            want_rule = baserule(cycle, key)
        for field, want in want_rule.items():
            require(rule.get(field) == want, "saved rule changed")
    return members


def setup_group(cycle, scope):
    run = os.environ["GITHUB_RUN_ID"]
    name = f"evo-p90-{run}-{cycle}-sg"
    require(not any(r.get("name") == name for r in catalog("security-groups", scope["project_id"])), "group name occupied")
    gid = direct_post(cycle, "group", "group", GROUPS, {"name": name, "description": name,
                      "project_id": scope["project_id"], "availability_zone_id": scope["zone_id"]})
    save(cycle, "scope", "scope", sg_id=gid)
    wait_for(lambda: group_card(cycle, detached=True).get("state") in ("created", "available", "running", "ready"))
    require(not owned_rule_set(cycle, gid), "new group is not empty")
    for key in ("egress", "ssh"):
        rid = direct_post(cycle, key, "rule", f"{GROUPS}/{gid}/rules", baserule(cycle, key), {"project_id": scope["project_id"]})
        wait_for(lambda: rid in owned_rule_set(cycle, gid))
    return gid


def workdir(cycle):
    return Path(os.environ["RUNNER_TEMP"]) / f"p90-compose-{cycle}"


def env_for(cycle, **extra):
    # The candidate has no need for the private journal credential itself beyond its checkpoint child.
    return dict(os.environ, P90_CORE_CYCLE=str(cycle), **extra)


def prepare_workspace(cycle, scope):
    folder = workdir(cycle)
    folder.mkdir(mode=0o700, exist_ok=False)
    config = {"project_id": scope["project_id"], "region": scope["zone_id"], "image_id": scope["image_id"],
              "flavor": FLAVOR, "disk_size": SIZE, "security_group": scope["sg_id"],
              "project_name": scope["vm_name"].removeprefix("evo-"), "timeout": 720}
    (folder / ".evo.yaml").write_text(json.dumps(config))  # JSON is valid YAML.
    (folder / "docker-compose.yml").write_text(
        f'services:\n  web:\n    image: {NGINX}\n    ports:\n      - "{scope["port"]}:80"\n')
    return folder


def run_cli(cycle, *args, timeout=160, cut=False, hold_rule=False, raw=False):
    env = env_for(cycle)
    if "--dry-run" in args or args[0] in ("logs", "verify", "status"):
        env["P90_CORE_READONLY"] = "1"
    if hold_rule:
        env["P90_CORE_HOLD_RULE"] = "1"
    if cut:
        env["P90_CORE_INTERRUPT_DELETE"] = "1"
    result = subprocess.run([env["P90_CORE_BIN"], "--json", *args], cwd=workdir(cycle), env=env,
                            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=timeout)
    if cut:
        require(result.returncode == 91 and row(cycle, "destroy-cut").get("state_matches") is True,
                "cold-destroy checkpoint was not reached")
        return None
    require(result.returncode == 0, "guarded CLI command failed")
    if raw:
        return result.stdout
    try:
        return json.loads(result.stdout)
    except (ValueError, UnicodeError):
        raise RuntimeError("Compute: malformed CLI result") from None


def local_record(cycle):
    path = workdir(cycle) / ".evo/state.json"
    if not path.exists():
        return {}
    data = json.loads(path.read_bytes())
    entries = data.get("deployments", [])
    scope = row(cycle, "scope")
    return next((r for r in reversed(entries) if r.get("vm_name") == scope["vm_name"] and r.get("project_id") == scope["project_id"]), {})


def interrupt_deploy(cycle):
    env = env_for(cycle)
    with subprocess.Popen([env["P90_CORE_BIN"], "--yes", "--json", "deploy", "--cidr", row(cycle, "scope")["cidr"]], cwd=workdir(cycle),
                          env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL) as proc:
        try:
            def checkpoint():
                require(proc.poll() is None, "deploy stopped before interruption checkpoint")
                rec = local_record(cycle)
                if not uid(rec.get("vm_id")) or not uid(rec.get("floating_ip_id")):
                    return False
                require(rec["vm_id"] == row(cycle, "vm").get("id") and
                        rec["floating_ip_id"] == row(cycle, "fip").get("id"), "local state differs from create receipts")
                require(rec.get("status") != "running", "deploy completed before interruption")
                return True

            wait_for(checkpoint, 780, 1)
            proc.send_signal(signal.SIGINT)
            rc = proc.wait(timeout=120)
            require(rc in (130, -signal.SIGINT), "unexpected interrupted deploy result")
            save(cycle, "lifecycle", "checkpoint", deploy_interrupted=True)
        finally:
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    proc.kill()


INIT = {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
    "protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "patch-e2e", "version": "1"}}}
READY = {"jsonrpc": "2.0", "method": "notifications/initialized"}


def mcp_result(packet, tool=None):
    result = packet.get("result", {})
    require(not packet.get("error") and result.get("content"), "MCP call failed")
    if tool == "logs":
        text = result["content"][0].get("text", "")
        require(not result.get("isError") and isinstance(text, str) and "GET / HTTP/" in text, "MCP application log unconfirmed")
        return {"logged": True}
    try:
        body = json.loads(result["content"][0]["text"])
    except (KeyError, IndexError, ValueError, TypeError):
        raise RuntimeError("Compute: malformed MCP result") from None
    require(isinstance(body, dict) and (not result.get("isError") or body.get("status") == "confirmation_required" or
                                      tool == "release_floating_ip" and body.get("status") == "refused" and body.get("reason") == "USAGE"), "MCP tool refused operation")
    return body


def mcp_stmt(tool, args, cycle=None):
    require(tool in ("deploy", "destroy", "deploy_plan", "deploy_apply", "status", "logs", "release_floating_ip"), "unexpected MCP operation")
    if tool == "release_floating_ip":
        require(set(args) in ({"id"}, {"id", "confirmed"}) and uid(args["id"]), "floating-IP call requires a single exact ID")
    if tool == "deploy_apply":
        require(isinstance(args.get("plan_id"), str) and re.fullmatch(r"plan-[a-f0-9]{12}", args["plan_id"]),
                "invalid plan reference")
        if set(args) != {"plan_id"}:
            require(cycle == 2 and set(args) in ({"plan_id", "confirmed"}, {"plan_id", "confirmed", "vm_id"})
                    and args.get("confirmed") is True, "unapproved plan application")
            scope = row(cycle, "scope")
            require(scope.get("creation_mode") == "plan" and scope.get("plan_id") == args["plan_id"]
                    and scope.get("ssh_key_fingerprint") and uid(scope.get("sg_id")), "plan lacks private scope")
            if "vm_id" in args:
                require(exact_id(args["vm_id"]) == row(cycle, "vm").get("id"), "another plan continuation VM")
    return {"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": tool, "arguments": args}}


def stdio(cycle, tool, args):
    env = env_for(cycle)
    if tool == "deploy_apply" and args.get("confirmed"):
        mcp_stmt(tool, args, cycle)
        env["P90_CORE_PLAN_APPLY"] = "1"
    if tool in ("deploy_plan", "status", "logs") or tool in ("deploy_apply", "release_floating_ip") and not args.get("confirmed"):
        env["P90_CORE_READONLY"] = "1"
    # Keep stdin open while the candidate works; EOF would cancel an in-flight tool.
    import selectors
    p = subprocess.Popen([env["P90_CORE_BIN"], "mcp", "--toolsets", "vpc" if tool == "release_floating_ip" else "deploy"], stdin=subprocess.PIPE,
                         stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, cwd=workdir(cycle), env=env, bufsize=0)
    leftover = b""
    def send(message):
        p.stdin.write((json.dumps(message) + "\n").encode())
        p.stdin.flush()

    def reply(token):
        nonlocal leftover
        deadline = time.monotonic() + 100
        with selectors.DefaultSelector() as selector:
            selector.register(p.stdout, selectors.EVENT_READ)
            while time.monotonic() < deadline:
                while b"\n" in leftover:
                    line, leftover = leftover.split(b"\n", 1)
                    packet = json.loads(line)
                    if packet.get("id") == token:
                        require(not packet.get("error"), "MCP protocol refused the request")
                        return packet
                require(len(leftover) <= 2_000_000, "MCP stdio reply too large")
                if not selector.select(max(0, deadline - time.monotonic())):
                    break
                chunk = os.read(p.stdout.fileno(), 65536)
                require(chunk, "MCP stdio closed unexpectedly")
                leftover += chunk
        raise RuntimeError("Compute: missing MCP stdio reply")
    try:
        send(INIT)
        reply(1)
        send(READY)
        send(mcp_stmt(tool, args, cycle))
        return mcp_result(reply(2), tool)
    finally:
        p.stdin.close()
        p.stdout.close()
        if p.poll() is None:
            p.terminate()
            try:
                p.wait(timeout=10)
            except subprocess.TimeoutExpired:
                p.kill()
                p.wait()


@contextlib.contextmanager
def http_server(cycle):
    import http.client
    import secrets
    import socket
    token = secrets.token_hex(32)
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    env = env_for(cycle, MCP_BEARER_TOKEN=token)
    proc = subprocess.Popen([env["P90_CORE_BIN"], "mcp", "--http", f"127.0.0.1:{port}",
                             "--allow-project", row(cycle, "scope")["project_id"], "--toolsets", "deploy"],
                            cwd=workdir(cycle), env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    session = None

    def request(method, path, message=None):
        nonlocal session
        connection = http.client.HTTPConnection("127.0.0.1", port, timeout=95)
        try:
            headers = {"Authorization": "Bearer " + token, "Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
            if session:
                headers["Mcp-Session-Id"] = session
            connection.request(method, path, None if message is None else json.dumps(message).encode(), headers)
            response = connection.getresponse()
            require(response.status in (200, 202), "MCP HTTP rejected the request")
            session = response.getheader("Mcp-Session-Id") or session
            data = response.read(2_000_001)
            require(len(data) <= 2_000_000, "MCP HTTP reply too large")
            if message is None or message.get("id") is None:
                return None
            messages = [json.loads(l[6:]) for l in data.splitlines() if l.startswith(b"data: ")]
            if not messages:
                messages = [json.loads(data)]
            return one([m for m in messages if m.get("id") == message["id"]], "no unique MCP HTTP reply")
        finally:
            connection.close()

    try:
        def ready():
            require(proc.poll() is None, "MCP HTTP did not start")
            try:
                request("GET", "/healthz")
            except OSError:
                return False
            return True
        wait_for(ready, 20, 0.25)
        request("POST", "/mcp", INIT)
        request("POST", "/mcp", READY)
        yield lambda tool, args: mcp_result(request("POST", "/mcp", mcp_stmt(tool, args, cycle)), tool)
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()


def through_mcp(send, tool, args, cycle, deadline):
    preview = send(tool, args)
    require(preview.get("status") == "confirmation_required", "missing MCP confirmation")
    # Echo only the candidate's compose identity, never unrelated arguments supplied by a reply.
    confirmed = {**args, "confirmed": True}
    if tool == "deploy" and preview.get("compose_hash"):
        confirmed["compose_hash"] = preview["compose_hash"]
    for _ in range(36):
        require(time.monotonic() < deadline, "MCP operation exceeded time allowance")
        if tool == "deploy" and local_record(cycle).get("vm_ip"):
            require(read_bound(cycle), "cannot verify resumed SSH target")
        result = send(tool, confirmed)
        if result.get("status") not in ("deploying", "creating", "deleting", "waiting"):
            return result
        next_args = result.get("next_args")
        require(result.get("next_tool") == tool and isinstance(next_args, dict), "incomplete continuation")
        require(set(next_args).issubset(set(confirmed) | {"confirmed","compose_hash","vm_id","deployment_id"}) and
                not next_args.get("force") and next_args.get("name") in (None, row(cycle, "scope")["vm_name"]),
                "continuation would widen scope")
        for key, expected in args.items():
            require(next_args.get(key) == expected, "continuation changed a scoped argument")
        require(next_args.get("confirmed") is True, "continuation lost its original confirmation")
        if confirmed.get("compose_hash"):
            require(next_args.get("compose_hash") == confirmed["compose_hash"], "continuation changed the Compose hash")
        if next_args.get("vm_id"):
            require(next_args["vm_id"] == row(cycle, "vm").get("id"), "continuation changed VM")
        confirmed = next_args
        time.sleep(2)
    raise RuntimeError("Compute: too many continuation calls")


def plan_preview(cycle, scope):
    args = {"mode": "manual", "product": "evo-p90", "name": scope["vm_name"], "zone": scope["zone_name"],
            "image": scope["image_id"], "flavor": scope["flavor_name"], "compose_file": "docker-compose.yml",
            "ports": [scope["port"]], "cidr": scope["cidr"], "disk_gb": scope["disk_cap_gb"], "disk_type": scope["disk_type_name"]}
    view = stdio(cycle, "deploy_plan", args)
    plan = view.get("plan_id")
    vm = view.get("vm") or {}
    net = view.get("network") or {}
    require(isinstance(plan, str) and re.fullmatch(r"plan-[a-f0-9]{12}", plan) and view.get("compose_required") is True
            and vm.get("name") == scope["vm_name"] and vm.get("image", {}).get("id") == scope["image_id"]
            and net.get("ports_source") == scope["cidr"] and net.get("open_ports") == [scope["port"]], "candidate VM plan does not match requested scope")
    approval = stdio(cycle, "deploy_apply", {"plan_id": plan})
    require(approval.get("status") == "confirmation_required", "plan application did not ask for confirmation")
    stored = json.loads((workdir(cycle) / ".evo" / "plans" / (plan + ".json")).read_bytes())
    require(isinstance(stored, dict) and stored.get("plan_id") == plan and stored.get("project_id") == scope["project_id"],
            "stored plan scope differs")
    network = stored["network"]
    if network.get("security_group_id") != scope["sg_id"]:
        save(cycle, "lifecycle", "checkpoint", plan_checked=True, apply_preview_only=True, shared_group_selected=True)
        return
    vm = stored["vm"]
    require(network.get("subnet_id") == scope["subnet_id"] and network.get("floating_ip") is True and
            not network.get("attach_fip_id") and vm.get("name") == scope["vm_name"] and vm["image"]["id"] == scope["image_id"]
            and vm["zone"]["id"] == scope["zone_id"] and vm["flavor"]["id"] == scope["flavor_id"]
            and vm["disk"].get("size_gb") == scope["disk_cap_gb"] and vm["disk"].get("type") == scope["disk_type_name"]
            and stored.get("ports_source") == scope["cidr"] and stored.get("ports") == [scope["port"]]
            and not stored.get("user_keys_hash") and not stored.get("source_params", {}).get("meta"),
            "stored plan differs from the reviewed VM specification")
    key = public_key_pair(stored.get("public_key", ""))
    local = public_key_pair((workdir(cycle) / ".evo" / "id_ed25519.pub").read_text())
    require(key == local, "plan's SSH key is not the run's key")
    fields = dict(creation_mode="plan", boot_name=scope["vm_name"] + "-disk", plan_id=plan,
                  ssh_key_fingerprint=hashlib.sha256(key.encode("ascii")).hexdigest())
    save(cycle, "scope", "scope", **fields)
    scope.update(fields)
    save(cycle, "lifecycle", "checkpoint", plan_checked=True, apply_preview_only=False)


def public_key_pair(value):
    parts = value.strip().split()
    require(len(parts) >= 2 and parts[0] == "ssh-ed25519", "unrecognized public key")
    decoded = base64.b64decode(parts[1], validate=True)
    require(len(decoded) == 51 and decoded[:19] == b"\x00\x00\x00\x0bssh-ed25519\x00\x00\x00 ", "invalid Ed25519 public key")
    return " ".join(parts[:2])


def refuse_bound_release(cycle):
    fid = exact_id(row(cycle, "fip").get("id"))
    held = stdio(cycle, "release_floating_ip", {"id": fid})
    require(held.get("status") == "refused" and held.get("reason") == "USAGE" and held.get("id") == fid
            and not row(cycle, "fip").get("delete_sent"), "occupied floating IP release did not refuse before consent")
    save(cycle, "lifecycle", "checkpoint", bound_release_refused=True)


def release_retained(cycle, deadline):
    rec = row(cycle, "fip")
    fid = exact_id(rec.get("id"))
    marker = rec.get("marker")
    require(marker and not rec.get("delete_sent"), "candidate did not preserve the requested address")
    def available():
        card = get(f"{API}/floating-ips/{fid}")
        require(card.get("id") == fid and card.get("description") == marker and card.get("name") == rec["name"]
                and not card.get("nat_gateway"), "retained IP changed")
        return card.get("state") == "available" and all(card.get(k) in (None, {}) for k in ("interface", "vm", "vm_id", "interface_id"))
    wait_for(available, 120)
    result = through_mcp(lambda tool, args: stdio(cycle, tool, args), "release_floating_ip", {"id":fid}, cycle, deadline)
    require(result.get("status") == "ok" and result.get("id") == fid and row(cycle, "fip").get("delete_sent"),
            "release of the retained owned address was not recorded")
    save(cycle, "lifecycle", "checkpoint", kept_then_released=True)


def check_status(cycle, answer):
    vm = exact_id(row(cycle, "vm").get("id"))
    require(answer.get("vm_id") == vm and answer.get("cloud_checked") is True and
            answer.get("cloud_state") in ("running", "active"), "status did not independently see the active VM")


def service_gone(code, body, errorcode):
    if code != 404:
        return False
    err = body[0] if isinstance(body, list) and len(body) == 1 else body
    if not isinstance(err, dict) or set(err) != {"code", "message"} or err["code"] != errorcode:
        return False
    if errorcode == "floating_ip_not_found":
        return isinstance(body, list) and err["message"] == "FloatingIp with this id is not found"
    messages = {
        "vm_not_found": {"vm not found", "vm with this id is not found", "virtual machine with this id is not found"},
        "security_group_not_found": {"security group not found", "security group with this id is not found"},
        "disk_not_found": {"disk not found", "disk with this id is not found"},
    }
    return isinstance(err["message"], str) and err["message"].lower() in messages.get(errorcode, set())


def confirm_gone(cycle, suffix, kind, collection, errorcode):
    rec = row(cycle, suffix)
    resource = exact_id(rec.get("id"))
    def missing():
        code, body = compute("GET", f"{API}/{collection}/{resource}")
        if service_gone(code, body, errorcode):
            return True
        require(code == 200 and isinstance(body, dict) and body.get("id") == resource, "resource absence is unconfirmed")
        return False
    wait_for(missing, 120, 3)
    save(cycle, suffix, kind, confirmed_absent=True)


def verify_app(cycle):
    vm = exact_id(row(cycle, "vm").get("id"))
    fip = row(cycle, "fip")
    fid = exact_id(fip.get("id"))
    fcard = get(f"{API}/floating-ips/{fid}")
    vcard = get(f"{API}/vms/{vm}")
    iface = exact_id(row(cycle, "iface").get("id"))
    require(fcard.get("id") == fid and (related_id(fcard, "interface") == iface or related_id(fcard, "vm") == vm) and vcard.get("id") == vm,
            "VM and floating IP relationship is unconfirmed")
    address = ipaddress.ip_address(fip.get("ip_address", ""))
    require(address.version == 4 and address.is_global and str(address) == fcard.get("ip_address"), "unconfirmed public address")
    verified = run_cli(cycle, "verify", timeout=100)
    require(isinstance(verified, list) and len(verified) == 1 and verified[0].get("healthy") is True,
            "candidate did not verify the exposed application port")
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *unused):
            raise RuntimeError("Compute: unexpected HTTP redirect")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    nonce = "p90-check-" + uuid.uuid4().hex  # Correlates this public HTTP request with this VM's SSH log.
    http = urllib.request.Request(f"http://{address}:{row(cycle,'scope')['port']}/", headers={"User-Agent": nonce})
    with opener.open(http, timeout=12) as r:
        require(r.status == 200 and b"nginx" in r.read(65536).lower(), "external Compose response not verified")
    require(read_bound(cycle), "SSH address changed")
    logs = run_cli(cycle, "logs", "--service", "web", "--tail", "25", raw=True, timeout=40)
    require(any(b"GET / HTTP/" in line and nonce.encode("ascii") in line for line in logs.splitlines()),
            "the VM did not log this particular HTTP request")
    check_status(cycle, run_cli(cycle, "status"))
    check_status(cycle, stdio(cycle, "status", {"refresh": True}))
    require(read_bound(cycle), "SSH address changed before MCP logs")
    require(stdio(cycle, "logs", {"service": "web", "tail": 25})["logged"], "workload log absent through MCP")
    save(cycle, "lifecycle", "checkpoint", application_verified=True, logs_verified=True, status_verified=True)


def read_bound(cycle):
    scope, vm, fip = row(cycle, "scope"), row(cycle, "vm"), row(cycle, "fip")
    vid, fid = exact_id(vm.get("id")), exact_id(fip.get("id"))
    vcard, fcard = get(f"{API}/vms/{vid}"), get(f"{API}/floating-ips/{fid}")
    require(vcard.get("id") == vid and vcard.get("name") == scope["vm_name"] and fcard.get("id") == fid
            and not fcard.get("nat_gateway") and fcard.get("name") == fip["name"], "scoped cards disagree")
    interfaces = vcard.get("interfaces")
    require(isinstance(interfaces, list) and len(interfaces) <= 1, "VM has unexpected interfaces")
    if not interfaces:
        return False
    interface = interfaces[0]
    require(interface.get("type") != "direct_ip" and interface.get("primary") is True, "unexpected VM interface")
    if not interface.get("floating_ip"):
        return False
    iid = exact_id(interface.get("id"))
    match = interface["floating_ip"]
    require(match.get("id") == fid and fcard.get("ip_address") == match.get("ip_address") and
            (related_id(fcard, "interface") == iid or related_id(fcard, "vm") == vid) and
            related_id(fcard, "interface") in (None, iid) and related_id(fcard, "vm") in (None, vid),
            "floating IP binding differs from direct create")
    icard = get(f"{API}/interfaces/{iid}")
    require(icard.get("id") == iid and related_id(icard, "vm") == vid and related_id(icard, "subnet") == scope["subnet_id"]
            and [g.get("id") for g in icard.get("security_groups", [])] == [scope["sg_id"]],
            "interface is outside the reviewed network")
    addr = ipaddress.ip_address(match.get("ip_address", ""))
    require(addr.version == 4 and addr.is_global, "no verified public address")
    local = local_record(cycle)
    require(local.get("vm_id") == vid and local.get("floating_ip_id") == fid and local.get("vm_ip") in (None, "", str(addr)),
            "local deployment changed or unavailable")
    for suffix, kind, fields in (("iface", "interface", dict(id=iid, vm_id=vid)),
                                 ("fip", "fip", dict(ip_address=str(addr), bound_vm_id=vid))):
        existing = row(cycle, suffix)
        require(all(existing.get(k) in ("", None, v) for k, v in fields.items()), "ownership receipt conflicts")
        if any(existing.get(k) != v for k, v in fields.items()):
            save(cycle, suffix, kind, **fields)
    return True


def witness_boot(cycle):
    vm = exact_id(row(cycle, "vm").get("id"))
    scope = row(cycle, "scope")
    attached = []
    for disk in catalog("disks", scope["project_id"]):
        vms = disk.get("vms", [])
        require(isinstance(vms, list), "unknown disk associations")
        if any(isinstance(v, dict) and v.get("id") == vm for v in vms):
            attached.append(disk)
    boot = one(attached, "VM does not have exactly its boot disk")
    require(boot.get("name") == scope["boot_name"] and
            len(boot["vms"]) == 1 and boot["vms"][0].get("primary") is True,
            "VM disk is not the boot disk created with this VM")
    require(row(cycle, "bootdisk").get("id") in (None, boot["id"]), "boot disk changed")
    save(cycle, "bootdisk", "disk", id=exact_id(boot["id"]), vm_id=vm)


def marker_evidence(cycle):
    fip = row(cycle, "fip")
    fid = exact_id(fip.get("id"))
    marker = fip.get("marker")
    require(marker, "no recorded create marker")
    card = get(f"{API}/floating-ips/{fid}")
    item = one([i for i in catalog("floating-ips", row(cycle, "scope")["project_id"]) if i["id"] == fid],
               "direct create ID missing from project collection")
    require(card.get("id") == fid, "unexpected IP card ID")
    fields = {}
    for stage, raw in (("list", item), ("card", card)):
        fields[stage + "_marker_present"] = bool(raw.get("description"))
        fields[stage + "_marker_exact"] = raw.get("description") == marker
    save(cycle, "fip", "fip", **fields)
    require(fields["list_marker_exact"] and fields["card_marker_exact"], "could not verify attempt marker by card and list")


def delete_once(cycle, suffix, kind, path, query=None):
    rec = row(cycle, suffix)
    if rec.get("delete_sent"):
        return
    save(cycle, suffix, kind, delete_sent=True)
    code, _ = compute("DELETE", path, query=query)
    require(code in (200, 202, 204, 404), "delete rejected or uncertain")


def retire_group(cycle):
    g = row(cycle, "group")
    if not g.get("post_sent") or g.get("confirmed_absent"):
        return
    gid = exact_id(g.get("id"))
    path = f"{GROUPS}/{gid}"
    if g.get("delete_sent"):
        confirm_gone(cycle, "group", "group", "security-groups", "security_group_not_found")
        return
    group_card(cycle, detached=True)
    current = owned_rule_set(cycle, gid)
    for suffix in ("app-rule", "ssh", "egress"):
        rule = row(cycle, suffix)
        if not rule.get("post_sent") and not rule.get("id"):
            continue
        rid = exact_id(rule.get("id"))  # An uncertain POST cannot license deletion of a guessed rule or its group.
        if rid in current:
            group_card(cycle, detached=True)
            require(rid in owned_rule_set(cycle, gid), "membership changed")
            delete_once(cycle, suffix, "rule", f"{path}/rules/{rid}")
            wait_for(lambda: rid not in owned_rule_set(cycle, gid))
        save(cycle, suffix, "rule", confirmed_absent=True)
        current = owned_rule_set(cycle, gid)
    group_card(cycle, detached=True)
    require(not owned_rule_set(cycle, gid), "group is not empty")
    delete_once(cycle, "group", "group", path)
    confirm_gone(cycle, "group", "group", "security-groups", "security_group_not_found")


COLLECTION = {"vm": ("vm", "vms", "vm_not_found"), "bootdisk": ("disk", "disks", "disk_not_found"),
              "fip": ("fip", "floating-ips", "floating_ip_not_found")}


def clean_resource(cycle, suffix):
    rec = row(cycle, suffix)
    if not rec or rec.get("confirmed_absent"):
        return
    kind, collection, missing = COLLECTION[suffix]
    rid = exact_id(rec.get("id"))
    path = f"{API}/{collection}/{rid}"
    code, card = compute("GET", path)
    if service_gone(code, card, missing):
        save(cycle, suffix, kind, confirmed_absent=True)
        return
    require(code == 200 and isinstance(card, dict) and card.get("id") == rid, "ambiguous resource visibility")
    if rec.get("delete_sent"):
        confirm_gone(cycle, suffix, kind, collection, missing)
        return
    scope = row(cycle, "scope")
    pid = exact_id(scope["project_id"])
    require(related_id(card, "project") in (None, pid), "resource project changed")
    if suffix == "vm":
        require(card.get("name") == scope["vm_name"] and related_id(card, "availability_zone") in (None, scope["zone_id"]),
                "VM no longer matches create receipt")
        witness_boot(cycle)
        for ni in card.get("interfaces", []):
            iid = exact_id(ni.get("id"))
            require(ni.get("primary") and ni.get("type") != "direct_ip" and iid == row(cycle, "iface").get("id"),
                    "unproven network interface; VM preserved")
            icard = get(f"{API}/interfaces/{iid}")
            require(icard.get("id") == iid and related_id(icard, "vm") == rid and related_id(icard, "subnet") == scope["subnet_id"] and
                    [g.get("id") for g in icard.get("security_groups", [])] == [scope["sg_id"]], "VM network scope changed")
            if ni.get("floating_ip"):
                require(related_id(ni, "floating_ip") == row(cycle, "fip").get("id"), "foreign floating IP attached")
        require(isinstance(card.get("interfaces"), list) and len(card["interfaces"]) == 1, "VM topology uncertain")
    elif suffix == "fip":
        require(card.get("description") == rec.get("marker") and bool(rec.get("marker")) and card.get("name") == rec.get("name")
                and all(card.get(k) in (None, {}) for k in ("vm", "interface", "nat_gateway")) and
                not card.get("vm_id") and not card.get("interface_id") and not card.get("nat_gateway_id")
                and card.get("state") in ("available", "created", "down", "unattached"), "address is occupied or identity changed")
    else:
        require(rec.get("vm_id") == row(cycle, "vm").get("id") and row(cycle, "vm").get("confirmed_absent")
                and card.get("name") == scope["boot_name"] and card.get("vms") == []
                and card.get("state") in ("available", "created"), "disk is occupied or identity changed")
    delete_once(cycle, suffix, kind, path, {"project_id": pid})
    confirm_gone(cycle, suffix, kind, collection, missing)


def cleanup_cycle(cycle):
    if cycle == 2 and row(2, "vm").get("post_sent"):
        import disk
        import network
        network.cleanup()
        disk.cleanup()
    # If VM disappearance is unconfirmed, preserve its possibly attached resources.
    clean_resource(cycle, "vm")
    clean_resource(cycle, "fip")
    clean_resource(cycle, "bootdisk")
    if row(cycle, "vm").get("post_sent"):
        require(row(cycle, "bootdisk").get("confirmed_absent"), "boot disk ID missing; billing fate unconfirmed")
    retire_group(cycle)


def cleanup():
    records = ledger()
    pending = False
    for cycle in (2, 1):
        if any(k.startswith(f"core.{cycle}.") for k in records):
            try:
                cleanup_cycle(cycle)
            except (RuntimeError, OSError, ValueError, KeyError):
                pending = True
    require(not pending, "one or more private receipts unresolved")


@contextlib.contextmanager
def active_budget(seconds):
    # Linux Actions runner, main thread. Recovery is a separate workflow step.
    def expire(*_):
        raise TimeoutError("Compute: active cycle time exhausted; use receipt-based cleanup")
    previous = signal.signal(signal.SIGALRM, expire)
    try:
        signal.setitimer(signal.ITIMER_REAL, seconds)
        yield
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous)


def run():
    template = preflight()
    for cycle in (1, 2):
        with active_budget(45 * 60):
            run_cycle(cycle, template)


def run_cycle(cycle, template):
    deadline = time.monotonic() + 45 * 60
    require(not any(k.startswith(f"core.{cycle}.") for k in ledger()), "fresh cycle required")
    name = "evo-p90-" + os.environ["GITHUB_RUN_ID"] + "-" + str(cycle)
    require(not any(m.get("name") == name for m in catalog("vms", template["project_id"])), "VM name already occupied")
    scope = {**template, "vm_name": name, "boot_name": name + "-boot", "port": 18084 + cycle}
    save(cycle, "scope", "scope", **scope)
    scope["sg_id"] = setup_group(cycle, scope)
    prepare_workspace(cycle, scope)
    if cycle == 2:
        plan_preview(cycle, scope)
    preview = run_cli(cycle, "--dry-run", "deploy", "--cidr", scope["cidr"])
    require(preview.get("status") == "dry_run", "CLI deploy preview absent")
    if cycle == 2 and scope.get("creation_mode") == "plan":
        applied = through_mcp(lambda tool, args: stdio(cycle, tool, args), "deploy_apply",
                              {"plan_id": scope["plan_id"]}, cycle, deadline)
        require(applied.get("status") == "running" and applied.get("vm_id") == exact_id(row(cycle, "vm").get("id")),
                "plan did not create the scoped machine")
        wait_for(lambda: read_bound(cycle), 120)
        save(cycle, "lifecycle", "checkpoint", actual_plan_apply=True)
    if cycle == 1:
        interrupt_deploy(cycle)
        wait_for(lambda: read_bound(cycle), 180)
    deploy = through_mcp(lambda tool, args: stdio(cycle, tool, args), "deploy", {"cidr": scope["cidr"]}, cycle, deadline)
    require(deploy.get("status") == "ok" and deploy.get("vm_id") == exact_id(row(cycle, "vm").get("id")), "deployment incomplete")
    wait_for(lambda: read_bound(cycle), 120)
    marker_evidence(cycle)
    witness_boot(cycle)
    verify_app(cycle)
    owned_rule_set(cycle, scope["sg_id"])
    require(exact_id(row(cycle, "app-rule").get("id")) in owned_rule_set(cycle, scope["sg_id"]), "application rule not seen")
    if cycle == 1:
        run_cli(cycle, "--yes", "destroy", cut=True)
        require(read_bound(cycle), "changed target after cold restart")
        partial = run_cli(cycle, "--yes", "destroy", timeout=300, hold_rule=True)
        rule = exact_id(row(cycle, "app-rule").get("id"))
        held = row(cycle, "rule-cut")
        require(partial.get("status") == "partial" and partial.get("cleanup_pending") is True and
                held.get("cut") is True and held.get("rule_id") == rule and held.get("group_id") == scope["sg_id"],
                "candidate did not keep an unresolved rule receipt")
        for suffix, (kind, collection, missing) in COLLECTION.items():
            confirm_gone(cycle, suffix, kind, collection, missing)
        require(rule in owned_rule_set(cycle, scope["sg_id"]), "retained candidate rule missing before retry")
        destroyed = run_cli(cycle, "--yes", "destroy", timeout=300)
        require(rule not in owned_rule_set(cycle, scope["sg_id"]) and not local_record(cycle),
                "candidate rule or local receipt survived recovery")
        save(cycle, "lifecycle", "checkpoint", rule_only_retry_verified=True)
    else:
        import disk
        import network
        refuse_bound_release(cycle)
        network.before_vm_destroy(cycle)
        network.cleanup()
        disk.run(cycle)
        disk.cleanup()
        witness_boot(cycle)
        with http_server(cycle) as send:
            destroyed = through_mcp(send, "destroy", {"vm_id": row(cycle, "vm")["id"], "release_ip": False}, cycle, deadline)
    if cycle == 1:
        require(destroyed.get("status") == "destroyed", "CLI destroy did not complete")
    else:
        require(destroyed.get("status") == "ok" and destroyed.get("deleted_vm") == row(cycle, "vm")["id"],
                "MCP destroy did not confirm our VM")
    require(not destroyed.get("cleanup_pending"), "candidate cleanup pending")
    if cycle == 2:
        release_retained(cycle, deadline)
    require(time.monotonic() < deadline, "cycle budget elapsed")
    for suffix, (kind, collection, missing) in COLLECTION.items():
        confirm_gone(cycle, suffix, kind, collection, missing)
    cleanup_cycle(cycle)
    save(cycle, "lifecycle", "checkpoint", cycle_checked=True)
