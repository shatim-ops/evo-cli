"""Hermetic ownership, cold recovery and destructive-guard tests."""

import json
import os
import sys
from pathlib import Path
import unittest
from unittest.mock import Mock, patch
import uuid

import misc


def uid(n):
    return str(uuid.UUID(int=n))


class MiscTest(unittest.TestCase):
    def setUp(self):
        self.book, self.cards = {}, {}
        self.private_dns = Mock(spec=["preflight", "run", "cleanup"])
        self.start(patch.dict(sys.modules, {"private_dns": self.private_dns}))
        self.common = Mock(spec=["call_evo", "ledger", "note", "service"])
        self.start(patch.object(misc, "common", self.common))
        self.start(patch.dict(os.environ, {"CLOUDRU_PROJECT_ID": uid(400), "P90_SOURCE_RUN": "111"}))
        self.start(patch.object(misc.time, "sleep"))
        self.common.ledger.side_effect = lambda: {k: dict(v) for k, v in self.book.items()}
        self.common.note.side_effect = self.note
        self.common.call_evo.side_effect = self.call
        self.common.service.side_effect = self.get_card
        self.created, self.deleted = [], []
        self.argfile = None
        self.versions = {}

    def start(self, patcher):
        patcher.start()
        self.addCleanup(patcher.stop)

    def note(self, kind, key, **fields):
        self.book.setdefault(key, {"kind": kind}).update(fields)

    def get_card(self, method, host, path, query=None, body=None):
        rid = path.rsplit("/", 1)[1]
        if host == "secretmanager":
            self.assertTrue(path.startswith("/v1/secrets/" + uid(4)))
            if path.endswith("/versions"):
                if method == "POST":
                    self.assertEqual(self.book[misc.SECRET]["version_pending"], True)
                    self.assertIn("data", body["payload"])
                    self.versions[2] = "ENABLED"
                    return 200, {"id": 2, "secretId": uid(4)}
                self.assertEqual(query, {"page.limit": 3, "page.offset": 0})
                return 200, {"versions": [{"id": k, "secretId": uid(4), "state": state}
                                          for k, state in self.versions.items()]}
            if "/versions/" in path:
                vid = int(rid)
                self.assertIn(vid, self.book[misc.SECRET]["versions"])
                if method == "DELETE":
                    self.versions[vid] = "DESTROYED"
                    return 200, {}
                return 200, {"id": vid, "secretId": uid(4), "state": self.versions[vid]}
            if method == "DELETE":
                self.assertEqual(query, {"force": "false"})
                self.assertEqual(set(self.versions.values()), {"DESTROYED"})
                self.cards.pop(rid)
                self.deleted.append(rid)
                return 200, {}
        self.assertEqual(method, "GET")
        if rid in self.cards:
            return 200, dict(self.cards[rid])
        noun = {uid(3): "certificate", uid(4): "secret"}[rid]
        return 404, {"code": 5, "message": noun + " not found"}

    def call(self, *args, timeout):
        op = args[args.index("call") + 1]
        params = {v.split("=", 1)[0]: v.split("=", 1)[1] for i, v in enumerate(args) if i and args[i - 1] == "--arg"}
        if op == "secret_create":
            self.assertEqual(self.book[misc.SECRET]["state"], "create-intent")
            argfile = Path(args[args.index("--args-file") + 1])
            record = json.loads(argfile.read_text())
            self.assertNotIn(record["value"], repr(self.book))
            self.assertNotIn(record["value"], repr(args))
            self.assertNotIn("/", record["path"])
            self.cards[uid(4)] = {"id": uid(4), "name": record["path"], "description": record["description"]}
            self.versions[1] = "ENABLED"
            self.created.append(op)
            return {"status": "created", "secret": {**self.cards[uid(4)], "path": record["path"],
                    "projectId": uid(400), "versions": [{"id": 1, "state": "ENABLED"}]}}
        if op == "secret_list":
            return {"secrets": []}
        if op == "cert_import":
            self.argfile = Path(args[args.index("--args-file") + 1])
            self.assertEqual(self.argfile.stat().st_mode & 0o777, 0o600)
            payload = json.loads(self.argfile.read_text())
            self.assertIn("PRIVATE KEY", payload["private_key"])
            self.assertNotIn("PRIVATE KEY", repr(args))
            self.assertEqual(self.book[misc.CERT]["state"], "create-intent")
            self.cards[uid(3)] = {"id": uid(3), "name": payload["name"], "description": payload["description"]}
            self.created.append(op)
            return {"status": "imported", "certificate": self.cards[uid(3)]}
        if op == "cert_versions":
            return {"versions": [{"id": uid(13)}]}
        if op == "cert_usage":
            return {"usage": []}
        if op.endswith("_delete"):
            key = {"cert_delete": misc.CERT}[op]
            rid = params.get("id") or params["record_id"]
            self.assertEqual(self.book[key]["id"], rid)
            self.assertTrue(self.book[key]["delete_started"])
            self.assertIn("--yes", args)
            if op == "cert_delete":
                self.assertEqual(params["force"], "false")
            self.cards.pop(rid)
            self.deleted.append(rid)
            return {"status": "deleted"}
        if op == "cert_list":
            return {"certificates": [], "project": uid(400), "count": 0}
        self.fail("Unexpected candidate invocation")

    def test_private_preflight_is_local_and_never_writes(self):
        misc.preflight()
        self.common.call_evo.assert_not_called()
        self.common.service.assert_not_called()
        self.common.note.assert_not_called()

    def test_secret_lifecycle_uses_uuid_and_receipted_versions_without_reading_values(self):
        misc.run_secret()
        self.assertEqual(self.book[misc.SECRET]["versions"], [1, 2])
        misc.cleanup()
        self.assertEqual(self.deleted, [uid(4)])
        self.assertEqual(self.book[misc.SECRET]["state"], "gone")
        for record in self.common.service.call_args_list:
            self.assertTrue(record.args[2].startswith("/v1/secrets/" + uid(4)))
            self.assertNotIn("payload", record.args[2])
        self.assertNotIn("secret_delete", repr(self.common.call_evo.call_args_list))
        self.assertNotIn("payload", repr(self.common.note.call_args_list))

    def test_nonreceipt_version_disables_parent_and_version_deletes(self):
        misc.run_secret()
        self.versions[3] = "ENABLED"
        with self.assertRaisesRegex(RuntimeError, misc.SECRET):
            misc.cleanup()
        self.assertFalse(any(c.args[0] == "DELETE" for c in self.common.service.call_args_list))
        self.assertNotIn("delete_started", self.book[misc.SECRET])

    def test_lost_version_post_response_is_not_adopted_by_name_or_order(self):
        misc.run_secret()
        self.book[misc.SECRET].update(versions=[1], version_pending=True)
        with self.assertRaisesRegex(RuntimeError, misc.SECRET):
            misc.cleanup()
        self.assertFalse(any(c.args[0] == "DELETE" for c in self.common.service.call_args_list))

    def test_private_dns_runs_instead_of_the_public_zone(self):
        misc.run()
        self.private_dns.preflight.assert_called_once_with()
        self.private_dns.run.assert_called_once_with()
        misc.cleanup()
        self.private_dns.cleanup.assert_called_once_with()
        self.assertEqual(self.created, ["cert_import", "secret_create"])

    def test_unresolved_dns_does_not_block_other_cleanup(self):
        misc.run_secret()
        self.private_dns.cleanup.side_effect = RuntimeError("unresolved")
        with self.assertRaisesRegex(RuntimeError, "private-dns"):
            misc.cleanup()
        self.assertEqual(self.book[misc.SECRET]["state"], "gone")

    def test_same_name_different_id_and_lost_create_response_are_never_adopted(self):
        self.book[misc.CERT] = {"kind": "certificate", "state": "create-intent", "name": "same"}
        self.cards[uid(3)] = dict(id=uid(3), name="same", description="same")
        with self.assertRaisesRegex(RuntimeError, misc.CERT):
            misc.cleanup()
        self.assertEqual(self.deleted, [])
        self.common.service.assert_not_called()
        self.common.call_evo.assert_not_called()

    def test_cert_private_material_never_goes_into_evidence_or_cloud_read(self):
        misc.run_certificate()
        self.assertFalse(self.argfile.exists())
        self.assertNotIn("PRIVATE KEY", repr(self.common.note.call_args_list))
        self.assertNotIn("private_key", repr(self.common.call_evo.call_args_list))
        misc.cleanup()
        self.assertEqual(self.deleted, [uid(3)])
        self.assertEqual(self.book[misc.CERT]["state"], "gone")

    def test_gateway_404_or_same_id_with_other_marker_remains_pending(self):
        self.book[misc.CERT] = {"kind": "certificate", "id": uid(3), "name": "own", "marker": "this-run"}
        for reply in [(404, {"message": "route not found", "code": 5}), (403, None),
                      (200, {"id": uid(3), "name": "own", "description": "other-run"})]:
            with self.subTest(reply=reply):
                self.common.service.side_effect = None
                self.common.service.return_value = reply
                with self.assertRaisesRegex(RuntimeError, misc.CERT):
                    misc.cleanup()
                self.assertNotIn("delete_started", self.book[misc.CERT])


if __name__ == "__main__":
    unittest.main()
