"""Disk/image scenario, using only local synthetic responses and receipts."""
import subprocess
import unittest
from unittest.mock import patch

import disk


def guid(n):
    return f"aaaaaaaa-aaaa-4aaa-8aaa-{n:012}"


class DiskScenario(unittest.TestCase):
    def setUp(self):
        self.vm, self.boot, self.data, self.image, self.zone, self.project = map(guid, range(1, 7))
        self.scope = dict(vm_name="owned", project_id=self.project, zone_id=self.zone, disk_type_name="SSD")
        self.rows = {"scope": self.scope, "lifecycle": {"application_verified": True},
                     "vm": {"id": self.vm}, "bootdisk": {"id": self.boot, "vm_id": self.vm}}
        self.vmcard = dict(id=self.vm, name="owned", state="running")
        self.cards = {self.boot: self.disk_card(self.boot, True)}
        self.steps = []
        self.writes = []
        self.bind(disk, "rec", lambda key: self.rows.get(key, {}))
        self.bind(disk, "save", self.save)
        self.bind(disk.core, "get", self.getvm)
        self.bind(disk, "compute", self.compute)
        self.bind(disk.core, "delete_once", self.emergency_delete)
        self.bind(disk.core, "catalog", lambda *_: [v for v in self.cards.values() if "vms" in v])
        self.bind(disk.core, "read_bound", lambda c: c == 2)
        self.bind(disk.core, "witness_boot", lambda c: self.assertTrue(c == 2 and self.boot in self.cards))
        self.bind(disk, "invoke", self.invoke)
        self.bind(disk, "refusal", self.refusal)
        self.bind(disk.time, "sleep", lambda _: self.fail("unexpected polling of an ambiguous response"))

    def bind(self, obj, name, value):
        mock = patch.object(obj, name, value)
        mock.start()
        self.addCleanup(mock.stop)

    def save(self, key, kind, **fields):
        self.rows.setdefault(key, {"kind": kind}).update(fields)

    def disk_card(self, did, boot=False):
        return dict(id=did, name="owned-boot" if boot else "owned-data", project_id=self.project,
                    availability_zone_id=self.zone, state="in_use" if boot else "available",
                    bootable=boot, vms=[{"id": self.vm, "primary": True}] if boot else [],
                    size=20 if boot else 10)

    def getvm(self, path):
        self.assertEqual(path, "/api/v1/vms/" + self.vm)
        return self.vmcard

    def compute(self, method, path, *, query=None, body=None):
        parts = path.split("/")
        did = parts[4]
        if method == "GET":
            self.assertEqual(len(parts), 5)
            if did in self.cards:
                return 200, self.cards[did]
            typ = "image" if parts[3] == "images" else "disk"
            return 404, [{"code": typ + "_not_found", "message": typ + " not found"}]
        self.writes.append((method, path, body))
        self.assertEqual(method, "POST")
        self.assertEqual(query, {"project_id": self.project})
        self.assertEqual(body, {"vm_id": self.vm})
        if did == self.boot and parts[5] == "attach":
            self.cards[did] = self.disk_card(did, True)
        elif did == self.data and parts[5] == "detach":
            self.cards[did].update(vms=[], state="available")
        else:
            self.fail("out of scope write")
        return 202, {}

    def emergency_delete(self, cycle, key, kind, path, query):
        self.assertEqual(cycle, 2)
        self.assertIn(key, (disk.DATA, disk.IMAGE))
        did = self.rows[key]["id"]
        self.assertEqual(query, {"project_id": self.project})
        self.assertEqual(path.split("/")[-1], did)
        self.writes.append(("DELETE", path, None))
        self.rows[key]["delete_sent"] = True
        del self.cards[did]

    def invoke(self, step, *argv):
        self.steps.append((step, argv))
        if step == "create-data":
            self.save(disk.DATA, "disk", name="owned-data", id=self.data, post_sent=True)
            self.cards[self.data] = self.disk_card(self.data)
        elif step == "resize-data":
            self.assertEqual(argv, ("disk", "resize", self.data, "--size-gb", "20"))
            self.cards[self.data]["size"] = 20
        elif step == "attach-data":
            self.assertEqual(argv, ("disk", "attach", self.data, "--vm", self.vm))
            self.cards[self.data].update(vms=[{"id": self.vm, "primary": False}], state="in_use")
        elif step in ("detach-data", "detach-boot"):
            did = self.data if step == "detach-data" else self.boot
            self.assertEqual(argv, ("disk", "detach", did, "--vm", self.vm))
            self.cards[did].update(vms=[], state="available")
            self.save("disk-op-" + step, "checkpoint", post_sent=True)
        elif step in ("delete-data", "delete-image"):
            key, did = (disk.DATA, self.data) if step == "delete-data" else (disk.IMAGE, self.image)
            self.rows[key]["delete_sent"] = True
            del self.cards[did]
        elif step == "stop-for-image":
            self.assertEqual(argv, ("vm", "stop", self.vm))
            self.vmcard["state"] = "stopped"
        elif step == "create-image":
            self.assertFalse(self.cards[self.boot]["vms"])
            self.assertEqual(argv[argv.index("--disk-id")+1], self.boot)
            self.assertEqual(argv[argv.index("--zone")+1], self.zone)
            self.save(disk.IMAGE, "image", name="owned-image", id=self.image, post_sent=True, source_disk_id=self.boot)
            self.cards[self.image] = {"id": self.image, "name": "owned-image", "project_id": self.project,
                                      "type":"private", "availability_zones":[{"state":"created","availability_zone_id":self.zone}]}
        elif step == "read-image":
            return {"ready": True, "image": {"id": self.image}}
        elif step == "restore-boot":
            self.assertFalse(disk._vm_disks())
            self.cards[self.boot] = self.disk_card(self.boot, True)
            self.save("disk-op-" + step, "checkpoint", post_sent=True)
        else:
            self.fail("unexpected step")
        return {}

    def refusal(self, step, argv, **env):
        if step == "invalid-zone":
            self.assertEqual(argv[0:2], ["disk","create"])
            return env["CLOUDRU_REGION"].lower() + " is not a known zone"
        self.assertEqual(step,"attached-delete")
        self.assertEqual(argv, ["disk","delete",self.data])
        self.assertEqual(self.cards[self.data]["vms"][0]["id"],self.vm)
        return "disk " + self.data + " attached"

    def test_product_lifecycle_then_exact_service_absence(self):
        disk.run(2)
        self.assertEqual([s[0] for s in self.steps],
                         ["create-data", "resize-data", "attach-data", "detach-data", "delete-data",
                          "stop-for-image", "detach-boot", "create-image", "read-image", "delete-image", "restore-boot"])
        self.assertTrue(all(self.rows["disk-result"][key] for key in
                            ("data_grew", "data_attachment_verified", "data_detached", "image_ready",
                             "image_removal_verified", "primary_restored", "d58_refused", "d59_refused")))
        self.assertTrue(all(self.rows[key]["confirmed_absent"] for key in (disk.IMAGE, disk.DATA)))
        self.assertEqual(self.writes, [])
        disk.cleanup()
        self.assertEqual(self.writes, [])

    def test_unknown_create_does_not_search_names_or_delete(self):
        self.save(disk.DATA, "disk", post_sent=True, name="owned-data")
        self.cards[self.data] = self.disk_card(self.data)
        with self.assertRaisesRegex(RuntimeError, "unresolved"):
            disk.cleanup()
        with self.assertRaisesRegex(RuntimeError, "previous disk attempt"):
            disk.data_checks()
        self.assertEqual(self.steps + self.writes, [])

    def test_foreign_attached_vm_blocks_emergency_detach_and_delete(self):
        self.invoke("create-data"); self.invoke("attach-data", "disk", "attach", self.data, "--vm", self.vm)
        self.cards[self.data]["vms"][0]["id"] = guid(99)
        with self.assertRaisesRegex(RuntimeError, "unresolved"):
            disk.cleanup()
        self.assertEqual(self.writes, [])

    def test_gateway_and_foreign_scope_never_confirm_absence(self):
        self.invoke("create-data"); self.rows[disk.DATA]["delete_sent"] = True
        self.bind(disk, "compute", lambda *a, **kw: (404, {"code": "route_not_found", "message": "not found"}))
        with self.assertRaisesRegex(RuntimeError, "unresolved"):
            disk.cleanup()
        self.assertNotIn("confirmed_absent", self.rows[disk.DATA])
        self.assertFalse(disk.absent(404, {"code":"disk_not_found","message":"Disk not found"}, disk.IMAGE))

    def test_recovery_uses_known_disk_only_and_is_idempotent(self):
        self.invoke("create-data"); self.invoke("attach-data", "disk", "attach", self.data, "--vm", self.vm)
        disk.cleanup(); disk.cleanup()
        self.assertEqual(self.writes, [("POST", "/api/v1/disks/"+self.data+"/detach", {"vm_id":self.vm}),
                                       ("DELETE", "/api/v1/disks/"+self.data, None)])
        self.assertTrue(self.rows[disk.DATA]["confirmed_absent"])

    def test_restore_blocked_by_extra_disk_and_recovers_existing_attachment(self):
        self.invoke("stop-for-image", "vm", "stop", self.vm)
        self.invoke("detach-boot", "disk", "detach", self.boot, "--vm", self.vm)
        extra = self.disk_card(guid(8))
        extra["vms"] = [{"id": self.vm, "primary": False}]
        self.cards[guid(8)] = extra
        with self.assertRaisesRegex(RuntimeError, "unexpectedly has a disk"):
            disk.cleanup()
        self.assertEqual(self.writes, [])
        del self.cards[guid(8)]
        disk.cleanup(); disk.cleanup()
        self.assertEqual(self.writes, [("POST", "/api/v1/disks/"+self.boot+"/attach", {"vm_id":self.vm})])
        self.assertTrue(self.rows["disk-result"]["primary_restored"])

    def test_unverified_vm_blocks_everything(self):
        with self.assertRaisesRegex(RuntimeError, "not verified"):
            disk.run(1)
        self.rows["lifecycle"].clear()
        with self.assertRaisesRegex(RuntimeError, "not verified"):
            disk.run(2)
        self.assertFalse(self.steps or self.writes)

class CandidateEnvelope(unittest.TestCase):
    def test_typed_cli_usage_is_before_consent(self):
        result=subprocess.CompletedProcess([],1,b"",b'trace\n{"error":{"code":1,"reason":"USAGE","message":"attached"}}\n')
        with patch.object(disk.core,"env_for",return_value={"P90_CORE_BIN":"/unused"}), patch.object(disk.core,"workdir",return_value="."), patch.object(disk.subprocess,"run",return_value=result) as run:
            self.assertEqual(disk.refusal("attached-delete",["disk","delete",guid(2)]),"attached")
            self.assertIn("--dry-run",run.call_args.args[0])
