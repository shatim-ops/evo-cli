#!/usr/bin/env python3
"""Tests of scripts/cloudru-specs.py without the network.

The docs site and curl are replaced by fakes: a page or file that fails
fails on chosen calls only, so the retry rules are checked as they run in
the api-contract workflow (#245, #217: every full pass failed on a
different one of 28 files, and three full passes ran out the job's time).

Usage: python3 scripts/cloudru-specs-test.py
"""
import importlib.util
import json
import os
import shutil
import tempfile
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("cloudru_specs", os.path.join(HERE, "cloudru-specs.py"))
specs = importlib.util.module_from_spec(spec)
spec.loader.exec_module(specs)

PAGES = {"alpha": "/docs/alpha/ug/topics/api-ref", "beta": "/docs/beta/ug/topics/api-ref",
         "gamma": "/docs/gamma/ug/topics/api-ref"}
SPEC_OF = {s: f"https://cloud.ru/docs/api/specs/{s}/ug/_specs/openapi.yaml" for s in PAGES}


class FakeSite:
    """The docs site: each service page links one spec; listed calls fail."""

    def __init__(self, page_failures=None, file_failures=None):
        # service -> how many first calls fail
        self.page_failures = dict(page_failures or {})
        self.file_failures = dict(file_failures or {})
        self.pages, self.files = [], []

    def get(self, url, attempts=3):
        service = url.split("/docs/")[1].split("/")[0]
        self.pages.append(service)
        if self.page_failures.get(service, 0) > 0:
            self.page_failures[service] -= 1
            return "000", ""
        return "200", f'<a href="{SPEC_OF[service]}">spec</a>'

    def download(self, url, path):
        service = url.split("/specs/")[1].split("/")[0]
        self.files.append(service)
        if self.file_failures.get(service, 0) > 0:
            self.file_failures[service] -= 1
            return False
        with open(path, "w") as f:
            f.write("openapi: 3.0.0\npaths:\n  /x: {}\n")
        return True


class Download(unittest.TestCase):
    def run_main(self, site, needs=()):
        out = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, out)
        with mock.patch.object(specs, "START_PAGES", PAGES), \
                mock.patch.object(specs, "get", site.get), \
                mock.patch.object(specs, "download", site.download), \
                mock.patch.object(specs, "contract_specs", lambda: sorted(needs)), \
                mock.patch.object(specs.time, "sleep", lambda s: None), \
                mock.patch.object(specs.sys, "argv", ["cloudru-specs.py", out]):
            rc = specs.main()
        with open(os.path.join(out, "manifest.json")) as f:
            return rc, json.load(f)

    def test_failed_page_retries_that_service_only(self):
        site = FakeSite(page_failures={"beta": 1})
        rc, manifest = self.run_main(site)
        self.assertEqual(rc, 0)
        self.assertEqual(manifest["missing"], [])
        self.assertEqual(sorted(site.pages), ["alpha", "beta", "beta", "gamma"])
        self.assertEqual([e["service"] for e in manifest["specs"]], ["alpha", "beta", "gamma"])

    def test_failed_file_retries_that_service_only(self):
        site = FakeSite(file_failures={"gamma": 1})
        rc, manifest = self.run_main(site)
        self.assertEqual(rc, 0)
        self.assertEqual(sorted(site.files), ["alpha", "beta", "gamma", "gamma"])
        self.assertEqual(len(manifest["specs"]), 3)

    def test_a_different_failure_each_round_still_completes(self):
        # As in #245: round 1 loses one service, round 2 another.
        site = FakeSite(page_failures={"alpha": 1}, file_failures={"beta": 2})
        rc, manifest = self.run_main(site)
        self.assertEqual(rc, 0)
        self.assertEqual(manifest["missing"], [])

    def test_spec_the_contract_needs_is_retried(self):
        # The page loads but lacks the link on the first call (seen 19.10 and
        # 23.10 on the administration page).
        site = FakeSite()
        calls = {"n": 0}
        plain = site.get

        def get(url, attempts=3):
            code, body = plain(url, attempts)
            if "/alpha/" in url:
                calls["n"] += 1
                if calls["n"] == 1:
                    return code, "no links yet"
            return code, body
        site.get = get
        rc, manifest = self.run_main(site, needs=["alpha/openapi.yaml"])
        self.assertEqual(rc, 0)
        self.assertEqual(calls["n"], 2)

    def test_lasting_failure_is_reported_after_the_rounds(self):
        site = FakeSite(page_failures={"beta": 99})
        rc, manifest = self.run_main(site)
        self.assertEqual(rc, 1)
        self.assertEqual(site.pages.count("beta"), specs.ROUNDS)
        self.assertIn("beta", manifest["missing"])
        self.assertEqual([e["service"] for e in manifest["specs"]], ["alpha", "gamma"])


if __name__ == "__main__":
    unittest.main()
