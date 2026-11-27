"""Cycle 2 disk and private-image checks; all normal writes use the candidate CLI."""
import json
import subprocess
import time

import core
from common import compute


C = 2
DATA = "data-disk"
IMAGE = "new-image"
BOOT = "bootdisk"
PRIVATE = "private"


def required(ok, reason):
    if not ok:
        raise RuntimeError("disk/image: " + reason)


def rec(key):
    return core.row(C, key)


def save(key, kind, **fields):
    core.save(C, key, kind, **fields)


def rid(key):
    return core.exact_id(rec(key).get("id"))


def path(key):
    return "/api/v1/" + ("images/" if key == IMAGE else "disks/") + rid(key)


def absent(status, content, key):
    if key != IMAGE:
        return core.service_gone(status, content, "disk_not_found")
    err = content[0] if isinstance(content, list) and len(content) == 1 else content
    return (status == 404 and isinstance(err, dict) and set(err) == {"code", "message"}
            and err.get("code") == "image_not_found"
            and str(err.get("message", "")).lower() in {"image not found", "image with this id is not found"})


def card(key, *, allow_gone=False):
    scope = rec("scope")
    row = rec(key)
    code, data = compute("GET", path(key))
    if allow_gone and absent(code, data, key):
        required(row.get("delete_sent") is True, "absence before delete was confirmed")
        return None
    required(code == 200 and isinstance(data, dict) and data.get("id") == rid(key), "incomplete resource card")
    required(data.get("name") == (scope.get("boot_name", scope["vm_name"] + "-boot") if key == BOOT else row.get("name")),
             "resource name differs from the receipt")
    required(core.related_id(data, "project") in (None, scope["project_id"]), "project differs from the receipt")
    if key == IMAGE:
        required(data.get("type") == PRIVATE and row.get("source_disk_id") == rid(BOOT),
                 "image is not the private image from the owned boot disk")
    else:
        required(core.related_id(data, "availability_zone") in (None, scope["zone_id"]), "disk zone differs")
        required(isinstance(data.get("vms"), list) and len(data["vms"]) <= 1, "ambiguous disk attachment")
        for vm in data["vms"]:
            required(isinstance(vm, dict) and vm.get("id") == rid("vm") and vm.get("primary") is (key == BOOT),
                     "disk attached outside the approved VM/role")
        required(data.get("bootable") is (key == BOOT), "unexpected disk bootable flag")
    return data


def invoke(step, *argv):
    env = core.env_for(C, P90_DISK_STEP=step)
    p = subprocess.run([env["P90_CORE_BIN"], "--yes", "--json", *argv], cwd=core.workdir(C), env=env,
                       stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=200, check=False)
    required(p.returncode == 0, "candidate command did not succeed")
    required(len(p.stdout) <= 2_000_000, "oversized candidate reply")
    result = json.loads(p.stdout)
    required(isinstance(result, dict), "candidate returned no result")
    return result


def refusal(step, argv, **overrides):
    env = core.env_for(C, P90_CORE_READONLY="1", P90_DISK_STEP=step, **overrides)
    result = subprocess.run([env["P90_CORE_BIN"], "--dry-run", "--json", *argv], cwd=core.workdir(C), env=env,
                            capture_output=True, timeout=90)
    required(result.returncode == 1, "refusal did not occur before consent")
    for data in (result.stdout, result.stderr):
        for line in data.splitlines():
            try:
                out = json.loads(line)
            except (ValueError, UnicodeError):
                continue
            if isinstance(out, dict):
                err = out.get("error")
                if isinstance(err, dict) and err.get("reason") == "USAGE" and isinstance(err.get("message"), str):
                    return err["message"].lower()
                if out.get("reason") == "USAGE" and isinstance(err, str):
                    return err.lower()
    raise RuntimeError("disk/image: candidate USAGE refusal missing")


def wait_for(check, seconds=100):
    until = time.monotonic() + seconds
    while True:
        if check():
            return
        required(time.monotonic() < until, "readback deadline reached")
        time.sleep(4)


def disk_state(key, *, attached, size=None):
    d = card(key)
    if size is not None and d.get("size") != size:
        return False
    return ((len(d["vms"]) == 1 and d.get("state") in ("in_use", "in-use", "attached"))
            if attached else (not d["vms"] and d.get("state") == "available"))


def image_ready():
    im = card(IMAGE)
    states = im.get("availability_zones")
    required(isinstance(states, list) and len(states) <= 1, "unexpected image zone set")
    if not states:
        return False
    z = states[0]
    required(isinstance(z, dict) and z.get("availability_zone_id") == rec("scope")["zone_id"],
             "image build moved to another zone")
    return z.get("state") == "created"


def vm_stopped():
    data = core.get("/api/v1/vms/" + rid("vm"))
    required(data.get("id") == rid("vm") and data.get("name") == rec("scope")["vm_name"], "VM identity changed")
    return str(data.get("state", "")).lower() in ("stopped", "shutoff")


def data_checks():
    scope = rec("scope")
    name = scope["vm_name"] + "-data"
    required(not rec(DATA), "previous disk attempt exists")
    bad_name = scope["vm_name"] + "-nozone"
    bad_zone = "ru.AZ-2-no-such-zone"
    required(not any(x.get("name") == bad_name for x in core.catalog("disks", scope["project_id"])),
             "negative disk name already occupied")
    message = refusal("invalid-zone", ["disk", "create", bad_name, "--size-gb", "10"], CLOUDRU_REGION=bad_zone)
    required(bad_zone.lower() in message and not any(x.get("name") == bad_name for x in
             core.catalog("disks", scope["project_id"])), "D58 invalid-zone refusal not established")
    invoke("create-data", "disk", "create", name, "--size-gb", "10", "--zone", scope["zone_id"],
           "--disk-type", scope["disk_type_name"])
    did = rid(DATA)
    wait_for(lambda: disk_state(DATA, attached=False, size=10))
    invoke("resize-data", "disk", "resize", did, "--size-gb", "20")
    wait_for(lambda: disk_state(DATA, attached=False, size=20))
    invoke("attach-data", "disk", "attach", did, "--vm", rid("vm"))
    wait_for(lambda: disk_state(DATA, attached=True, size=20))
    message = refusal("attached-delete", ["disk", "delete", did])
    required(did in message and ("attached" in message or "подключ" in message) and
             not rec(DATA).get("delete_sent") and disk_state(DATA, attached=True, size=20),
             "D59 attached disk refusal not established")
    invoke("detach-data", "disk", "detach", did, "--vm", rid("vm"))
    wait_for(lambda: disk_state(DATA, attached=False, size=20))
    remove(DATA)
    save("disk-result", "checkpoint", data_grew=True, data_attachment_verified=True, data_detached=True, d58_refused=True, d59_refused=True)


def remove(key, *, recovery=False):
    kind = "image" if key == IMAGE else "disk"
    row = rec(key)
    if not row or row.get("confirmed_absent"):
        return
    required(core.uid(row.get("id")), "outbound create has no direct resource ID; manual review required")
    if card(key, allow_gone=True) is not None:
        if key == DATA:
            required(disk_state(DATA, attached=False), "data disk is still attached")
        if not row.get("delete_sent"):
            if recovery:
                core.delete_once(C, key, kind, path(key), {"project_id": rec("scope")["project_id"]})
            else:
                invoke("delete-image" if key == IMAGE else "delete-data", kind, "delete", rid(key))
        wait_for(lambda: card(key, allow_gone=True) is None, 130)
    save(key, kind, confirmed_absent=True)


def image_checks():
    scope = rec("scope")
    required(not rec(IMAGE), "previous image attempt exists")
    required(disk_state(BOOT, attached=True), "source is not the own attached primary disk")
    invoke("stop-for-image", "vm", "stop", rid("vm"))
    wait_for(vm_stopped, 100)
    invoke("detach-boot", "disk", "detach", rid(BOOT), "--vm", rid("vm"))
    wait_for(lambda: disk_state(BOOT, attached=False), 100)
    save("disk-result", "checkpoint", own_boot_detached=True)
    name = scope["vm_name"] + "-image"
    invoke("create-image", "image", "create", name, "--disk-id", rid(BOOT), "--zone", scope["zone_id"],
           "--description", name)
    # An image may take longer than this window to build. Never spend beyond
    # the approved job deadline merely to obtain a green result.
    wait_for(image_ready, 360)
    out = invoke("read-image", "image", "get", rid(IMAGE))
    required(out.get("ready") is True and rid(IMAGE) in
             (out.get("id"), out.get("image_id"), (out.get("image") or {}).get("id")), "candidate image readback failed")
    remove(IMAGE)
    restore_boot()
    save("disk-result", "checkpoint", image_ready=True, image_removal_verified=True)


def restore_boot(*, recovery=False):
    if not rec("disk-op-detach-boot"):
        return
    required(vm_stopped(), "VM state is not suitable for restoring its primary disk")
    cur = card(BOOT)
    if cur["vms"]:
        required(disk_state(BOOT, attached=True), "primary disk is not ready")
        save("disk-result", "checkpoint", primary_restored=True)
        return
    required(disk_state(BOOT, attached=False), "cannot establish that the owned boot disk is free")
    if not rec("disk-op-restore-boot"):
        if recovery:
            # Re-attach only when the original cycle's VM has no disk. Do
            # not turn an ambiguous power/detach result into a second POST.
            required(not _vm_disks(), "VM unexpectedly has a disk")
            save("disk-op-restore-boot", "checkpoint", post_sent=True)
            code, _ = compute("POST", path(BOOT) + "/attach", query={"project_id": rec("scope")["project_id"]},
                             body={"vm_id": rid("vm")})
            required(code in (200, 201, 202, 204), "boot disk restoration not accepted")
        else:
            required(not _vm_disks(), "VM unexpectedly has a disk")
            invoke("restore-boot", "disk", "attach", rid(BOOT), "--vm", rid("vm"))
    wait_for(lambda: disk_state(BOOT, attached=True))
    save("disk-result", "checkpoint", primary_restored=True)


def _vm_disks():
    vm = rid("vm")
    disks = core.catalog("disks", rec("scope")["project_id"])
    found = []
    for d in disks:
        required(isinstance(d.get("vms"), list) and all(isinstance(v, dict) and core.uid(v.get("id")) for v in d["vms"]),
                 "disk attachments cannot be enumerated")
        if any(v["id"] == vm for v in d["vms"]):
            found.append(d["id"])
    return found


def run(cycle):
    required(cycle == C and rec("lifecycle").get("application_verified") and core.read_bound(C),
             "the second Compose cycle is not verified")
    core.witness_boot(C)
    data_checks()
    image_checks()


def cleanup():
    # No new image or disk is ever created by this independent recovery path.
    if not rec(DATA) and not rec(IMAGE) and not rec("disk-op-detach-boot"):
        return
    pending = False
    for key in (IMAGE, DATA):
        try:
            if key == DATA and rec(DATA).get("id") and not rec(DATA).get("confirmed_absent"):
                d = card(DATA, allow_gone=True)
                if d and d["vms"]:
                    if not rec("disk-op-detach-data"):
                        save("disk-op-detach-data", "checkpoint", post_sent=True)
                        code, _ = compute("POST", path(DATA) + "/detach", query={"project_id": rec("scope")["project_id"]},
                                         body={"vm_id": rid("vm")})
                        required(code in (200, 201, 202, 204), "emergency detach not accepted")
                    wait_for(lambda: disk_state(DATA, attached=False))
            remove(key, recovery=True)
        except (RuntimeError, OSError, ValueError, KeyError):
            pending = True
    if rec("disk-op-detach-boot") and not rec("disk-result").get("primary_restored"):
        restore_boot(recovery=True)
    required(not pending, "one or more directly owned disk/image receipts unresolved")
