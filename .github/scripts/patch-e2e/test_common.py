import copy
import json
import os
import unittest
from unittest.mock import Mock, patch

import common
import driver


ENV = {"P90_RECEIPT_REPOSITORY": "fixture-org/receipts", "P90_RECEIPT_BRANCH": "e2e",
       "P90_RECEIPT_TOKEN": "fixture", "P90_RECEIPT_HMAC_KEY": "a1"*32,
       "P90_SOURCE_RUN": "31", "P90_SUITE": "core", "GITHUB_SHA": "bb"*20,
       "GITHUB_REPOSITORY_ID": "100", "CLOUDRU_PROJECT_ID": "fixture-project"}


class BookTests(unittest.TestCase):
    def setUp(self):
        self.env = patch.dict(os.environ, ENV)
        self.env.start()
        self.addCleanup(self.env.stop)
        self.written = None
        self.head = None
        self.hide_put = False
        self.git = patch.object(common.ReceiptBook, "git", autospec=True, side_effect=self.remote)
        self.git.start()
        self.addCleanup(self.git.stop)

    def remote(self, _book, method, path, body=None):
        if not path:
            return 200, {"private": True, "default_branch": "main"}
        if path.startswith("/branches/"):
            return 200, {}
        if method == "GET":
            if self.head is None:
                return 404, None
            return 200, {"sha": self.head, "content": self.written, "encoding": "base64"}
        if self.head != body.get("sha"):
            return 409, None
        self.written = body["content"]
        self.head = __import__("hashlib").sha1(self.written.encode()).hexdigest()
        if self.hide_put:
            raise RuntimeError("write acknowledgement lost")
        return 201, {}

    def test_cold_recovery_and_lost_ack(self):
        self.hide_put = True
        common.ReceiptBook().start()
        common.note("vm", "vm:1", own_id="from-post", status="created")
        common.note("vm", "vm:1", status="checked")
        common.ReceiptBook().start(recovery=True)
        self.assertEqual(common.ledger()["vm:1"], {"kind": "vm", "own_id": "from-post", "status": "checked"})
        with self.assertRaises(RuntimeError):
            common.ReceiptBook().start()

    def test_tamper_project_and_empty_source_rejected(self):
        with self.assertRaises(RuntimeError):
            common.ReceiptBook().start(recovery=True)
        common.ReceiptBook().start()
        with patch.dict(os.environ, {"CLOUDRU_PROJECT_ID": "elsewhere"}), self.assertRaises(RuntimeError):
            common.ledger()
        decoded = json.loads(common.base64.b64decode(self.written))
        decoded["payload"]["records"]["vm"] = {"id": "someone-elses"}
        self.written = common.base64.b64encode(json.dumps(decoded).encode()).decode()
        with self.assertRaises(RuntimeError):
            common.ledger()

    def test_receipt_branch_cannot_be_default_or_public(self):
        book = common.ReceiptBook()
        book.git = Mock(return_value=(200, {"private": False}))
        with self.assertRaises(RuntimeError):
            book.verify_repository()
        book.git = Mock(return_value=(200, {"private": True, "default_branch": "e2e"}))
        with self.assertRaises(RuntimeError):
            book.verify_repository()

    def test_unlisted_service_and_redirect_paths_refused_before_auth(self):
        with patch.object(common, "cloud_auth", side_effect=AssertionError):
            for endpoint in ("kafka", "redis", "unlisted"):
                with self.assertRaises(RuntimeError):
                    common.service("GET", endpoint, "/v1/clusters")
            for path in ("https://elsewhere.test/v1", "//elsewhere", "/api/v1/../foreign"):
                with self.assertRaises(RuntimeError):
                    common.compute("GET", path)

    def test_explicit_error_before_paid_scenarios(self):
        with patch.dict(os.environ, {"P90_SUITE": "invalid"}), patch.object(driver, "public") as published, self.assertRaises(RuntimeError):
            driver.preflight()
        published.assert_not_called()
