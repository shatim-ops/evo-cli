"""Offline bindings for the paid dispatch, recovery and source addresses."""
import os
from pathlib import Path
import re
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

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

    def validate(self, value, parent=dispatch.PREVIOUS_LAUNCHER, base=common.CANDIDATE, path=dispatch.WORKFLOW):
        with patch.object(dispatch.subprocess, "check_output", side_effect=[parent, base, path]), \
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
        for bad_args in (dict(parent=common.CANDIDATE), dict(base="f" * 40),
                         dict(parent=dispatch.PREVIOUS_LAUNCHER + " " + common.CANDIDATE),
                         dict(base=common.CANDIDATE + " " + "f" * 40)):
            with self.subTest(bad_args=bad_args), self.assertRaises(ValueError):
                self.validate({}, **bad_args)
        for key in ("REVIEWED", "GITHUB_WORKFLOW_SHA", "GITHUB_REF", "GITHUB_RUN_ATTEMPT"):
            with patch.dict(os.environ, {key: "different"}), self.assertRaises(ValueError):
                self.validate({})

    def test_full_dispatch_on_previous_revision_blocks_new_revision(self):
        current = dict(id=556, display_title="P90 full · candidate", head_sha=self.sha)
        previous = dict(current, id=123, head_sha=dispatch.PREVIOUS_LAUNCHER)

        def history(path):
            if path == "pulls/2725":
                return {"state": "open", "head": {"sha": common.CANDIDATE}}
            query = parse_qs(urlsplit(path).query)
            self.assertEqual(query["branch"], [dispatch.BRANCH])
            rows = [r for r in (current, previous) if "head_sha" not in query or r["head_sha"] in query["head_sha"]]
            return dict(total_count=len(rows), workflow_runs=rows)

        with patch.object(dispatch, "validate_source"), patch.object(dispatch, "api", history), \
                self.assertRaisesRegex(ValueError, "duplicate"):
            dispatch.validate()

    def test_recover_only_original_completed_paid_attempt(self):
        old = dict(status="completed", event="workflow_dispatch", run_attempt=1, head_sha=self.sha,
                   head_branch=dispatch.BRANCH, path=dispatch.WORKFLOW, display_title="P90 full · candidate")
        with patch.dict(os.environ, {"P90_MODE": "recover", "ORIGINAL_RUN": "123"}):
            self.assertEqual(self.validate(old), "123")
            for field, value in (("run_attempt", 2), ("head_sha", dispatch.PREVIOUS_LAUNCHER), ("status", "in_progress"),
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

    def test_all_cloud_steps_use_the_protected_environment_after_guard(self):
        text = (Path(__file__).resolve().parents[3] / dispatch.WORKFLOW).read_text()
        prefix, jobs = text.split("\njobs:\n")
        self.assertEqual(re.findall(r"^  ([\w-]+):$", jobs, re.M), ["guard", "cloud"])
        guard, cloud = jobs.split("\n  cloud:\n")
        self.assertNotIn("secrets.", prefix + guard)
        self.assertNotIn("environment:", guard)
        self.assertNotIn("$P90_DRIVER", guard)
        self.assertIn("patch-e2e/dispatch.py", guard)
        self.assertRegex(cloud, r"(?m)^    needs: guard$")
        self.assertNotRegex(cloud, r"(?m)^    if:")
        self.assertRegex(cloud, r"(?m)^    environment: cloud-e2e$")
        allowed_secrets = {"CLOUDRU_PROJECT_ID", "CLOUDRU_KEY_ID", "CLOUDRU_SECRET",
                           "P90_RECEIPT_TOKEN", "P90_RECEIPT_HMAC_KEY"}
        self.assertEqual(set(re.findall(r"secrets\.([A-Z0-9_]+)", cloud)), allowed_secrets)
        self.assertNotIn("secrets.", cloud.split("      - id: start")[0])
        self.assertEqual(re.findall(r'^        run: python3 "\$P90_DRIVER" (\w+)$', cloud, re.M),
                         ["begin", "preflight", "full", "cleanup"])


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
