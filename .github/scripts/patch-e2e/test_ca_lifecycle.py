"""Run the HTTP, log and stop/start scenario entirely against in-memory responses."""
import copy
import os
import time
import unittest
from unittest.mock import patch

import apps
import ca_lifecycle as ca

PROJECT = "00000000-0000-4000-8000-000000000001"
APP = "00000000-0000-4000-8000-000000000002"
OP = "00000000-0000-4000-8000-000000000003"
IMAGE = "sample.cr.cloud.ru/image@sha256:" + "2" * 64


class CALifecycleTest(unittest.TestCase):
    def setUp(self):
        self.book, self.calls, self.state = {}, [], "running"
        self.addCleanup(patch.stopall)
        patch.dict(os.environ, {"P90_CA_IMAGE": IMAGE, "P90_CA_BIN": "/bin/false", "P90_SOURCE_RUN": "55",
                                "CLOUDRU_PROJECT_ID": PROJECT}).start()
        patch.object(ca.common, "ledger", side_effect=lambda: copy.deepcopy(self.book)).start()
        patch.object(ca.common, "note", side_effect=self.note).start()
        self.https = patch.object(ca.http.client, "HTTPSConnection").start()
        self.response = self.https.return_value.getresponse.return_value
        self.response.status = 200
        self.response.read.return_value = b"p90-ready\n"
        self.response.getheader.side_effect = lambda k, default=None: "10" if k == "Content-Length" else default
        self.evo = patch.object(ca.common, "call_evo", side_effect=self.tool).start()
        self.corrupt = False
        self.rec = self.receipt("cli")

    def note(self, kind, key, **fields):
        self.book[key] = {**self.book.get(key, {}), "kind": kind, **fields}

    def receipt(self, mode):
        start = int(time.time())
        rec = {"kind": "container_app", "name": apps.app_name("55", mode), "project": PROJECT,
               "image": IMAGE, "status": "created", "start": start, "until": start + apps.WINDOW,
               "resource_id": APP, "create_operation": OP}
        self.book["ca-" + mode] = dict(rec)
        return rec

    def card(self, host=None):
        name = self.rec["name"]
        return {"id": APP, "name": name, "description": apps.marker(name), "projectId": PROJECT, "status": self.state,
                "configuration": {"ingress": {"publiclyAccessible": True,
                                  "publicUri": host if host is not None else name + ".containerapps.ru"}}}

    def tool(self, *args, timeout):
        n = args[args.index("call") + 1].removeprefix("ca_")
        self.assertIn("name=" + self.rec["name"], args)
        self.assertEqual(args[args.index("--project-id") + 1], PROJECT)
        if n == "get":
            card = self.card()
            if self.corrupt and self.state == "stopped":
                card["id"] = OP
            return card
        if n == "logs":
            return {"logs": [{"message":"p90-ready"}]}
        self.assertIn("--yes", args)
        self.assertEqual(self.book["ca-lifecycle:" + self.rec["name"].split("-")[-1]]["phase"], n + "-intent")
        self.calls.append(n)
        self.state = {"stop": "stopped", "start": "running"}[n]
        return {"status": "stopping" if n == "stop" else "starting", "name": self.rec["name"], "action":n,
                "next_tool":"ca_get","next_args":{"name":self.rec["name"]}}

    def test_http_log_stop_start_and_readback_both_receipts(self):
        for mode in ("mcp", "cli"):
            self.rec = self.receipt(mode)
            ca.exercise(self.rec)
            entry = self.book["ca-lifecycle:" + mode]
            self.assertEqual((entry["phase"], entry["first_http"], entry["restart_http"], entry["log_lines"]),
                             ("exercised", 200, 200, 1))
        self.assertEqual(self.calls, ["stop","start","stop","start"])
        self.assertEqual(self.https.return_value.request.call_count, 4)
        for call in self.https.return_value.request.call_args_list:
            self.assertEqual(call.args, ("GET", "/"))
            self.assertEqual(call.kwargs["headers"], {"Accept-Encoding": "identity", "Connection": "close"})
        with self.assertRaisesRegex(RuntimeError, "already attempted"):
            ca.exercise(self.rec)

    def test_no_mutation_when_identity_changes_between_stop_and_start(self):
        self.corrupt = True
        with self.assertRaisesRegex(RuntimeError, "identity changed"):
            ca.exercise(self.rec)
        self.assertEqual(self.calls, ["stop"])
        self.assertNotEqual(self.book["ca-lifecycle:cli"]["phase"], "exercised")

    def test_expired_window_or_lost_receipt_never_probes(self):
        with patch.object(ca.time, "time", return_value=self.rec["until"]), self.assertRaisesRegex(RuntimeError, "window exhausted"):
            ca.exercise(self.rec)
        self.evo.assert_not_called()
        self.https.assert_not_called()
        self.book.clear()
        with self.assertRaisesRegex(RuntimeError, "direct creation receipt missing"):
            ca.exercise(self.rec)

    def test_replacement_url_userinfo_ip_redirect_and_custom_domain_rejected(self):
        name = self.rec["name"]
        base = name + ".containerapps.ru"
        for url in ("http://" + base, "https://" + base + ":8443", "https://x@" + base, "https://127.0.0.1",
                    base + ".invalid", "neighbors-app.containerapps.ru", base + "/private", base + "?token=other",
                    base + "\n", base + "/#changed"):
            with self.subTest(url=url), self.assertRaises(RuntimeError):
                ca.public_host(self.card(url),name)
        card = self.card(base)
        card["configuration"]["ingress"]["customDomains"] = ["elsewhere.invalid"]
        with self.assertRaises(RuntimeError):
            ca.public_host(card,name)
        self.https.assert_not_called()
        self.response.status = 302
        with self.assertRaisesRegex(RuntimeError, "unexpected readiness response"):
            ca.exercise(self.rec)
        self.assertEqual(self.https.call_count, 1)
        self.assertEqual(self.calls, [])

    def test_truncated_ready_response_is_not_accepted(self):
        self.response.read.return_value = b"p90-ready"
        with self.assertRaisesRegex(RuntimeError,"body differs"):
            ca.exercise(self.rec)
        self.assertEqual(self.calls, [])


if __name__ == "__main__":
    unittest.main()
