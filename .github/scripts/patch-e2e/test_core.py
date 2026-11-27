"""Offline resource identity, recovery and normal Compose entry-point checks."""
import contextlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch
import uuid

import core


def identity(number):
    return str(uuid.UUID(int=number))


P, Z, G, R, V, F, I, D = (identity(i) for i in range(1, 9))
GONE_FIP = [{"code": "floating_ip_not_found", "message": "FloatingIp with this id is not found"}]
GONE_SG = [{"code": "security_group_not_found", "message": "Security group with this id is not found"}]


class CoreTests(unittest.TestCase):
    def setUp(self):
        self.records = {}
        p = patch.object(core, "ledger", side_effect=lambda: self.records)
        p.start()
        self.addCleanup(p.stop)
        p = patch.object(core, "note", side_effect=self.note)
        self.writer = p.start()
        self.addCleanup(p.stop)
        p = patch.object(core, "compute")
        self.cloud = p.start()
        self.addCleanup(p.stop)
        p = patch.dict("os.environ", {"GITHUB_RUN_ID": "123", "CLOUDRU_PROJECT_ID": P})
        p.start()
        self.addCleanup(p.stop)

    def note(self, kind, key, **fields):
        self.records.setdefault(key, {"kind": kind}).update(fields)

    def init_group(self):
        core.save(1, "scope", "scope", project_id=P, zone_id=Z, vm_name="evo-p90-123-1", port=18085, cidr="93.184.216.34/32")
        core.save(1, "group", "group", post_sent=True, name="evo-p90-123-1-sg", id=G)
        return {"id": G, "name": "evo-p90-123-1-sg", "description": "evo-p90-123-1-sg",
                "has_interfaces": False, "default": False, "project_id": P, "availability_zone_id": Z}

    def test_only_protected_public_runner_singleton_can_open_ingress(self):
        for value in ("", "0.0.0.0/0", "1.2.3.4", "1.1.1.0/24", "10.0.0.1/32",
                      "224.0.0.1/32", "127.0.0.1/32", "2001:4860::1/128"):
            with self.subTest(value=value), patch.dict("os.environ", {"P90_RUNNER_CIDR": value}):
                with self.assertRaises(RuntimeError):
                    core.preflight()
                self.cloud.assert_not_called()
        self.init_group()
        with patch.dict("os.environ", {"P90_RUNNER_CIDR":"93.184.216.34/32"}):
            self.assertEqual(core.runner_cidr(), core.baserule(1, "ssh")["remote_ip_prefix"])
        self.assertEqual(core.baserule(1, "egress")["remote_ip_prefix"], "0.0.0.0/0")

    def test_empty_or_failed_post_receipt_never_sweeps_by_name(self):
        core.cleanup()
        self.cloud.assert_not_called()
        core.save(1, "group", "group", post_sent=True, name="would-match-someone-else")
        for _ in range(2):
            with self.assertRaises(RuntimeError):
                core.cleanup()
        self.cloud.assert_not_called()

    def test_missing_durable_checkpoint_prevents_post(self):
        self.writer.side_effect = RuntimeError("journal unavailable")
        with self.assertRaises(RuntimeError):
            core.direct_post(1, "group", "group", core.GROUPS, {"name": "unique"})
        self.cloud.assert_not_called()

    def test_ambiguous_post_never_recreates_or_substitutes_id(self):
        self.cloud.return_value = (202, {"operation_id": R})
        for _ in range(2):
            with self.assertRaises(RuntimeError):
                core.direct_post(1, "group", "group", core.GROUPS, {"name": "unique"})
        self.assertEqual(self.cloud.call_count, 1)
        self.assertTrue(core.row(1, "group")["post_sent"])
        self.assertNotIn("id", core.row(1, "group"))

    def test_nonempty_initial_counts_survive_recovery(self):
        self.cloud.return_value = (201, {"id": G, "ingress_rules_count": 1, "egress_rules_count": 0})
        with self.assertRaises(RuntimeError):
            core.direct_post(1, "group", "group", core.GROUPS, {"name": "unique"})
        self.assertEqual(core.row(1, "group")["id"], G)
        self.cloud.reset_mock()
        with self.assertRaises(RuntimeError):
            core.owned_rule_set(1, G)
        self.cloud.assert_not_called()

    def test_group_with_foreign_member_incomplete_page_or_interface_is_preserved(self):
        for condition in ("foreign", "missing total", "interfaces", "conflicting counts", "other project", "wrong description"):
            with self.subTest(condition=condition):
                self.records.clear()
                card = self.init_group()
                members = {"items": [], "total": 0}
                if condition == "foreign":
                    members = {"items": [{"id": R, "direction": "ingress"}], "total": 1}
                elif condition == "missing total":
                    del members["total"]
                elif condition == "conflicting counts":
                    card["egress_rules_count"] = 1
                elif condition == "interfaces":
                    card["has_interfaces"] = True
                elif condition == "other project":
                    card["project_id"] = V
                else:
                    card["description"] = "other"
                self.cloud.reset_mock()
                self.cloud.side_effect = lambda method, path, **opts: (200, members if path.endswith("/rules") else card)
                with self.assertRaises(RuntimeError):
                    core.retire_group(1)
                self.assertTrue(all(c.args[0] == "GET" for c in self.cloud.call_args_list))
                if condition == "foreign":
                    members.update(items=[], total=0)
                    with self.assertRaises(RuntimeError):
                        core.retire_group(1)
                    self.assertTrue(all(c.args[0] == "GET" for c in self.cloud.call_args_list))

    def test_only_exact_rules_then_empty_group_checked_after_delete(self):
        card = self.init_group()
        core.save(1, "ssh", "rule", id=R, post_sent=True)
        rule = {"id": R, **core.baserule(1, "ssh")}
        visible = {R: rule}
        deletions = []
        def request(method, path, **opts):
            if method == "DELETE":
                deletions.append(path)
                if path.endswith("/rules/" + R):
                    visible.pop(R)
                else:
                    self.assertFalse(visible)
                return (204, None)
            if path.endswith("/rules"):
                return (200, {"items": list(visible.values()), "total": len(visible)})
            return (404, GONE_SG) if path in deletions else (200, card)
        self.cloud.side_effect = request
        core.retire_group(1)
        self.assertEqual(deletions, [core.GROUPS + "/" + G + "/rules/" + R, core.GROUPS + "/" + G])
        self.assertTrue(core.row(1, "group")["confirmed_absent"])
        self.assertTrue(core.row(1, "ssh")["confirmed_absent"])
        core.retire_group(1)
        self.assertEqual(len(deletions), 2)

    def test_retained_fip_uses_only_exact_readback_no_second_delete(self):
        for bad in ({"code": "project_not_found", "message": "Not found"}, [],
                    [dict(GONE_FIP[0], message="Not found")], dict(GONE_FIP[0]),
                    [dict(GONE_FIP[0], detail="route")], GONE_FIP * 2):
            with self.subTest(body=bad):
                self.assertFalse(core.service_gone(404, bad, "floating_ip_not_found"))
        self.assertFalse(core.service_gone(200, GONE_FIP, "floating_ip_not_found"))
        core.save(1, "scope", "scope", project_id=P)
        core.save(1, "fip", "fip", post_sent=True, id=F, name="mine", marker="private-attempt-marker")
        card = {"id": F, "name": "mine", "description": "private-attempt-marker", "state": "available"}
        self.cloud.side_effect = [(200, card), (202, None), (404, {"code": "project_not_found", "message": "unknown"})]
        with self.assertRaises(RuntimeError):
            core.clean_resource(1, "fip")
        self.assertNotIn("confirmed_absent", core.row(1, "fip"))
        self.assertTrue(core.row(1, "fip")["delete_sent"])
        self.cloud.side_effect = [(404, GONE_FIP)]
        core.clean_resource(1, "fip")
        self.assertTrue(core.row(1, "fip")["confirmed_absent"])
        calls = self.cloud.call_args_list
        self.assertEqual([c.args[0] for c in calls], ["GET", "DELETE", "GET", "GET"])
        self.assertTrue(all(c.args[1] == core.API + "/floating-ips/" + F for c in calls))

    def test_foreign_binding_or_changed_marker_preserves_fip(self):
        core.save(1, "scope", "scope", project_id=P)
        core.save(1, "fip", "fip", post_sent=True, id=F, name="mine", marker="mark")
        for extra in ({"nat_gateway": {"id": G}}, {"vm": R}, {"interface_id": I}, {"description": "other"}, {"project_id": Z}):
            self.cloud.reset_mock()
            self.cloud.return_value = (200, {"id": F, "name": "mine", "description": "mark", "state": "available", **extra})
            with self.assertRaises(RuntimeError):
                core.clean_resource(1, "fip")
            self.assertEqual(self.cloud.call_count, 1)
            self.assertEqual(self.cloud.call_args.args[0], "GET")

    def test_resumed_ssh_must_prove_vm_and_interface_and_state(self):
        core.save(1, "scope", "scope", vm_name="test", subnet_id=G, sg_id=R)
        core.save(1, "vm", "vm", id=V, post_sent=True)
        core.save(1, "fip", "fip", id=F, post_sent=True, name="owned")
        address = "93.184.216.34"
        iface = {"id": I, "primary": True, "type": "regular", "floating_ip": {"id": F, "ip_address": address}}
        cards = {V: {"id": V, "name": "test", "interfaces": [iface]}, F: {"id": F, "name": "owned", "vm": V, "ip_address": address},
                 I: {"id": I, "vm": {"id": V}, "subnet": {"id": G}, "security_groups": [{"id": R}]}}
        self.cloud.side_effect = lambda method, path, **opts: (200, cards[path.rsplit("/", 1)[1]])
        local = {"vm_id": V, "vm_ip": address, "floating_ip_id": F}
        with patch.object(core, "local_record", return_value=local):
            self.assertTrue(core.read_bound(1))
            cards[I]["security_groups"] = [{"id": D}]
            with self.assertRaises(RuntimeError):
                core.read_bound(1)
            cards[I]["security_groups"] = [{"id": R}]
            local["vm_ip"] = "1.2.3.4"
            with self.assertRaises(RuntimeError):
                core.read_bound(1)
        self.assertTrue(all(obj.args[0] == "GET" for obj in self.cloud.call_args_list))

    def test_previews_refusals_and_continuation_are_distinct(self):
        def mcp(status):
            return {"result": {"isError": True, "content": [{"text": json.dumps({"status": status})}]}}
        self.assertEqual(core.mcp_result(mcp("confirmation_required"))["status"], "confirmation_required")
        with self.assertRaises(RuntimeError):
            core.mcp_result(mcp("error"))
        send = Mock(side_effect=[{"status": "confirmation_required"},
                                 {"status": "deleting", "next_tool": "destroy", "next_args": {"vm_id": R, "confirmed": True}}])
        core.save(1, "vm", "vm", id=V)
        core.save(1, "scope", "scope", vm_name="unique")
        with self.assertRaises(RuntimeError):
            core.through_mcp(send, "destroy", {"vm_id": V}, 1, float("inf"))
        self.assertEqual(send.call_count, 2)
        self.assertEqual(send.call_args.args[1]["vm_id"], V)

    def test_plan_only_becomes_executable_for_its_own_network_and_public_key(self):
        import base64
        import hashlib
        public = "ssh-ed25519 " + base64.b64encode(b"\x00\x00\x00\x0bssh-ed25519" + b"\x00\x00\x00 " + b"x" * 32).decode()
        plan_id = "plan-000000000001"
        for selection in ("matching", "shared", "wrong subnet", "stale key"):
            with self.subTest(selection=selection), tempfile.TemporaryDirectory() as tmp, patch.dict("os.environ", {"RUNNER_TEMP":tmp}):
                self.records.clear()
                scope = dict(project_id=P, sg_id=G, image_id=V, subnet_id=I, zone_id=Z, zone_name=core.ZONE,
                             flavor_id=F, flavor_name=core.FLAVOR, disk_type_name="SSD", disk_cap_gb=20, vm_name="evo-example",
                             cidr="93.184.216.34/32", port=18086)
                core.save(2, "scope", "scope", **scope)
                folder = core.workdir(2) / ".evo"
                (folder / "plans").mkdir(parents=True)
                (folder / "id_ed25519.pub").write_text(public + ("changed" if selection == "stale key" else ""))
                net = {"security_group_id":R if selection == "shared" else G,
                       "subnet_id":D if selection == "wrong subnet" else I, "floating_ip":True}
                vm = {"name":scope["vm_name"], "flavor":{"id":F}, "zone":{"id":Z}, "image":{"id":V},
                      "disk":{"size_gb":20, "type":"SSD"}}
                data = {"plan_id":plan_id, "project_id":P, "network":net, "vm":vm, "ports":[18086], "public_key":public,
                        "ports_source":scope["cidr"]}
                (folder / "plans" / (plan_id + ".json")).write_text(json.dumps(data))
                view = {"plan_id":plan_id, "vm":vm, "compose_required":True, "cost":{"total_monthly_rub":100},
                        "network":{"open_ports":[18086], "ports_source":scope["cidr"]}}
                with patch.object(core, "stdio", side_effect=[view, {"status":"confirmation_required"}]) as call:
                    if selection in ("wrong subnet", "stale key"):
                        with self.assertRaises((RuntimeError, ValueError)):
                            core.plan_preview(2, scope)
                    else:
                        core.plan_preview(2, scope)
                    self.assertEqual(call.call_count, 2)
                    self.assertEqual(call.call_args.args, (2, "deploy_apply", {"plan_id":plan_id}))
                if selection == "matching":
                    saved = core.row(2, "scope")
                    self.assertEqual(saved["ssh_key_fingerprint"], hashlib.sha256(public.encode()).hexdigest())
                    self.assertEqual(saved["boot_name"], "evo-example-disk")
                    core.mcp_stmt("deploy_apply", {"plan_id":plan_id, "confirmed":True}, 2)
                    with self.assertRaises(RuntimeError):
                        core.mcp_stmt("deploy_apply", {"plan_id":"plan-000000000002", "confirmed":True}, 2)
                else:
                    with self.assertRaises(RuntimeError):
                        core.mcp_stmt("deploy_apply", {"plan_id":plan_id, "confirmed":True}, 2)
        self.cloud.assert_not_called()

    def test_kept_address_cannot_be_released_by_a_changed_continuation(self):
        core.save(2, "scope", "scope", vm_name="mine")
        core.save(2, "vm", "vm", id=V)
        send = Mock(side_effect=[{"status":"confirmation_required"}, {"status":"deleting", "next_tool":"destroy",
                        "next_args":{"vm_id":V,"release_ip":True,"confirmed":True}}])
        with self.assertRaises(RuntimeError):
            core.through_mcp(send, "destroy", {"vm_id":V, "release_ip":False}, 2, float("inf"))
        self.assertEqual(send.call_count, 2)

    def test_retained_address_cannot_replace_the_typed_id_or_attach_to_another_vm(self):
        core.save(2, "fip", "fip", id=F, name="mine", marker="attempt", post_sent=True)
        with patch.object(core, "stdio", return_value={"status":"refused","reason":"USAGE","id":F}) as call:
            core.refuse_bound_release(2)
            call.assert_called_once_with(2, "release_floating_ip", {"id":F})
        self.cloud.return_value = (200, {"id":F,"description":"attempt","name":"mine","state":"available","vm":{"id":V}})
        def one_observation(fn, *unused):
            if not fn():
                raise RuntimeError("occupied")
        with patch.object(core, "wait_for", side_effect=one_observation), patch.object(core, "stdio") as call:
            with self.assertRaises(RuntimeError):
                core.release_retained(2, float("inf"))
            call.assert_not_called()
        def release(send, tool, args, cycle, until):
            self.assertEqual((tool, args, cycle), ("release_floating_ip", {"id":F}, 2))
            core.save(2, "fip", "fip", delete_sent=True)
            return {"status":"ok", "id":F}
        self.cloud.return_value = (200, {"id":F,"description":"attempt","name":"mine","state":"available","vm":None})
        with patch.object(core,"through_mcp",side_effect=release):
            core.release_retained(2,float("inf"))
        self.assertTrue(core.row(2,"lifecycle")["kept_then_released"])

    def test_candidate_image_login_template_is_checked_before_resources(self):
        key = {"name":"public_key", "field_type":"public_key"}
        core.check_image_access({"image_metadata":[key, {"name":"name","field_type":"linux_login"}]})
        for fields in ([], [key, {"name":"linux_password","mandatory_group":"authentication"}],
                       [key, {"name":"password", "field_type":"linux_password"}], [None]):
            with self.subTest(fields=fields), self.assertRaises(RuntimeError):
                core.check_image_access({"image_metadata":fields})
        self.cloud.assert_not_called()

    def test_paged_inventory_rejects_bare_arrays_even_when_the_first_batch_looks_complete(self):
        for count in (0, 1, core.LIMIT):
            with self.subTest(count=count):
                self.cloud.return_value = (200, [{"id":identity(i+1)} for i in range(count)])
                with self.assertRaises(RuntimeError):
                    core.catalog("disks", P)
        self.cloud.return_value = (200, [{"id":Z, "name":"zone"}])
        self.assertEqual(core.catalog("availability-zones", paged=False), [{"id":Z, "name":"zone"}])

    def test_apply_cannot_use_confirmation_or_another_plan_as_a_write(self):
        self.assertEqual(core.mcp_stmt("deploy_apply", {"plan_id": "plan-012345abcdef"})["params"]["name"], "deploy_apply")
        for args in ({"plan_id":"plan-012345abcdef", "confirmed":True}, {"plan_id":"different"}, {}):
            with self.subTest(args=args), self.assertRaises(RuntimeError):
                core.mcp_stmt("deploy_apply", args)

    def test_stdio_waits_for_initialization_and_completed_tool_before_eof(self):
        with tempfile.TemporaryDirectory() as tmp:
            entry = Path(tmp) / "mcp-stub"
            entry.write_text("""#!/usr/bin/env python3
import sys,json
for line in sys.stdin:
    p=json.loads(line)
    if p['method']=='initialize':
        print(json.dumps({'jsonrpc':'2.0','id':1,'result':{'protocolVersion':'2025-06-18'}}),flush=True)
    if p['method']=='tools/call':
        print(json.dumps({'jsonrpc':'2.0','id':2,'result':{'content':[{'text':'{"status":"ok"}'}]}}),flush=True)
""")
            entry.chmod(0o700)
            with patch.dict("os.environ", {"P90_CORE_BIN": str(entry), "RUNNER_TEMP": tmp}):
                core.workdir(1).mkdir()
                self.assertEqual(core.stdio(1, "deploy", {}), {"status": "ok"})
        self.cloud.assert_not_called()

    def test_cycle_hooks_only_start_second_after_first_confirmed_clean(self):
        import sys
        actions = []
        disk = Mock()
        net = Mock()
        def run_cli(cycle, *args, **opts):
            actions.append((cycle, "cli", args, opts))
            if "--dry-run" in args:
                return {"status": "dry_run"}
            if opts.get("cut"):
                return {}
            if opts.get("hold_rule"):
                core.save(1, "rule-cut", "checkpoint", rule_id=R, group_id=G, cut=True)
                return {"status": "partial", "cleanup_pending": True}
            return {"status":"destroyed"}
        def mcp(send, tool, args, cycle, deadline):
            actions.append((cycle, "mcp", tool))
            core.save(cycle, "vm", "vm", id=V, post_sent=True)
            core.save(cycle, "app-rule", "rule", id=R)
            return {"status":"ok", "vm_id":V, "deleted_vm":V}
        def members(cycle, group):
            legitimate = any(a[1] == "cli" and a[2][-1] == "destroy" and not a[3] for a in actions if a[0] == cycle)
            # timeout=300 is part of the final CLI destroy invocation.
            legitimate = legitimate or any(a[1] == "cli" and a[2][-1] == "destroy" and a[3] == {"timeout":300}
                                           for a in actions if a[0] == cycle)
            return {} if legitimate else {R:{}}
        def clean(c):
            actions.append((c, "checked"))
        with contextlib.ExitStack() as stack:
            stack.enter_context(patch.dict(sys.modules, {"disk": disk, "network": net}))
            for func, implementation in {
                "preflight": lambda: {"project_id":P, "cidr":"93.184.216.34/32"}, "catalog":lambda *a:[], "setup_group":lambda *a:G,
                "prepare_workspace":lambda *a: None, "plan_preview":lambda c,s: actions.append((c,"plan preview")), "run_cli":run_cli, "interrupt_deploy":lambda c:actions.append((c,"cut deploy")),
                "through_mcp":mcp, "read_bound":lambda *a:True, "local_record":lambda *a:{}, "marker_evidence":lambda c:None,
                "witness_boot":lambda c:None, "verify_app":lambda c:None, "refuse_bound_release":lambda c:None, "release_retained":lambda c,deadline:None, "owned_rule_set":members,
                "http_server":lambda c:contextlib.nullcontext(Mock()), "confirm_gone":lambda *a:None, "cleanup_cycle":clean,
            }.items():
                stack.enter_context(patch.object(core, func, implementation))
            core.run()
        self.assertLess(actions.index((1, "checked")), actions.index((2, "mcp", "deploy")))
        self.assertIn((2, "mcp", "destroy"), actions)
        self.assertTrue(core.row(1, "lifecycle")["rule_only_retry_verified"])
        disk.run.assert_called_once_with(2)
        net.before_vm_destroy.assert_called_once_with(2)
        net.cleanup.assert_called_once()
        disk.cleanup.assert_called_once()


if __name__ == "__main__":
    unittest.main()
