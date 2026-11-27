"""Offline bindings for the paid dispatch, recovery and source addresses."""
import os
from pathlib import Path
import unittest
from unittest.mock import patch

import common
import dispatch
import egress


class DispatchTest(unittest.TestCase):
    def setUp(self):
        self.sha = "8" * 40
        env = {"GITHUB_RUN_ID": "556", "GITHUB_EVENT_NAME": "workflow_dispatch",
               "GITHUB_REPOSITORY": dispatch.REPO, "GITHUB_REF": "refs/heads/" + dispatch.BRANCH,
               "GITHUB_SHA": self.sha, "GITHUB_WORKFLOW_SHA": self.sha,
               "GITHUB_RUN_ATTEMPT": "1", "REVIEWED": self.sha,
               "P90_MODE": "full", "ORIGINAL_RUN": ""}
        p = patch.dict(os.environ, env)
        p.start()
        self.addCleanup(p.stop)

    def validate(self, value, base=common.CANDIDATE, path=dispatch.WORKFLOW):
        with patch.object(dispatch.subprocess, "check_output", side_effect=[base, path]) as git, \
                patch.object(dispatch, "api", side_effect=lambda path: {"state": "open", "head": {"sha": common.CANDIDATE}} if path == "pulls/2725" else value):
            return dispatch.validate()

    def test_full_exact_head_and_first_dispatch(self):
        current = dict(id=556, display_title="P90 full · candidate")
        self.assertEqual(self.validate(dict(total_count=1, workflow_runs=[current])), "556")
        for data in (dict(total_count=2, workflow_runs=[current]), dict(total_count=2, workflow_runs=[current, current])):
            with self.assertRaises(ValueError):
                self.validate(data)
        with self.assertRaises(ValueError):
            self.validate({}, path=dispatch.WORKFLOW + "\ninternal/engine/destroy.go")
        with self.assertRaises(ValueError):
            self.validate({}, base="f" * 40)
        for key in ("REVIEWED", "GITHUB_WORKFLOW_SHA", "GITHUB_REF", "GITHUB_RUN_ATTEMPT"):
            with patch.dict(os.environ, {key: "different"}), self.assertRaises(ValueError):
                self.validate({})

    def test_recover_only_original_completed_paid_attempt(self):
        old = dict(status="completed", event="workflow_dispatch", run_attempt=1, head_sha=self.sha,
                   head_branch=dispatch.BRANCH, path=dispatch.WORKFLOW, display_title="P90 full · candidate")
        with patch.dict(os.environ, {"P90_MODE": "recover", "ORIGINAL_RUN": "123"}):
            self.assertEqual(self.validate(old), "123")
            for field, value in (("run_attempt", 2), ("head_sha", "e"*40), ("status", "in_progress"),
                                 ("path", "other.yml"), ("display_title", "P90 recover · candidate")):
                with self.assertRaises(ValueError):
                    self.validate(dict(old, **{field: value}))

    def test_workflow_defines_only_reviewed_suites(self):
        path = Path(__file__).resolve().parents[3] / dispatch.WORKFLOW
        text = path.read_text()
        self.assertIn("suite: [" + ", ".join(common.SUITES) + "]", text)
        self.assertIn("fail-fast: false", text)
        self.assertNotIn("schedule:", text)
        self.assertNotIn("workflow_call:", text)
        for forbidden in ("cloud-live.sh", "cloud-readonly.sh", "dbaas-delete.sh", "cleanup_only", "redis_standalone_probe"):
            self.assertNotIn(forbidden, text)
        self.assertNotIn("secrets.", text.split("  cloud:")[1].split("      - id: start")[0])


class EgressTest(unittest.TestCase):
    def test_only_one_agreed_public_ipv4(self):
        with patch.object(egress, "read", side_effect=["1.2.3.4\n", "1.2.3.4\n"]):
            self.assertEqual(egress.determine(""), "1.2.3.4/32")
        for a, b in (("1.2.3.4", "8.8.8.8"), ("not-ip", ""), ("127.0.0.1", "127.0.0.1")):
            with patch.object(egress, "read", side_effect=[a, b]), self.assertRaises(ValueError):
                egress.determine("")
        for fixed in ("0.0.0.0/0", "224.0.0.1/32", "127.0.0.1/32", "10.0.0.1/32"):
            with self.assertRaises(ValueError):
                egress.determine(fixed)
