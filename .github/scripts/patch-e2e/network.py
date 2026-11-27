"""Run-owned networking fixtures. Native candidate performs each single fenced mutation."""
import ipaddress
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time
import uuid

import common


CIDR = "10.203.40.0/24"
DEST = "198.18.255.0/24"
PORT = 80
TABLE = {
    "vpc": ("vpc", "/v1/vpcs", "vpcs", "vpc_create", "vpc_delete"),
    "route": ("vpc", "", "routes", "vpc_route_create", "vpc_route_delete"),
    "subnet": ("compute", "/api/v1/subnets", "items", "subnet_create", "subnet_delete"),
    "backend": ("compute", "/api/v1/vms", "items", "provision", "destroy"),
    "tg": ("nlb", "/v2/targetGroups", "target_groups", "tg_create", "tg_delete"),
    "lb": ("nlb", "/v2/networkLoadBalancers", "balancers", "lb_create", "lb_delete"),
    "snat": ("compute", "/api/v1/nat-gateways", "items", "snat_create", "snat_delete"),
    "address": ("compute", "/api/v1/floating-ips", "items", "", ""),
    "nic": ("compute", "/api/v1/interfaces", "items", "", ""),
    "boot": ("compute", "/api/v1/disks", "items", "", "disk_delete"),
}
DEPS = {"vpc": ("route", "subnet", "lb", "tg"), "subnet": ("backend", "lb"),
        "backend": ("tg", "route"), "tg": ("lb",)}
POLL = 36


def require(ok, reason):
    if not ok:
        raise RuntimeError("network: " + reason)


def identity(value):
    require(isinstance(value, str), "missing exact ID")
    try:
        actual = uuid.UUID(value)
    except ValueError:
        raise RuntimeError("network: invalid ID") from None
    require(str(actual) == value and actual.int != 0, "noncanonical ID")
    return value


def project():
    return identity(os.environ["CLOUDRU_PROJECT_ID"])


def state(key):
    return common.ledger().get("network." + key, {})


def mark(key, **fields):
    common.note("network-" + key, "network." + key, **fields)


def path(key):
    if key == "route":
        return "/v1/vpcs/" + owned("vpc") + "/routes/static"
    return TABLE[key][1]


def owned(key):
    r = state(key)
    require(r.get("project") == project() and r.get("returned_directly") is True, "direct create receipt missing")
    return identity(r.get("id"))


def name(key):
    run = os.environ["P90_SOURCE_RUN"]
    require(re.fullmatch(r"[1-9][0-9]{0,19}", run), "invalid run")
    return "evo-p90-" + run + "-" + key


def relation(card, key):
    ids = []
    for field in (key, key + "_id", key + "Id"):
        val = card.get(field)
        if isinstance(val, dict):
            val = val.get("id")
        if val is not None:
            ids.append(identity(val))
    require(len(set(ids)) <= 1, "contradictory relationship")
    return ids[0] if ids else None


def scope(card):
    require(isinstance(card, dict) and relation(card, "project") in (None, project()), "foreign project")
    return card


def get(host, endpoint, params=None):
    status, data = common.service("GET", host, endpoint, query=params)
    require(status == 200 and isinstance(data, dict), "unverified GET response")
    return data


def catalog(key):
    host, _, field = TABLE[key][:3]
    endpoint, result = path(key), {}
    params = {} if key == "route" else {"project_id": project()}
    offset, total, tokens = 0, None, set()
    for _ in range(30):
        if host == "compute":
            params.update(limit=100, offset=offset)
        body = get(host, endpoint, params)
        scope(body)
        items = body.get(field)
        require(isinstance(items, list), "invalid collection")
        for item in items:
            scope(item)
            rid = identity(item.get("id"))
            require(rid not in result, "duplicate collection ID")
            require(key != "route" or relation(item, "vpc") in (None, owned("vpc")), "route in foreign VPC")
            result[rid] = item
        if host == "compute":
            count = body.get("total")
            require(type(count) is int and count >= len(result) and (total is None or count == total)
                    and body.get("offset", offset) == offset, "incomplete project inventory")
            require(not any(body.get(k) for k in ("next", "next_page_token", "nextPageToken", "has_more")), "unknown continuation")
            total, offset = count, len(result)
            if offset == count:
                return result
            require(items, "empty intermediate page")
        else:
            out, inp = ("nextPageToken", "pageToken") if host == "vpc" else ("next_page_token", "page_token")
            require(not any(body.get(k) for k in ("nextPageToken", "next_page_token", "next", "has_more") if k != out), "unknown continuation")
            token = body.get(out, "")
            require(isinstance(token, str), "invalid continuation")
            if not token:
                return result
            require(items and token not in tokens, "nonadvancing collection")
            tokens.add(token)
            params[inp] = token
    raise RuntimeError("network: page bound reached")


def invoke(key, tool, args, method, endpoint, shape, expected=None, query=None, group=None):
    spec = dict(Key=key, Host=TABLE[key][0] + ".api.cloud.ru", Path=endpoint, Method=method,
                Project=project(), Shape=shape, Query=query or {}, Expected=expected, GroupID=group or "")
    directory = Path(os.environ["RUNNER_TEMP"]) / "p90-private-net"
    directory.mkdir(mode=0o700, exist_ok=True)
    with tempfile.NamedTemporaryFile(mode="w", dir=directory) as af:
        json.dump(args, af)
        af.flush()
        cmd = [os.environ["P90_NET_BIN"], "--yes", "tools", "call", tool, "--args-file", af.name, "--json"]
        # Candidate output for provision includes a private SSH credential. Never persist it.
        process = subprocess.run(cmd, cwd=directory, env={**os.environ, "P90_NET_CALL": json.dumps(spec)},
                                 stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=240)
    return process.returncode == 0


def sleep_poll(i):
    if i < POLL - 1:
        time.sleep(5)


def operation(key, delete=False):
    row = state(key)
    host = TABLE[key][0]
    if host == "compute":
        return
    op = identity(row.get("delete_operation" if delete else "operation"))
    url = ("/v1/vpcs/operations/" if host == "vpc" else "/v2/operations/") + op
    for i in range(POLL):
        card = get(host, url, {"id": op} if host == "nlb" else None)
        require(card.get("id") == op and type(card.get("done")) is bool and not card.get("error"), "operation unavailable or failed")
        if card["done"]:
            rid = card.get("resourceId" if host == "vpc" else "resource_id")
            require(rid is None or identity(rid) == row.get("id", rid), "operation resource mismatch")
            if delete:
                require(row.get("delete_resource_id") in (None, row.get("id")), "delete receipt mismatch")
            else:
                mark(key, id=identity(rid or row.get("id")))
            return
        sleep_poll(i)
    raise RuntimeError("network: operation still pending")


def create(key, args, body, *, group=None):
    require(not state(key), "create already attempted")
    rows = catalog(key)
    require(not any(r.get("name") == name(key) for r in rows.values()), "name occupied")
    if key == "route":
        require(not rows, "new VPC contains other routes")
    mark(key, project=project(), name=name(key), attempted=True)
    host = TABLE[key][0]
    shape = "vm" if key == "backend" else host + "op" if host != "compute" else "object"
    endpoint = "/api/v1.1/vms" if key == "backend" else path(key)
    ok = invoke(key, TABLE[key][3], args, "POST", endpoint, shape, body,
                {"project_id": project()} if key == "snat" else None, group)
    require(state(key).get("returned_directly") is True, "missing original POST response")
    operation(key)
    rid = owned(key)
    require(ok, "native create reported a failure")
    for i in range(POLL):
        if rid in catalog(key):
            return rid
        sleep_poll(i)
    raise RuntimeError("network: issued resource not visible")


def one_row(value):
    require(isinstance(value, list) and len(value) == 1 and isinstance(value[0], dict), "unexpected number of children")
    return value[0]


def vm_children(card, net):
    vid, sid, gid = owned("backend"), owned("subnet"), identity(net["sg_id"])
    require(card.get("id") == vid and card.get("name") == name("backend"), "backend identity differs")
    interface = one_row(card.get("interfaces"))
    iid = identity(interface.get("id"))
    require(interface.get("type") == "regular" and not interface.get("floating_ip") and not interface.get("public_ip"), "unexpected public interface")
    nic = scope(get("compute", "/api/v1/interfaces/" + iid))
    require(nic.get("id") == iid and relation(nic, "vm") == vid and relation(nic, "subnet") == sid and
            not nic.get("floating_ip") and one_row(nic.get("security_groups")).get("id") == gid,
            "backend interface relationship differs")
    attached = []
    for disk in catalog("boot").values():
        vms = disk.get("vms")
        require(isinstance(vms, list) and all(isinstance(v, dict) and identity(v.get("id")) for v in vms), "unknown disk attachments")
        if any(v["id"] == vid for v in vms):
            attached.append(disk)
    boot = one_row(attached)
    require(boot.get("name") == name("backend") + "-boot" and one_row(boot["vms"]).get("primary") is True,
            "boot disk relationship differs")
    for key, child in (("nic", nic), ("boot", boot)):
        cid = identity(child["id"])
        old = state(key)
        require(old.get("id", cid) == cid and old.get("parent", vid) == vid, "previous child receipt differs")
        if not old:
            mark(key, project=project(), id=cid, parent=vid, seen_attached=True)
    return iid


def backend_spec(net, subnet):
    n = name("backend")
    args = dict(name=n, project_id=project(), region=net["zone_id"], flavor=net["flavor_name"], image_id=net["image_id"],
                subnet_id=subnet, disk_size=20, no_public_ip=True, wait=False)
    body = [dict(name=n, project_id=project(), availability_zone_id=net["zone_id"], flavor_name=net["flavor_name"],
                 image_name=net["image_name"], disks=[dict(name=n + "-boot", size=20, disk_type_name="SSD")],
                 interfaces=[dict(type="regular", subnet_id=subnet, interface_security_enabled=True, security_groups=[net["sg_id"]])])]
    return args, body


def preflight():
    require(Path(os.environ["P90_NET_BIN"]).is_file(), "fenced candidate not built")
    for key in ("vpc", "subnet", "backend", "tg", "lb", "snat"):
        require(not any(row.get("name") == name(key) for row in catalog(key).values()), "name occupied")


def target_and_listener(key, card):
    require(card.get("id") == owned(key) and card.get("name") == name(key) and relation(card, "vpc") == owned("vpc"),
            "target scope differs")
    if key == "tg":
        vm = one_row(card.get("target_vms"))
        require(vm.get("id") == owned("backend"), "foreign backend")
        iface = relation(vm, "interface")
        require(iface in (None, state("nic")["id"]), "foreign target interface")
        return
    rule = one_row(card.get("rules"))
    listener = one_row(rule.get("listeners"))
    require(relation(rule, "target_group") == owned("tg") and listener.get("port") == PORT and
            listener.get("target_port") == PORT, "different listener or backend")
    ext = card.get("external_address")
    require(isinstance(ext, dict), "no allocated listener address")
    ip = ipaddress.IPv4Address(ext["ipv4"])
    require(ip.is_global and not ip.is_multicast, "invalid listener address")
    fields = dict(parent=owned("lb"), ip=str(ip))
    if ext.get("id"):
        fields["id"] = identity(ext["id"])
    old = state("listener")
    require(all(old.get(k, v) == v for k, v in fields.items()), "listener identity changed")
    if not old:
        # A service-managed IPv4 address may not appear as a standalone Compute FIP.
        # Do not equate absence from that API alone with its release.
        matches = [r for r in catalog("address").values() if r.get("ip_address") == str(ip)]
        require(len(matches) <= 1, "ambiguous address inventory")
        if matches:
            observed = identity(matches[0]["id"])
            require(fields.get("id") in (None, observed), "conflicting external address IDs")
            fields["compute_id"] = observed
        mark("listener", **fields)


def before_vm_destroy(cycle):
    require(cycle == 2, "hook only valid in cycle two")
    book = common.ledger()
    net = book["core.2.scope"]
    require(net.get("project_id") == project() and identity(net.get("sg_id")) == book["core.2.group"]["id"]
            and net["flavor_name"] == "lowcost10-2-4" and net["zone_name"] == "ru.AZ-2" and
            net["image_name"] == "ubuntu-22.04", "core scope mismatch")
    vpc = create("vpc", {"name": name("vpc"), "description": name("vpc")},
                 {"projectId": project(), "name": name("vpc"), "description": name("vpc")})
    vpc_card = scope(get("vpc", path("vpc") + "/" + vpc))
    require(vpc_card.get("id") == vpc and vpc_card.get("name") == name("vpc"), "VPC identity differs")
    import private_dns
    private_dns.negative(vpc)
    subnet = create("subnet", dict(name=name("subnet"), cidr=CIDR, vpc_id=vpc, zone=net["zone_name"], routed_network=False),
                    dict(name=name("subnet"), subnet_address=CIDR, project_id=project(), vpc_id=vpc,
                         availability_zone_id=net["zone_id"], routed_network=False))
    sn = catalog("subnet")[subnet]
    require(relation(sn, "vpc") == vpc and sn.get("subnet_address") == CIDR and sn.get("routed_network") is False,
            "subnet outside requested network")
    args, body = backend_spec(net, subnet)
    vm = create("backend", args, body, group=net["sg_id"])
    for i in range(POLL):
        card = scope(get("compute", path("backend") + "/" + vm))
        if card.get("state") == "running":
            break
        require(card.get("state") in ("creating", "starting", "provisioning"), "unexpected backend state")
        sleep_poll(i)
    else:
        raise RuntimeError("network: backend not running")
    nic = vm_children(card, net)
    route = create("route", dict(vpc_id=vpc, subnet=DEST, next_hop_vm_id=vm, next_hop_interface_id=nic, description=name("route")),
                   dict(projectId=project(), vpcId=vpc, subnet=DEST, description=name("route"),
                        nextHop=dict(virtualMachine=dict(virtualMachine=dict(id=vm), interface=dict(id=nic)))))
    check_route(catalog("route")[route])
    tg = create("tg", dict(name=name("tg"), vm_id=vm, interface_id=nic),
                dict(project_id=project(), name=name("tg"), target_vm_specs=[dict(id=vm, interface_id=nic)]))
    target_and_listener("tg", scope(get("nlb", path("tg") + "/" + tg)))
    args = dict(name=name("lb"), target_group_id=tg, vpc_id=vpc, subnet_id=subnet, zone=net["zone_name"],
                port=PORT, target_port=PORT, hc_port=PORT, external=True)
    lb = create("lb", args, dict(project_id=project(), name=name("lb"), vpc_id=vpc,
                    availability_zone_specs=[dict(name=net["zone_name"], subnet_ids=[subnet])],
                    rules=[dict(name="rule-" + name("lb"), target_group_id=tg,
                                listeners=[dict(name="lst-80", port=PORT, target_port=PORT)], health_check=dict(port=PORT))],
                    external_address_spec={"allocate": True}))
    target_and_listener("lb", scope(get("nlb", path("lb") + "/" + lb)))
    gateways = catalog("snat")
    zones = [identity(relation(g, "availability_zone")) for g in gateways.values()]
    if net["zone_id"] in zones:
        mark("snat", project=project(), occupied=True)
        return
    if not only_run_vms(net):
        mark("snat", project=project(), occupied=True, skip_reason="other zone VM or unproven zone")
        return
    create("snat", dict(name=name("snat"), zone=net["zone_name"]),
           dict(name=name("snat"), project_id=project(), availability_zone_id=net["zone_id"], nat_type="Public sNAT"))


def only_run_vms(net):
    # SNAT changes the default egress of the whole zone. A shared tenant can
    # have workloads without a pre-existing gateway. Never route those VMs.
    permitted = {owned("backend"), identity(common.ledger()["core.2.vm"]["id"])}
    for rid, vm in catalog("backend").items():
        if rid not in permitted and relation(vm, "availability_zone") in (None, net["zone_id"]):
            return False
    return True


def require_net_zone(net):
    require(only_run_vms(net), "other workloads may depend on the gateway")


def check_route(card):
    require(card.get("id") == owned("route") and card.get("subnet") == DEST and relation(card, "vpc") in (None, owned("vpc")),
            "different route")
    hop = card.get("nextHop", {}).get("virtualMachine", {})
    require(hop.get("virtualMachine", {}).get("id") == owned("backend") and
            hop.get("interface", {}).get("id") == state("nic").get("id"), "route next hop differs")


def safe_delete(key, card):
    scope(card)
    rid = owned(key)
    require(card.get("id") == rid and (key == "route" or card.get("name") == name(key)), "different object")
    net = common.ledger()["core.2.scope"]
    if key in ("tg", "lb"):
        target_and_listener(key, scope(get("nlb", path(key) + "/" + rid)))
    elif key == "route":
        check_route(card)
    elif key == "vpc":
        require(not catalog("route") and not any(relation(r, "vpc") in (None, rid) for r in catalog("subnet").values()),
                "VPC might contain another resource")
    elif key == "subnet":
        require(relation(card, "vpc") == owned("vpc") and card.get("subnet_address") == CIDR
                and not any(relation(r, "subnet") in (None, rid) for r in catalog("nic").values()), "subnet may have another interface")
    elif key == "backend":
        vm_children(scope(get("compute", path(key) + "/" + rid)), net)
    elif key == "snat":
        require_net_zone(net)
        require(relation(card, "availability_zone") == net["zone_id"], "gateway zone differs")


def cleanup_boot(vid):
    child = state("boot")
    require(child.get("parent") == vid and child.get("seen_attached"), "missing exact disk receipt")
    did = identity(child.get("id"))
    disk = catalog("boot").get(did)
    if disk is None:
        return
    direct = scope(get("compute", path("boot") + "/" + did))
    for item in (disk, direct):
        require(item.get("id") == did and item.get("name") == name("backend") + "-boot" and
                item.get("vms") == [], "remaining boot disk is no longer detached or differs")
    if not child.get("delete_dispatched"):
        invoke("boot", "disk_delete", {"disk_id": did}, "DELETE", path("boot") + "/" + did,
               "object", query={"project_id": project()})
    require(state("boot").get("delete_accepted"), "boot disk deletion unconfirmed")
    for i in range(POLL):
        if did not in catalog("boot"):
            return
        sleep_poll(i)
    raise RuntimeError("network: owned boot disk still exists")


def cleanup_key(key):
    row = state(key)
    if not row or row.get("absent_confirmed") or row.get("occupied"):
        return
    # The wire checkpoint precedes the cloud write. Local preflight alone cannot create anything.
    if not row.get("create_dispatched"):
        mark(key, absent_confirmed=True, never_dispatched=True)
        return
    require(row.get("returned_directly") is True, "ambiguous create; no ordinary lookup can prove ownership")
    if not row.get("id"):
        operation(key)
    rid = owned(key)
    for dep in DEPS.get(key, ()):
        rd = state(dep)
        require(not rd or rd.get("absent_confirmed"), "child cleanup still pending")
    if not row.get("delete_dispatched"):
        card = catalog(key).get(rid)
        require(card is not None, "no positive pre-delete ownership check")
        safe_delete(key, card)
        args = {"id": rid}
        if key == "route":
            args = {"vpc_id": owned("vpc"), "route_id": rid}
        elif key == "subnet":
            args = {"subnet_id": rid}
        elif key == "backend":
            args = {"vm_id": rid, "release_ip": False}
        host = TABLE[key][0]
        invoke(key, TABLE[key][4], args, "DELETE", path(key) + "/" + rid,
               "object" if host == "compute" else host + "op",
               query={"project_id": project()} if key in ("backend", "snat") else None)
    require(state(key).get("delete_accepted") is True, "no direct deletion response")
    operation(key, delete=True)
    for i in range(POLL):
        if rid not in catalog(key):
            if key == "backend":
                cleanup_boot(rid)
                for part in ("boot", "nic"):
                    child = state(part)
                    require(child.get("seen_attached") and child.get("parent") == rid, "child receipt missing")
                    require(identity(child["id"]) not in catalog(part), "private backend child still present")
                    mark(part, absent_confirmed=True)
            elif key == "lb":
                child = state("listener")
                require(child.get("parent") == rid and child.get("ip"), "allocated listener address not witnessed")
                remaining = [v for v in catalog("lb").values()
                             if isinstance(v.get("external_address"), dict) and v["external_address"].get("ipv4") == child["ip"]]
                require(not remaining, "listener address still published by a balancer")
                if child.get("compute_id"):
                    require(identity(child["compute_id"]) not in catalog("address"), "witnessed public address remains")
                mark("listener", absent_from_own_balancer=True,
                     independent_fip_absence=bool(child.get("compute_id")))
            mark(key, absent_confirmed=True)
            return
        sleep_poll(i)
    raise RuntimeError("network: issued object remains visible")


def cleanup():
    pending = []
    for key in ("snat", "lb", "tg", "route", "backend", "subnet", "vpc"):
        try:
            cleanup_key(key)
        except Exception:
            pending.append(key)
    require(not pending, "own cleanup incomplete: " + ", ".join(pending))
