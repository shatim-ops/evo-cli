"""Exercise the exact candidate CLI allowlist without any network calls."""

from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
import os
import sys
import unittest
from unittest.mock import Mock, patch

import reads


class ScopedReadsTest(unittest.TestCase):
    project = "synthetic-private-project"
    private = "synthetic-private-name-and-address"

    def setUp(self):
        self.common = Mock(spec=["call_evo", "note"])
        self.common.call_evo.side_effect = self.reply
        self.start(patch.dict(sys.modules, {"common": self.common}))
        self.start(patch.dict(os.environ, {
            "CLOUDRU_PROJECT_ID": self.project, "GITHUB_RUN_ID": "42", "GITHUB_RUN_ATTEMPT": "1",
        }))
        self.responses = {}
        self.calls = []

    def start(self, patcher):
        patcher.start()
        self.addCleanup(patcher.stop)

    def reply(self, *args, timeout):
        self.assertEqual(args[-1], "--json")
        self.assertGreater(timeout, 0)
        self.calls.append(args)
        command = args[:2]
        if args[0] == "--dry-run":
            self.assertEqual(args[1:5], ("--project-id", self.project, "tools", "call"))
            self.assertNotIn("--yes", args)
            self.assertNotIn("confirmed=true", args)
            return {"status": "dry_run", "preview": {"action": args[5]}}
        if command in self.responses:
            return self.responses[command]
        if command == ("metrics", "history"):
            return {"project": self.project, "match": args[2], "count": 0, "series": []}
        if args[0] == "history":
            return {"lines": [], "live": []}
        field = {**{argv[:2]: field for argv, field in reads.INVENTORY},
                 ("iam", "service-accounts"): "service_accounts"}[command]
        if field == "certificates":
            return {"project": self.project, field: None, "count": 0}
        return {field: [{"id": self.private}]}

    def test_only_declared_reads_no_sensitive_output(self):
        sink = StringIO()
        with redirect_stdout(sink), redirect_stderr(sink):
            reads.run()
        self.assertEqual(sink.getvalue(), "")
        commands = [args[5] if args[0] == "--dry-run" else " ".join(args[:2]) if args[0] != "history" else "history" for args in self.calls]
        self.assertEqual(commands, [
            "metrics history", "vm list", "disk list", "backup list", "image list",
            "vpc list", "vpc subnets", "vpc security-groups", "vpc fips", "snat list", "ar list",
            "cert list", "dns zones", "k8s list", "lb list", "tg list", "secret list",
            "ar_create", "vpc_create", "secret_create", "k8s_create",
            "iam service-accounts", "history",
        ])
        self.assertEqual(self.calls[0], ("metrics", "history", "evo_e2e_p90_absent_42_1", "--since", "1m", "--project-id", self.project, "--json"))
        for args in self.calls:
            self.assertEqual(args[args.index("--project-id") + 1], self.project)
        self.assertEqual(self.calls[-1], ("history", "--project-id", self.project, "--json"))
        notes = repr(self.common.note.call_args_list)
        self.assertNotIn(self.private, notes)
        self.assertNotIn(self.project, notes)
        self.common.note.assert_any_call("read", "history", evidence="local-journal", lines=0, live_rows=0)

    def test_wrong_project_stops_before_other_services(self):
        self.responses[("metrics", "history")] = {"project": "other", "count": 0, "series": []}
        with self.assertRaises(ValueError):
            reads.run()
        self.assertEqual(len(self.calls), 1)
        self.common.note.assert_not_called()

    def test_missing_or_malformed_projection_is_not_evidence(self):
        for body in ({}, {"secrets": None}, {"secrets": [None]}, {"secrets": [], "count": False}):
            with self.subTest(body=body):
                self.responses[("secret", "list")] = body
                with self.assertRaises(ValueError):
                    reads.run()
        for invocation in self.common.note.call_args_list:
            self.assertNotEqual(invocation.args[1], "secret-list")

    def test_service_blind_aggregate_reads_are_excluded_before_the_network(self):
        reads.run()
        for cmd in (("org", "quotas"), ("audit", "events"), ("billing", "consumption")):
            self.assertNotIn(cmd, [args[:2] for args in self.calls])
            self.common.note.assert_any_call("read", "-".join(cmd), evidence="not-exercised-no-upstream-service-filter")


if __name__ == "__main__":
    unittest.main()
