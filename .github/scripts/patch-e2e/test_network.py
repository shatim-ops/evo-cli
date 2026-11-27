import os
import unittest
from unittest.mock import patch
from uuid import UUID

import network as n


def ident(i):
    return str(UUID(int=i))


P, VR, VM, NIC, SG, SUB, TG, LB, RT, OP, DISK, OTHER = map(ident, range(1, 13))


class NetworkTest(unittest.TestCase):
    def setUp(self):
        self.book = {}
        self.env = patch.dict(os.environ, {"CLOUDRU_PROJECT_ID": P, "P90_SOURCE_RUN": "123"})
        self.env.start()
        self.addCleanup(self.env.stop)
        self.patches = [
            patch.object(n.common, "ledger", side_effect=lambda: self.book),
            patch.object(n.common, "note", side_effect=self.note),
        ]
        for p in self.patches:
            p.start()
            self.addCleanup(p.stop)

    def note(self, kind, key, **fields):
        self.book[key] = {**self.book.get(key, {}), "kind": kind, **fields}

    def own(self, key, rid, **extra):
        n.mark(key, project=P, returned_directly=True, create_dispatched=True, id=rid, **extra)

    def test_complete_project_inventory_required(self):
        def entry(rid, p=P):
            return {"id": rid, "project_id": p}
        for pages in (
            [{"items": [], "total": 1}],
            [{"items": [entry(VM)]}],
            [{"items": [entry(VM)], "total": 2}, {"items": [entry(VM)], "total": 2}],
            [{"items": [entry(VM, OTHER)], "total": 1}],
            [{"items": [entry(VM)], "total": 2}, {"items": [], "total": 1}],
            [{"items": [], "total": 0, "next_page_token": "bad"}],
        ):
            with self.subTest(pages=pages), patch.object(n, "get", side_effect=pages), self.assertRaises(RuntimeError):
                n.catalog("backend")
        with patch.object(n, "get", side_effect=[{"items": [entry(VM)], "total": 2, "offset": 0},
                                                 {"items": [entry(OTHER)], "total": 2, "offset": 1}]):
            self.assertEqual(set(n.catalog("backend")), {VM, OTHER})

    def test_no_name_recovery_for_ambiguous_post(self):
        n.mark("route", attempted=True, project=P, create_dispatched=True)
        with patch.object(n, "catalog") as listing, patch.object(n, "invoke") as tool, self.assertRaises(RuntimeError):
            n.cleanup_key("route")
        listing.assert_not_called()
        tool.assert_not_called()

    def test_route_in_other_vpc_and_nonadvancing_token_rejected(self):
        self.own("vpc", VR)
        for result in ([{"routes": [{"id": RT, "vpcId": OTHER}]}],
                       [{"routes": [{"id": RT}], "nextPageToken": "same"},
                        {"routes": [{"id": VM}], "nextPageToken": "same"}]):
            with patch.object(n, "get", side_effect=result), self.assertRaises(RuntimeError):
                n.catalog("route")

    def test_route_operation_resource_must_match_post(self):
        self.own("route", RT, operation=OP)
        with patch.object(n, "get", return_value={"id": OP, "done": True, "resourceId": OTHER}), self.assertRaises(RuntimeError):
            n.operation("route")
        self.assertEqual(n.owned("route"), RT)

    def test_only_original_route_allowed_and_then_gone_with_accepted_delete(self):
        self.book["core.2.scope"] = {"sg_id": SG}
        self.own("vpc", VR)
        self.own("route", RT)
        self.own("backend", VM)
        n.mark("nic", id=NIC, parent=VM)
        def route(id_, iface=NIC):
            return {"id": id_, "subnet": n.DEST, "vpcId": VR,
                    "nextHop": {"virtualMachine": {"virtualMachine": {"id": VM}, "interface": {"id": iface}}}}
        for card in (route(RT, OTHER), route(OTHER)):
            with patch.object(n, "catalog", return_value={RT: card}), patch.object(n, "invoke") as tool, self.assertRaises(RuntimeError):
                n.cleanup_key("route")
            tool.assert_not_called()
        def remove(key, _, args, method, endpoint, *unused, **kwargs):
            self.assertEqual((key, method, endpoint, args), ("route", "DELETE", f"/v1/vpcs/{VR}/routes/static/{RT}",
                                                            {"route_id": RT, "vpc_id": VR}))
            n.mark("route", delete_dispatched=True, delete_accepted=True, delete_operation=OP)
            return True
        with patch.object(n, "catalog", side_effect=[{RT: route(RT), OTHER: route(OTHER)}, {OTHER: route(OTHER)}]), \
             patch.object(n, "invoke", side_effect=remove) as call, \
             patch.object(n, "get", return_value={"id": OP, "resourceId": RT, "done": True}):
            n.cleanup_key("route")
            n.cleanup_key("route")
            call.assert_called_once()
        self.assertTrue(n.state("route")["absent_confirmed"])

    def test_foreign_member_prevents_cascade(self):
        self.own("vpc", VR)
        self.book["core.2.scope"] = {}
        with patch.object(n, "catalog", side_effect=[{VR: {"name": n.name("vpc"), "id": VR}}, {OTHER: {}}]), \
             patch.object(n, "invoke") as tool, self.assertRaises(RuntimeError):
            n.cleanup_key("vpc")
        tool.assert_not_called()

    def test_foreign_listener_and_backend_rejected(self):
        for k, rid in (("vpc", VR), ("tg", TG), ("backend", VM), ("lb", LB)):
            self.own(k, rid)
        n.mark("nic", id=NIC)
        good = dict(id=TG, name=n.name("tg"), vpc_id=VR, target_vms=[{"id": VM, "interface_id": NIC}])
        n.target_and_listener("tg", good)
        for change in ({"target_vms": [{"id": OTHER}]}, {"target_vms": [{"id": VM, "interface_id": OTHER}]}, {"vpc_id": OTHER}):
            with self.assertRaises(RuntimeError):
                n.target_and_listener("tg", {**good, **change})
        with self.assertRaises(RuntimeError):
            n.target_and_listener("lb", dict(id=LB, name=n.name("lb"), vpc_id=VR,
                                            rules=[{"target_group_id": OTHER, "listeners": [{"port": 80, "target_port": 80}]}]))

    def test_subnet_backend_private_shape(self):
        net = dict(sg_id=SG, zone_id=OTHER, flavor_name="lowcost10-2-4", image_id=TG, image_name="ubuntu-22.04")
        args, body = n.backend_spec(net, SUB)
        self.assertTrue(args["no_public_ip"])
        self.assertEqual(body[0]["disks"], [dict(name=n.name("backend") + "-boot", size=20, disk_type_name="SSD")])
        self.assertEqual(body[0]["interfaces"], [dict(type="regular", subnet_id=SUB, interface_security_enabled=True, security_groups=[SG])])

    def test_snat_never_redirects_another_private_vm(self):
        self.book["core.2.vm"] = {"id": RT}
        self.own("backend", VM)
        net = {"zone_id": TG}
        rows = {VM: {}, RT: {}, OTHER: {"id": OTHER, "availability_zone_id": NIC}}
        with patch.object(n, "catalog", return_value=rows):
            self.assertTrue(n.only_run_vms(net))
            rows[OTHER]["availability_zone_id"] = TG
            self.assertFalse(n.only_run_vms(net))
            del rows[OTHER]["availability_zone_id"]
            self.assertFalse(n.only_run_vms(net))

    def test_cold_recovery_never_removes_reattached_boot(self):
        n.mark("boot", parent=VM, id=DISK, seen_attached=True)
        old = dict(id=DISK, name=n.name("backend") + "-boot", vms=[])
        moved = {**old, "vms": [{"id": OTHER}]}
        with patch.object(n, "catalog", return_value={DISK: old}), patch.object(n, "get", return_value=moved), \
             patch.object(n, "invoke") as tool, self.assertRaises(RuntimeError):
            n.cleanup_boot(VM)
        tool.assert_not_called()
        def remove(_k, _tool, args, method, url, *_unused, **kwargs):
            self.assertEqual((args, method, url, kwargs["query"]), ({"disk_id": DISK}, "DELETE", "/api/v1/disks/" + DISK, {"project_id": P}))
            n.mark("boot", delete_dispatched=True, delete_accepted=True)
        with patch.object(n, "catalog", side_effect=[{DISK: old}, {}]), patch.object(n, "get", return_value=old), \
             patch.object(n, "invoke", side_effect=remove) as tool:
            n.cleanup_boot(VM)
        tool.assert_called_once()

    def test_lb_address_has_separate_receipt_only_when_witnessed(self):
        self.own("lb", LB)
        self.own("vpc", VR)
        self.own("tg", TG)
        addr = "1.2.3.5"  # Local fixture, never contacted.
        card = {"id": LB, "name": n.name("lb"), "vpc_id": VR, "rules": [
            {"target_group_id": TG, "listeners": [{"port": 80, "target_port": 80}]}],
                "external_address": {"ipv4": addr}}
        with patch.object(n, "catalog", return_value={}):
            n.target_and_listener("lb", card)
        self.assertNotIn("compute_id", n.state("listener"))
        del self.book["network.listener"]
        with patch.object(n, "catalog", return_value={OTHER: {"id": OTHER, "ip_address": addr}}):
            n.target_and_listener("lb", card)
        self.assertEqual(n.state("listener")["compute_id"], OTHER)

    def test_failed_delete_never_retried_after_restart(self):
        self.own("backend", VM, delete_dispatched=True)
        with patch.object(n, "invoke") as tool, self.assertRaises(RuntimeError):
            n.cleanup_key("backend")
        tool.assert_not_called()
        self.assertFalse(n.state("backend").get("absent_confirmed"))


if __name__ == "__main__":
    unittest.main()
