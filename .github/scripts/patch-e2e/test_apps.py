"""Offline protocol and safety checks. Live entry points have no approval override."""
import json
import os
import subprocess
import tempfile
import time
import unittest
from pathlib import Path
from unittest.mock import patch

import apps

PROJECT = "00000000-0000-4000-8000-000000000001"
APP = "00000000-0000-4000-8000-000000000002"
OP = "00000000-0000-4000-8000-000000000003"
IMAGE = "sample.cr.cloud.ru/http@sha256:" + "1" * 64
ABSENT = (499, {"message": "Serverless with name is not found"})


class ContainerAppsTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        env = patch.dict(os.environ, {"P90_CA_BIN": "/bin/false", "RUNNER_TEMP": tmp.name,
                                     "P90_SOURCE_RUN": "74", "CLOUDRU_PROJECT_ID": PROJECT,
                                     "P90_CA_IMAGE": IMAGE})
        env.start()
        self.addCleanup(env.stop)
        self.book = {}
        def save(kind, key, **fields):
            self.book[key] = {**self.book.get(key, {"kind": kind}), **fields}
        self.cloud = self.replace(apps, "ca_api")
        self.candidate = self.replace(apps, "candidate")
        self.evo = self.replace(apps.common, "call_evo")
        self.replace(apps.common, "ledger").side_effect = lambda: self.book
        self.replace(apps.common, "note").side_effect = save

    def replace(self, owner, key):
        p = patch.object(owner, key, autospec=True)
        self.addCleanup(p.stop)
        return p.start()

    def make(self, mode="cli"):
        self.cloud.side_effect = [ABSENT, (200, {"id": OP, "resourceId": APP})]
        return apps.create("ca-" + mode, mode, PROJECT, "74", IMAGE)

    @staticmethod
    def card(record):
        return {"id": APP, "projectId": PROJECT, "name": record["name"], "description": apps.marker(record["name"])}

    def test_preflight_validates_project_and_run_offline(self):
        self.assertEqual(apps.preflight(), (PROJECT, "74"))
        for field, value in (("CLOUDRU_PROJECT_ID", "invalid"), ("P90_SOURCE_RUN", "../old")):
            with self.subTest(field=field), patch.dict(os.environ, {field: value}), self.assertRaisesRegex(RuntimeError, "invalid CA scope"):
                apps.preflight()
        self.cloud.assert_not_called()
        self.evo.assert_not_called()
        self.candidate.assert_not_called()

    def test_other_receipts_and_inaccessible_ledger(self):
        self.book["vm"] = {"kind": "virtual_machine"}
        apps.cleanup()
        with patch.object(apps.common, "ledger", side_effect=RuntimeError("receipt unavailable")):
            with self.assertRaises(RuntimeError):
                apps.cleanup()
        self.cloud.assert_not_called()

    def test_create_intent_durable_before_one_post_and_no_replay_after_lost_response(self):
        def wire(method, *_):
            if method == "GET":
                return ABSENT
            self.assertEqual(self.book["ca-cli"]["status"], "create_intent")
            raise RuntimeError("lost")
        self.cloud.side_effect = wire
        for _ in range(2):
            with self.assertRaises(RuntimeError):
                apps.create("ca-cli", "cli", PROJECT, "74", IMAGE)
        self.assertEqual([c.args[0] for c in self.cloud.call_args_list], ["GET", "POST"])
        self.candidate.assert_not_called()

    def test_uid_must_come_from_direct_create_response(self):
        self.cloud.side_effect = [ABSENT, (200, {"id": OP})]
        with self.assertRaises(RuntimeError):
            apps.create("ca-cli", "cli", PROJECT, "74", IMAGE)
        self.assertEqual(self.book["ca-cli"]["create_operation"], OP)
        self.assertNotIn("resource_id", self.book["ca-cli"])
        with self.assertRaises(RuntimeError):
            apps.finish("ca-cli", self.book["ca-cli"])
        self.candidate.assert_not_called()

    def test_preexisting_namesake_no_post(self):
        self.cloud.return_value = (200, {"name": "p90-ca-74-cli", "id": APP})
        with self.assertRaises(RuntimeError):
            apps.create("ca-cli", "cli", PROJECT, "74", IMAGE)
        self.assertEqual(self.cloud.call_count, 1)
        self.assertEqual(self.book, {})

    def test_project_owned_immutable_amd64_manifest_before_create(self):
        self.evo.return_value = {"registries": [{"id": APP, "name": "sample", "type": "DOCKER", "status": "ACTIVE"}]}
        descriptor = {"digest": IMAGE.rsplit("@", 1)[1], "platform": {"os": "linux", "architecture": "amd64"}}
        with patch.dict(os.environ, {"CLOUDRU_KEY_ID": "synthetic", "CLOUDRU_SECRET": "example"}):
            for accepted in (True, False):
                desc = descriptor if accepted else {**descriptor, "digest": "sha256:" + "2" * 64}
                results = [subprocess.CompletedProcess([], 0, b""), subprocess.CompletedProcess(
                    [], 0, json.dumps({"Descriptor": desc}).encode())]
                with patch.object(subprocess, "run", side_effect=results) as docker:
                    if accepted:
                        apps.verify_image(PROJECT, IMAGE)
                    else:
                        with self.assertRaises(RuntimeError):
                            apps.verify_image(PROJECT, IMAGE)
                self.assertEqual([c.args[0][1] for c in docker.call_args_list], ["login", "manifest"])
        self.evo.return_value = {"registries": []}
        with patch.object(subprocess, "run") as docker, self.assertRaises(RuntimeError):
            apps.verify_image(PROJECT, IMAGE)
        docker.assert_not_called()

    def test_deleted_identity_and_continuation_both_surfaces(self):
        for mode in apps.MODES:
            key = "ca-" + mode
            rec = self.make(mode)
            self.cloud.side_effect = [(200, self.card(rec)), (200, self.card(rec)), ABSENT]
            self.evo.return_value = {"containers": []}
            def invoke(channel, op, target, path, timeout):
                self.assertEqual((channel, target), (mode, rec))
                if op == "get":
                    return None
                self.assertEqual(self.book[key]["status"], "delete_intent")
                now = int(time.time() * 1000)
                Path(path).write_text(f"attempt {now}\n200 {now}\n", encoding="ascii")
                return {"status": "deleting", "name": rec["name"], "operation_id": OP,
                        "next_tool": "ca_get", "next_args": {"name": rec["name"]}}
            self.candidate.side_effect = invoke
            apps.finish(key, rec)
            self.assertEqual(self.book[key]["status"], "gone")
            self.assertEqual(self.book[key]["physical_deletes"], 1)
            self.assertTrue(self.book[key]["original_uid_absent"])

    def test_foreign_replacement_blocks_mutation(self):
        rec = self.make()
        for field, new in {"id": OP, "description": "another run", "projectId": OP, "name": "different"}.items():
            with self.subTest(field=field):
                self.cloud.side_effect = [(200, {**self.card(rec), field: new})]
                with self.assertRaises(RuntimeError):
                    apps.finish("ca-cli", rec)
        self.candidate.assert_not_called()

    def test_replacement_after_intent_preserves_receipt_without_delete(self):
        rec = self.make()
        foreign = {**self.card(rec), "id": OP}
        self.cloud.side_effect = [(200, self.card(rec)), (200, foreign)]
        with self.assertRaisesRegex(RuntimeError, "identity changed before delete"):
            apps.cleanup()
        self.assertEqual(self.book["ca-cli"]["status"], "delete_intent")
        self.cloud.side_effect = [(200, foreign)]
        with self.assertRaisesRegex(RuntimeError, "identity changed"):
            apps.cleanup()
        self.candidate.assert_not_called()

    def test_lost_delete_only_readback_without_repeated_delete(self):
        rec = self.make()
        self.book["ca-cli"]["status"] = "delete_intent"
        self.cloud.side_effect = [ABSENT]
        self.evo.return_value = {"containers": [{"id": APP, "name": "old"}]}
        with self.assertRaises(RuntimeError):
            apps.cleanup()
        self.candidate.assert_not_called()
        self.assertEqual(self.book["ca-cli"]["status"], "delete_intent")
        self.cloud.side_effect = [ABSENT]
        self.evo.return_value = {"containers": []}
        apps.cleanup()
        self.assertEqual(self.book["ca-cli"]["status"], "gone")
        self.assertNotIn("physical_deletes", self.book["ca-cli"])
        self.assertEqual(rec["resource_id"], APP)

    def test_generic_route_errors_are_not_deletion_evidence(self):
        for code, data in [(404, {}), (404, {"message":"route not found"}), (403, {}), (499, {"message":"invalid image"})]:
            self.assertFalse(apps.absent(code, data))


if __name__ == "__main__":
    unittest.main()
