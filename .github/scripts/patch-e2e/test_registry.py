"""Offline tests of the registry receipt, manifest and cascade."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch
import uuid

import registry


def uid(n):
    return str(uuid.UUID(int=n))


class RegistryTests(unittest.TestCase):
    cfg = "sha256:" + "a" * 64
    digest = "sha256:" + "b" * 64
    name = "p90-" + "d" * 20

    def setUp(self):
        self.book, self.calls, self.pushed, self.present = {}, [], False, False
        self.marker = self.name + ".cr.cloud.ru/probe"
        self.common = Mock(spec=["ledger", "note", "service"])
        self.start(patch.object(registry, "common", self.common))
        self.start(patch.dict(os.environ, {"CLOUDRU_PROJECT_ID": uid(10)}))
        self.common.ledger.side_effect = lambda: {k: dict(v) for k, v in self.book.items()}
        self.common.note.side_effect = lambda k, key, **fs: self.book.setdefault(key, {"kind": k}).update(fs)
        self.common.service.side_effect = self.status
        self.start(patch.object(registry.secrets, "token_hex", return_value="d" * 20))
        self.start(patch.object(registry, "preflight"))
        self.start(patch.object(registry, "build", return_value=self.cfg))
        self.start(patch.object(registry, "docker_login", return_value={}))
        self.start(patch.object(registry, "tool", side_effect=self.tool))
        self.start(patch.object(registry.time, "sleep"))
        self.inv = self.start(patch.object(registry, "catalog_get", side_effect=self.catalog_get))
        self.docker = self.start(patch.object(registry, "command", side_effect=self.command))

    def start(self, patcher):
        result = patcher.start()
        self.addCleanup(patcher.stop)
        return result

    def status(self, method, host, path, query):
        self.assertEqual((method, host, path, query), ("GET", "ar", "/v1/registries/" + uid(1), {"projectId": uid(10)}))
        if self.present:
            return 200, {"id": uid(1), "name": self.name}
        return 404, {"code": 5, "message": "registry not found"}

    def tool(self, op, *flags, **params):
        self.calls.append(op)
        if op == "ar_list":
            return {"registries": [{"id": uid(1), "name": self.name}] if self.present else []}
        if op == "ar_create":
            self.assertEqual(self.book[registry.KEY]["state"], "create-intent")
            self.assertEqual(params["public"], "false")
            self.assertEqual(params["tariff"], "BASIC")
            self.present = True
            return {"status": "creating", "operation_id": uid(2)}
        if op == "ar_operation":
            self.assertEqual(params["operation_id"], uid(2))
            return {"done": True, "id": uid(2), "resourceId": uid(1)}
        if op == "ar_get":
            self.assertEqual(params["id"], uid(1))
            return {"id": uid(1), "name": self.name, "isPublic": False, "tariff": "BASIC",
                    "registryType": "DOCKER", "status": "ACTIVE", "projectId": uid(10)}
        if op == "ar_delete":
            self.assertEqual(params, {"id": uid(1)})
            self.assertTrue(self.book[registry.KEY]["delete_started"])
            self.present = False
            return {"status": "deleting"}
        self.fail("unexpected service operation")

    def catalog_get(self, host, path):
        self.assertEqual(host, self.name + ".cr.cloud.ru")
        if path == "/v2/_catalog?n=2":
            return {"repositories": ["probe"] if self.pushed else []}
        self.assertEqual(path, "/v2/probe/tags/list?n=2")
        return {"name": "probe", "tags": ["run"]}

    def command(self, args, **kwargs):
        if args[:2] == ["docker", "push"]:
            self.assertEqual(self.book[registry.KEY]["state"], "push-intent")
            self.assertEqual(args[-1], self.marker + ":run")
            self.pushed = True
            return b""
        if args[:3] == ["docker", "image", "inspect"]:
            return json.dumps([{"Id": self.cfg, "RepoDigests": [self.marker + "@" + self.digest]}]).encode()
        self.assertEqual(args[:3], ["docker", "manifest", "inspect"])
        self.assertIn(args[-1], (self.marker + ":run", self.marker + "@" + self.digest))
        if "--verbose" in args:
            return json.dumps({"Descriptor": {"digest": self.digest, "platform": {"os": "linux", "architecture": "amd64"}}}).encode()
        return json.dumps({"config": {"digest": self.cfg}, "layers": [{"size": 123}]}).encode()

    def test_create_private_by_receipt_push_exact_digest_confirm_absence(self):
        self.assertEqual(registry.prepare(), self.marker + "@" + self.digest)
        self.assertEqual(self.book[registry.KEY]["id"], uid(1))
        self.assertEqual(registry.image(), self.marker + "@" + self.digest)
        registry.cleanup()
        registry.cleanup()
        self.assertEqual(self.book[registry.KEY]["state"], "gone")
        self.assertEqual(self.calls.count("ar_create"), 1)
        self.assertEqual(self.calls.count("ar_delete"), 1)

    def test_foreign_artifacts_do_not_get_cascaded(self):
        registry.prepare()
        self.inv.side_effect = lambda *a: {"repositories": ["probe", "foreign"]}
        with self.assertRaisesRegex(RuntimeError, "unexpected registry repositories"):
            registry.cleanup()
        self.assertNotIn("ar_delete", self.calls)

    def test_unsettled_app_or_unknown_push_blocks_cascade(self):
        registry.prepare()
        self.book["ca-cli"] = {"kind": "container_app", "status": "created"}
        with self.assertRaisesRegex(RuntimeError, "depends"):
            registry.cleanup()
        del self.book["ca-cli"]
        self.book[registry.KEY]["state"] = "push-intent"
        with self.assertRaisesRegex(RuntimeError, "disposition unknown"):
            registry.cleanup()
        self.assertNotIn("ar_delete", self.calls)

    def test_missing_owned_post_is_never_replaced_by_matching_name(self):
        self.present = True
        self.book[registry.KEY] = {"kind": "artifact-registry", "name": self.name, "state": "create-intent"}
        with self.assertRaisesRegex(RuntimeError, "no receipt"):
            registry.cleanup()
        self.assertEqual(self.calls, [])

    def test_ambiguous_DELETE_only_reads(self):
        registry.prepare()
        self.book[registry.KEY]["delete_started"] = True
        with self.assertRaisesRegex(RuntimeError, "incomplete"):
            registry.cleanup()
        self.assertNotIn("ar_delete", self.calls)

    def test_local_service_compiles_and_returns_only_the_expected_http_body(self):
        test = '''package main

import ("net/http/httptest"; "testing")
func TestReady(t *testing.T) {
    w := httptest.NewRecorder()
    ready(w, httptest.NewRequest("GET", "/", nil))
    if w.Code != 200 || w.Body.String() != "p90-ready\\n" { t.Fatal("unexpected response") }
}
'''
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / "main.go").write_text(registry.SERVER)
            (root / "main_test.go").write_text(test)
            checked = subprocess.run(["go", "test", "main.go", "main_test.go", "-v"], cwd=folder,
                                     env={**os.environ, "GOPROXY": "off", "GOTOOLCHAIN": "local", "GOENV": "off"},
                                     capture_output=True, timeout=120)
        self.assertEqual(checked.returncode, 0, checked.stderr.decode()[:400])
        self.assertIn(b"\np90-ready\n", checked.stdout)


class CatalogAuthenticationTest(unittest.TestCase):
    def test_untrusted_bearer_challenge_never_receives_credentials(self):
        with patch.dict(os.environ, {"CLOUDRU_KEY_ID": "fake-key", "CLOUDRU_SECRET": "fake-secret"}):
            with patch.object(registry, "catalog_request", return_value=(401, {}, 'Bearer realm="https://other.cr.cloud.ru/token"')) as req:
                with self.assertRaisesRegex(RuntimeError, "origin"):
                    registry.catalog_get("owned.cr.cloud.ru", "/v2/_catalog?n=2")
                req.assert_called_once_with("owned.cr.cloud.ru", "/v2/_catalog?n=2")


if __name__ == "__main__":
    unittest.main()
